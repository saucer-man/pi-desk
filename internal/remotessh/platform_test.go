package remotessh

import (
	"context"
	"errors"
	"slices"
	"testing"
)

type platformRunner struct{}

func (platformRunner) LookPath(string) (string, error) { return "/usr/bin/ssh", nil }
func (platformRunner) IsFile(string) bool              { return true }
func (platformRunner) CombinedOutput(context.Context, string, ...string) ([]byte, error) {
	return nil, errors.New("unexpected combined output")
}
func (platformRunner) Run(_ context.Context, _ string, args ...string) (commandOutput, error) {
	switch args[len(args)-1] {
	case "-s":
		return commandOutput{Stdout: []byte("Linux\n")}, nil
	case "-m":
		return commandOutput{Stdout: []byte("x86_64\n")}, nil
	default:
		return commandOutput{}, errors.New("unexpected command")
	}
}

func TestNormalizeRemotePlatform(t *testing.T) {
	tests := []struct {
		os, arch string
		want     RemotePlatform
		ok       bool
	}{
		{"Linux", "x86_64", RemotePlatform{OS: "linux", Arch: "amd64"}, true},
		{"Linux", "aarch64", RemotePlatform{OS: "linux", Arch: "arm64"}, true},
		{"Darwin", "arm64", RemotePlatform{OS: "darwin", Arch: "arm64"}, true},
		{"FreeBSD", "amd64", RemotePlatform{}, false},
		{"Linux", "riscv64", RemotePlatform{}, false},
	}
	for _, test := range tests {
		got, ok := normalizeRemotePlatform(test.os, test.arch)
		if got != test.want || ok != test.ok {
			t.Fatalf("normalizeRemotePlatform(%q, %q) = %#v, %v", test.os, test.arch, got, ok)
		}
	}
}

func TestConnectionSupervisorProbesFixedRemotePlatform(t *testing.T) {
	supervisor := newTestSupervisor(t, connectionProberFunc(func(context.Context, string) (ConnectionPreflight, error) {
		return supervisorPreflight("config-a", "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), nil
	}))
	supervisor.locator = newLocator(platformRunner{}, "linux", "")
	ready, err := supervisor.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	platform, err := supervisor.ProbePlatform(context.Background(), ready.Generation)
	if err != nil {
		t.Fatalf("ProbePlatform returned an error: %v", err)
	}
	if platform != (RemotePlatform{OS: "linux", Arch: "amd64"}) {
		t.Fatalf("ProbePlatform = %#v", platform)
	}
}

func TestPlatformProbeInvocationIsFixed(t *testing.T) {
	runner := &fakeCommandRunner{paths: map[string]string{"ssh": "/usr/bin/ssh"}}
	locator := newLocator(runner, "linux", "")
	invocation, err := locator.platformProbeInvocation("work", "-m")
	if err != nil {
		t.Fatalf("platformProbeInvocation returned an error: %v", err)
	}
	if !slices.Equal(invocation.Args[len(invocation.Args)-4:], []string{"--", "work", "uname", "-m"}) {
		t.Fatalf("unexpected platform probe suffix: %#v", invocation.Args)
	}
	if _, err := locator.platformProbeInvocation("work", "--help"); err == nil {
		t.Fatal("arbitrary uname flag was accepted")
	}
}
