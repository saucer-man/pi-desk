package remotessh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxEffectiveConfigBytes = 64 << 10

var (
	ErrEffectiveConfigInvalid = errors.New("invalid effective OpenSSH configuration")
	ErrSSHEnvironmentUnsafe   = errors.New("effective OpenSSH configuration has unsafe environment settings")
	ErrSSHPolicyMismatch      = errors.New("effective OpenSSH configuration does not satisfy the fixed policy")
	ErrSSHConfigPreflight     = errors.New("OpenSSH config preflight failed")
)

// EffectiveConfig is a bounded, non-secret projection of ssh -G output. Raw
// output and SetEnv values are deliberately not retained.
type EffectiveConfig struct {
	HostName                     string
	User                         string
	Port                         int
	HostKeyAlias                 string
	ProxyCommand                 bool
	ProxyCommandSHA256           string
	ProxyJump                    bool
	ProxyJumpSHA256              string
	SetEnvCount                  int
	LocalCommand                 bool
	BatchMode                    bool
	StrictHostKeyChecking        string
	PasswordAuthentication       bool
	KbdInteractiveAuthentication bool
	PermitLocalCommand           bool
	RequestTTY                   string
	ControlMaster                string
	ControlPersist               string
	ClearAllForwardings          bool
	ForwardAgent                 bool
	ForwardX11                   bool
	ForwardX11Trusted            bool
	Tunnel                       bool
	UpdateHostKeys               bool
	NumberOfPasswordPrompts      int
	Fingerprint                  string
	seen                         map[string]bool
}

// ParseEffectiveConfig parses only the line-oriented output produced by
// OpenSSH -G. Unknown keys are accepted because OpenSSH adds keys over time;
// safety-critical keys are validated separately by Validate.
func ParseEffectiveConfig(output []byte) (EffectiveConfig, error) {
	if len(output) > maxEffectiveConfigBytes {
		return EffectiveConfig{}, fmt.Errorf("%w: output exceeds %d bytes", ErrEffectiveConfigInvalid, maxEffectiveConfigBytes)
	}
	if !utf8.Valid(output) {
		return EffectiveConfig{}, fmt.Errorf("%w: output is not valid UTF-8", ErrEffectiveConfigInvalid)
	}
	config := EffectiveConfig{seen: make(map[string]bool)}
	for lineNumber, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return EffectiveConfig{}, fmt.Errorf("%w: line %d has no value", ErrEffectiveConfigInvalid, lineNumber+1)
		}
		key := strings.ToLower(fields[0])
		if key == "keyboardinteractiveauthentication" {
			key = "kbdinteractiveauthentication"
		}
		value := strings.Join(fields[1:], " ")
		if config.seen[key] && isSingletonEffectiveKey(key) {
			return EffectiveConfig{}, fmt.Errorf("%w: duplicate key %q on line %d", ErrEffectiveConfigInvalid, key, lineNumber+1)
		}
		config.seen[key] = true
		switch key {
		case "hostname":
			config.HostName = value
		case "user":
			config.User = value
		case "port":
			port, err := strconv.Atoi(fields[1])
			if err != nil || port < 1 || port > 65535 {
				return EffectiveConfig{}, fmt.Errorf("%w: line %d has invalid port", ErrEffectiveConfigInvalid, lineNumber+1)
			}
			config.Port = port
		case "hostkeyalias":
			if !isDisabledValue(value) {
				config.HostKeyAlias = value
			}
		case "proxycommand":
			config.ProxyCommand = !isDisabledValue(value)
			if config.ProxyCommand {
				config.ProxyCommandSHA256 = hashEffectiveValue(value)
			}
		case "proxyjump":
			config.ProxyJump = !isDisabledValue(value)
			if config.ProxyJump {
				config.ProxyJumpSHA256 = hashEffectiveValue(value)
			}
		case "setenv":
			config.SetEnvCount++
		case "localcommand":
			config.LocalCommand = !isDisabledValue(value)
		case "batchmode":
			config.BatchMode = parseOpenSSHBool(value)
		case "stricthostkeychecking":
			config.StrictHostKeyChecking = strings.ToLower(fields[1])
		case "passwordauthentication":
			config.PasswordAuthentication = parseOpenSSHBool(value)
		case "kbdinteractiveauthentication":
			config.KbdInteractiveAuthentication = parseOpenSSHBool(value)
		case "permitlocalcommand":
			config.PermitLocalCommand = parseOpenSSHBool(value)
		case "requesttty":
			config.RequestTTY = strings.ToLower(fields[1])
		case "controlmaster":
			config.ControlMaster = strings.ToLower(fields[1])
		case "controlpersist":
			config.ControlPersist = strings.ToLower(fields[1])
		case "clearallforwardings":
			config.ClearAllForwardings = parseOpenSSHBool(value)
		case "forwardagent":
			config.ForwardAgent = parseOpenSSHBool(value)
		case "forwardx11":
			config.ForwardX11 = parseOpenSSHBool(value)
		case "forwardx11trusted":
			config.ForwardX11Trusted = parseOpenSSHBool(value)
		case "tunnel":
			config.Tunnel = parseOpenSSHBool(value)
		case "updatehostkeys":
			config.UpdateHostKeys = parseOpenSSHBool(value)
		case "numberofpasswordprompts":
			count, err := strconv.Atoi(fields[1])
			if err != nil || count < 0 {
				return EffectiveConfig{}, fmt.Errorf("%w: line %d has invalid password prompt count", ErrEffectiveConfigInvalid, lineNumber+1)
			}
			config.NumberOfPasswordPrompts = count
		}
	}
	config.Fingerprint = effectiveConfigFingerprint(config)
	return config, nil
}

func (config EffectiveConfig) Validate() error {
	if config.SetEnvCount > 0 {
		return fmt.Errorf("%w: %d effective SetEnv directives", ErrSSHEnvironmentUnsafe, config.SetEnvCount)
	}
	for _, key := range []string{
		"hostname", "user", "port", "batchmode", "stricthostkeychecking", "passwordauthentication",
		"kbdinteractiveauthentication", "permitlocalcommand", "requesttty",
		"controlmaster", "controlpersist", "clearallforwardings", "forwardagent",
		"forwardx11", "forwardx11trusted", "tunnel", "updatehostkeys",
		"numberofpasswordprompts",
	} {
		if !config.seen[key] {
			return fmt.Errorf("%w: missing %s", ErrSSHPolicyMismatch, key)
		}
	}
	if config.HostName == "" || config.User == "" || config.Port == 0 {
		return fmt.Errorf("%w: missing hostname, user or port", ErrSSHPolicyMismatch)
	}
	if !config.BatchMode || config.StrictHostKeyChecking != "true" || config.PasswordAuthentication || config.KbdInteractiveAuthentication || config.PermitLocalCommand || config.ClearAllForwardings == false || config.ForwardAgent || config.ForwardX11 || config.ForwardX11Trusted || config.Tunnel || config.UpdateHostKeys || config.NumberOfPasswordPrompts != 0 {
		return fmt.Errorf("%w: strict, batch, forwarding, authentication or update policy mismatch", ErrSSHPolicyMismatch)
	}
	if config.RequestTTY != "false" || config.ControlMaster != "false" || config.ControlPersist != "no" {
		return fmt.Errorf("%w: tty or connection reuse policy mismatch", ErrSSHPolicyMismatch)
	}
	return nil
}

func isSingletonEffectiveKey(key string) bool {
	switch key {
	case "hostname", "user", "port", "hostkeyalias", "proxycommand", "proxyjump", "localcommand",
		"batchmode", "stricthostkeychecking", "passwordauthentication", "kbdinteractiveauthentication",
		"permitlocalcommand", "requesttty", "controlmaster", "controlpersist", "clearallforwardings",
		"forwardagent", "forwardx11", "forwardx11trusted", "tunnel", "updatehostkeys", "numberofpasswordprompts":
		return true
	default:
		return false
	}
}

func isDisabledValue(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "" || value == "none" || value == "no" || value == "false"
}

func parseOpenSSHBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "true", "on", "1":
		return true
	default:
		return false
	}
}

func hashEffectiveValue(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func effectiveConfigFingerprint(config EffectiveConfig) string {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "hostname=%s\nuser=%s\nport=%d\nhostkeyalias=%s\nproxycommand=%s\nproxyjump=%s\nsetenv=%d\nlocalcommand=%t\nbatchmode=%t\nstricthostkeychecking=%s\npasswordauthentication=%t\nkbdinteractiveauthentication=%t\npermitlocalcommand=%t\nrequesttty=%s\ncontrolmaster=%s\ncontrolpersist=%s\nclearallforwardings=%t\nforwardagent=%t\nforwardx11=%t\nforwardx11trusted=%t\ntunnel=%t\nupdatehostkeys=%t\nnumberofpasswordprompts=%d\n", config.HostName, config.User, config.Port, config.HostKeyAlias, config.ProxyCommandSHA256, config.ProxyJumpSHA256, config.SetEnvCount, config.LocalCommand, config.BatchMode, config.StrictHostKeyChecking, config.PasswordAuthentication, config.KbdInteractiveAuthentication, config.PermitLocalCommand, config.RequestTTY, config.ControlMaster, config.ControlPersist, config.ClearAllForwardings, config.ForwardAgent, config.ForwardX11, config.ForwardX11Trusted, config.Tunnel, config.UpdateHostKeys, config.NumberOfPasswordPrompts)
	return hex.EncodeToString(hash.Sum(nil))
}

// PreflightConfig runs ssh -G and validates its effective fixed policy. The
// caller must only invoke this after explicit user Connect consent because
// OpenSSH may evaluate Match exec or ProxyCommand while resolving config.
func (locator *Locator) PreflightConfig(ctx context.Context, hostAlias string) (EffectiveConfig, error) {
	invocation, err := locator.ConfigInvocation(hostAlias)
	if err != nil {
		return EffectiveConfig{}, err
	}
	output, err := locator.runner.CombinedOutput(ctx, invocation.Executable, invocation.Args...)
	if err != nil {
		return EffectiveConfig{}, fmt.Errorf("%w: %v", ErrSSHConfigPreflight, err)
	}
	config, err := ParseEffectiveConfig(output)
	if err != nil {
		return EffectiveConfig{}, err
	}
	if err := config.Validate(); err != nil {
		return EffectiveConfig{}, err
	}
	return config, nil
}
