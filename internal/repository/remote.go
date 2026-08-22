package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"pi-desk/internal/remotessh"
)

const maxRemoteRepositoryFiles = 2000

type remoteRuntime interface {
	ValidateGeneration(uint64) error
	AcquireRead(context.Context, remotessh.RuntimeLeaseRequest) (*remotessh.RuntimeLease, error)
	FindFiles(context.Context, *remotessh.RuntimeLease, remotessh.RuntimeSearchFindRequest) (remotessh.RuntimeSearchFindResult, error)
	ReadGit(context.Context, *remotessh.RuntimeLease, remotessh.RuntimeGitReadRequest) (remotessh.RuntimeGitReadResult, error)
	StatFile(context.Context, *remotessh.RuntimeLease, string) (remotessh.RuntimeFileInfo, error)
	ReadFile(context.Context, *remotessh.RuntimeLease, string, int, int) (remotessh.RuntimeFileRead, error)
}

// RemoteBackend projects Repository data through one host-minted root
// capability. It cannot construct a capability, reconnect a target, or touch a
// local anchor path.
type RemoteBackend struct {
	runtime remoteRuntime
	root    *remotessh.RuntimeRootCapability
	nextID  atomic.Uint64
}

func NewRemoteBackend(runtime *remotessh.RuntimeLeaseSupervisor, root *remotessh.RuntimeRootCapability) (*RemoteBackend, error) {
	if runtime == nil || root == nil || root.Generation() == 0 || root.WorkspaceID() == "" {
		return nil, errors.New("remote repository runtime and root capability are required")
	}
	if err := runtime.ValidateGeneration(root.Generation()); err != nil {
		return nil, err
	}
	return &RemoteBackend{runtime: runtime, root: root}, nil
}

func newRemoteBackend(runtime remoteRuntime, root *remotessh.RuntimeRootCapability) *RemoteBackend {
	return &RemoteBackend{runtime: runtime, root: root}
}

func (backend *RemoteBackend) WorkspaceID() string {
	if backend == nil || backend.root == nil {
		return ""
	}
	return backend.root.WorkspaceID()
}

func (backend *RemoteBackend) Generation() uint64 {
	if backend == nil || backend.root == nil {
		return 0
	}
	return backend.root.Generation()
}

func (backend *RemoteBackend) ValidateBinding() error {
	if backend == nil || backend.runtime == nil || backend.Generation() == 0 {
		return errors.New("remote repository backend is unavailable")
	}
	return backend.runtime.ValidateGeneration(backend.Generation())
}

func (backend *RemoteBackend) Snapshot(ctx context.Context) (Snapshot, error) {
	lease, err := backend.acquire(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer lease.Release()
	found, err := backend.runtime.FindFiles(ctx, lease, remotessh.RuntimeSearchFindRequest{Path: ".", Pattern: "**", Limit: maxRemoteRepositoryFiles})
	if err != nil {
		return Snapshot{}, err
	}
	files := make([]File, 0, len(found.Paths))
	for _, candidate := range found.Paths {
		if !validRemoteRepositoryPath(candidate) {
			return Snapshot{}, errors.New("remote repository returned an invalid file path")
		}
		files = append(files, File{Path: candidate, Name: path.Base(candidate)})
	}
	sortRemoteFiles(files)
	status := GitStatus{}
	gitResult, err := backend.runtime.ReadGit(ctx, lease, remotessh.RuntimeGitReadRequest{Operation: "status"})
	if err == nil {
		status, err = parseRemoteStatus(remoteGitPart(gitResult, "status"))
	} else if errors.Is(err, remotessh.ErrRuntimeGitUnavailable) {
		err = nil
	} else {
		return Snapshot{}, err
	}
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Files: files, Truncated: found.BudgetReached, Git: status}, nil
}

func (backend *RemoteBackend) Diff(ctx context.Context, logicalPath string) (FileDiff, error) {
	logicalPath, err := normalizeRemoteRepositoryPath(logicalPath)
	if err != nil {
		return FileDiff{}, err
	}
	lease, err := backend.acquire(ctx)
	if err != nil {
		return FileDiff{}, err
	}
	defer lease.Release()
	result, err := backend.runtime.ReadGit(ctx, lease, remotessh.RuntimeGitReadRequest{Operation: "diff", Path: logicalPath})
	if err != nil {
		return FileDiff{}, err
	}
	staged := remoteGitPart(result, "staged")
	working := remoteGitPart(result, "working")
	stagedText, stagedTruncated := boundedRemoteUTF8(staged, maxDiffBytes)
	workingText, workingTruncated := boundedRemoteUTF8(working, maxDiffBytes)
	diff := FileDiff{
		Path: logicalPath, Staged: stagedText, Working: workingText,
		Binary: isBinaryDiff(staged) || isBinaryDiff(working), Truncated: stagedTruncated || workingTruncated,
	}
	if len(staged) != 0 || len(working) != 0 {
		return diff, nil
	}
	preview, err := backend.previewWithLease(ctx, lease, logicalPath)
	if err != nil {
		return FileDiff{}, err
	}
	diff.Content, diff.Binary, diff.Truncated = preview.Content, preview.Binary, preview.Truncated
	return diff, nil
}

func (backend *RemoteBackend) Branches(ctx context.Context) (BranchInventory, error) {
	lease, err := backend.acquire(ctx)
	if err != nil {
		return BranchInventory{}, err
	}
	defer lease.Release()
	result, err := backend.runtime.ReadGit(ctx, lease, remotessh.RuntimeGitReadRequest{Operation: "branches"})
	if err != nil {
		return BranchInventory{}, err
	}
	worktrees, err := parseRemoteWorktreeBranches(remoteGitPart(result, "worktrees"))
	if err != nil {
		return BranchInventory{}, err
	}
	branches, err := parseBranches(remoteGitPart(result, "refs"), worktrees)
	if err != nil {
		return BranchInventory{}, err
	}
	return BranchInventory{Branches: branches}, nil
}

func (backend *RemoteBackend) Preview(ctx context.Context, logicalPath string) (FilePreview, error) {
	logicalPath, err := normalizeRemoteRepositoryPath(logicalPath)
	if err != nil {
		return FilePreview{}, err
	}
	lease, err := backend.acquire(ctx)
	if err != nil {
		return FilePreview{}, err
	}
	defer lease.Release()
	return backend.previewWithLease(ctx, lease, logicalPath)
}

func (backend *RemoteBackend) previewWithLease(ctx context.Context, lease *remotessh.RuntimeLease, logicalPath string) (FilePreview, error) {
	info, err := backend.runtime.StatFile(ctx, lease, logicalPath)
	if err != nil {
		return FilePreview{}, err
	}
	if info.Kind != "file" {
		return FilePreview{}, errors.New("remote repository path is not a regular file")
	}
	read, err := backend.runtime.ReadFile(ctx, lease, logicalPath, 1, 2000)
	if errors.Is(err, remotessh.ErrRuntimeFileUnsupported) {
		return FilePreview{Path: logicalPath, Size: info.Size, Binary: true}, nil
	}
	if err != nil {
		return FilePreview{}, err
	}
	return FilePreview{
		Path: logicalPath, Content: read.Content, Size: info.Size,
		Truncated: read.Truncated || read.LineTruncated || read.NextLine > 0,
	}, nil
}

func (backend *RemoteBackend) acquire(ctx context.Context) (*remotessh.RuntimeLease, error) {
	if backend == nil || backend.runtime == nil || backend.root == nil {
		return nil, errors.New("remote repository backend is unavailable")
	}
	owner := "repository-read-" + strconv.FormatUint(backend.nextID.Add(1), 10)
	return backend.runtime.AcquireRead(ctx, remotessh.RuntimeLeaseRequest{Root: backend.root, OwnerID: owner})
}

func remoteGitPart(result remotessh.RuntimeGitReadResult, name string) []byte {
	for _, part := range result.Parts {
		if part.Name == name && part.Offset >= 0 && part.Size >= 0 && part.Offset+part.Size <= int64(len(result.Blob)) {
			return result.Blob[part.Offset : part.Offset+part.Size]
		}
	}
	return nil
}

func parseRemoteStatus(output []byte) (GitStatus, error) {
	status := GitStatus{IsRepository: true}
	records := bytes.Split(output, []byte{0})
	for index := 0; index < len(records); index++ {
		if len(records[index]) > 4096 || !utf8.Valid(records[index]) {
			return GitStatus{}, errors.New("remote Git status record is invalid")
		}
		record := string(records[index])
		if record == "" {
			continue
		}
		if strings.HasPrefix(record, "## ") {
			parseBranchHeader(strings.TrimPrefix(record, "## "), &status)
			if status.Branch == "" || !validRemoteDisplayText(status.Branch) {
				return GitStatus{}, errors.New("remote Git branch header is invalid")
			}
			continue
		}
		if len(record) < 4 || record[2] != ' ' || !validRemoteGitStatus(record[0]) || !validRemoteGitStatus(record[1]) {
			return GitStatus{}, fmt.Errorf("invalid remote Git status record")
		}
		changed := ChangedFile{Path: record[3:], IndexStatus: string(record[0]), WorktreeStatus: string(record[1])}
		if !validRemoteRepositoryPath(changed.Path) {
			return GitStatus{}, errors.New("remote Git returned an invalid changed path")
		}
		if record[0] == 'R' || record[0] == 'C' || record[1] == 'R' || record[1] == 'C' {
			index++
			if index >= len(records) || !validRemoteRepositoryPath(string(records[index])) {
				return GitStatus{}, errors.New("remote Git rename status is invalid")
			}
			changed.OriginalPath = string(records[index])
		}
		status.Files = append(status.Files, changed)
	}
	return status, nil
}

func parseRemoteWorktreeBranches(output []byte) (map[string]string, error) {
	worktrees := make(map[string]string)
	var worktreePath string
	for _, raw := range bytes.Split(output, []byte{0}) {
		field := string(raw)
		if field == "" {
			worktreePath = ""
			continue
		}
		switch {
		case strings.HasPrefix(field, "worktree "):
			worktreePath = strings.TrimPrefix(field, "worktree ")
			if !validRemoteAbsolutePath(worktreePath) {
				return nil, errors.New("remote Git returned an invalid worktree path")
			}
		case strings.HasPrefix(field, "branch "):
			branch := strings.TrimPrefix(field, "branch ")
			if worktreePath == "" || !strings.HasPrefix(branch, "refs/heads/") {
				return nil, errors.New("remote Git returned an invalid worktree branch")
			}
			worktrees[branch] = worktreePath
		}
	}
	return worktrees, nil
}

func normalizeRemoteRepositoryPath(value string) (string, error) {
	if !validRemoteRepositoryPath(value) {
		return "", errors.New("remote repository path is invalid")
	}
	return value, nil
}

func validRemoteRepositoryPath(value string) bool {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) || path.IsAbs(value) || path.Clean(value) != value || strings.Contains(value, "\\") {
		return false
	}
	for _, component := range strings.Split(value, "/") {
		if component == "" || component == "." || component == ".." || len(component) > 255 {
			return false
		}
	}
	for _, char := range value {
		if char == 0 || unicode.IsControl(char) || unicode.Is(unicode.Cf, char) {
			return false
		}
	}
	return true
}

func validRemoteGitStatus(value byte) bool {
	return strings.ContainsRune(" MTADRCU?!", rune(value))
}

func validRemoteDisplayText(value string) bool {
	if value == "" || !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if char == 0 || unicode.IsControl(char) || unicode.Is(unicode.Cf, char) {
			return false
		}
	}
	return true
}

func validRemoteAbsolutePath(value string) bool {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) || !path.IsAbs(value) || path.Clean(value) != value || strings.Contains(value, "\\") {
		return false
	}
	for _, char := range value {
		if char == 0 || unicode.IsControl(char) || unicode.Is(unicode.Cf, char) {
			return false
		}
	}
	return true
}

func boundedRemoteUTF8(value []byte, limit int) (string, bool) {
	truncated := len(value) > limit
	if truncated {
		value = value[:limit]
	}
	return strings.ToValidUTF8(string(value), "\uFFFD"), truncated
}

func sortRemoteFiles(files []File) {
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Path) < strings.ToLower(files[j].Path) })
}
