package appservice

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"pi-desk/internal/domain"
	"pi-desk/internal/piruntime"
)

const (
	piMaintenanceTimeout      = 10 * time.Minute
	maxMaintenanceErrorOutput = 16 << 10
)

type piMaintainer interface {
	Run(context.Context, domain.PiMaintenanceAction) (piruntime.Invocation, string, error)
}

type PiMaintenanceService struct {
	maintainer      piMaintainer
	runtimeProber   RuntimeProber
	prepareSessions func() (func(), error)
	mu              sync.Mutex
}

func NewPiMaintenanceService(locator *piruntime.Locator, agent *AgentService) *PiMaintenanceService {
	return newPiMaintenanceService(piruntime.NewMaintainer(locator), locator, agent.preparePiMaintenance)
}

func newPiMaintenanceService(maintainer piMaintainer, prober RuntimeProber, prepareSessions func() (func(), error)) *PiMaintenanceService {
	return &PiMaintenanceService{maintainer: maintainer, runtimeProber: prober, prepareSessions: prepareSessions}
}

func (service *PiMaintenanceService) MaintainPi(request domain.PiMaintenanceRequest) (domain.PiMaintenanceResult, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	releaseSessions := func() {}
	if service.prepareSessions != nil {
		var err error
		releaseSessions, err = service.prepareSessions()
		if err != nil {
			return domain.PiMaintenanceResult{}, err
		}
	}
	defer releaseSessions()
	ctx, cancel := context.WithTimeout(context.Background(), piMaintenanceTimeout)
	defer cancel()
	invocation, output, err := service.maintainer.Run(ctx, request.Action)
	result := domain.PiMaintenanceResult{Action: request.Action, Output: output}
	if invocation.PiPath != "" {
		result.Command = invocation.PiPath
	} else {
		result.Command = invocation.Executable
	}
	if service.runtimeProber != nil {
		probeCtx, probeCancel := context.WithTimeout(context.Background(), runtimeProbeTimeout)
		result.Runtime = service.runtimeProber.Probe(probeCtx)
		probeCancel()
	}
	if err != nil {
		if output != "" {
			return result, errors.New(strings.TrimSpace(err.Error() + ": " + truncateMaintenanceOutput(output)))
		}
		return result, err
	}
	return result, nil
}

func truncateMaintenanceOutput(output string) string {
	if len(output) <= maxMaintenanceErrorOutput {
		return output
	}
	value := []byte(output)[:maxMaintenanceErrorOutput]
	return strings.ToValidUTF8(string(value), "?") + "\n... error output truncated by Pi Desk ..."
}
