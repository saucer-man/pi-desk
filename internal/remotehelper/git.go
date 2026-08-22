package remotehelper

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	MethodGitRead       = "git.read"
	maxGitOutputBytes   = 4 << 20
	maxGitDiffPartBytes = 1 << 20
	maxGitConfigBytes   = 64 << 10
	maxGitFilters       = 128
)

var (
	ErrGitInvalid     = errors.New("remote Git request is invalid")
	ErrGitUnavailable = errors.New("remote Git is unavailable")
	ErrGitUnsafe      = errors.New("remote Git configuration is unsafe")
	ErrGitOutputLimit = errors.New("remote Git output exceeds the safety limit")
)

type GitReadRequest struct {
	RootHandle string `json:"rootHandle"`
	Operation  string `json:"operation"`
	Path       string `json:"path,omitempty"`
}

type GitOutputPart struct {
	Name   string `json:"name"`
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type GitReadResponse struct {
	Operation string          `json:"operation"`
	Parts     []GitOutputPart `json:"parts"`
}

func (manager *rootManager) Git(ctx context.Context, request GitReadRequest) (GitReadResponse, []byte, error) {
	if !validRootHandle(request.RootHandle) {
		return GitReadResponse{}, nil, ErrGitInvalid
	}
	switch request.Operation {
	case "status", "files", "branches":
		if request.Path != "" {
			return GitReadResponse{}, nil, ErrGitInvalid
		}
	case "diff":
		if validateRelativePath(request.Path, false) != nil {
			return GitReadResponse{}, nil, ErrGitInvalid
		}
	default:
		return GitReadResponse{}, nil, ErrGitInvalid
	}
	capability, err := manager.lookup(request.RootHandle)
	if err != nil {
		return GitReadResponse{}, nil, ErrGitInvalid
	}
	// ponytail: serialize Git reads per helper; add a small process semaphore only
	// if concurrent Repository refresh becomes measurably slow.
	manager.gitMu.Lock()
	defer manager.gitMu.Unlock()
	if err := ctx.Err(); err != nil {
		return GitReadResponse{}, nil, err
	}
	base, err := safeGitBaseArgs(ctx, capability)
	if err != nil {
		return GitReadResponse{}, nil, err
	}
	var outputs []namedGitOutput
	switch request.Operation {
	case "status":
		value, err := runBoundedGit(ctx, capability.canonical, append(base, "status", "--porcelain=v1", "-z", "--branch", "--untracked-files=all"), maxGitOutputBytes)
		if err != nil {
			return GitReadResponse{}, nil, err
		}
		outputs = []namedGitOutput{{name: "status", value: value}}
	case "files":
		value, err := runBoundedGit(ctx, capability.canonical, append(base, "ls-files", "-co", "--exclude-standard", "-z"), maxGitOutputBytes)
		if err != nil {
			return GitReadResponse{}, nil, err
		}
		outputs = []namedGitOutput{{name: "files", value: value}}
	case "diff":
		pathspec := ":(top,literal)" + request.Path
		staged, err := runBoundedGit(ctx, capability.canonical, append(base, "diff", "--cached", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--", pathspec), maxGitDiffPartBytes)
		if err != nil {
			return GitReadResponse{}, nil, err
		}
		working, err := runBoundedGit(ctx, capability.canonical, append(base, "diff", "--no-ext-diff", "--no-textconv", "--no-color", "--unified=3", "--", pathspec), maxGitDiffPartBytes)
		if err != nil {
			return GitReadResponse{}, nil, err
		}
		outputs = []namedGitOutput{{name: "staged", value: staged}, {name: "working", value: working}}
	case "branches":
		worktrees, err := runBoundedGit(ctx, capability.canonical, append(base, "worktree", "list", "--porcelain", "-z"), maxGitOutputBytes)
		if err != nil {
			return GitReadResponse{}, nil, err
		}
		format := "%(refname)%09%(refname:short)%09%(HEAD)%09%(upstream:short)%09%(objectname:short)%09%(symref)"
		refs, err := runBoundedGit(ctx, capability.canonical, append(base, "for-each-ref", "--format="+format, "refs/heads", "refs/remotes"), maxGitOutputBytes)
		if err != nil {
			return GitReadResponse{}, nil, err
		}
		outputs = []namedGitOutput{{name: "worktrees", value: worktrees}, {name: "refs", value: refs}}
	}
	if !sameCapabilityRoot(capability) {
		return GitReadResponse{}, nil, ErrGitUnsafe
	}
	return packGitOutputs(request.Operation, outputs)
}

type namedGitOutput struct {
	name  string
	value []byte
}

func packGitOutputs(operation string, outputs []namedGitOutput) (GitReadResponse, []byte, error) {
	response := GitReadResponse{Operation: operation, Parts: make([]GitOutputPart, 0, len(outputs))}
	var blob []byte
	for _, output := range outputs {
		if len(blob)+len(output.value) > maxGitOutputBytes*2 {
			return GitReadResponse{}, nil, ErrGitOutputLimit
		}
		digest := sha256.Sum256(output.value)
		response.Parts = append(response.Parts, GitOutputPart{
			Name: output.name, Offset: int64(len(blob)), Size: int64(len(output.value)), SHA256: hex.EncodeToString(digest[:]),
		})
		blob = append(blob, output.value...)
	}
	return response, blob, nil
}

func sameCapabilityRoot(capability *rootCapability) bool {
	current, err := os.Stat(capability.canonical)
	return err == nil && os.SameFile(capability.info, current)
}

func safeGitBaseArgs(ctx context.Context, capability *rootCapability) ([]string, error) {
	const gitExecutable = "/usr/bin/git"
	if info, err := os.Stat(gitExecutable); err != nil || !info.Mode().IsRegular() {
		return nil, ErrGitUnavailable
	}
	if !sameCapabilityRoot(capability) {
		return nil, ErrGitUnsafe
	}
	versionOutput, err := runBoundedGitExecutable(ctx, capability.canonical, gitExecutable, []string{"--version"}, 4096)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrGitUnavailable
	}
	if !supportedGitVersion(string(versionOutput)) {
		return nil, ErrGitUnavailable
	}
	base := []string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "credential.helper=", "-c", "core.pager=cat", "-c", "pager.status=false", "-c", "pager.diff=false", "-c", "diff.external="}
	top, err := runBoundedGitExecutable(ctx, capability.canonical, gitExecutable, append(append([]string{}, base...), "rev-parse", "--show-toplevel"), 4096)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrGitUnavailable
	}
	if strings.TrimSpace(string(top)) != capability.canonical {
		return nil, ErrGitUnavailable
	}
	filterKeys, err := gitConfigFilterKeys(ctx, capability.canonical, gitExecutable, base)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrGitUnsafe
	}
	drivers := make(map[string]struct{})
	for _, line := range strings.Split(strings.TrimSpace(string(filterKeys)), "\n") {
		if line == "" {
			continue
		}
		match := gitFilterKey.FindStringSubmatch(line)
		if len(match) != 2 || len(drivers) >= maxGitFilters {
			return nil, ErrGitUnsafe
		}
		drivers[match[1]] = struct{}{}
	}
	for driver := range drivers {
		base = append(base,
			"-c", "filter."+driver+".clean=/bin/cat",
			"-c", "filter."+driver+".smudge=/bin/cat",
			"-c", "filter."+driver+".process=",
			"-c", "filter."+driver+".required=false",
		)
	}
	return base, nil
}

var gitFilterKey = regexp.MustCompile(`^filter\.([A-Za-z0-9_-]{1,128})\.(?:clean|smudge|process|required)$`)

func gitConfigFilterKeys(ctx context.Context, directory, executable string, base []string) ([]byte, error) {
	args := append(append([]string{}, base...), "config", "--local", "--name-only", "--get-regexp", `^filter\..*\.(clean|smudge|process|required)$`)
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = directory
	command.Env = safeGitEnvironment()
	command.Stdin = nil
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	var output limitedSearchBuffer
	output.limit = maxGitConfigBytes
	command.Stdout = &output
	err := command.Run()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && exitError.ExitCode() == 1 && !output.exceeded {
			return nil, nil
		}
		return nil, err
	}
	if output.exceeded {
		return nil, ErrGitOutputLimit
	}
	return bytes.Clone(output.Bytes()), nil
}

func supportedGitVersion(output string) bool {
	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) < 3 || fields[0] != "git" || fields[1] != "version" {
		return false
	}
	parts := strings.Split(fields[2], ".")
	if len(parts) < 2 {
		return false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	return majorErr == nil && minorErr == nil && (major > 2 || major == 2 && minor >= 31)
}

func runBoundedGit(ctx context.Context, directory string, args []string, limit int64) ([]byte, error) {
	return runBoundedGitExecutable(ctx, directory, "/usr/bin/git", args, limit)
}

func runBoundedGitExecutable(ctx context.Context, directory, executable string, args []string, limit int64) ([]byte, error) {
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir = directory
	command.Env = safeGitEnvironment()
	command.Stdin = nil
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	var output limitedSearchBuffer
	output.limit = limit
	command.Stdout = &output
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return output.Bytes(), ErrGitUnavailable
	}
	if output.exceeded {
		return nil, ErrGitOutputLimit
	}
	return bytes.Clone(output.Bytes()), nil
}

func safeGitEnvironment() []string {
	return []string{
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/bin/false",
		"GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat", "PAGER=cat", "HOME=/nonexistent", "XDG_CONFIG_HOME=/nonexistent",
		"LC_ALL=C", "LANG=C",
	}
}
