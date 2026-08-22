package remotessh

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxHostAliasBytes = 512

var ErrInvalidTarget = errors.New("invalid OpenSSH host alias")

type Target struct {
	HostAlias string
}

type Invocation struct {
	Executable string
	Args       []string
}

func NewTarget(hostAlias string) (Target, error) {
	if hostAlias == "" {
		return Target{}, fmt.Errorf("%w: host alias is required", ErrInvalidTarget)
	}
	if len(hostAlias) > maxHostAliasBytes {
		return Target{}, fmt.Errorf("%w: host alias exceeds %d bytes", ErrInvalidTarget, maxHostAliasBytes)
	}
	if !utf8.ValidString(hostAlias) {
		return Target{}, fmt.Errorf("%w: host alias is not valid UTF-8", ErrInvalidTarget)
	}
	if strings.HasPrefix(hostAlias, "-") {
		return Target{}, fmt.Errorf("%w: host alias cannot start with '-'", ErrInvalidTarget)
	}
	if strings.ContainsAny(hostAlias, "*?") {
		return Target{}, fmt.Errorf("%w: host alias must be a concrete target", ErrInvalidTarget)
	}
	for _, value := range hostAlias {
		if unicode.IsControl(value) || unicode.IsSpace(value) {
			return Target{}, fmt.Errorf("%w: host alias cannot contain whitespace or control characters", ErrInvalidTarget)
		}
	}
	return Target{HostAlias: hostAlias}, nil
}

// ConfigInvocation returns a non-connecting ssh -G invocation using the same
// command-line policy that will be applied to helper sessions. Callers must only
// execute it after the user explicitly requests a connection: ssh -G can execute
// config-side Match exec and ProxyCommand behavior. OpenSSH uses the first value
// obtained for each option, so command-line policy takes precedence over user
// configuration while aliases, identities, agents, and ProxyJump are resolved.
func (locator *Locator) ConfigInvocation(hostAlias string) (Invocation, error) {
	target, executable, err := locator.resolveTarget(hostAlias)
	if err != nil {
		return Invocation{}, err
	}
	args := []string{"-G"}
	args = append(args, connectionPolicyArgs()...)
	args = append(args, "--", target.HostAlias)
	return Invocation{Executable: executable, Args: locator.withTestConfig(args)}, nil
}

// connectionProbeInvocation builds a real, non-mutating SSH connection using
// the fixed policy. It remains private so callers cannot replace the fixed
// command with user-controlled remote shell input.
func (locator *Locator) connectionProbeInvocation(hostAlias string) (Invocation, error) {
	target, executable, err := locator.resolveTarget(hostAlias)
	if err != nil {
		return Invocation{}, err
	}
	args := append([]string{}, connectionPolicyArgs()...)
	args = append(args, "-n", "-v")
	args = append(args, "--", target.HostAlias, "true")
	return Invocation{Executable: executable, Args: locator.withTestConfig(args)}, nil
}

func (locator *Locator) platformProbeInvocation(hostAlias, flag string) (Invocation, error) {
	if flag != "-s" && flag != "-m" && flag != "-sm" {
		return Invocation{}, errors.New("invalid fixed uname query")
	}
	target, executable, err := locator.resolveTarget(hostAlias)
	if err != nil {
		return Invocation{}, err
	}
	args := append([]string{}, connectionPolicyArgs()...)
	args = append(args, "-n", "-v")
	args = append(args, "--", target.HostAlias, "uname", flag)
	return Invocation{Executable: executable, Args: locator.withTestConfig(args)}, nil
}

func (locator *Locator) withTestConfig(args []string) []string {
	if locator.testConfigPath == "" {
		return args
	}
	return append([]string{"-F", locator.testConfigPath}, args...)
}

func (locator *Locator) resolveTarget(hostAlias string) (Target, string, error) {
	target, err := NewTarget(hostAlias)
	if err != nil {
		return Target{}, "", err
	}
	executable, err := locator.Resolve()
	if err != nil {
		return Target{}, "", err
	}
	return target, executable, nil
}

func connectionPolicyArgs() []string {
	options := []string{
		"PermitLocalCommand=no",
		"RemoteCommand=none",
		"RequestTTY=no",
		"ControlMaster=no",
		"ControlPath=none",
		"ControlPersist=no",
		"ForkAfterAuthentication=no",
		"ClearAllForwardings=yes",
		"ForwardAgent=no",
		"ForwardX11=no",
		"ForwardX11Trusted=no",
		"Tunnel=no",
		"EscapeChar=none",
		"StrictHostKeyChecking=yes",
		"UpdateHostKeys=no",
		"VerifyHostKeyDNS=no",
		"BatchMode=yes",
		"PasswordAuthentication=no",
		"KbdInteractiveAuthentication=no",
		"SendEnv=-*",
		"NumberOfPasswordPrompts=0",
		"ConnectionAttempts=1",
		"ConnectTimeout=15",
		"ServerAliveInterval=15",
		"ServerAliveCountMax=2",
		"LogLevel=ERROR",
	}
	args := make([]string, 0, 1+len(options)*2)
	args = append(args, "-T")
	for _, option := range options {
		args = append(args, "-o", option)
	}
	return args
}
