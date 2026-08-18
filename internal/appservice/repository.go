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
	"time"

	"pi-desk/internal/domain"
	"pi-desk/internal/repository"
	"pi-desk/internal/workspace"

	"github.com/natefinch/atomic"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const repositoryTimeout = 15 * time.Second

type workspaceResolver interface {
	ResolvePath(string) (workspace.Record, error)
}

type repositoryScanner interface {
	Snapshot(context.Context, string) (repository.Snapshot, error)
	Diff(context.Context, string, string) (repository.FileDiff, error)
	Branches(context.Context, string) (repository.BranchInventory, error)
}

type RepositoryService struct {
	catalog      workspaceResolver
	scanner      repositoryScanner
	openFile     func(string) error
	openFileWith func(string) error
	revealFile   func(string) error
}

func NewRepositoryService(catalog *workspace.Catalog, scanner *repository.Scanner) *RepositoryService {
	return &RepositoryService{
		catalog: catalog,
		scanner: scanner,
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

func newRepositoryService(catalog workspaceResolver, scanner repositoryScanner) *RepositoryService {
	return &RepositoryService{catalog: catalog, scanner: scanner}
}

func (service *RepositoryService) Snapshot(request domain.RepositoryRequest) (domain.RepositorySnapshot, error) {
	if service.catalog == nil || service.scanner == nil {
		return domain.RepositorySnapshot{}, errors.New("repository service is unavailable")
	}
	record, err := service.trustedWorkspace(request.WorkspacePath)
	if err != nil {
		return domain.RepositorySnapshot{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), repositoryTimeout)
	defer cancel()
	snapshot, err := service.scanner.Snapshot(ctx, record.Path)
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
	if service.catalog == nil || service.scanner == nil {
		return domain.RepositoryFileDiff{}, errors.New("repository service is unavailable")
	}
	record, err := service.trustedWorkspace(request.WorkspacePath)
	if err != nil {
		return domain.RepositoryFileDiff{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), repositoryTimeout)
	defer cancel()
	diff, err := service.scanner.Diff(ctx, record.Path, request.Path)
	if err != nil {
		return domain.RepositoryFileDiff{}, err
	}
	return domain.RepositoryFileDiff{
		Path: diff.Path, Staged: diff.Staged, Working: diff.Working, Content: diff.Content, Binary: diff.Binary, Truncated: diff.Truncated,
	}, nil
}

func (service *RepositoryService) Branches(request domain.RepositoryRequest) (domain.GitBranchInventory, error) {
	if service.catalog == nil || service.scanner == nil {
		return domain.GitBranchInventory{}, errors.New("repository service is unavailable")
	}
	record, err := service.trustedWorkspace(request.WorkspacePath)
	if err != nil {
		return domain.GitBranchInventory{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), repositoryTimeout)
	defer cancel()
	inventory, err := service.scanner.Branches(ctx, record.Path)
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
	record, err := service.trustedWorkspace(request.WorkspacePath)
	if err != nil {
		return domain.RepositoryFilePreview{}, err
	}
	preview, err := repository.PreviewFile(record.Path, request.Path)
	if err != nil {
		return domain.RepositoryFilePreview{}, err
	}
	return domain.RepositoryFilePreview{
		Path: filepath.ToSlash(strings.TrimSpace(request.Path)), AbsolutePath: preview.Path,
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
	sourcePath, err := service.resolveFile(domain.RepositoryFileRequest{WorkspacePath: request.WorkspacePath, Path: request.Path})
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

func (service *RepositoryService) trustedWorkspace(path string) (workspace.Record, error) {
	record, err := service.registeredWorkspace(path)
	if err != nil {
		return workspace.Record{}, err
	}
	if record.Trust != "approve" {
		return workspace.Record{}, errors.New("workspace access requires trust approval")
	}
	return record, nil
}

func (service *RepositoryService) registeredWorkspace(path string) (workspace.Record, error) {
	if service.catalog == nil {
		return workspace.Record{}, errors.New("repository service is unavailable")
	}
	return service.catalog.ResolvePath(strings.TrimSpace(path))
}

func (service *RepositoryService) resolveFile(request domain.RepositoryFileRequest) (string, error) {
	if service.catalog == nil {
		return "", errors.New("repository service is unavailable")
	}
	record, err := service.trustedWorkspace(request.WorkspacePath)
	if err != nil {
		return "", err
	}
	return repository.ResolveFile(record.Path, request.Path)
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
