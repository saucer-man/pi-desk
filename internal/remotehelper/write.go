package remotehelper

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
)

const MethodFileWrite = "file.write"

var (
	ErrFileConflict = errors.New("remote file changed before conditional write")
	ErrFileWrite    = errors.New("remote file could not be written atomically")
)

type FileWriteRequest struct {
	RootHandle     string `json:"rootHandle"`
	Path           string `json:"path"`
	ExpectedSHA256 string `json:"expectedSha256,omitempty"`
	ExpectedAbsent bool   `json:"expectedAbsent,omitempty"`
}

type FileWriteResponse struct {
	Path                         string `json:"path"`
	Size                         int64  `json:"size"`
	SHA256                       string `json:"sha256"`
	Created                      bool   `json:"created"`
	ExtendedMetadataNotPreserved bool   `json:"extendedMetadataNotPreserved"`
}

type existingFileState struct {
	info   os.FileInfo
	digest string
	uid    int
	gid    int
}

func (manager *rootManager) Write(ctx context.Context, request FileWriteRequest, content []byte) (FileWriteResponse, error) {
	if err := validateWriteRequest(request, content); err != nil {
		return FileWriteResponse{}, err
	}
	capability, err := manager.lookup(request.RootHandle)
	if err != nil {
		return FileWriteResponse{}, err
	}
	capability.mutationMu.Lock()
	defer capability.mutationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return FileWriteResponse{}, err
	}

	state, exists, err := inspectWriteTarget(ctx, capability.root, request.Path)
	if err != nil {
		return FileWriteResponse{}, err
	}
	if request.ExpectedAbsent {
		if exists {
			return FileWriteResponse{}, ErrFileConflict
		}
	} else if !exists || state.digest != request.ExpectedSHA256 {
		return FileWriteResponse{}, ErrFileConflict
	}

	temporaryPath, temporary, err := createWriteTemporary(capability.root, path.Dir(request.Path))
	if err != nil {
		return FileWriteResponse{}, ErrFileWrite
	}
	keepTemporary := true
	defer func() {
		_ = temporary.Close()
		if keepTemporary {
			_ = capability.root.Remove(temporaryPath)
		}
	}()
	if err := writeContext(ctx, temporary, content); err != nil {
		return FileWriteResponse{}, err
	}
	if exists {
		if err := applyExistingMetadata(temporary, state); err != nil {
			return FileWriteResponse{}, ErrFileUnsupported
		}
	}
	if err := temporary.Sync(); err != nil {
		return FileWriteResponse{}, ErrFileWrite
	}
	if err := temporary.Close(); err != nil {
		return FileWriteResponse{}, ErrFileWrite
	}
	if err := ctx.Err(); err != nil {
		return FileWriteResponse{}, err
	}

	current, stillExists, err := inspectWriteTarget(ctx, capability.root, request.Path)
	if err != nil {
		return FileWriteResponse{}, err
	}
	if request.ExpectedAbsent {
		if stillExists {
			return FileWriteResponse{}, ErrFileConflict
		}
	} else if !stillExists || !os.SameFile(state.info, current.info) || current.digest != request.ExpectedSHA256 || current.info.Mode().Perm() != state.info.Mode().Perm() || current.uid != state.uid || current.gid != state.gid {
		return FileWriteResponse{}, ErrFileConflict
	}
	if request.ExpectedAbsent {
		if err := capability.root.Link(temporaryPath, request.Path); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return FileWriteResponse{}, ErrFileConflict
			}
			return FileWriteResponse{}, ErrFileWrite
		}
		if err := capability.root.Remove(temporaryPath); err == nil {
			keepTemporary = false
		}
	} else {
		if err := capability.root.Rename(temporaryPath, request.Path); err != nil {
			return FileWriteResponse{}, ErrFileWrite
		}
		keepTemporary = false
	}
	digest := sha256.Sum256(content)
	return FileWriteResponse{
		Path: request.Path, Size: int64(len(content)), SHA256: hex.EncodeToString(digest[:]),
		Created: !exists, ExtendedMetadataNotPreserved: exists,
	}, nil
}

func validateWriteRequest(request FileWriteRequest, content []byte) error {
	if !validRootHandle(request.RootHandle) || validateRelativePath(request.Path, false) != nil || len(content) > maxReadableFileBytes {
		return ErrFileInvalidPath
	}
	if request.ExpectedAbsent == (request.ExpectedSHA256 != "") {
		return ErrFileInvalidPath
	}
	if request.ExpectedSHA256 != "" && !validWriteSHA256(request.ExpectedSHA256) {
		return ErrFileInvalidPath
	}
	return nil
}

func validWriteSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, char := range value {
		if char < '0' || (char > '9' && char < 'a') || char > 'f' {
			return false
		}
	}
	return true
}

func inspectWriteTarget(ctx context.Context, root *os.Root, logicalPath string) (existingFileState, bool, error) {
	info, err := root.Lstat(logicalPath)
	if errors.Is(err, fs.ErrNotExist) {
		return existingFileState{}, false, nil
	}
	if err != nil {
		return existingFileState{}, false, ErrFileWrite
	}
	if !info.Mode().IsRegular() {
		return existingFileState{}, false, ErrFileUnsupported
	}
	file, err := root.Open(logicalPath)
	if err != nil {
		return existingFileState{}, false, ErrFileWrite
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return existingFileState{}, false, ErrFileConflict
	}
	uid, gid, ok := fileOwnership(openedInfo)
	if !ok {
		return existingFileState{}, false, ErrFileUnsupported
	}
	if openedInfo.Size() < 0 || openedInfo.Size() > maxReadableFileBytes {
		return existingFileState{}, false, ErrFileOutputLimit
	}
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(&contextReader{ctx: ctx, reader: file}, maxReadableFileBytes+1))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return existingFileState{}, false, err
		}
		return existingFileState{}, false, ErrFileWrite
	}
	if written != openedInfo.Size() {
		return existingFileState{}, false, ErrFileConflict
	}
	return existingFileState{info: openedInfo, digest: hex.EncodeToString(digest.Sum(nil)), uid: uid, gid: gid}, true, nil
}

func createWriteTemporary(root *os.Root, parent string) (string, *os.File, error) {
	for range 4 {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", nil, err
		}
		name := ".pi-desk-write-" + hex.EncodeToString(random[:])
		if parent != "." {
			name = path.Join(parent, name)
		}
		file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return name, file, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", nil, err
		}
	}
	return "", nil, ErrFileWrite
}

func writeContext(ctx context.Context, file *os.File, content []byte) error {
	for len(content) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		written, err := file.Write(content)
		if err != nil {
			return ErrFileWrite
		}
		if written <= 0 || written > len(content) {
			return ErrFileWrite
		}
		content = content[written:]
	}
	return nil
}

func applyExistingMetadata(file *os.File, state existingFileState) error {
	if err := file.Chown(state.uid, state.gid); err != nil {
		return err
	}
	if err := file.Chmod(state.info.Mode().Perm()); err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil || info.Mode().Perm() != state.info.Mode().Perm() {
		return ErrFileUnsupported
	}
	uid, gid, ok := fileOwnership(info)
	if !ok || uid != state.uid || gid != state.gid {
		return ErrFileUnsupported
	}
	return nil
}
