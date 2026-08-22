package remotessh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
)

const maxRuntimeGitBlobBytes = 8 << 20

var (
	ErrRuntimeGitInvalid     = errors.New("remote Git request is invalid")
	ErrRuntimeGitUnavailable = errors.New("remote Git is unavailable")
	ErrRuntimeGitUnsafe      = errors.New("remote Git configuration is unsafe")
)

type RuntimeGitReadRequest struct {
	Operation string
	Path      string
}

type RuntimeGitOutputPart struct {
	Name   string
	Offset int64
	Size   int64
	SHA256 string
}

type RuntimeGitReadResult struct {
	Operation string
	Parts     []RuntimeGitOutputPart
	Blob      []byte
}

type runtimeGitGeneration interface {
	ReadGit(context.Context, string, RuntimeGitReadRequest) (RuntimeGitReadResult, error)
}

func (supervisor *RuntimeLeaseSupervisor) ReadGit(ctx context.Context, lease *RuntimeLease, request RuntimeGitReadRequest) (RuntimeGitReadResult, error) {
	if !validRuntimeGitRequest(request) {
		return RuntimeGitReadResult{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeGitInvalid)
	}
	fileGeneration, rootHandle, err := supervisor.authorizeFileLease(lease)
	if err != nil {
		return RuntimeGitReadResult{}, err
	}
	generation, ok := fileGeneration.(runtimeGitGeneration)
	if !ok {
		return RuntimeGitReadResult{}, runtimeLifecycleError(ErrHelperProtocolMismatch)
	}
	response, err := generation.ReadGit(ctx, rootHandle, request)
	if err != nil {
		return RuntimeGitReadResult{}, err
	}
	if !validRuntimeGitResult(response, request.Operation) {
		supervisor.Disconnect(context.Background())
		return RuntimeGitReadResult{}, runtimeLifecycleError(ErrHelperProtocolMismatch)
	}
	return response, nil
}

func validRuntimeGitRequest(request RuntimeGitReadRequest) bool {
	switch request.Operation {
	case "status", "files", "branches":
		return request.Path == ""
	case "diff":
		return validRemoteRelativePath(request.Path, false)
	default:
		return false
	}
}

func validRuntimeGitResult(result RuntimeGitReadResult, operation string) bool {
	if result.Operation != operation || len(result.Blob) > maxRuntimeGitBlobBytes {
		return false
	}
	expected := map[string][]string{
		"status": {"status"}, "files": {"files"}, "diff": {"staged", "working"}, "branches": {"worktrees", "refs"},
	}[operation]
	if !slices.Equal(partNames(result.Parts), expected) {
		return false
	}
	offset := int64(0)
	for _, part := range result.Parts {
		if part.Offset != offset || part.Size < 0 || part.Size > int64(len(result.Blob))-offset || !validLowerHex(part.SHA256, 64) {
			return false
		}
		end := offset + part.Size
		digest := sha256.Sum256(result.Blob[offset:end])
		if hex.EncodeToString(digest[:]) != part.SHA256 {
			return false
		}
		offset = end
	}
	return offset == int64(len(result.Blob))
}

func partNames(parts []RuntimeGitOutputPart) []string {
	names := make([]string, len(parts))
	for index, part := range parts {
		names[index] = part.Name
	}
	return names
}
