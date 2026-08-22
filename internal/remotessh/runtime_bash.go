package remotessh

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	maxRuntimeBashCommandBytes = 64 << 10
	maxRuntimeBashOutputBytes  = 16 << 20
)

var (
	ErrRuntimeBashInvalid = errors.New("remote Bash request is invalid")
	ErrRuntimeBashStart   = errors.New("remote Bash process could not start")
)

type RuntimeBashResult struct {
	ProcessID       string `json:"processId"`
	ExitCode        int    `json:"exitCode"`
	Output          string `json:"output"`
	OutputBytes     int64  `json:"outputBytes"`
	OutputTruncated bool   `json:"outputTruncated"`
}

type runtimeBashGeneration interface {
	RunBash(context.Context, string, string) (RuntimeBashResult, error)
}

func (supervisor *RuntimeLeaseSupervisor) RunBash(ctx context.Context, lease *RuntimeLease, command string) (RuntimeBashResult, error) {
	if command == "" || len(command) > maxRuntimeBashCommandBytes || !utf8.ValidString(command) || strings.ContainsRune(command, 0) {
		return RuntimeBashResult{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeBashInvalid)
	}
	fileGeneration, rootHandle, err := supervisor.authorizeMutationLease(lease)
	if err != nil {
		return RuntimeBashResult{}, err
	}
	generation, ok := fileGeneration.(runtimeBashGeneration)
	if !ok {
		return RuntimeBashResult{}, runtimeLifecycleError(ErrHelperProtocolMismatch)
	}
	result, err := generation.RunBash(ctx, rootHandle, command)
	if err != nil {
		supervisor.revokeOutcomeUnknown(err)
		return RuntimeBashResult{}, err
	}
	if !validRuntimeIdentity("process-", result.ProcessID) || result.OutputBytes < 0 || result.OutputBytes > maxRuntimeBashOutputBytes {
		supervisor.Disconnect(context.Background())
		return RuntimeBashResult{}, runtimeLifecycleError(ErrHelperProtocolMismatch)
	}
	return result, nil
}
