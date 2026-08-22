package remotessh

import (
	"context"
	"errors"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
)

const (
	maxRuntimeSearchResults       = 2000
	maxRuntimeSearchPatternBytes  = 4 << 10
	maxRuntimeSearchResponseBytes = 128 << 10
)

var ErrRuntimeSearchInvalid = errors.New("remote search request is invalid")

type RuntimeSearchFindRequest struct {
	Path    string
	Pattern string
	Limit   int
}

type RuntimeSearchFindResult struct {
	Paths                   []string `json:"paths"`
	SkippedUnsupportedPaths int      `json:"skippedUnsupportedPaths"`
	BudgetReached           bool     `json:"budgetReached"`
}

type RuntimeSearchGrepRequest struct {
	Path    string
	Pattern string
	Glob    string
	Limit   int
}

type RuntimeSearchGrepMatch struct {
	Path          string `json:"path"`
	Line          int    `json:"line"`
	Text          string `json:"text"`
	LineTruncated bool   `json:"lineTruncated"`
}

type RuntimeSearchGrepResult struct {
	Matches                 []RuntimeSearchGrepMatch `json:"matches"`
	SkippedUnsupportedPaths int                      `json:"skippedUnsupportedPaths"`
	BudgetReached           bool                     `json:"budgetReached"`
}

type runtimeSearchGeneration interface {
	FindFiles(context.Context, string, RuntimeSearchFindRequest) (RuntimeSearchFindResult, error)
	GrepFiles(context.Context, string, RuntimeSearchGrepRequest) (RuntimeSearchGrepResult, error)
}

func (supervisor *RuntimeLeaseSupervisor) FindFiles(ctx context.Context, lease *RuntimeLease, request RuntimeSearchFindRequest) (RuntimeSearchFindResult, error) {
	if !validRuntimeSearchPath(request.Path) || !validRuntimeSearchGlob(request.Pattern, false) || request.Limit < 1 || request.Limit > maxRuntimeSearchResults {
		return RuntimeSearchFindResult{}, runtimeSearchError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeSearchInvalid)
	}
	generation, rootHandle, err := supervisor.authorizeSearchLease(lease)
	if err != nil {
		return RuntimeSearchFindResult{}, err
	}
	response, err := generation.FindFiles(ctx, rootHandle, request)
	if err != nil {
		return RuntimeSearchFindResult{}, err
	}
	if !validRuntimeFindResult(response, request.Path, request.Limit) {
		supervisor.Disconnect(context.Background())
		return RuntimeSearchFindResult{}, runtimeLifecycleError(ErrHelperProtocolMismatch)
	}
	return response, nil
}

func (supervisor *RuntimeLeaseSupervisor) GrepFiles(ctx context.Context, lease *RuntimeLease, request RuntimeSearchGrepRequest) (RuntimeSearchGrepResult, error) {
	if !validRuntimeSearchPath(request.Path) || !validRuntimeSearchText(request.Pattern) || request.Limit < 1 || request.Limit > maxRuntimeSearchResults || request.Glob != "" && !validRuntimeSearchGlob(request.Glob, true) {
		return RuntimeSearchGrepResult{}, runtimeSearchError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeSearchInvalid)
	}
	if _, err := regexp.Compile(request.Pattern); err != nil {
		return RuntimeSearchGrepResult{}, runtimeSearchError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeSearchInvalid)
	}
	generation, rootHandle, err := supervisor.authorizeSearchLease(lease)
	if err != nil {
		return RuntimeSearchGrepResult{}, err
	}
	response, err := generation.GrepFiles(ctx, rootHandle, request)
	if err != nil {
		return RuntimeSearchGrepResult{}, err
	}
	if !validRuntimeGrepResult(response, request.Path, request.Limit) {
		supervisor.Disconnect(context.Background())
		return RuntimeSearchGrepResult{}, runtimeLifecycleError(ErrHelperProtocolMismatch)
	}
	return response, nil
}

func (supervisor *RuntimeLeaseSupervisor) authorizeSearchLease(lease *RuntimeLease) (runtimeSearchGeneration, string, error) {
	generation, rootHandle, err := supervisor.authorizeFileLease(lease)
	if err != nil {
		return nil, "", err
	}
	search, ok := generation.(runtimeSearchGeneration)
	if !ok {
		return nil, "", runtimeLifecycleError(ErrHelperProtocolMismatch)
	}
	return search, rootHandle, nil
}

func validRuntimeFindResult(result RuntimeSearchFindResult, rootPath string, limit int) bool {
	if result.SkippedUnsupportedPaths < 0 || len(result.Paths) > limit || len(result.Paths) > maxRuntimeSearchResults || !slices.IsSorted(result.Paths) {
		return false
	}
	seen := make(map[string]struct{}, len(result.Paths))
	budget := 0
	for _, candidate := range result.Paths {
		if !validRemoteRelativePath(candidate, false) || !runtimeSearchWithin(rootPath, candidate) {
			return false
		}
		if _, duplicate := seen[candidate]; duplicate {
			return false
		}
		seen[candidate] = struct{}{}
		budget += len(candidate) + 3
	}
	return budget <= maxRuntimeSearchResponseBytes
}

func validRuntimeGrepResult(result RuntimeSearchGrepResult, rootPath string, limit int) bool {
	if result.SkippedUnsupportedPaths < 0 || len(result.Matches) > limit || len(result.Matches) > maxRuntimeSearchResults {
		return false
	}
	budget := 0
	for index, match := range result.Matches {
		if !validRemoteRelativePath(match.Path, false) || !runtimeSearchWithin(rootPath, match.Path) || match.Line < 1 || len(match.Text) > 16<<10 || !utf8.ValidString(match.Text) {
			return false
		}
		if index > 0 {
			previous := result.Matches[index-1]
			if previous.Path > match.Path || previous.Path == match.Path && previous.Line >= match.Line {
				return false
			}
		}
		budget += len(match.Path) + len(match.Text) + 32
	}
	return budget <= maxRuntimeSearchResponseBytes
}

func validRuntimeSearchPath(value string) bool {
	return validRemoteRelativePath(value, true)
}

func validRuntimeSearchText(value string) bool {
	if value == "" || len(value) > maxRuntimeSearchPatternBytes || !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if char == 0 || unicode.Is(unicode.Cf, char) || unicode.IsControl(char) && char != '\t' {
			return false
		}
	}
	return true
}

func validRuntimeSearchGlob(value string, allowEmpty bool) bool {
	if value == "" {
		return allowEmpty
	}
	if !validRuntimeSearchText(value) || strings.Contains(value, "\\") || path.IsAbs(value) || !doublestar.ValidatePattern(value) {
		return false
	}
	for _, component := range strings.Split(value, "/") {
		if component == ".." {
			return false
		}
	}
	return true
}

func runtimeSearchWithin(rootPath, candidate string) bool {
	return rootPath == "." || strings.HasPrefix(candidate, rootPath+"/")
}

func runtimeSearchError(code FailureCode, reason FailureReason, cause error) error {
	return runtimeFileError(code, reason, cause)
}
