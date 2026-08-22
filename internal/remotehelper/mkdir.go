package remotehelper

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"strings"
)

const MethodFileMkdir = "file.mkdir"

var ErrMutationOutcomeUnknown = errors.New("remote mutation outcome is unknown")

type FileMkdirRequest struct {
	RootHandle string `json:"rootHandle"`
	Path       string `json:"path"`
}

type FileMkdirResponse struct {
	Path    string   `json:"path"`
	Created []string `json:"created"`
}

func (manager *rootManager) Mkdir(ctx context.Context, request FileMkdirRequest) (FileMkdirResponse, error) {
	if validateRelativePath(request.Path, false) != nil {
		return FileMkdirResponse{}, ErrFileInvalidPath
	}
	capability, err := manager.lookup(request.RootHandle)
	if err != nil {
		return FileMkdirResponse{}, err
	}
	capability.mutationMu.Lock()
	defer capability.mutationMu.Unlock()
	response := FileMkdirResponse{Path: request.Path, Created: []string{}}
	current := ""
	for _, component := range strings.Split(request.Path, "/") {
		if err := ctx.Err(); err != nil {
			if len(response.Created) > 0 {
				return FileMkdirResponse{}, ErrMutationOutcomeUnknown
			}
			return FileMkdirResponse{}, err
		}
		current = componentPath(current, component)
		info, statErr := capability.root.Lstat(current)
		switch {
		case statErr == nil:
			if !info.IsDir() {
				return FileMkdirResponse{}, ErrFileConflict
			}
			continue
		case !errors.Is(statErr, fs.ErrNotExist):
			return FileMkdirResponse{}, ErrFileWrite
		}
		if err := capability.root.Mkdir(current, 0o755); err != nil {
			if errors.Is(err, fs.ErrExist) {
				info, statErr = capability.root.Lstat(current)
				if statErr == nil && info.IsDir() {
					continue
				}
				return FileMkdirResponse{}, ErrFileConflict
			}
			return FileMkdirResponse{}, ErrFileWrite
		}
		response.Created = append(response.Created, current)
	}
	return response, nil
}

func componentPath(parent, component string) string {
	if parent == "" {
		return component
	}
	return path.Join(parent, component)
}
