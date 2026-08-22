package remotessh

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"pi-desk/internal/remotehelper"
	"pi-desk/internal/remoteprotocol"
)

const (
	maxRuntimeTerminalReplayBytes = 1 << 20
	maxRuntimeTerminalInputBytes  = 64 << 10
)

var ErrRuntimeTerminalInvalid = errors.New("remote Terminal request is invalid")

type RuntimeTerminalEvent struct {
	Type     string
	Sequence uint64
	Data     []byte
	ExitCode int
	Error    error
}

type RuntimeTerminalSession struct {
	runtime   *installedHelperGeneration
	requestID string
	processID string
	events    chan RuntimeTerminalEvent
	done      chan struct{}
	closeOnce sync.Once

	mu       sync.Mutex
	replay   []byte
	sequence uint64
}

type runtimeTerminalGeneration interface {
	StartTerminal(context.Context, context.Context, string, int, int) (*RuntimeTerminalSession, error)
}

func (supervisor *RuntimeLeaseSupervisor) StartTerminal(ctx context.Context, lease *RuntimeLease, columns, rows int) (*RuntimeTerminalSession, error) {
	if columns < 20 || columns > 500 || rows < 5 || rows > 300 {
		return nil, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeTerminalInvalid)
	}
	fileGeneration, rootHandle, err := supervisor.authorizeMutationLease(lease)
	if err != nil {
		return nil, err
	}
	generation, ok := fileGeneration.(runtimeTerminalGeneration)
	if !ok {
		return nil, runtimeLifecycleError(ErrHelperProtocolMismatch)
	}
	supervisor.mu.Lock()
	if _, exists := supervisor.terminalLeases[lease.id]; exists {
		supervisor.mu.Unlock()
		return nil, runtimeResourceError(ErrHelperRuntimeLimit)
	}
	supervisor.terminalLeases[lease.id] = struct{}{}
	supervisor.mu.Unlock()
	session, err := generation.StartTerminal(ctx, lease.Context(), rootHandle, columns, rows)
	if err != nil {
		supervisor.revokeOutcomeUnknown(err)
		supervisor.mu.Lock()
		delete(supervisor.terminalLeases, lease.id)
		supervisor.mu.Unlock()
		return nil, err
	}
	go func() {
		<-session.Done()
		supervisor.mu.Lock()
		delete(supervisor.terminalLeases, lease.id)
		supervisor.mu.Unlock()
	}()
	return session, nil
}

func (session *RuntimeTerminalSession) ProcessID() string                   { return session.processID }
func (session *RuntimeTerminalSession) Events() <-chan RuntimeTerminalEvent { return session.events }
func (session *RuntimeTerminalSession) Done() <-chan struct{}               { return session.done }

func (session *RuntimeTerminalSession) Replay() (uint64, []byte) {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.sequence, append([]byte(nil), session.replay...)
}

func (session *RuntimeTerminalSession) Input(data []byte) error {
	if len(data) == 0 || len(data) > maxRuntimeTerminalInputBytes {
		return runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeTerminalInvalid)
	}
	select {
	case <-session.done:
		return runtimeLifecycleError(ErrHelperRuntimeStopping)
	default:
	}
	err := session.runtime.writer.Write(remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindEvent, ID: session.requestID,
		Generation: session.runtime.generation, Method: remoteprotocol.MethodTerminalInput,
	}, append([]byte(nil), data...))
	if err != nil {
		session.runtime.stopTransport(err)
		return runtimeOutcomeUnknownError()
	}
	return nil
}

func (session *RuntimeTerminalSession) Resize(columns, rows int) error {
	if columns < 20 || columns > 500 || rows < 5 || rows > 300 {
		return runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeTerminalInvalid)
	}
	payload, err := remoteprotocol.EncodePayload(remoteprotocol.TerminalResize{Columns: columns, Rows: rows})
	if err != nil {
		return err
	}
	if err := session.runtime.writer.Write(remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindEvent, ID: session.requestID,
		Generation: session.runtime.generation, Method: remoteprotocol.MethodTerminalResize, Payload: payload,
	}, nil); err != nil {
		session.runtime.stopTransport(err)
		return runtimeLifecycleError(ErrHelperRuntimeStopping)
	}
	return nil
}

func (session *RuntimeTerminalSession) Close() error {
	var result error
	session.closeOnce.Do(func() {
		result = session.runtime.writer.Write(remoteprotocol.Envelope{
			Version: remoteprotocol.Version, Kind: remoteprotocol.KindCancel,
			ID: session.requestID, Generation: session.runtime.generation,
		}, nil)
		if result != nil {
			session.runtime.stopTransport(result)
			result = runtimeOutcomeUnknownError()
		}
	})
	return result
}

func (runtime *installedHelperGeneration) StartTerminal(startupContext, lifetimeContext context.Context, rootHandle string, columns, rows int) (*RuntimeTerminalSession, error) {
	if startupContext == nil {
		startupContext = context.Background()
	}
	if lifetimeContext == nil {
		return nil, runtimeLifecycleError(ErrHelperRuntimeInvalidLease)
	}
	payload, err := remoteprotocol.EncodePayload(remotehelper.TerminalRunRequest{RootHandle: rootHandle, Columns: columns, Rows: rows})
	if err != nil {
		return nil, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeTerminalInvalid)
	}
	runtime.stateMu.Lock()
	if runtime.shutdown || runtime.transportErr != nil || len(runtime.pending) >= maxHelperPendingRequests {
		err := runtime.transportErr
		if err == nil {
			err = ErrHelperRuntimeStopping
		}
		runtime.stateMu.Unlock()
		return nil, runtimeLifecycleError(err)
	}
	runtime.nextRequest++
	requestID := fmt.Sprintf("terminal-%d", runtime.nextRequest)
	results := make(chan helperCallResult, 2)
	runtime.pending[requestID] = results
	runtime.stateMu.Unlock()
	if err := runtime.writer.Write(remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest, ID: requestID,
		Generation: runtime.generation, Method: remotehelper.MethodTerminalRun, Payload: payload,
	}, nil); err != nil {
		runtime.stopTransport(err)
		return nil, runtimeOutcomeUnknownError()
	}
	if err := runtime.sendStreamCredit(requestID, 32<<10); err != nil {
		runtime.stopTransport(err)
		return nil, runtimeOutcomeUnknownError()
	}
	var processID string
	select {
	case result := <-results:
		if result.err != nil {
			return nil, result.err
		}
		var accepted remoteprotocol.ProcessAccepted
		frame := result.frame
		if frame.Envelope.Kind == remoteprotocol.KindError && frame.Envelope.Error != nil && !frame.Envelope.Error.OutcomeUnknown {
			switch frame.Envelope.Error.Code {
			case "REMOTE_INVALID_REQUEST":
				return nil, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeTerminalInvalid)
			case "REMOTE_RESOURCE_LIMIT":
				return nil, runtimeResourceError(ErrHelperRuntimeLimit)
			case "REMOTE_TERMINAL_START_FAILED":
				return nil, runtimeFileError(FailureConnect, ReasonUnknown, ErrRuntimeTerminalInvalid)
			case "REMOTE_CANCELLED":
				return nil, lifecycleError(FailureCancelled, ReasonCancelled, context.Canceled)
			}
		}
		if frame.Envelope.Kind != remoteprotocol.KindEvent || frame.Envelope.Method != remoteprotocol.MethodProcessAccepted || len(frame.Blob) != 0 || remoteprotocol.DecodePayload(frame.Envelope.Payload, &accepted) != nil || !validRuntimeIdentity("process-", accepted.ProcessID) {
			_ = runtime.Kill()
			return nil, runtimeOutcomeUnknownError()
		}
		processID = accepted.ProcessID
	case <-startupContext.Done():
		_ = runtime.writer.Write(remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindCancel, ID: requestID, Generation: runtime.generation}, nil)
		go drainTerminalStartup(results, runtime.readDone)
		return nil, lifecycleError(FailureCancelled, ReasonCancelled, startupContext.Err())
	case <-runtime.readDone:
		return nil, runtime.transportFailure()
	}
	session := &RuntimeTerminalSession{
		runtime: runtime, requestID: requestID, processID: processID,
		events: make(chan RuntimeTerminalEvent, 32), done: make(chan struct{}),
	}
	go session.consume(lifetimeContext, results)
	return session, nil
}

func drainTerminalStartup(results <-chan helperCallResult, readDone <-chan struct{}) {
	for {
		select {
		case result := <-results:
			if result.err != nil || result.frame.Envelope.Kind == remoteprotocol.KindResponse || result.frame.Envelope.Kind == remoteprotocol.KindError {
				return
			}
		case <-readDone:
			return
		}
	}
}

func (session *RuntimeTerminalSession) consume(lifetime context.Context, results <-chan helperCallResult) {
	defer close(session.done)
	defer close(session.events)
	lifetimeDone := lifetime.Done()
	var outputBytes int64
	for {
		select {
		case result := <-results:
			if result.err != nil {
				session.emit(RuntimeTerminalEvent{Type: "disconnected", Error: runtimeOutcomeUnknownError()}, true)
				return
			}
			frame := result.frame
			switch frame.Envelope.Kind {
			case remoteprotocol.KindEvent:
				var event remoteprotocol.StreamData
				if frame.Envelope.Method != remoteprotocol.MethodStreamData || remoteprotocol.DecodePayload(frame.Envelope.Payload, &event) != nil || event.Stream != "terminal" || event.Sequence != session.sequence+1 || len(frame.Blob) == 0 || len(frame.Blob) > 32<<10 {
					_ = session.runtime.Kill()
					session.emit(RuntimeTerminalEvent{Type: "disconnected", Error: ErrHelperProtocolMismatch}, true)
					return
				}
				outputBytes += int64(len(frame.Blob))
				session.mu.Lock()
				session.sequence = event.Sequence
				session.replay = append(session.replay, frame.Blob...)
				if len(session.replay) > maxRuntimeTerminalReplayBytes {
					session.replay = append([]byte(nil), session.replay[len(session.replay)-maxRuntimeTerminalReplayBytes:]...)
				}
				session.mu.Unlock()
				session.emit(RuntimeTerminalEvent{Type: "output", Sequence: event.Sequence, Data: append([]byte(nil), frame.Blob...)}, false)
				if err := session.runtime.sendStreamCredit(session.requestID, uint32(len(frame.Blob))); err != nil {
					session.runtime.stopTransport(err)
					return
				}
			case remoteprotocol.KindResponse:
				var terminal remotehelper.TerminalRunResponse
				if len(frame.Blob) != 0 || remoteprotocol.DecodePayload(frame.Envelope.Payload, &terminal) != nil || terminal.OutputBytes != outputBytes {
					_ = session.runtime.Kill()
					session.emit(RuntimeTerminalEvent{Type: "disconnected", Error: ErrHelperProtocolMismatch}, true)
					return
				}
				session.emit(RuntimeTerminalEvent{Type: "exit", ExitCode: terminal.ExitCode, Sequence: session.sequence}, true)
				return
			case remoteprotocol.KindError:
				remoteError := frame.Envelope.Error
				if remoteError == nil || remoteError.OutcomeUnknown != (remoteError.Code == "REMOTE_OUTCOME_UNKNOWN") {
					_ = session.runtime.Kill()
					session.emit(RuntimeTerminalEvent{Type: "disconnected", Error: ErrHelperProtocolMismatch}, true)
					return
				}
				var terminalErr error
				switch remoteError.Code {
				case "REMOTE_CANCELLED":
					terminalErr = context.Canceled
				case "REMOTE_OUTCOME_UNKNOWN":
					_ = session.runtime.Kill()
					terminalErr = runtimeOutcomeUnknownError()
				case "REMOTE_TERMINAL_START_FAILED":
					terminalErr = ErrRuntimeTerminalInvalid
				default:
					_ = session.runtime.Kill()
					terminalErr = ErrHelperProtocolMismatch
				}
				session.emit(RuntimeTerminalEvent{Type: "exit", ExitCode: -1, Sequence: session.sequence, Error: terminalErr}, true)
				return
			}
		case <-lifetimeDone:
			lifetimeDone = nil
			_ = session.Close()
		case <-session.runtime.readDone:
			session.emit(RuntimeTerminalEvent{Type: "disconnected", Error: runtimeOutcomeUnknownError()}, true)
			return
		}
	}
}

func (session *RuntimeTerminalSession) emit(event RuntimeTerminalEvent, terminal bool) {
	select {
	case session.events <- event:
		return
	default:
	}
	if terminal {
		select {
		case <-session.events:
		default:
		}
		select {
		case session.events <- event:
		default:
		}
	}
}
