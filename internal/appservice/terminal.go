package appservice

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"sync"

	"pi-desk/internal/domain"
	"pi-desk/internal/remotessh"
	terminalruntime "pi-desk/internal/terminal"
	"pi-desk/internal/workspace"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	terminalEventName = "terminal:event"
	maxTerminalInput  = 64 << 10
)

var ErrRemoteTerminalInactive = errors.New("remote terminal requires an active Pi task")

type terminalRuntime interface {
	Start(terminalruntime.StartConfig) (terminalruntime.Snapshot, error)
	Snapshot(string) terminalruntime.Snapshot
	Write(string, []byte) error
	Resize(string, int, int) error
	Stop(string) error
	Shutdown()
}

type remoteTerminalThread struct {
	workspaceID string
	active      bool
}

type TerminalService struct {
	catalog       workspaceResolver
	mu            sync.RWMutex
	runtime       terminalRuntime
	remote        *terminalruntime.RemoteManager
	remoteThreads map[string]remoteTerminalThread
}

func NewTerminalService(catalog *workspace.Catalog) *TerminalService {
	return &TerminalService{catalog: catalog, remoteThreads: make(map[string]remoteTerminalThread)}
}

func newTerminalService(catalog workspaceResolver, runtime terminalRuntime) *TerminalService {
	return &TerminalService{catalog: catalog, runtime: runtime, remoteThreads: make(map[string]remoteTerminalThread)}
}

func (service *TerminalService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	app := application.Get()
	emit := func(event terminalruntime.Event) {
		app.Event.Emit(terminalEventName, domain.TerminalEvent{
			ThreadID: event.ThreadID,
			Type:     event.Type, Generation: event.Generation, Sequence: event.Sequence,
			DataB64:  base64.StdEncoding.EncodeToString(event.Data),
			ExitCode: event.ExitCode, Error: event.Error,
		})
	}
	runtime := terminalruntime.NewManager(ctx, emit)
	remote := terminalruntime.NewRemoteManager(ctx, emit)
	service.mu.Lock()
	service.runtime = runtime
	service.remote = remote
	service.mu.Unlock()
	return nil
}

func (service *TerminalService) ServiceShutdown() error {
	service.mu.Lock()
	runtime, remote := service.runtime, service.remote
	service.runtime, service.remote = nil, nil
	service.remoteThreads = make(map[string]remoteTerminalThread)
	service.mu.Unlock()
	if runtime != nil {
		runtime.Shutdown()
	}
	if remote != nil {
		remote.Shutdown()
	}
	return nil
}

func (service *TerminalService) Start(request domain.StartTerminalRequest) (domain.TerminalState, error) {
	record, err := service.trustedWorkspace(request.WorkspaceID, request.WorkspacePath)
	if err != nil {
		return domain.TerminalState{}, err
	}
	threadID := strings.TrimSpace(request.ThreadID)
	runtime, err := service.runtimeFor(threadID, remoteWorkspaceID(record))
	if err != nil {
		return domain.TerminalState{}, err
	}
	cwd := record.Path
	if record.Location.Kind == workspace.KindSSH {
		cwd = record.Location.SSH.CanonicalRoot
	}
	snapshot, err := runtime.Start(terminalruntime.StartConfig{
		ThreadID: threadID, CWD: cwd, Columns: request.Columns, Rows: request.Rows,
	})
	if err != nil {
		return domain.TerminalState{}, err
	}
	return mapTerminalSnapshot(snapshot), nil
}

func (service *TerminalService) Snapshot(request domain.TerminalRequest) (domain.TerminalState, error) {
	threadID := strings.TrimSpace(request.ThreadID)
	if threadID == "" {
		return domain.TerminalState{}, errors.New("terminal thread id is required")
	}
	var runtime terminalRuntime
	var err error
	if strings.TrimSpace(request.WorkspaceID) != "" {
		record, workspaceErr := service.trustedWorkspace(request.WorkspaceID, "")
		if workspaceErr != nil {
			return domain.TerminalState{}, workspaceErr
		}
		runtime, err = service.runtimeFor(threadID, remoteWorkspaceID(record))
	} else {
		runtime, err = service.runtimeForThread(threadID)
	}
	if err != nil {
		return domain.TerminalState{}, err
	}
	return mapTerminalSnapshot(runtime.Snapshot(threadID)), nil
}

func (service *TerminalService) Write(request domain.TerminalWriteRequest) error {
	runtime, err := service.runtimeForThread(strings.TrimSpace(request.ThreadID))
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
	runtime, err := service.runtimeForThread(strings.TrimSpace(request.ThreadID))
	if err != nil {
		return err
	}
	return runtime.Resize(strings.TrimSpace(request.ThreadID), request.Columns, request.Rows)
}

func (service *TerminalService) Stop(request domain.TerminalRequest) error {
	runtime, err := service.runtimeForThread(strings.TrimSpace(request.ThreadID))
	if err != nil {
		return err
	}
	return runtime.Stop(strings.TrimSpace(request.ThreadID))
}

func (service *TerminalService) stopThreadIfRunning(threadID string) error {
	runtime, err := service.runtimeForThread(strings.TrimSpace(threadID))
	if err != nil {
		return err
	}
	err = runtime.Stop(strings.TrimSpace(threadID))
	if errors.Is(err, terminalruntime.ErrNotRunning) {
		return nil
	}
	return err
}

func (service *TerminalService) trustedWorkspace(id, workspacePath string) (workspace.Record, error) {
	if service.catalog == nil {
		return workspace.Record{}, errors.New("terminal service is unavailable")
	}
	id, workspacePath = strings.TrimSpace(id), strings.TrimSpace(workspacePath)
	if id != "" && workspacePath != "" {
		return workspace.Record{}, errors.New("terminal workspace id and path are mutually exclusive")
	}
	var record workspace.Record
	var err error
	if id != "" {
		resolver, ok := service.catalog.(interface {
			ResolveID(string) (workspace.Record, error)
		})
		if !ok {
			return workspace.Record{}, errors.New("terminal workspace id lookup is unavailable")
		}
		record, err = resolver.ResolveID(id)
	} else {
		record, err = service.catalog.ResolvePath(workspacePath)
	}
	if err != nil {
		return workspace.Record{}, err
	}
	if record.Trust != "approve" {
		return workspace.Record{}, errors.New("terminal access requires workspace trust approval")
	}
	if record.Location.Kind == workspace.KindLocal {
		verified, verifyErr := service.catalog.ResolvePath(record.Path)
		if verifyErr != nil || verified.ID != record.ID {
			return workspace.Record{}, errors.New("registered local workspace boundary changed")
		}
	}
	return record, nil
}

func (service *TerminalService) bindRemoteThread(threadID string, runtime *remotessh.RuntimeLeaseSupervisor, lease *remotessh.RuntimeLease, cwd string) error {
	threadID = strings.TrimSpace(threadID)
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.remote == nil {
		return errors.New("remote terminal service is not ready")
	}
	if current, exists := service.remoteThreads[threadID]; exists && current.workspaceID != lease.WorkspaceID() {
		return errors.New("remote terminal thread workspace identity cannot change")
	}
	if _, exists := service.remoteThreads[threadID]; !exists && len(service.remoteThreads) >= 500 {
		return errors.New("remote terminal thread identity limit reached")
	}
	if err := service.remote.Bind(threadID, runtime, lease, cwd); err != nil {
		return err
	}
	service.remoteThreads[threadID] = remoteTerminalThread{workspaceID: lease.WorkspaceID(), active: true}
	return nil
}

func (service *TerminalService) unbindRemoteThread(threadID string) error {
	threadID = strings.TrimSpace(threadID)
	service.mu.Lock()
	remote := service.remote
	if current, owned := service.remoteThreads[threadID]; owned {
		current.active = false
		service.remoteThreads[threadID] = current
	}
	service.mu.Unlock()
	if remote == nil {
		return nil
	}
	return remote.Unbind(threadID)
}

func (service *TerminalService) runtimeFor(threadID, remoteWorkspaceID string) (terminalRuntime, error) {
	service.mu.RLock()
	local, remote := service.runtime, service.remote
	owner, owned := service.remoteThreads[strings.TrimSpace(threadID)]
	service.mu.RUnlock()
	if remoteWorkspaceID != "" {
		if remote == nil || !owned || !owner.active || owner.workspaceID != remoteWorkspaceID {
			return nil, ErrRemoteTerminalInactive
		}
		return remote, nil
	}
	if owned {
		return nil, errors.New("terminal thread belongs to a remote workspace")
	}
	if local == nil {
		return nil, errors.New("terminal service is not ready")
	}
	return local, nil
}

func (service *TerminalService) runtimeForThread(threadID string) (terminalRuntime, error) {
	service.mu.RLock()
	local, remote := service.runtime, service.remote
	owner, owned := service.remoteThreads[strings.TrimSpace(threadID)]
	service.mu.RUnlock()
	if owned {
		if remote == nil || !owner.active {
			return nil, ErrRemoteTerminalInactive
		}
		return remote, nil
	}
	if local == nil {
		return nil, errors.New("terminal service is not ready")
	}
	return local, nil
}

func remoteWorkspaceID(record workspace.Record) string {
	if record.Location.Kind == workspace.KindSSH {
		return record.ID
	}
	return ""
}

func mapTerminalSnapshot(snapshot terminalruntime.Snapshot) domain.TerminalState {
	return domain.TerminalState{
		ThreadID:   snapshot.ThreadID,
		CWD:        snapshot.CWD,
		Shell:      snapshot.Shell,
		Running:    snapshot.Running,
		Generation: snapshot.Generation,
		Sequence:   snapshot.Sequence,
		OutputB64:  base64.StdEncoding.EncodeToString(snapshot.Output),
	}
}
