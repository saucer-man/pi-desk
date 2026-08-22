package remotessh

import (
	"context"
	"errors"
	"fmt"
)

var ErrSSHConnectionProbe = errors.New("OpenSSH connection probe failed")

// ConnectionPreflight binds the effective OpenSSH configuration to the actual
// host key observed during a real strict, noninteractive connection.
type ConnectionPreflight struct {
	Config  EffectiveConfig
	HostKey HostKeyEvidence
}

// ConnectionProbeError exposes only stable, redacted classification. Its cause
// contains process status or a parser error, never the captured SSH stderr.
type ConnectionProbeError struct {
	Failure ConnectionFailure
	cause   error
}

func (err *ConnectionProbeError) Error() string {
	return fmt.Sprintf("%s: %s", err.Failure.Code, err.Failure.Reason)
}

func (err *ConnectionProbeError) Unwrap() error {
	return err.cause
}

// ProbeConnection runs effective-config validation before making a real SSH
// connection. The remote command is fixed to POSIX true and cannot be supplied
// by the caller. Raw debug output remains in bounded process memory only.
func (locator *Locator) ProbeConnection(ctx context.Context, hostAlias string) (ConnectionPreflight, error) {
	config, err := locator.PreflightConfig(ctx, hostAlias)
	if err != nil {
		if failure, ok := connectionContextFailure(ctx); ok {
			return ConnectionPreflight{}, &ConnectionProbeError{Failure: failure, cause: fmt.Errorf("%w: %w", ErrSSHConnectionProbe, err)}
		}
		return ConnectionPreflight{}, err
	}
	invocation, err := locator.connectionProbeInvocation(hostAlias)
	if err != nil {
		return ConnectionPreflight{}, err
	}
	output, runErr := locator.runner.Run(ctx, invocation.Executable, invocation.Args...)
	if runErr != nil {
		failure := ClassifyOpenSSHFailure(output.Stderr)
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			failure = ConnectionFailure{Code: FailureCancelled, Reason: ReasonCancelled}
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			failure = ConnectionFailure{Code: FailureConnect, Reason: ReasonConnectionTimeout}
		case errors.Is(runErr, ErrProbeOutputTooLarge):
			failure = ConnectionFailure{Code: FailureOutputLimit, Reason: ReasonOutputLimit}
		}
		return ConnectionPreflight{}, &ConnectionProbeError{Failure: failure, cause: fmt.Errorf("%w: %w", ErrSSHConnectionProbe, runErr)}
	}
	if len(output.Stdout) != 0 {
		return ConnectionPreflight{}, &ConnectionProbeError{
			Failure: ConnectionFailure{Code: FailureConnect, Reason: ReasonHostOutput},
			cause:   ErrSSHConnectionProbe,
		}
	}
	evidence, err := ParseHostKeyEvidence(output.Stderr)
	if err != nil {
		return ConnectionPreflight{}, &ConnectionProbeError{
			Failure: ConnectionFailure{Code: FailureConnect, Reason: ReasonHostKeyEvidence},
			cause:   fmt.Errorf("%w: %w", ErrSSHConnectionProbe, err),
		}
	}
	return ConnectionPreflight{Config: config, HostKey: evidence}, nil
}

func connectionContextFailure(ctx context.Context) (ConnectionFailure, bool) {
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return ConnectionFailure{Code: FailureCancelled, Reason: ReasonCancelled}, true
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return ConnectionFailure{Code: FailureConnect, Reason: ReasonConnectionTimeout}, true
	default:
		return ConnectionFailure{}, false
	}
}
