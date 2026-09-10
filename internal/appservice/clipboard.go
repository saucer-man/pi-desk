package appservice

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"pi-desk/internal/domain"
	"pi-desk/internal/repository"
	"pi-desk/internal/workspace"
)

// ClipboardFiles returns references to local files: workspace files use
// workspace-relative paths and other files use absolute paths. Ordinary
// files are never read, uploaded, moved or copied by this operation.
func (service *RepositoryService) ClipboardFiles(request domain.RepositoryRequest) ([]domain.RepositoryFile, error) {
	files := []domain.RepositoryFile{}
	if service.clipboardFiles == nil {
		return files, nil
	}
	paths, err := service.clipboardFiles()
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return files, nil
	}
	if len(paths) > 128 {
		return nil, errors.New("paste at most 128 files at a time")
	}
	record, err := service.trustedWorkspace(request.WorkspaceID, request.WorkspacePath)
	if err != nil {
		return nil, err
	}
	if record.Location.Kind == workspace.KindSSH {
		return nil, errors.New("local clipboard files cannot be referenced in an SSH workspace")
	}
	root, err := workspace.CanonicalDirectory(record.Path)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	for _, path := range paths {
		if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
			return nil, errors.New("invalid clipboard file path")
		}
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil, errors.New("a copied file is no longer available")
		}
		relative, err := filepath.Rel(root, canonical)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
			if _, err := repository.ResolveFile(root, filepath.ToSlash(relative)); err != nil {
				return nil, errors.New("only existing regular files inside the current workspace can be pasted")
			}
			path = filepath.ToSlash(relative)
		} else {
			info, err := os.Stat(canonical)
			if err != nil || !info.Mode().IsRegular() {
				return nil, errors.New("only existing regular files can be pasted")
			}
			path = filepath.ToSlash(canonical)
		}
		if !seen[path] {
			files = append(files, domain.RepositoryFile{Path: path, Name: filepath.Base(canonical)})
			seen[path] = true
		}
	}
	// Recheck trust and root identity after inspecting the clipboard targets.
	current, err := service.trustedWorkspace(request.WorkspaceID, request.WorkspacePath)
	if err != nil {
		return nil, err
	}
	if current.ID != record.ID || current.Path != record.Path || current.Location.Kind != record.Location.Kind {
		return nil, errors.New("workspace changed while reading clipboard files")
	}
	return files, nil
}
