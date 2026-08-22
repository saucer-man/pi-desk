package remotehelper

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
)

const (
	MethodSearchFind = "search.find"
	MethodSearchGrep = "search.grep"

	maxSearchCandidates    = 100_000
	maxSearchResults       = 2_000
	maxSearchPatternBytes  = 4 << 10
	maxSearchResponseBytes = 128 << 10
	maxSearchReadBytes     = int64(1 << 30)
	defaultSearchTimeout   = 30 * time.Second
	maxSearchTimeout       = 120 * time.Second
	maxGitCandidateBytes   = 16 << 20
)

var (
	ErrSearchInvalid        = errors.New("remote search request is invalid")
	ErrSearchGitUnavailable = errors.New("remote Git candidate enumeration is unavailable")
)

type SearchFindRequest struct {
	RootHandle string `json:"rootHandle"`
	Path       string `json:"path"`
	Pattern    string `json:"pattern"`
	Limit      int    `json:"limit"`
}

type SearchFindResponse struct {
	Paths                   []string `json:"paths"`
	SkippedUnsupportedPaths int      `json:"skippedUnsupportedPaths"`
	BudgetReached           bool     `json:"budgetReached"`
}

type SearchGrepRequest struct {
	RootHandle string `json:"rootHandle"`
	Path       string `json:"path"`
	Pattern    string `json:"pattern"`
	Glob       string `json:"glob,omitempty"`
	Limit      int    `json:"limit"`
}

type SearchGrepMatch struct {
	Path          string `json:"path"`
	Line          int    `json:"line"`
	Text          string `json:"text"`
	LineTruncated bool   `json:"lineTruncated"`
}

type SearchGrepResponse struct {
	Matches                 []SearchGrepMatch `json:"matches"`
	SkippedUnsupportedPaths int               `json:"skippedUnsupportedPaths"`
	BudgetReached           bool              `json:"budgetReached"`
}

func (manager *rootManager) Find(ctx context.Context, request SearchFindRequest) (SearchFindResponse, error) {
	capability, err := manager.searchCapability(request.RootHandle, request.Path)
	if err != nil || !validSearchGlob(request.Pattern, false) || request.Limit < 1 || request.Limit > maxSearchResults {
		return SearchFindResponse{}, ErrSearchInvalid
	}
	ctx, cancel := searchContext(ctx)
	defer cancel()
	response := SearchFindResponse{Paths: make([]string, 0, min(request.Limit, 128))}
	err = enumerateSearchCandidates(ctx, capability, request.Path, func(candidate string) error {
		relative := searchRelativePath(request.Path, candidate)
		matchName := relative
		if !strings.Contains(request.Pattern, "/") {
			matchName = path.Base(relative)
		}
		if doublestar.MatchUnvalidated(request.Pattern, matchName) {
			projected := len(candidate) + 3
			if len(response.Paths) >= request.Limit || searchPathBytes(response.Paths)+projected > maxSearchResponseBytes {
				response.BudgetReached = true
				return errSearchBudget
			}
			response.Paths = append(response.Paths, candidate)
		}
		return nil
	}, &response.SkippedUnsupportedPaths)
	if errors.Is(err, errSearchBudget) {
		response.BudgetReached = true
		err = nil
	}
	if err != nil {
		return SearchFindResponse{}, err
	}
	slices.Sort(response.Paths)
	return response, nil
}

func (manager *rootManager) Grep(ctx context.Context, request SearchGrepRequest) (SearchGrepResponse, error) {
	capability, err := manager.searchCapability(request.RootHandle, request.Path)
	if err != nil || !validSearchText(request.Pattern) || request.Limit < 1 || request.Limit > maxSearchResults || request.Glob != "" && !validSearchGlob(request.Glob, true) {
		return SearchGrepResponse{}, ErrSearchInvalid
	}
	expression, err := regexp.Compile(request.Pattern)
	if err != nil {
		return SearchGrepResponse{}, ErrSearchInvalid
	}
	ctx, cancel := searchContext(ctx)
	defer cancel()
	response := SearchGrepResponse{Matches: make([]SearchGrepMatch, 0, min(request.Limit, 128))}
	var totalRead int64
	responseBytes := 0
	err = enumerateSearchCandidates(ctx, capability, request.Path, func(candidate string) error {
		relative := searchRelativePath(request.Path, candidate)
		if request.Glob != "" {
			matchName := relative
			if !strings.Contains(request.Glob, "/") {
				matchName = path.Base(relative)
			}
			if !doublestar.MatchUnvalidated(request.Glob, matchName) {
				return nil
			}
		}
		file, err := openRootRead(capability.root, candidate)
		if err != nil {
			response.SkippedUnsupportedPaths++
			return nil
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxReadableFileBytes {
			response.SkippedUnsupportedPaths++
			return nil
		}
		if totalRead+info.Size() > maxSearchReadBytes {
			response.BudgetReached = true
			return errSearchBudget
		}
		totalRead += info.Size()
		start := len(response.Matches)
		startBytes := responseBytes
		scanner := bufio.NewScanner(io.LimitReader(&contextReader{ctx: ctx, reader: file}, maxReadableFileBytes+1))
		scanner.Buffer(make([]byte, 64<<10), maxReadLineBytes+1)
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			line := scanner.Bytes()
			if !utf8.Valid(line) || bytes.IndexByte(line, 0) >= 0 {
				response.Matches = response.Matches[:start]
				responseBytes = startBytes
				response.SkippedUnsupportedPaths++
				return nil
			}
			if !expression.Match(line) {
				continue
			}
			text := string(line)
			truncated := false
			const maxProjectedLine = 16 << 10
			if len(text) > maxProjectedLine {
				text = truncateUTF8(text, maxProjectedLine)
				truncated = true
			}
			projected := len(candidate) + len(text) + 32
			if len(response.Matches) >= request.Limit || responseBytes+projected > maxSearchResponseBytes {
				response.BudgetReached = true
				return errSearchBudget
			}
			response.Matches = append(response.Matches, SearchGrepMatch{Path: candidate, Line: lineNumber, Text: text, LineTruncated: truncated})
			responseBytes += projected
		}
		if err := scanner.Err(); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			response.Matches = response.Matches[:start]
			responseBytes = startBytes
			response.SkippedUnsupportedPaths++
		}
		return nil
	}, &response.SkippedUnsupportedPaths)
	if errors.Is(err, errSearchBudget) {
		response.BudgetReached = true
		err = nil
	}
	if err != nil {
		return SearchGrepResponse{}, err
	}
	slices.SortFunc(response.Matches, func(left, right SearchGrepMatch) int {
		if result := strings.Compare(left.Path, right.Path); result != 0 {
			return result
		}
		return left.Line - right.Line
	})
	return response, nil
}

var errSearchBudget = errors.New("remote search budget reached")

func (manager *rootManager) searchCapability(handle, logicalPath string) (*rootCapability, error) {
	if validateRelativePath(logicalPath, true) != nil {
		return nil, ErrSearchInvalid
	}
	capability, err := manager.lookup(handle)
	if err != nil {
		return nil, ErrSearchInvalid
	}
	info, err := capability.root.Stat(logicalPath)
	if err != nil || !info.IsDir() {
		return nil, ErrSearchInvalid
	}
	return capability, nil
}

func searchContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := defaultSearchTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		} else if remaining > maxSearchTimeout {
			timeout = maxSearchTimeout
		}
	}
	return context.WithTimeout(ctx, timeout)
}

func validSearchText(value string) bool {
	if value == "" || len(value) > maxSearchPatternBytes || !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if char == 0 || unicode.Is(unicode.Cf, char) || unicode.IsControl(char) && char != '\t' {
			return false
		}
	}
	return true
}

func validSearchGlob(value string, allowEmpty bool) bool {
	if value == "" {
		return allowEmpty
	}
	if !validSearchText(value) || strings.Contains(value, "\\") || path.IsAbs(value) || !doublestar.ValidatePattern(value) {
		return false
	}
	for _, component := range strings.Split(value, "/") {
		if component == ".." {
			return false
		}
	}
	return true
}

func searchRelativePath(rootPath, candidate string) string {
	if rootPath == "." {
		return candidate
	}
	return strings.TrimPrefix(candidate, rootPath+"/")
}

func searchPathBytes(paths []string) int {
	total := 0
	for _, value := range paths {
		total += len(value) + 3
	}
	return total
}

func enumerateSearchCandidates(ctx context.Context, capability *rootCapability, searchPath string, visit func(string) error, skipped *int) error {
	gitMarker, gitErr := capability.root.Lstat(".git")
	if gitErr == nil && (gitMarker.IsDir() || gitMarker.Mode().IsRegular()) {
		candidates, unsupported, err := gitSearchCandidates(ctx, capability)
		*skipped += unsupported
		if err != nil {
			return err
		}
		for index, candidate := range candidates {
			if index >= maxSearchCandidates {
				return errSearchBudget
			}
			if !searchCandidateWithin(searchPath, candidate) {
				continue
			}
			info, statErr := capability.root.Lstat(candidate)
			if statErr != nil || !info.Mode().IsRegular() {
				*skipped++
				continue
			}
			if err := visit(candidate); err != nil {
				return err
			}
		}
		return nil
	}
	seen := 0
	return fs.WalkDir(capability.root.FS(), searchPath, func(candidate string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			*skipped++
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if candidate == searchPath {
			return nil
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "node_modules") {
			return fs.SkipDir
		}
		if validateRelativePath(candidate, false) != nil {
			*skipped++
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			*skipped++
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			*skipped++
			return nil
		}
		seen++
		if seen > maxSearchCandidates {
			return errSearchBudget
		}
		return visit(candidate)
	})
}

func searchCandidateWithin(searchPath, candidate string) bool {
	return searchPath == "." || strings.HasPrefix(candidate, searchPath+"/")
}

func gitSearchCandidates(ctx context.Context, capability *rootCapability) ([]string, int, error) {
	const gitExecutable = "/usr/bin/git"
	if info, err := os.Stat(gitExecutable); err != nil || !info.Mode().IsRegular() {
		return nil, 0, ErrSearchGitUnavailable
	}
	baseArgs := []string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "credential.helper=", "-c", "core.pager=cat"}
	top, err := runSearchGit(ctx, capability.canonical, gitExecutable, append(baseArgs, "rev-parse", "--show-toplevel"), 4096)
	if err != nil || strings.TrimSpace(string(top)) != capability.canonical {
		return nil, 0, ErrSearchGitUnavailable
	}
	output, err := runSearchGit(ctx, capability.canonical, gitExecutable, append(baseArgs, "ls-files", "-co", "--exclude-standard", "-z"), maxGitCandidateBytes)
	if err != nil {
		return nil, 0, ErrSearchGitUnavailable
	}
	parts := bytes.Split(output, []byte{0})
	candidates := make([]string, 0, min(len(parts), maxSearchCandidates))
	skipped := 0
	for _, raw := range parts {
		if len(raw) == 0 {
			continue
		}
		candidate := string(raw)
		if validateRelativePath(candidate, false) != nil {
			skipped++
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates, skipped, nil
}

func runSearchGit(ctx context.Context, directory, executable string, args []string, limit int64) ([]byte, error) {
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = directory
	command.Env = []string{
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0", "HOME=/nonexistent", "XDG_CONFIG_HOME=/nonexistent",
		"LC_ALL=C", "LANG=C",
	}
	command.Stdin = nil
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	var output limitedSearchBuffer
	output.limit = limit
	command.Stdout = &output
	if err := command.Run(); err != nil || output.exceeded {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrSearchGitUnavailable
	}
	return output.Bytes(), nil
}

type limitedSearchBuffer struct {
	bytes.Buffer
	limit    int64
	exceeded bool
}

func (buffer *limitedSearchBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := buffer.limit - int64(buffer.Len())
	if remaining <= 0 {
		buffer.exceeded = true
		return original, nil
	}
	if int64(len(value)) > remaining {
		value = value[:remaining]
		buffer.exceeded = true
	}
	_, _ = buffer.Buffer.Write(value)
	return original, nil
}
