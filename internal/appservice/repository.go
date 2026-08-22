package appservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"pi-desk/internal/domain"
	"pi-desk/internal/repository"
	"pi-desk/internal/workspace"

	"github.com/natefinch/atomic"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const repositoryTimeout = 15 * time.Second

var ErrRemoteRepositoryStale = errors.New("REMOTE_DISCONNECTED: remote repository is disconnected or stale")

type workspaceResolver interface {
	ResolvePath(string) (workspace.Record, error)
}

type repositoryWorkspaceResolver interface {
	workspaceResolver
	ResolveID(string) (workspace.Record, error)
}

type repositoryScanner interface {
	Snapshot(context.Context, string) (repository.Snapshot, error)
	Diff(context.Context, string, string) (repository.FileDiff, error)
	Branches(context.Context, string) (repository.BranchInventory, error)
}

type remoteRepositoryBackend interface {
	WorkspaceID() string
	Generation() uint64
	ValidateBinding() error
	Snapshot(context.Context) (repository.Snapshot, error)
	Diff(context.Context, string) (repository.FileDiff, error)
	Branches(context.Context) (repository.BranchInventory, error)
	Preview(context.Context, string) (repository.FilePreview, error)
}

type remoteRepositoryBinding struct {
	backend    remoteRepositoryBackend
	generation uint64
}

type RepositoryService struct {
	catalog      repositoryWorkspaceResolver
	scanner      repositoryScanner
	openFile     func(string) error
	openFileWith func(string) error
	revealFile   func(string) error
	remoteMu     sync.RWMutex
	remote       map[string]*remoteRepositoryBinding
	remoteSeen   map[string]uint64
}

func NewRepositoryService(catalog *workspace.Catalog, scanner *repository.Scanner) *RepositoryService {
	return &RepositoryService{
		catalog:    catalog,
		scanner:    scanner,
		remote:     make(map[string]*remoteRepositoryBinding),
		remoteSeen: make(map[string]uint64),
		openFile: func(path string) error {
			app := application.Get()
			if app == nil || app.Browser == nil {
				return errors.New("desktop file opener is unavailable")
			}
			return app.Browser.OpenFile(path)
		},
		openFileWith: openWithChooser,
		revealFile: func(path string) error {
			app := application.Get()
			if app == nil || app.Env == nil {
				return errors.New("desktop file manager is unavailable")
			}
			return app.Env.OpenFileManager(path, true)
		},
	}
}

func newRepositoryService(catalog repositoryWorkspaceResolver, scanner repositoryScanner) *RepositoryService {
	return &RepositoryService{
		catalog: catalog, scanner: scanner,
		remote: make(map[string]*remoteRepositoryBinding), remoteSeen: make(map[string]uint64),
	}
}

func (service *RepositoryService) Snapshot(request domain.RepositoryRequest) (domain.RepositorySnapshot, error) {
	if service.catalog == nil {
		return domain.RepositorySnapshot{}, errors.New("repository service is unavailable")
	}
	record, err := service.trustedWorkspace(request.WorkspaceID, request.WorkspacePath)
	if err != nil {
		return domain.RepositorySnapshot{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), repositoryTimeout)
	defer cancel()
	var snapshot repository.Snapshot
	if record.Location.Kind == workspace.KindSSH {
		binding, backendErr := service.remoteBackend(record.ID)
		if backendErr != nil {
			return domain.RepositorySnapshot{}, backendErr
		}
		snapshot, err = binding.backend.Snapshot(ctx)
		if staleErr := service.validateRemoteCompletion(record.ID, binding); staleErr != nil {
			return domain.RepositorySnapshot{}, staleErr
		}
	} else if service.scanner == nil {
		err = errors.New("repository service is unavailable")
	} else {
		snapshot, err = service.scanner.Snapshot(ctx, record.Path)
	}
	if err != nil {
		return domain.RepositorySnapshot{}, err
	}
	result := domain.RepositorySnapshot{
		Files:     make([]domain.RepositoryFile, 0, len(snapshot.Files)),
		Truncated: snapshot.Truncated,
		Git: domain.GitStatus{
			IsRepository: snapshot.Git.IsRepository,
			Branch:       snapshot.Git.Branch,
			Detached:     snapshot.Git.Detached,
			Ahead:        snapshot.Git.Ahead,
			Behind:       snapshot.Git.Behind,
			Files:        make([]domain.GitChangedFile, 0, len(snapshot.Git.Files)),
		},
	}
	for _, file := range snapshot.Files {
		result.Files = append(result.Files, domain.RepositoryFile{Path: file.Path, Name: file.Name})
	}
	for _, file := range snapshot.Git.Files {
		result.Git.Files = append(result.Git.Files, domain.GitChangedFile{
			Path: file.Path, OriginalPath: file.OriginalPath, IndexStatus: file.IndexStatus, WorktreeStatus: file.WorktreeStatus,
		})
	}
	return result, nil
}

func (service *RepositoryService) Diff(request domain.RepositoryFileRequest) (domain.RepositoryFileDiff, error) {
	if service.catalog == nil {
		return domain.RepositoryFileDiff{}, errors.New("repository service is unavailable")
	}
	record, err := service.trustedWorkspace(request.WorkspaceID, request.WorkspacePath)
	if err != nil {
		return domain.RepositoryFileDiff{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), repositoryTimeout)
	defer cancel()
	var diff repository.FileDiff
	if record.Location.Kind == workspace.KindSSH {
		binding, backendErr := service.remoteBackend(record.ID)
		if backendErr != nil {
			return domain.RepositoryFileDiff{}, backendErr
		}
		diff, err = binding.backend.Diff(ctx, request.Path)
		if staleErr := service.validateRemoteCompletion(record.ID, binding); staleErr != nil {
			return domain.RepositoryFileDiff{}, staleErr
		}
	} else if service.scanner == nil {
		err = errors.New("repository service is unavailable")
	} else {
		diff, err = service.scanner.Diff(ctx, record.Path, request.Path)
	}
	if err != nil {
		return domain.RepositoryFileDiff{}, err
	}
	return domain.RepositoryFileDiff{
		Path: diff.Path, Staged: diff.Staged, Working: diff.Working, Content: diff.Content, Binary: diff.Binary, Truncated: diff.Truncated,
	}, nil
}

func (service *RepositoryService) Branches(request domain.RepositoryRequest) (domain.GitBranchInventory, error) {
	if service.catalog == nil {
		return domain.GitBranchInventory{}, errors.New("repository service is unavailable")
	}
	record, err := service.trustedWorkspace(request.WorkspaceID, request.WorkspacePath)
	if err != nil {
		return domain.GitBranchInventory{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), repositoryTimeout)
	defer cancel()
	var inventory repository.BranchInventory
	if record.Location.Kind == workspace.KindSSH {
		binding, backendErr := service.remoteBackend(record.ID)
		if backendErr != nil {
			return domain.GitBranchInventory{}, backendErr
		}
		inventory, err = binding.backend.Branches(ctx)
		if staleErr := service.validateRemoteCompletion(record.ID, binding); staleErr != nil {
			return domain.GitBranchInventory{}, staleErr
		}
	} else if service.scanner == nil {
		err = errors.New("repository service is unavailable")
	} else {
		inventory, err = service.scanner.Branches(ctx, record.Path)
	}
	if err != nil {
		return domain.GitBranchInventory{}, err
	}
	result := domain.GitBranchInventory{Branches: make([]domain.GitBranch, 0, len(inventory.Branches))}
	for _, branch := range inventory.Branches {
		result.Branches = append(result.Branches, domain.GitBranch{
			Name: branch.Name, FullName: branch.FullName, Remote: branch.Remote, Current: branch.Current,
			Upstream: branch.Upstream, Commit: branch.Commit, WorktreePath: branch.WorktreePath,
		})
	}
	return result, nil
}

func (service *RepositoryService) PreviewFile(request domain.RepositoryFileRequest) (domain.RepositoryFilePreview, error) {
	record, err := service.trustedWorkspace(request.WorkspaceID, request.WorkspacePath)
	if err != nil {
		return domain.RepositoryFilePreview{}, err
	}
	var preview repository.FilePreview
	if record.Location.Kind == workspace.KindSSH {
		binding, backendErr := service.remoteBackend(record.ID)
		if backendErr != nil {
			return domain.RepositoryFilePreview{}, backendErr
		}
		ctx, cancel := context.WithTimeout(context.Background(), repositoryTimeout)
		defer cancel()
		preview, err = binding.backend.Preview(ctx, request.Path)
		if staleErr := service.validateRemoteCompletion(record.ID, binding); staleErr != nil {
			return domain.RepositoryFilePreview{}, staleErr
		}
	} else {
		preview, err = repository.PreviewFile(record.Path, request.Path)
	}
	if err != nil {
		return domain.RepositoryFilePreview{}, err
	}
	absolutePath := preview.Path
	if record.Location.Kind == workspace.KindSSH {
		absolutePath = ""
	}
	return domain.RepositoryFilePreview{
		Path: filepath.ToSlash(strings.TrimSpace(request.Path)), AbsolutePath: absolutePath,
		Content: preview.Content, Size: preview.Size, Binary: preview.Binary, Truncated: preview.Truncated,
	}, nil
}

func (service *RepositoryService) OpenFile(request domain.RepositoryFileRequest) error {
	if service.openFile == nil {
		return errors.New("desktop file opener is unavailable")
	}
	path, err := service.resolveFile(request)
	if err != nil {
		return err
	}
	return service.openFile(path)
}

func (service *RepositoryService) OpenFileWith(request domain.RepositoryFileRequest) error {
	if service.openFileWith == nil {
		return errors.New("system Open With dialog is unavailable")
	}
	path, err := service.resolveFile(request)
	if err != nil {
		return err
	}
	return service.openFileWith(path)
}

func (service *RepositoryService) SaveFileAs(request domain.RepositorySaveFileRequest) error {
	sourcePath, err := service.resolveFile(domain.RepositoryFileRequest{WorkspaceID: request.WorkspaceID, WorkspacePath: request.WorkspacePath, Path: request.Path})
	if err != nil {
		return err
	}
	outputPath := strings.TrimSpace(request.OutputPath)
	if outputPath == "" || !filepath.IsAbs(outputPath) {
		return errors.New("save destination must be an absolute path")
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer source.Close()
	if err := atomic.WriteFile(outputPath, source); err != nil {
		return fmt.Errorf("save file copy: %w", err)
	}
	if info, statErr := os.Stat(sourcePath); statErr == nil {
		if chmodErr := os.Chmod(outputPath, info.Mode().Perm()); chmodErr != nil {
			return fmt.Errorf("preserve saved file permissions: %w", chmodErr)
		}
	}
	return nil
}

func (service *RepositoryService) RevealFile(request domain.RepositoryFileRequest) error {
	if service.revealFile == nil {
		return errors.New("desktop file manager is unavailable")
	}
	path, err := service.resolveFile(request)
	if err != nil {
		return err
	}
	return service.revealFile(path)
}

func (service *RepositoryService) trustedWorkspace(id, workspacePath string) (workspace.Record, error) {
	record, err := service.registeredWorkspace(id, workspacePath)
	if err != nil {
		return workspace.Record{}, err
	}
	if record.Trust != "approve" {
		return workspace.Record{}, errors.New("workspace access requires trust approval")
	}
	return record, nil
}

func (service *RepositoryService) registeredWorkspace(id, workspacePath string) (workspace.Record, error) {
	if service.catalog == nil {
		return workspace.Record{}, errors.New("repository service is unavailable")
	}
	id, workspacePath = strings.TrimSpace(id), strings.TrimSpace(workspacePath)
	if id != "" && workspacePath != "" {
		return workspace.Record{}, errors.New("repository workspace id and path are mutually exclusive")
	}
	if id != "" {
		record, err := service.catalog.ResolveID(id)
		if err != nil {
			return workspace.Record{}, err
		}
		if record.Location.Kind == workspace.KindLocal {
			verified, err := service.catalog.ResolvePath(record.Path)
			if err != nil || verified.ID != record.ID {
				return workspace.Record{}, errors.New("registered local workspace boundary changed")
			}
		}
		return record, nil
	}
	return service.catalog.ResolvePath(workspacePath)
}

func (service *RepositoryService) resolveFile(request domain.RepositoryFileRequest) (string, error) {
	if service.catalog == nil {
		return "", errors.New("repository service is unavailable")
	}
	record, err := service.trustedWorkspace(request.WorkspaceID, request.WorkspacePath)
	if err != nil {
		return "", err
	}
	if record.Location.Kind == workspace.KindSSH {
		return "", errors.New("remote files cannot be opened, revealed, or copied through local filesystem APIs")
	}
	return repository.ResolveFile(record.Path, request.Path)
}

func (service *RepositoryService) bindRemoteWorkspace(workspaceID string, backend remoteRepositoryBackend) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" || backend == nil || backend.WorkspaceID() != workspaceID || backend.Generation() == 0 {
		return errors.New("remote repository binding is invalid")
	}
	record, err := service.trustedWorkspace(workspaceID, "")
	if err != nil {
		return err
	}
	if record.Location.Kind != workspace.KindSSH {
		return errors.New("remote repository requires an SSH workspace")
	}
	if err := backend.ValidateBinding(); err != nil {
		return ErrRemoteRepositoryStale
	}
	service.remoteMu.Lock()
	defer service.remoteMu.Unlock()
	if current := service.remote[workspaceID]; current != nil && current.backend == backend {
		return nil
	}
	if backend.Generation() <= service.remoteSeen[workspaceID] {
		return ErrRemoteRepositoryStale
	}
	service.remote[workspaceID] = &remoteRepositoryBinding{backend: backend, generation: backend.Generation()}
	service.remoteSeen[workspaceID] = backend.Generation()
	return nil
}

// unbindRemoteWorkspace requires the generation being revoked so a late
// completion from an old target generation cannot unbind a newer backend.
func (service *RepositoryService) unbindRemoteWorkspace(workspaceID string, generation uint64) {
	workspaceID = strings.TrimSpace(workspaceID)
	service.remoteMu.Lock()
	current := service.remote[workspaceID]
	if current != nil && generation == current.generation {
		delete(service.remote, workspaceID)
	}
	service.remoteMu.Unlock()
}

func (service *RepositoryService) remoteBackend(workspaceID string) (*remoteRepositoryBinding, error) {
	service.remoteMu.RLock()
	binding := service.remote[workspaceID]
	service.remoteMu.RUnlock()
	if binding == nil || binding.backend.ValidateBinding() != nil {
		return nil, ErrRemoteRepositoryStale
	}
	return binding, nil
}

func (service *RepositoryService) validateRemoteCompletion(workspaceID string, binding *remoteRepositoryBinding) error {
	service.remoteMu.RLock()
	current := service.remote[workspaceID]
	service.remoteMu.RUnlock()
	if binding == nil || current != binding || binding.backend.ValidateBinding() != nil {
		return ErrRemoteRepositoryStale
	}
	return nil
}

func openWithChooser(path string) error {
	if runtime.GOOS != "windows" {
		return errors.New("system Open With dialog is currently available on Windows only")
	}
	if err := exec.Command("rundll32.exe", "shell32.dll,OpenAs_RunDLL", path).Start(); err != nil {
		return fmt.Errorf("open system Open With dialog: %w", err)
	}
	return nil
}
