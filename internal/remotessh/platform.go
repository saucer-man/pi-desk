package remotessh

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrUnsupportedRemotePlatform = errors.New("remote platform is unsupported")

type RemotePlatform struct {
	OS   string
	Arch string
}

// ProbePlatform runs only fixed uname queries on an already-ready generation.
// It does not accept a remote command or caller-controlled SSH options.
func (supervisor *ConnectionSupervisor) ProbePlatform(ctx context.Context, generation uint64) (RemotePlatform, error) {
	if supervisor.locator == nil {
		return RemotePlatform{}, errors.New("SSH locator is unavailable")
	}
	bound, release, err := supervisor.bindGenerationContext(ctx, generation)
	if err != nil {
		return RemotePlatform{}, err
	}
	defer release()

	combined, combinedErr := supervisor.runUname(bound, "-sm")
	if combinedErr == nil {
		fields := strings.Fields(combined)
		if len(fields) != 2 {
			return RemotePlatform{}, ErrUnsupportedRemotePlatform
		}
		platform, ok := normalizeRemotePlatform(fields[0], fields[1])
		if !ok {
			return RemotePlatform{}, ErrUnsupportedRemotePlatform
		}
		return platform, nil
	}
	osName, err := supervisor.runUname(bound, "-s")
	if err != nil {
		return RemotePlatform{}, err
	}
	architecture, err := supervisor.runUname(bound, "-m")
	if err != nil {
		return RemotePlatform{}, err
	}
	platform, ok := normalizeRemotePlatform(osName, architecture)
	if !ok {
		return RemotePlatform{}, ErrUnsupportedRemotePlatform
	}
	return platform, nil
}

func (supervisor *ConnectionSupervisor) runUname(ctx context.Context, flag string) (string, error) {
	invocation, err := supervisor.locator.platformProbeInvocation(supervisor.hostAlias, flag)
	if err != nil {
		return "", err
	}
	var output commandOutput
	if platformRunner, ok := supervisor.locator.runner.(platformCommandRunner); ok {
		output, err = platformRunner.RunPlatform(ctx, invocation.Executable, invocation.Args...)
	} else {
		output, err = supervisor.locator.runner.Run(ctx, invocation.Executable, invocation.Args...)
	}
	if err != nil {
		return "", fmt.Errorf("probe remote platform: %w", err)
	}
	value := strings.TrimSpace(string(output.Stdout))
	if value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return "", ErrUnsupportedRemotePlatform
	}
	return value, nil
}

func normalizeRemotePlatform(osName, architecture string) (RemotePlatform, bool) {
	var platform RemotePlatform
	switch strings.ToLower(strings.TrimSpace(osName)) {
	case "linux":
		platform.OS = "linux"
	case "darwin":
		platform.OS = "darwin"
	default:
		return RemotePlatform{}, false
	}
	switch strings.ToLower(strings.TrimSpace(architecture)) {
	case "x86_64", "amd64":
		platform.Arch = "amd64"
	case "aarch64", "arm64":
		platform.Arch = "arm64"
	default:
		return RemotePlatform{}, false
	}
	return platform, true
}
