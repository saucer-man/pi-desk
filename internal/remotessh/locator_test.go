package remotessh

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type fakeCommandRunner struct {
	paths         map[string]string
	files         map[string]bool
	lookErr       error
	output        []byte
	runOutput     commandOutput
	combinedErr   error
	runErr        error
	commandName   string
	commandArgs   []string
	combinedCalls int
	runCalls      int
}

func (runner *fakeCommandRunner) LookPath(file string) (string, error) {
	if path := runner.paths[file]; path != "" {
		return path, nil
	}
	if runner.lookErr != nil {
		return "", runner.lookErr
	}
	return "", errors.New("not found")
}

func (runner *fakeCommandRunner) IsFile(path string) bool {
	return runner.files[path]
}

func (runner *fakeCommandRunner) CombinedOutput(_ context.Context, name string, args ...string) ([]byte, error) {
	runner.combinedCalls++
	runner.commandName = name
	runner.commandArgs = append([]string(nil), args...)
	return append([]byte(nil), runner.output...), runner.combinedErr
}

func (runner *fakeCommandRunner) Run(_ context.Context, name string, args ...string) (commandOutput, error) {
	runner.runCalls++
	runner.commandName = name
	runner.commandArgs = append([]string(nil), args...)
	return commandOutput{
		Stdout: append([]byte(nil), runner.runOutput.Stdout...),
		Stderr: append([]byte(nil), runner.runOutput.Stderr...),
	}, runner.runErr
}

func TestResolvePrefersWindowsSystemOpenSSH(t *testing.T) {
	windir := `C:\Windows`
	systemPath := filepath.Join(windir, "System32", "OpenSSH", "ssh.exe")
	runner := &fakeCommandRunner{
		files: map[string]bool{systemPath: true},
		paths: map[string]string{"ssh.exe": `C:\Program Files\Git\usr\bin\ssh.exe`},
	}

	path, err := newLocator(runner, "windows", windir).Resolve()
	if err != nil {
		t.Fatalf("Resolve returned an error: %v", err)
	}
	if path != filepath.Clean(systemPath) {
		t.Fatalf("Resolve returned %q, want %q", path, filepath.Clean(systemPath))
	}
}

func TestResolveFallsBackToPath(t *testing.T) {
	runner := &fakeCommandRunner{
		files: map[string]bool{},
		paths: map[string]string{"ssh.exe": `C:\tools\ssh.exe`},
	}

	path, err := newLocator(runner, "windows", `C:\Windows`).Resolve()
	if err != nil {
		t.Fatalf("Resolve returned an error: %v", err)
	}
	if path != `C:\tools\ssh.exe` {
		t.Fatalf("Resolve returned %q", path)
	}
}

func TestResolveReportsMissingOpenSSH(t *testing.T) {
	runner := &fakeCommandRunner{lookErr: errors.New("missing")}

	_, err := newLocator(runner, "linux", "").Resolve()
	if !errors.Is(err, ErrOpenSSHNotFound) {
		t.Fatalf("Resolve error = %v, want ErrOpenSSHNotFound", err)
	}
}

func TestProbeParsesPortableAndWindowsVersions(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{name: "portable", output: "OpenSSH_10.3p1, OpenSSL 3.5.7", want: "10.3p1"},
		{name: "windows", output: "OpenSSH_for_Windows_9.5p2, LibreSSL 3.8.2", want: "9.5p2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeCommandRunner{
				paths:  map[string]string{"ssh": "/usr/bin/ssh"},
				output: []byte(test.output),
			}
			info, err := newLocator(runner, "linux", "").Probe(context.Background())
			if err != nil {
				t.Fatalf("Probe returned an error: %v", err)
			}
			if info.Executable != "/usr/bin/ssh" || info.Version != test.want {
				t.Fatalf("unexpected client info: %#v", info)
			}
			if runner.commandName != "/usr/bin/ssh" || !slices.Equal(runner.commandArgs, []string{"-V"}) {
				t.Fatalf("unexpected probe command: %q %#v", runner.commandName, runner.commandArgs)
			}
		})
	}
}

func TestProbeRejectsUnrecognizedVersion(t *testing.T) {
	runner := &fakeCommandRunner{
		paths:  map[string]string{"ssh": "/usr/bin/ssh"},
		output: []byte("not an OpenSSH client"),
	}

	if _, err := newLocator(runner, "linux", "").Probe(context.Background()); err == nil {
		t.Fatal("Probe should reject an unrecognized version")
	}
}

func TestSSHCommandInheritsEnvironment(t *testing.T) {
	command := newSSHCommand(context.Background(), "ssh")
	if command.Env != nil {
		t.Fatalf("OpenSSH command Env = %#v, want nil for inherited environment", command.Env)
	}
}

func TestBoundedOutputDrainsWhileLimitingMemory(t *testing.T) {
	output := &boundedOutput{limit: 4}
	n, err := output.Write([]byte("123456"))
	if err != nil || n != 6 || output.buffer.String() != "1234" || !output.overflow {
		t.Fatalf("unexpected bounded output: n=%d err=%v value=%q overflow=%v", n, err, output.buffer.String(), output.overflow)
	}
}

func TestParseVersion(t *testing.T) {
	if got := parseVersion([]byte("prefix OpenSSH_9.9p2 suffix")); got != "9.9p2" {
		t.Fatalf("parseVersion = %q", got)
	}
	if got := parseVersion([]byte("LibreSSL only")); got != "" {
		t.Fatalf("parseVersion should reject unrelated output, got %q", got)
	}
}

func TestNewTargetValidation(t *testing.T) {
	for _, value := range []string{"dev", "user@dev.example", "build:2222", "[2001:db8::1]"} {
		t.Run("valid_"+value, func(t *testing.T) {
			target, err := NewTarget(value)
			if err != nil || target.HostAlias != value {
				t.Fatalf("NewTarget(%q) = %#v, %v", value, target, err)
			}
		})
	}

	invalid := []string{"", " dev", "dev ", "dev host", "dev\nhost", "dev*", "dev?", "-oProxyCommand=bad", strings.Repeat("a", maxHostAliasBytes+1), string([]byte{0xff})}
	for _, value := range invalid {
		t.Run("invalid", func(t *testing.T) {
			if _, err := NewTarget(value); !errors.Is(err, ErrInvalidTarget) {
				t.Fatalf("NewTarget(%q) error = %v, want ErrInvalidTarget", value, err)
			}
		})
	}
}

func TestConnectionProbeInvocationUsesFixedRemoteCommand(t *testing.T) {
	runner := &fakeCommandRunner{paths: map[string]string{"ssh": "/usr/bin/ssh"}}
	invocation, err := newLocator(runner, "linux", "").connectionProbeInvocation("build-prod")
	if err != nil {
		t.Fatalf("connectionProbeInvocation returned an error: %v", err)
	}
	if len(invocation.Args) < 7 || invocation.Args[len(invocation.Args)-5] != "-n" || invocation.Args[len(invocation.Args)-4] != "-v" || !slices.Equal(invocation.Args[len(invocation.Args)-2:], []string{"build-prod", "true"}) || invocation.Args[len(invocation.Args)-3] != "--" {
		t.Fatalf("unexpected connection probe boundary: %#v", invocation.Args)
	}
	if slices.Contains(invocation.Args, "sh") || slices.Contains(invocation.Args, "-c") {
		t.Fatalf("connection probe must not invoke a caller-controlled shell: %#v", invocation.Args)
	}
}

func TestSFTPAndHelperInvocationsUseFixedSubsystemAndTemplate(t *testing.T) {
	runner := &fakeCommandRunner{paths: map[string]string{"ssh": "/usr/bin/ssh"}}
	locator := newLocator(runner, "linux", "")
	sftpInvocation, err := locator.sftpInvocation("build-prod")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sftpInvocation.Args[len(sftpInvocation.Args)-3:], []string{"--", "build-prod", "sftp"}) || sftpInvocation.Args[0] != "-s" {
		t.Fatalf("unexpected SFTP invocation: %#v", sftpInvocation.Args)
	}
	artifact := helperArtifactForTest("linux", "amd64", []byte("helper"))
	helperInvocation, err := locator.helperInvocation("build-prod", artifact)
	if err != nil {
		t.Fatal(err)
	}
	wantCommand := `exec "$HOME/.cache/pi-desk/remote-helper/1/` + artifact.SHA256 + `/helper" serve-stdio`
	if !slices.Equal(helperInvocation.Args[len(helperInvocation.Args)-3:], []string{"--", "build-prod", wantCommand}) {
		t.Fatalf("unexpected helper invocation: %#v", helperInvocation.Args)
	}
	if strings.Contains(wantCommand, "sh -c") || strings.Contains(wantCommand, "root") {
		t.Fatalf("helper template contains a shell or caller path: %q", wantCommand)
	}
}

func TestConfigInvocationAppliesFixedPolicyBeforeTarget(t *testing.T) {
	runner := &fakeCommandRunner{paths: map[string]string{"ssh": "/usr/bin/ssh"}}
	invocation, err := newLocator(runner, "linux", "").ConfigInvocation("build-prod")
	if err != nil {
		t.Fatalf("ConfigInvocation returned an error: %v", err)
	}
	if invocation.Executable != "/usr/bin/ssh" {
		t.Fatalf("unexpected executable: %q", invocation.Executable)
	}
	if len(invocation.Args) < 4 || invocation.Args[0] != "-G" || invocation.Args[len(invocation.Args)-2] != "--" || invocation.Args[len(invocation.Args)-1] != "build-prod" {
		t.Fatalf("unexpected argument boundary: %#v", invocation.Args)
	}
	if !slices.Contains(invocation.Args, "-T") {
		t.Fatalf("missing non-TTY policy: %#v", invocation.Args)
	}
	for _, option := range []string{
		"PermitLocalCommand=no",
		"RemoteCommand=none",
		"ControlMaster=no",
		"ControlPath=none",
		"ControlPersist=no",
		"ClearAllForwardings=yes",
		"ForwardAgent=no",
		"StrictHostKeyChecking=yes",
		"BatchMode=yes",
		"PasswordAuthentication=no",
		"KbdInteractiveAuthentication=no",
		"SendEnv=-*",
		"NumberOfPasswordPrompts=0",
		"UpdateHostKeys=no",
		"ConnectTimeout=15",
		"ServerAliveInterval=15",
		"ServerAliveCountMax=2",
	} {
		if !slices.Contains(invocation.Args, option) {
			t.Fatalf("missing fixed option %q in %#v", option, invocation.Args)
		}
	}
	for _, forbidden := range []string{
		"StrictHostKeyChecking=ask",
		"BatchMode=no",
		"NumberOfPasswordPrompts=1",
		"SetEnv=-*",
	} {
		if slices.Contains(invocation.Args, forbidden) {
			t.Fatalf("unexpected permissive option %q in %#v", forbidden, invocation.Args)
		}
	}
}
