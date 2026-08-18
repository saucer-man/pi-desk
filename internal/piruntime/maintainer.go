package piruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"pi-desk/internal/domain"
)

const (
	piPackageName           = "@earendil-works/pi-coding-agent"
	maintenanceProbeTimeout = 8 * time.Second
)

var (
	ErrPiAlreadyInstalled = errors.New("Pi is already installed; use an update action instead")
	ErrPiNotInstalled     = errors.New("Pi CLI was not found; install Pi first")
)

type Maintainer struct {
	locator *Locator
}

func NewMaintainer(locator *Locator) *Maintainer {
	return &Maintainer{locator: locator}
}

func (maintainer *Maintainer) Run(ctx context.Context, action domain.PiMaintenanceAction) (Invocation, string, error) {
	if maintainer == nil || maintainer.locator == nil {
		return Invocation{}, "", errors.New("Pi maintenance is unavailable")
	}
	probeCtx, probeCancel := context.WithTimeout(ctx, maintenanceProbeTimeout)
	status := maintainer.locator.Probe(probeCtx)
	probeCancel()
	var (
		invocation Invocation
		err        error
	)
	switch action {
	case domain.PiInstall:
		if status.State == domain.RuntimeReady {
			return Invocation{}, "", ErrPiAlreadyInstalled
		}
		if status.State != domain.RuntimeMissing {
			return Invocation{}, "", fmt.Errorf("Pi installation is unavailable: %s", status.Message)
		}
		invocation, err = maintainer.locator.NPMInvocation("install", "-g", "--ignore-scripts", piPackageName)
	case domain.PiUpdateSelf:
		invocation, err = maintainer.piInvocation(status, "update", "--self")
	default:
		return Invocation{}, "", fmt.Errorf("unsupported Pi maintenance action %q", action)
	}
	if err != nil {
		return Invocation{}, "", err
	}
	output, runErr := maintainer.locator.Run(ctx, invocation)
	return invocation, strings.TrimSpace(strings.ToValidUTF8(string(output), "?")), runErr
}

func (maintainer *Maintainer) piInvocation(status domain.PiRuntimeStatus, args ...string) (Invocation, error) {
	if status.State != domain.RuntimeReady {
		return Invocation{}, ErrPiNotInstalled
	}
	return maintainer.locator.Invocation(args...)
}
