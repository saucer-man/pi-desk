package appservice

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"sync"

	"pi-desk/internal/domain"
	terminalruntime "pi-desk/internal/terminal"
	"pi-desk/internal/workspace"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	terminalEventName = "terminal:event"
	maxTerminalInput  = 64 << 10
)

type terminalRuntime interface {
	Start(terminalruntime.StartConfig) (terminalruntime.Snapshot, error)
	Snapshot(string) terminalruntime.Snapshot
	Write(string, []byte) error
	Resize(string, int, int) error
	Stop(string) error
	Shutdown()
}

type TerminalService struct {
	catalog workspaceResolver
	mu      sync.RWMutex
	runtime terminalRuntime
}

func NewTerminalService(catalog *workspace.Catalog) *TerminalService {
	return &TerminalService{catalog: catalog}
}

func newTerminalService(catalog workspaceResolver, runtime terminalRuntime) *TerminalService {
	return &TerminalService{catalog: catalog, runtime: runtime}
}

func (service *TerminalService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	app := application.Get()
	runtime := terminalruntime.NewManager(ctx, func(event terminalruntime.Event) {
		app.Event.Emit(terminalEventName, domain.TerminalEvent{
			ThreadID: event.ThreadID,
			Type:     event.Type,
			Sequence: event.Sequence,
			DataB64:  base64.StdEncoding.EncodeToString(event.Data),
			ExitCode: event.ExitCode,
			Error:    event.Error,
		})
	})
	service.mu.Lock()
	service.runtime = runtime
	service.mu.Unlock()
	return nil
}

func (service *TerminalService) ServiceShutdown() error {
	service.mu.Lock()
	runtime := service.runtime
	service.runtime = nil
	service.mu.Unlock()
	if runtime != nil {
		runtime.Shutdown()
	}
	return nil
}

func (service *TerminalService) Start(request domain.StartTerminalRequest) (domain.TerminalState, error) {
	runtime, err := service.getRuntime()
	if err != nil {
		return domain.TerminalState{}, err
	}
	record, err := service.trustedWorkspace(request.WorkspacePath)
	if err != nil {
		return domain.TerminalState{}, err
	}
	snapshot, err := runtime.Start(terminalruntime.StartConfig{
		ThreadID: strings.TrimSpace(request.ThreadID),
		CWD:      record.Path,
		Columns:  request.Columns,
		Rows:     request.Rows,
	})
	if err != nil {
		return domain.TerminalState{}, err
	}
	return mapTerminalSnapshot(snapshot), nil
}

func (service *TerminalService) Snapshot(request domain.TerminalRequest) (domain.TerminalState, error) {
	runtime, err := service.getRuntime()
	if err != nil {
		return domain.TerminalState{}, err
	}
	threadID := strings.TrimSpace(request.ThreadID)
	if threadID == "" {
		return domain.TerminalState{}, errors.New("terminal thread id is required")
	}
	return mapTerminalSnapshot(runtime.Snapshot(threadID)), nil
}

func (service *TerminalService) Write(request domain.TerminalWriteRequest) error {
	runtime, err := service.getRuntime()
	if err != nil {
		return err
	}
	if len(request.Data) == 0 {
		return nil
	}
	if len(request.Data) > maxTerminalInput {
		return errors.New("terminal input exceeds the 64 KiB limit")
	}
	return runtime.Write(strings.TrimSpace(request.ThreadID), []byte(request.Data))
}

func (service *TerminalService) Resize(request domain.TerminalResizeRequest) error {
	runtime, err := service.getRuntime()
	if err != nil {
		return err
	}
	return runtime.Resize(strings.TrimSpace(request.ThreadID), request.Columns, request.Rows)
}

func (service *TerminalService) Stop(request domain.TerminalRequest) error {
	runtime, err := service.getRuntime()
	if err != nil {
		return err
	}
	return runtime.Stop(strings.TrimSpace(request.ThreadID))
}

func (service *TerminalService) stopThreadIfRunning(threadID string) error {
	runtime, err := service.getRuntime()
	if err != nil {
		return err
	}
	err = runtime.Stop(strings.TrimSpace(threadID))
	if errors.Is(err, terminalruntime.ErrNotRunning) {
		return nil
	}
	return err
}

func (service *TerminalService) trustedWorkspace(path string) (workspace.Record, error) {
	if service.catalog == nil {
		return workspace.Record{}, errors.New("terminal service is unavailable")
	}
	record, err := service.catalog.ResolvePath(strings.TrimSpace(path))
	if err != nil {
		return workspace.Record{}, err
	}
	if record.Trust != "approve" {
		return workspace.Record{}, errors.New("terminal access requires workspace trust approval")
	}
	return record, nil
}

func (service *TerminalService) getRuntime() (terminalRuntime, error) {
	service.mu.RLock()
	runtime := service.runtime
	service.mu.RUnlock()
	if runtime == nil {
		return nil, errors.New("terminal service is not ready")
	}
	return runtime, nil
}

func mapTerminalSnapshot(snapshot terminalruntime.Snapshot) domain.TerminalState {
	return domain.TerminalState{
		ThreadID:  snapshot.ThreadID,
		CWD:       snapshot.CWD,
		Shell:     snapshot.Shell,
		Running:   snapshot.Running,
		Sequence:  snapshot.Sequence,
		OutputB64: base64.StdEncoding.EncodeToString(snapshot.Output),
	}
}
