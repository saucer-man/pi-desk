package remotessh

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverSSHConfigIsStaticAndTracksIncludeFiles(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	includeDir := filepath.Join(sshDir, "conf.d")
	if err := os.MkdirAll(includeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(sshDir, "config")
	if err := os.WriteFile(configPath, []byte(`Host *
  SendEnv PI_*

Host prod ?
  HostName prod.example.test
  ProxyJump bastion

Match exec "test -f /tmp/should-never-run"
  SetEnv BAD=value

Include conf.d/*.conf
`), 0o600); err != nil {
		t.Fatal(err)
	}
	includedPath := filepath.Join(includeDir, "prod.conf")
	if err := os.WriteFile(includedPath, []byte("Host bastion\n  HostName bastion.example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := DiscoverSSHConfig(DiscoveryOptions{HomeDir: home, ConfigPath: configPath})
	if err != nil {
		t.Fatalf("DiscoverSSHConfig returned an error: %v", err)
	}
	if len(result) != 2 || result[0].Name != "prod" || result[1].Name != "bastion" {
		t.Fatalf("unexpected aliases: %#v", result)
	}
	for _, alias := range result {
		if alias.Risk.HasMatchExec != true || alias.Risk.HasSetEnv != true {
			t.Fatalf("alias %q did not inherit static risks: %#v", alias.Name, alias.Risk)
		}
	}
	if !result[0].Risk.HasProxyJump {
		t.Fatalf("prod should report ProxyJump risk: %#v", result[0].Risk)
	}
}

func TestDiscoverSSHConfigIgnoresMissingIncludeWithoutExecutingAnything(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, ".ssh", "config")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("Host visible\nInclude missing/*.conf\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := DiscoverSSHConfig(DiscoveryOptions{HomeDir: home, ConfigPath: configPath})
	if err != nil {
		t.Fatalf("DiscoverSSHConfig returned an error: %v", err)
	}
	if len(result) != 1 || result[0].Name != "visible" {
		t.Fatalf("unexpected aliases: %#v", result)
	}
}

func TestDiscoverSSHConfigEnforcesBudgets(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config")
	if err := os.WriteFile(configPath, []byte("Host one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := DiscoverSSHConfig(DiscoveryOptions{ConfigPath: configPath, HomeDir: directory, MaxBytes: 1})
	if !errors.Is(err, ErrConfigDiscoveryBudget) {
		t.Fatalf("budget error = %v, want ErrConfigDiscoveryBudget", err)
	}
}

func TestConfigWordsHandlesQuotesAndComments(t *testing.T) {
	words, err := configWords(`ProxyCommand "ssh -W %h:%p jump" # comment`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ProxyCommand", "ssh -W %h:%p jump"}
	if strings.Join(words, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("configWords = %#v, want %#v", words, want)
	}
}

func FuzzConfigWords(f *testing.F) {
	f.Add(`Host prod # comment`)
	f.Add(`ProxyCommand "ssh -W %h:%p jump"`)
	f.Add(`Match exec "test -f /tmp/value"`)
	f.Fuzz(func(t *testing.T, line string) {
		_, _ = configWords(line)
	})
}
