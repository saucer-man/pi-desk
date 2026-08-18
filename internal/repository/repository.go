package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"pi-desk/internal/gitexec"
)

const (
	maxFiles     = 5000
	maxDepth     = 32
	maxDiffBytes = 1 << 20
	maxFileBytes = 1 << 20
)

var ErrOutputTooLarge = gitexec.ErrOutputTooLarge

type File struct {
	Path string
	Name string
}

type ChangedFile struct {
	Path           string
	OriginalPath   string
	IndexStatus    string
	WorktreeStatus string
}

type GitStatus struct {
	IsRepository bool
	Branch       string
	Detached     bool
	Ahead        int
	Behind       int
	Files        []ChangedFile
}

type Snapshot struct {
	Files     []File
	Truncated bool
	Git       GitStatus
}

type FileDiff struct {
	Path      string
	Staged    string
	Working   string
	Content   string
	Binary    bool
	Truncated bool
}

type Branch struct {
	Name         string
	FullName     string
	Remote       bool
	Current      bool
	Upstream     string
	Commit       string
	WorktreePath string
}

type BranchInventory struct {
	Branches []Branch
}

type commandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type Scanner struct {
	runner commandRunner
}

func New() *Scanner {
	return &Scanner{runner: gitexec.Runner{}}
}

func newScanner(runner commandRunner) *Scanner {
	return &Scanner{runner: runner}
}

func (scanner *Scanner) Snapshot(ctx context.Context, root string) (Snapshot, error) {
	isRepository := scanner.isRepository(ctx, root)
	files, truncated, err := scanner.listFiles(ctx, root, isRepository)
	if err != nil {
		return Snapshot{}, err
	}
	status := GitStatus{IsRepository: isRepository}
	if isRepository {
		output, err := scanner.runner.Run(ctx, root, "status", "--porcelain=v1", "-z", "--branch", "--untracked-files=all")
		if err != nil {
			return Snapshot{}, fmt.Errorf("read git status: %w", err)
		}
		status, err = parseStatus(output)
		if err != nil {
			return Snapshot{}, err
		}
	}
	return Snapshot{Files: files, Truncated: truncated, Git: status}, nil
}

func (scanner *Scanner) Diff(ctx context.Context, root, path string) (FileDiff, error) {
	normalized, err := normalizeRelativePath(path)
	if err != nil {
		return FileDiff{}, err
	}
	if !scanner.isRepository(ctx, root) {
		return FileDiff{}, errors.New("workspace is not a Git repository")
	}
	pathspec := ":(top,literal)" + normalized
	staged, err := scanner.runner.Run(ctx, root, "diff", "--cached", "--no-ext-diff", "--no-color", "--unified=3", "--", pathspec)
	if err != nil {
		return FileDiff{}, fmt.Errorf("read staged diff: %w", err)
	}
	working, err := scanner.runner.Run(ctx, root, "diff", "--no-ext-diff", "--no-color", "--unified=3", "--", pathspec)
	if err != nil {
		return FileDiff{}, fmt.Errorf("read working tree diff: %w", err)
	}
	stagedText, stagedTruncated := boundedUTF8(staged, maxDiffBytes)
	workingText, workingTruncated := boundedUTF8(working, maxDiffBytes)
	result := FileDiff{
		Path:      normalized,
		Staged:    stagedText,
		Working:   workingText,
		Binary:    isBinaryDiff(staged) || isBinaryDiff(working),
		Truncated: stagedTruncated || workingTruncated,
	}
	if len(staged) > 0 || len(working) > 0 {
		return result, nil
	}
	content, binary, truncated, err := readWorkspaceFile(root, normalized)
	if err != nil {
		return FileDiff{}, err
	}
	result.Content = content
	result.Binary = binary
	result.Truncated = truncated
	return result, nil
}

func (scanner *Scanner) Branches(ctx context.Context, root string) (BranchInventory, error) {
	if !scanner.isRepository(ctx, root) {
		return BranchInventory{}, errors.New("workspace is not a Git repository")
	}
	worktreeOutput, err := scanner.runner.Run(ctx, root, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return BranchInventory{}, fmt.Errorf("list Git worktrees: %w", err)
	}
	worktrees, err := parseWorktreeBranches(worktreeOutput)
	if err != nil {
		return BranchInventory{}, err
	}
	format := "%(refname)%09%(refname:short)%09%(HEAD)%09%(upstream:short)%09%(objectname:short)%09%(symref)"
	output, err := scanner.runner.Run(ctx, root, "for-each-ref", "--format="+format, "refs/heads", "refs/remotes")
	if err != nil {
		return BranchInventory{}, fmt.Errorf("list Git branches: %w", err)
	}
	branches, err := parseBranches(output, worktrees)
	if err != nil {
		return BranchInventory{}, err
	}
	return BranchInventory{Branches: branches}, nil
}

func (scanner *Scanner) isRepository(ctx context.Context, root string) bool {
	output, err := scanner.runner.Run(ctx, root, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(string(output)) == "true"
}

func (scanner *Scanner) listFiles(ctx context.Context, root string, isRepository bool) ([]File, bool, error) {
	if isRepository {
		output, err := scanner.runner.Run(ctx, root, "ls-files", "-co", "--exclude-standard", "-z")
		if err != nil {
			return nil, false, fmt.Errorf("list repository files: %w", err)
		}
		return parseFiles(output)
	}
	return walkFiles(ctx, root)
}

func parseFiles(output []byte) ([]File, bool, error) {
	paths := bytes.Split(output, []byte{0})
	files := make([]File, 0, min(len(paths), maxFiles))
	truncated := false
	for _, raw := range paths {
		if len(raw) == 0 {
			continue
		}
		if len(files) == maxFiles {
			truncated = true
			break
		}
		path := filepath.ToSlash(string(raw))
		if !validRelativePath(path) {
			return nil, false, fmt.Errorf("git returned an invalid repository path %q", path)
		}
		files = append(files, File{Path: path, Name: filepath.Base(filepath.FromSlash(path))})
	}
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Path) < strings.ToLower(files[j].Path) })
	return files, truncated, nil
}

func walkFiles(ctx context.Context, root string) ([]File, bool, error) {
	files := make([]File, 0)
	truncated := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		depth := strings.Count(filepath.ToSlash(relative), "/") + 1
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" || depth >= maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if len(files) == maxFiles {
			truncated = true
			return filepath.SkipAll
		}
		normalized := filepath.ToSlash(relative)
		files = append(files, File{Path: normalized, Name: entry.Name()})
		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("walk workspace: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Path) < strings.ToLower(files[j].Path) })
	return files, truncated, nil
}

func parseStatus(output []byte) (GitStatus, error) {
	status := GitStatus{IsRepository: true}
	records := bytes.Split(output, []byte{0})
	for index := 0; index < len(records); index++ {
		record := string(records[index])
		if record == "" {
			continue
		}
		if strings.HasPrefix(record, "## ") {
			parseBranchHeader(strings.TrimPrefix(record, "## "), &status)
			continue
		}
		if len(record) < 4 || record[2] != ' ' {
			return GitStatus{}, fmt.Errorf("invalid git status record %q", record)
		}
		changed := ChangedFile{
			Path:           filepath.ToSlash(record[3:]),
			IndexStatus:    string(record[0]),
			WorktreeStatus: string(record[1]),
		}
		if !validRelativePath(changed.Path) {
			return GitStatus{}, fmt.Errorf("git returned an invalid changed path %q", changed.Path)
		}
		if record[0] == 'R' || record[0] == 'C' || record[1] == 'R' || record[1] == 'C' {
			index++
			if index >= len(records) || len(records[index]) == 0 {
				return GitStatus{}, errors.New("git rename status is missing its original path")
			}
			changed.OriginalPath = filepath.ToSlash(string(records[index]))
			if !validRelativePath(changed.OriginalPath) {
				return GitStatus{}, fmt.Errorf("git returned an invalid original path %q", changed.OriginalPath)
			}
		}
		status.Files = append(status.Files, changed)
	}
	return status, nil
}

func parseBranchHeader(header string, status *GitStatus) {
	if strings.HasPrefix(header, "HEAD (no branch)") {
		status.Detached = true
		status.Branch = "HEAD"
	} else {
		name := header
		if position := strings.Index(name, "..."); position >= 0 {
			name = name[:position]
		}
		name = strings.TrimPrefix(name, "No commits yet on ")
		name = strings.TrimPrefix(name, "Initial commit on ")
		if position := strings.Index(name, " ["); position >= 0 {
			name = name[:position]
		}
		status.Branch = strings.TrimSpace(name)
	}
	if position := strings.Index(header, "["); position >= 0 {
		tracking := strings.TrimSuffix(header[position+1:], "]")
		for _, item := range strings.Split(tracking, ",") {
			fields := strings.Fields(strings.TrimSpace(item))
			if len(fields) != 2 {
				continue
			}
			value, err := strconv.Atoi(fields[1])
			if err != nil {
				continue
			}
			switch fields[0] {
			case "ahead":
				status.Ahead = value
			case "behind":
				status.Behind = value
			}
		}
	}
}

func parseWorktreeBranches(output []byte) (map[string]string, error) {
	worktrees := make(map[string]string)
	var path string
	for _, raw := range bytes.Split(output, []byte{0}) {
		field := string(raw)
		if field == "" {
			path = ""
			continue
		}
		switch {
		case strings.HasPrefix(field, "worktree "):
			path = strings.TrimPrefix(field, "worktree ")
			if path == "" || !filepath.IsAbs(path) {
				return nil, fmt.Errorf("Git returned an invalid worktree path %q", path)
			}
		case strings.HasPrefix(field, "branch "):
			branch := strings.TrimPrefix(field, "branch ")
			if path == "" || !strings.HasPrefix(branch, "refs/heads/") {
				return nil, errors.New("Git returned an invalid worktree branch record")
			}
			worktrees[branch] = filepath.Clean(filepath.FromSlash(path))
		}
	}
	return worktrees, nil
}

func parseBranches(output []byte, worktrees map[string]string) ([]Branch, error) {
	lines := bytes.Split(output, []byte{'\n'})
	branches := make([]Branch, 0, min(len(lines), maxFiles))
	for _, raw := range lines {
		if len(raw) == 0 {
			continue
		}
		fields := strings.Split(string(raw), "\t")
		if len(fields) != 6 {
			return nil, fmt.Errorf("Git returned an invalid branch record %q", raw)
		}
		fullName := fields[0]
		remote := strings.HasPrefix(fullName, "refs/remotes/")
		if (!strings.HasPrefix(fullName, "refs/heads/") && !remote) || fields[1] == "" {
			return nil, fmt.Errorf("Git returned an invalid branch name %q", fullName)
		}
		if fields[5] != "" || (remote && strings.HasSuffix(fullName, "/HEAD")) {
			continue
		}
		if len(branches) == maxFiles {
			return nil, errors.New("Git branch count exceeds the safety limit")
		}
		branches = append(branches, Branch{
			Name:         fields[1],
			FullName:     fullName,
			Remote:       remote,
			Current:      fields[2] == "*",
			Upstream:     fields[3],
			Commit:       fields[4],
			WorktreePath: worktrees[fullName],
		})
	}
	sort.Slice(branches, func(i, j int) bool {
		left, right := branches[i], branches[j]
		if left.Current != right.Current {
			return left.Current
		}
		if left.Remote != right.Remote {
			return !left.Remote
		}
		return strings.ToLower(left.Name) < strings.ToLower(right.Name)
	})
	return branches, nil
}

func validRelativePath(path string) bool {
	_, err := normalizeRelativePath(path)
	return err == nil
}

func normalizeRelativePath(path string) (string, error) {
	path = filepath.ToSlash(path)
	if path == "" || filepath.IsAbs(filepath.FromSlash(path)) {
		return "", errors.New("file path must be relative to the workspace")
	}
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("file path escapes the workspace")
	}
	return cleaned, nil
}

func ResolveFile(root, path string) (string, error) {
	normalized, err := normalizeRelativePath(path)
	if err != nil {
		return "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	resolvedRoot, err = filepath.Abs(resolvedRoot)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	candidate, err := filepath.EvalSymlinks(filepath.Join(resolvedRoot, filepath.FromSlash(normalized)))
	if err != nil {
		return "", fmt.Errorf("resolve workspace file: %w", err)
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve workspace file: %w", err)
	}
	relative, err := filepath.Rel(resolvedRoot, candidate)
	if err != nil {
		return "", fmt.Errorf("verify workspace file: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("resolved file escapes the workspace")
	}
	info, err := os.Stat(candidate)
	if err != nil {
		return "", fmt.Errorf("inspect workspace file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("workspace path is not a regular file")
	}
	return candidate, nil
}

type FilePreview struct {
	Path      string
	Content   string
	Size      int64
	Binary    bool
	Truncated bool
}

func PreviewFile(root, path string) (FilePreview, error) {
	resolved, err := ResolveFile(root, path)
	if err != nil {
		return FilePreview{}, err
	}
	file, err := os.Open(resolved)
	if err != nil {
		return FilePreview{}, fmt.Errorf("open workspace file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return FilePreview{}, fmt.Errorf("inspect workspace file: %w", err)
	}
	content, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil {
		return FilePreview{}, fmt.Errorf("read workspace file: %w", err)
	}
	truncated := len(content) > maxFileBytes
	if truncated {
		content = content[:maxFileBytes]
	}
	binary := bytes.IndexByte(content, 0) >= 0 || !utf8.Valid(content)
	preview := FilePreview{Path: resolved, Size: info.Size(), Binary: binary, Truncated: truncated}
	if !binary {
		preview.Content = string(content)
	}
	return preview, nil
}

func readWorkspaceFile(root, path string) (string, bool, bool, error) {
	preview, err := PreviewFile(root, path)
	if err != nil {
		return "", false, false, err
	}
	return preview.Content, preview.Binary, preview.Truncated, nil
}

func isBinaryDiff(diff []byte) bool {
	return bytes.Contains(diff, []byte("GIT binary patch")) || bytes.Contains(diff, []byte("Binary files "))
}

func boundedUTF8(value []byte, limit int) (string, bool) {
	if len(value) <= limit {
		return string(value), false
	}
	value = value[:limit]
	for len(value) > 0 && !utf8.Valid(value) {
		value = value[:len(value)-1]
	}
	return string(value), true
}
