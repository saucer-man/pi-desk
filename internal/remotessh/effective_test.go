package remotessh

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const validEffectiveConfig = `host build-prod
user deploy
hostname build.example.test
port 2222
hostkeyalias build.example.test
proxycommand none
proxyjump none
batchmode yes
stricthostkeychecking true
passwordauthentication no
kbdinteractiveauthentication no
permitlocalcommand no
requesttty false
controlmaster false
controlpersist no
clearallforwardings yes
forwardagent no
forwardx11 no
forwardx11trusted no
tunnel false
updatehostkeys false
numberofpasswordprompts 0
`

func TestParseAndValidateEffectiveConfig(t *testing.T) {
	config, err := ParseEffectiveConfig([]byte(validEffectiveConfig))
	if err != nil {
		t.Fatalf("ParseEffectiveConfig returned an error: %v", err)
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate returned an error: %v", err)
	}
	if config.HostName != "build.example.test" || config.User != "deploy" || config.Port != 2222 || config.Fingerprint == "" {
		t.Fatalf("unexpected effective config: %#v", config)
	}
	if config.ProxyCommand || config.ProxyJump || config.SetEnvCount != 0 {
		t.Fatalf("unexpected risky config values: %#v", config)
	}
	withoutAlias, err := ParseEffectiveConfig([]byte(strings.Replace(validEffectiveConfig, "hostkeyalias build.example.test", "hostkeyalias none", 1)))
	if err != nil || withoutAlias.HostKeyAlias != "" {
		t.Fatalf("disabled HostKeyAlias was not normalized: config=%#v err=%v", withoutAlias, err)
	}
}

func TestEffectiveConfigAllowsRepeatedNonPolicyKeysAndHashesProxyCommands(t *testing.T) {
	withRepeatedIdentity := validEffectiveConfig + "identityfile ~/.ssh/id_ed25519\nidentityfile ~/.ssh/id_rsa\n"
	config, err := ParseEffectiveConfig([]byte(withRepeatedIdentity))
	if err != nil {
		t.Fatalf("repeated IdentityFile should be accepted: %v", err)
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate returned an error: %v", err)
	}

	first, err := ParseEffectiveConfig([]byte(strings.Replace(validEffectiveConfig, "proxycommand none", "proxycommand ssh -W %h:%p jump-one", 1)))
	if err != nil {
		t.Fatal(err)
	}
	second, err := ParseEffectiveConfig([]byte(strings.Replace(validEffectiveConfig, "proxycommand none", "proxycommand ssh -W %h:%p jump-two", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if first.ProxyCommandSHA256 == "" || first.ProxyCommandSHA256 == second.ProxyCommandSHA256 || first.Fingerprint == second.Fingerprint {
		t.Fatalf("proxy command drift was not reflected in hashes: first=%#v second=%#v", first, second)
	}
}

func TestEffectiveConfigRejectsSetEnv(t *testing.T) {
	config, err := ParseEffectiveConfig([]byte(validEffectiveConfig + "setenv PI_TOKEN=secret\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Validate(); !errors.Is(err, ErrSSHEnvironmentUnsafe) {
		t.Fatalf("Validate error = %v, want ErrSSHEnvironmentUnsafe", err)
	}
}

func TestEffectiveConfigKeepsLocalCommandDisabledByPolicy(t *testing.T) {
	config, err := ParseEffectiveConfig([]byte(validEffectiveConfig + "localcommand echo ignored\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !config.LocalCommand {
		t.Fatal("configured LocalCommand should be reflected in the safe projection")
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("PermitLocalCommand=no should keep LocalCommand disabled: %v", err)
	}
}

func TestEffectiveConfigRejectsPolicyMismatchAndMalformedOutput(t *testing.T) {
	config, err := ParseEffectiveConfig([]byte(strings.Replace(validEffectiveConfig, "batchmode yes", "batchmode no", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Validate(); !errors.Is(err, ErrSSHPolicyMismatch) {
		t.Fatalf("Validate error = %v, want ErrSSHPolicyMismatch", err)
	}
	if _, err := ParseEffectiveConfig([]byte(validEffectiveConfig + "batchmode yes\n")); !errors.Is(err, ErrEffectiveConfigInvalid) {
		t.Fatalf("duplicate policy key error = %v, want ErrEffectiveConfigInvalid", err)
	}
	if _, err := ParseEffectiveConfig([]byte("hostname\n")); !errors.Is(err, ErrEffectiveConfigInvalid) {
		t.Fatalf("malformed output error = %v, want ErrEffectiveConfigInvalid", err)
	}
	if _, err := ParseEffectiveConfig([]byte(strings.Repeat("x", maxEffectiveConfigBytes+1))); !errors.Is(err, ErrEffectiveConfigInvalid) {
		t.Fatalf("oversized output error = %v, want ErrEffectiveConfigInvalid", err)
	}
}

func TestPreflightConfigRunsOnlyTheProvidedInvocation(t *testing.T) {
	runner := &fakeCommandRunner{
		paths:  map[string]string{"ssh": "/usr/bin/ssh"},
		output: []byte(validEffectiveConfig),
	}
	config, err := newLocator(runner, "linux", "").PreflightConfig(context.Background(), "build-prod")
	if err != nil {
		t.Fatalf("PreflightConfig returned an error: %v", err)
	}
	if config.Fingerprint == "" || runner.commandName != "/usr/bin/ssh" {
		t.Fatalf("unexpected preflight result: %#v command=%q", config, runner.commandName)
	}
	if len(runner.commandArgs) == 0 || runner.commandArgs[0] != "-G" {
		t.Fatalf("preflight did not use ssh -G: %#v", runner.commandArgs)
	}
}
