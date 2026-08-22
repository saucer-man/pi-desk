package remotessh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"pi-desk/internal/processutil"
	"pi-desk/internal/remotehelper"
	"pi-desk/internal/remoteprotocol"
)

const maxHelperPendingRequests = 64

// InstalledHelperGenerationFactory starts only the exact manifest-selected
// helper already installed by HelperInstaller. It does not install, reconnect,
// open a workspace root, or accept a remote command from its caller.
type InstalledHelperGenerationFactory struct {
	installer *HelperInstaller
	artifact  HelperArtifact
}

func NewInstalledHelperGenerationFactory(installer *HelperInstaller, artifact HelperArtifact) (*InstalledHelperGenerationFactory, error) {
	if installer == nil || installer.locator == nil || installer.supervisor == nil {
		return nil, errors.New("a locator-backed helper installer is required")
	}
	if err := artifact.Validate(); err != nil {
		return nil, err
	}
	return &InstalledHelperGenerationFactory{installer: installer, artifact: artifact}, nil
}

func (factory *InstalledHelperGenerationFactory) Start(startupContext, lifetimeContext context.Context, generation uint64) (HelperGeneration, error) {
	if startupContext == nil {
		startupContext = context.Background()
	}
	if lifetimeContext == nil || generation == 0 {
		return nil, runtimeLifecycleError(ErrHelperRuntimeInvalidLease)
	}
	if err := factory.installer.supervisor.ValidateGeneration(generation); err != nil {
		return nil, err
	}
	invocation, err := factory.installer.locator.helperInvocation(factory.installer.supervisor.hostAlias, factory.artifact)
	if err != nil {
		return nil, err
	}
	processContext, processCancel := context.WithCancel(lifetimeContext)
	command := exec.CommandContext(processContext, invocation.Executable, invocation.Args...)
	processutil.ConfigureBackground(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		processCancel()
		return nil, ErrHelperRuntimeStart
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		processCancel()
		return nil, ErrHelperRuntimeStart
	}
	stderr := &boundedOutput{limit: maxConnectionProbeStderrBytes}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		processCancel()
		return nil, helperProcessError(startupContext, stderr, err)
	}
	runtime := &installedHelperGeneration{
		generation: generation,
		stdin:      stdin,
		writer:     remoteprotocol.NewWriter(stdin, remoteprotocol.Limits{}),
		reader:     remoteprotocol.NewReader(stdout, remoteprotocol.Limits{}),
		stderr:     stderr,
		cancel:     processCancel,
		done:       make(chan struct{}),
		readDone:   make(chan struct{}),
		pending:    make(map[string]chan helperCallResult),
	}
	go func() {
		waitErr := command.Wait()
		runtime.waitMu.Lock()
		runtime.waitErr = waitErr
		runtime.waitMu.Unlock()
		close(runtime.done)
	}()

	stopStartupCancellation := context.AfterFunc(startupContext, processCancel)
	result, handshakeErr := runtime.handshake(factory.artifact)
	if !stopStartupCancellation() || startupContext.Err() != nil {
		handshakeErr = startupContext.Err()
	}
	if handshakeErr != nil {
		_ = runtime.Kill()
		runtime.waitBriefly()
		return nil, runtime.startFailure(startupContext, handshakeErr)
	}
	if err := factory.installer.supervisor.ValidateGeneration(generation); err != nil {
		_ = runtime.Kill()
		runtime.waitBriefly()
		return nil, err
	}
	runtime.identity = result
	return runtime, nil
}

type installedHelperGeneration struct {
	generation uint64
	identity   HelperProbeResult
	stdin      io.WriteCloser
	writer     *remoteprotocol.Writer
	reader     *remoteprotocol.Reader
	stderr     *boundedOutput
	cancel     context.CancelFunc
	done       chan struct{}

	stateMu       sync.Mutex
	nextRequest   uint64
	pending       map[string]chan helperCallResult
	shutdown      bool
	readDone      chan struct{}
	transportOnce sync.Once
	transportErr  error

	waitMu  sync.Mutex
	waitErr error
}

type helperCallResult struct {
	frame remoteprotocol.Frame
	err   error
}

func (runtime *installedHelperGeneration) Generation() uint64    { return runtime.generation }
func (runtime *installedHelperGeneration) Done() <-chan struct{} { return runtime.done }

func (runtime *installedHelperGeneration) handshake(artifact HelperArtifact) (HelperProbeResult, error) {
	nonce, err := helperProbeNonce()
	if err != nil {
		return HelperProbeResult{}, ErrHelperHandshake
	}
	helloID := "hello-runtime-" + nonce[:16]
	helloPayload, err := remoteprotocol.EncodePayload(remotehelper.HelloRequest{
		Nonce: nonce, ClientVersion: "pi-desk-runtime-v1",
	})
	if err != nil {
		return HelperProbeResult{}, ErrHelperHandshake
	}
	if err := runtime.writer.Write(remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest,
		ID: helloID, Generation: runtime.generation, Method: remotehelper.MethodHello,
		Payload: helloPayload,
	}, nil); err != nil {
		return HelperProbeResult{}, err
	}
	helloFrame, err := runtime.reader.Read()
	if err != nil {
		return HelperProbeResult{}, err
	}
	var hello remotehelper.HelloResponse
	if err := validateHelperResponse(helloFrame, helloID, runtime.generation, &hello); err != nil {
		return HelperProbeResult{}, err
	}
	if hello.Nonce != nonce || hello.ProtocolVersion != artifact.ProtocolVersion || hello.BuildHash != artifact.BuildIdentity || hello.OS != artifact.OS || hello.Architecture != artifact.Architecture || !sameCapabilities(hello.Capabilities, requiredHelperCapabilities()) {
		return HelperProbeResult{}, ErrHelperRuntimeIdentity
	}
	pingID := "ping-runtime-" + nonce[:16]
	if err := writeEmptyHelperRequest(runtime.writer, pingID, runtime.generation, remotehelper.MethodPing); err != nil {
		return HelperProbeResult{}, err
	}
	pingFrame, err := runtime.reader.Read()
	if err != nil {
		return HelperProbeResult{}, err
	}
	var ping remotehelper.PingResponse
	if err := validateHelperResponse(pingFrame, pingID, runtime.generation, &ping); err != nil || !ping.OK {
		if err != nil {
			return HelperProbeResult{}, err
		}
		return HelperProbeResult{}, ErrHelperRuntimeIdentity
	}
	runtime.startReader()
	capabilities := append([]string(nil), hello.Capabilities...)
	slices.Sort(capabilities)
	return HelperProbeResult{
		ProtocolVersion: hello.ProtocolVersion, BuildIdentity: hello.BuildHash,
		OS: hello.OS, Architecture: hello.Architecture, Capabilities: capabilities,
	}, nil
}

func (runtime *installedHelperGeneration) OpenRoot(ctx context.Context, requestedRoot string) (rootOpenResponse, error) {
	payload, err := remoteprotocol.EncodePayload(remotehelper.RootOpenRequest{Path: requestedRoot})
	if err != nil {
		return rootOpenResponse{}, runtimeLifecycleError(ErrHelperRootInvalid)
	}
	frame, requestID, err := runtime.call(ctx, "root-open", remotehelper.MethodRootOpen, payload, false)
	if err != nil {
		return rootOpenResponse{}, err
	}
	response, err := decodeRootOpenResponse(frame, requestID, runtime.generation)
	if err != nil && !errors.Is(err, ErrHelperRootInvalid) && !errors.Is(err, ErrHelperRootOpen) && !errors.Is(err, ErrHelperRootUnsupported) && !errors.Is(err, ErrHelperRuntimeLimit) && !errors.Is(err, context.Canceled) {
		_ = runtime.Kill()
	}
	return response, err
}

func decodeRootOpenResponse(frame remoteprotocol.Frame, requestID string, generation uint64) (rootOpenResponse, error) {
	envelope := frame.Envelope
	if envelope.ID != requestID || envelope.Generation != generation || len(frame.Blob) != 0 {
		return rootOpenResponse{}, ErrHelperProtocolMismatch
	}
	if envelope.Kind == remoteprotocol.KindError {
		if envelope.Error == nil {
			return rootOpenResponse{}, ErrHelperProtocolMismatch
		}
		switch envelope.Error.Code {
		case "REMOTE_INVALID_REQUEST":
			return rootOpenResponse{}, runtimeLifecycleError(ErrHelperRootInvalid)
		case "REMOTE_ROOT_OPEN_FAILED":
			return rootOpenResponse{}, runtimeLifecycleError(ErrHelperRootOpen)
		case "REMOTE_UNSUPPORTED_FILE_LAYOUT":
			return rootOpenResponse{}, lifecycleError(FailureUnsupportedFileLayout, ReasonUnsupportedFileLayout, ErrHelperRootUnsupported)
		case "REMOTE_RESOURCE_LIMIT":
			return rootOpenResponse{}, runtimeResourceError(ErrHelperRuntimeLimit)
		case "REMOTE_CANCELLED":
			return rootOpenResponse{}, lifecycleError(FailureCancelled, ReasonCancelled, context.Canceled)
		default:
			return rootOpenResponse{}, ErrHelperProtocolMismatch
		}
	}
	if envelope.Kind != remoteprotocol.KindResponse {
		return rootOpenResponse{}, ErrHelperProtocolMismatch
	}
	var response remotehelper.RootOpenResponse
	if err := remoteprotocol.DecodePayload(envelope.Payload, &response); err != nil {
		return rootOpenResponse{}, ErrHelperProtocolMismatch
	}
	return rootOpenResponse{
		Handle: response.Handle, CanonicalPath: response.CanonicalPath,
		Device: response.Device, Inode: response.Inode,
	}, nil
}

func (runtime *installedHelperGeneration) StatFile(ctx context.Context, rootHandle, logicalPath string) (RuntimeFileInfo, error) {
	var response remotehelper.FileInfoResponse
	if err := runtime.requestFile(ctx, remotehelper.MethodFileStat, remotehelper.FileRequest{RootHandle: rootHandle, Path: logicalPath}, &response); err != nil {
		return RuntimeFileInfo{}, err
	}
	return projectRuntimeFileInfo(response), nil
}

func (runtime *installedHelperGeneration) ListFiles(ctx context.Context, rootHandle, logicalPath string) (RuntimeFileList, error) {
	var response remotehelper.FileListResponse
	if err := runtime.requestFile(ctx, remotehelper.MethodFileList, remotehelper.FileRequest{RootHandle: rootHandle, Path: logicalPath}, &response); err != nil {
		return RuntimeFileList{}, err
	}
	entries := make([]RuntimeFileInfo, len(response.Entries))
	for index, entry := range response.Entries {
		entries[index] = projectRuntimeFileInfo(entry)
	}
	return RuntimeFileList{
		Path: response.Path, Entries: entries,
		SkippedUnsupportedPaths: response.SkippedUnsupportedPaths, Truncated: response.Truncated,
	}, nil
}

func (runtime *installedHelperGeneration) ReadFile(ctx context.Context, rootHandle, logicalPath string, startLine, maxLines int) (RuntimeFileRead, error) {
	var response remotehelper.FileReadResponse
	if err := runtime.requestFile(ctx, remotehelper.MethodFileRead, remotehelper.FileReadRequest{
		RootHandle: rootHandle, Path: logicalPath, StartLine: startLine, MaxLines: maxLines,
	}, &response); err != nil {
		return RuntimeFileRead{}, err
	}
	return RuntimeFileRead{
		Path: response.Path, Content: response.Content, StartLine: response.StartLine,
		EndLine: response.EndLine, NextLine: response.NextLine, Truncated: response.Truncated,
		LineTruncated: response.LineTruncated,
	}, nil
}

func (runtime *installedHelperGeneration) ReadImage(ctx context.Context, rootHandle, logicalPath string) (RuntimeFileImage, error) {
	payload, err := remoteprotocol.EncodePayload(remotehelper.FileRequest{RootHandle: rootHandle, Path: logicalPath})
	if err != nil {
		return RuntimeFileImage{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeFileInvalid)
	}
	frame, requestID, err := runtime.call(ctx, "image", remotehelper.MethodFileImage, payload, false)
	if err != nil {
		return RuntimeFileImage{}, err
	}
	response, err := decodeImageResponse(frame, requestID, runtime.generation)
	if err != nil && !errors.Is(err, ErrRuntimeFileInvalid) && !errors.Is(err, ErrRuntimeFileNotFound) && !errors.Is(err, ErrRuntimeFileUnsupported) && !errors.Is(err, ErrRuntimeFileOutputLimit) && !errors.Is(err, context.Canceled) {
		_ = runtime.Kill()
	}
	return response, err
}

func decodeImageResponse(frame remoteprotocol.Frame, requestID string, generation uint64) (RuntimeFileImage, error) {
	envelope := frame.Envelope
	if envelope.ID != requestID || envelope.Generation != generation {
		return RuntimeFileImage{}, ErrHelperProtocolMismatch
	}
	if envelope.Kind == remoteprotocol.KindError {
		return RuntimeFileImage{}, decodeFileError(envelope.Error)
	}
	if envelope.Kind != remoteprotocol.KindResponse {
		return RuntimeFileImage{}, ErrHelperProtocolMismatch
	}
	var response remotehelper.FileImageResponse
	if remoteprotocol.DecodePayload(envelope.Payload, &response) != nil {
		return RuntimeFileImage{}, ErrHelperProtocolMismatch
	}
	return RuntimeFileImage{
		Path: response.Path, MIME: response.MIME, Size: response.Size,
		SHA256: response.SHA256, Content: frame.Blob,
	}, nil
}

func (runtime *installedHelperGeneration) Content(ctx context.Context, rootHandle, logicalPath string) (RuntimeFileContent, error) {
	payload, err := remoteprotocol.EncodePayload(remotehelper.FileRequest{RootHandle: rootHandle, Path: logicalPath})
	if err != nil {
		return RuntimeFileContent{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeFileInvalid)
	}
	frame, requestID, err := runtime.call(ctx, "content", remotehelper.MethodFileContent, payload, false)
	if err != nil {
		return RuntimeFileContent{}, err
	}
	envelope := frame.Envelope
	if envelope.ID != requestID || envelope.Generation != runtime.generation {
		_ = runtime.Kill()
		return RuntimeFileContent{}, ErrHelperProtocolMismatch
	}
	if envelope.Kind == remoteprotocol.KindError {
		err := decodeFileError(envelope.Error)
		if errors.Is(err, ErrHelperProtocolMismatch) {
			_ = runtime.Kill()
		}
		return RuntimeFileContent{}, err
	}
	var response remotehelper.FileContentResponse
	if envelope.Kind != remoteprotocol.KindResponse || remoteprotocol.DecodePayload(envelope.Payload, &response) != nil {
		_ = runtime.Kill()
		return RuntimeFileContent{}, ErrHelperProtocolMismatch
	}
	return RuntimeFileContent{Path: response.Path, Size: response.Size, SHA256: response.SHA256, Content: frame.Blob}, nil
}

func (runtime *installedHelperGeneration) Mkdir(ctx context.Context, rootHandle, logicalPath string) (RuntimeFileMkdirResult, error) {
	payload, err := remoteprotocol.EncodePayload(remotehelper.FileMkdirRequest{RootHandle: rootHandle, Path: logicalPath})
	if err != nil {
		return RuntimeFileMkdirResult{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeFileInvalid)
	}
	frame, requestID, dispatched, err := runtime.callFrame(ctx, "mkdir", remotehelper.MethodFileMkdir, payload, nil, false)
	if err != nil {
		if dispatched {
			return RuntimeFileMkdirResult{}, runtimeOutcomeUnknownError()
		}
		return RuntimeFileMkdirResult{}, err
	}
	var response remotehelper.FileMkdirResponse
	if err := decodeMutationResponse(frame, requestID, runtime.generation, &response); err != nil {
		if errors.Is(err, ErrHelperProtocolMismatch) {
			_ = runtime.Kill()
			return RuntimeFileMkdirResult{}, runtimeOutcomeUnknownError()
		}
		return RuntimeFileMkdirResult{}, err
	}
	return RuntimeFileMkdirResult{Path: response.Path, Created: response.Created}, nil
}

func decodeMutationResponse(frame remoteprotocol.Frame, requestID string, generation uint64, output any) error {
	envelope := frame.Envelope
	if envelope.ID != requestID || envelope.Generation != generation || len(frame.Blob) != 0 {
		return ErrHelperProtocolMismatch
	}
	if envelope.Kind == remoteprotocol.KindError {
		if envelope.Error == nil || envelope.Error.OutcomeUnknown != (envelope.Error.Code == "REMOTE_OUTCOME_UNKNOWN") {
			return ErrHelperProtocolMismatch
		}
		switch envelope.Error.Code {
		case "REMOTE_INVALID_REQUEST":
			return runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeFileInvalid)
		case "REMOTE_FILE_CONFLICT":
			return runtimeFileError(FailureFileConflict, ReasonFileConflict, ErrRuntimeFileConflict)
		case "REMOTE_FILE_WRITE_FAILED":
			return runtimeFileError(FailureFileWrite, ReasonFileWrite, ErrRuntimeFileWrite)
		case "REMOTE_UNSUPPORTED_FILE_LAYOUT":
			return runtimeFileError(FailureUnsupportedFileLayout, ReasonUnsupportedFileLayout, ErrRuntimeFileUnsupported)
		case "REMOTE_CANCELLED":
			return lifecycleError(FailureCancelled, ReasonCancelled, context.Canceled)
		case "REMOTE_OUTCOME_UNKNOWN":
			return runtimeOutcomeUnknownError()
		default:
			return ErrHelperProtocolMismatch
		}
	}
	if envelope.Kind != remoteprotocol.KindResponse || remoteprotocol.DecodePayload(envelope.Payload, output) != nil {
		return ErrHelperProtocolMismatch
	}
	return nil
}

func (runtime *installedHelperGeneration) WriteFile(ctx context.Context, rootHandle string, request RuntimeFileWriteRequest) (RuntimeFileWriteResult, error) {
	payload, err := remoteprotocol.EncodePayload(remotehelper.FileWriteRequest{
		RootHandle: rootHandle, Path: request.Path,
		ExpectedSHA256: request.ExpectedSHA256, ExpectedAbsent: request.ExpectedAbsent,
	})
	if err != nil {
		return RuntimeFileWriteResult{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeFileInvalid)
	}
	frame, requestID, dispatched, err := runtime.callFrame(ctx, "write", remotehelper.MethodFileWrite, payload, request.Content, false)
	if err != nil {
		if dispatched {
			return RuntimeFileWriteResult{}, runtimeOutcomeUnknownError()
		}
		return RuntimeFileWriteResult{}, err
	}
	response, err := decodeWriteResponse(frame, requestID, runtime.generation)
	if errors.Is(err, ErrHelperProtocolMismatch) {
		_ = runtime.Kill()
		return RuntimeFileWriteResult{}, runtimeOutcomeUnknownError()
	}
	return response, err
}

func decodeWriteResponse(frame remoteprotocol.Frame, requestID string, generation uint64) (RuntimeFileWriteResult, error) {
	envelope := frame.Envelope
	if envelope.ID != requestID || envelope.Generation != generation || len(frame.Blob) != 0 {
		return RuntimeFileWriteResult{}, ErrHelperProtocolMismatch
	}
	if envelope.Kind == remoteprotocol.KindError {
		if envelope.Error == nil || envelope.Error.OutcomeUnknown != (envelope.Error.Code == "REMOTE_OUTCOME_UNKNOWN") {
			return RuntimeFileWriteResult{}, ErrHelperProtocolMismatch
		}
		switch envelope.Error.Code {
		case "REMOTE_INVALID_REQUEST":
			return RuntimeFileWriteResult{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeFileInvalid)
		case "REMOTE_FILE_CONFLICT":
			return RuntimeFileWriteResult{}, runtimeFileError(FailureFileConflict, ReasonFileConflict, ErrRuntimeFileConflict)
		case "REMOTE_FILE_WRITE_FAILED":
			return RuntimeFileWriteResult{}, runtimeFileError(FailureFileWrite, ReasonFileWrite, ErrRuntimeFileWrite)
		case "REMOTE_UNSUPPORTED_FILE_LAYOUT":
			return RuntimeFileWriteResult{}, runtimeFileError(FailureUnsupportedFileLayout, ReasonUnsupportedFileLayout, ErrRuntimeFileUnsupported)
		case "REMOTE_OUTPUT_LIMIT":
			return RuntimeFileWriteResult{}, runtimeFileError(FailureOutputLimit, ReasonOutputLimit, ErrRuntimeFileOutputLimit)
		case "REMOTE_CANCELLED":
			return RuntimeFileWriteResult{}, lifecycleError(FailureCancelled, ReasonCancelled, context.Canceled)
		case "REMOTE_OUTCOME_UNKNOWN":
			return RuntimeFileWriteResult{}, runtimeOutcomeUnknownError()
		default:
			return RuntimeFileWriteResult{}, ErrHelperProtocolMismatch
		}
	}
	if envelope.Kind != remoteprotocol.KindResponse {
		return RuntimeFileWriteResult{}, ErrHelperProtocolMismatch
	}
	var response remotehelper.FileWriteResponse
	if remoteprotocol.DecodePayload(envelope.Payload, &response) != nil {
		return RuntimeFileWriteResult{}, ErrHelperProtocolMismatch
	}
	return RuntimeFileWriteResult{
		Path: response.Path, Size: response.Size, SHA256: response.SHA256,
		Created: response.Created, ExtendedMetadataNotPreserved: response.ExtendedMetadataNotPreserved,
	}, nil
}

func runtimeOutcomeUnknownError() error {
	return runtimeFileError(FailureOutcomeUnknown, ReasonOutcomeUnknown, ErrRuntimeOutcomeUnknown)
}

func (runtime *installedHelperGeneration) HashFile(ctx context.Context, rootHandle, logicalPath string) (RuntimeFileHash, error) {
	var response remotehelper.FileHashResponse
	if err := runtime.requestFile(ctx, remotehelper.MethodFileHash, remotehelper.FileRequest{RootHandle: rootHandle, Path: logicalPath}, &response); err != nil {
		return RuntimeFileHash{}, err
	}
	return RuntimeFileHash{Path: response.Path, Size: response.Size, SHA256: response.SHA256}, nil
}

func (runtime *installedHelperGeneration) RunBash(ctx context.Context, rootHandle, command string) (RuntimeBashResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return RuntimeBashResult{}, lifecycleError(FailureCancelled, ReasonCancelled, err)
	}
	payload, err := remoteprotocol.EncodePayload(remotehelper.BashRunRequest{RootHandle: rootHandle, Command: command})
	if err != nil {
		return RuntimeBashResult{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeBashInvalid)
	}
	runtime.stateMu.Lock()
	if runtime.shutdown || runtime.transportErr != nil || len(runtime.pending) >= maxHelperPendingRequests {
		err := runtime.transportErr
		if err == nil {
			err = ErrHelperRuntimeStopping
		}
		runtime.stateMu.Unlock()
		return RuntimeBashResult{}, runtimeLifecycleError(err)
	}
	runtime.nextRequest++
	requestID := fmt.Sprintf("bash-%d", runtime.nextRequest)
	result := make(chan helperCallResult, 2)
	runtime.pending[requestID] = result
	runtime.stateMu.Unlock()
	requestEnvelope := remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest, ID: requestID,
		Generation: runtime.generation, Method: remotehelper.MethodBashRun, Payload: payload,
		TimeoutMillis: helperTimeoutMillis(ctx),
	}
	if err := runtime.writer.Write(requestEnvelope, nil); err != nil {
		runtime.stopTransport(err)
		return RuntimeBashResult{}, runtimeOutcomeUnknownError()
	}
	if err := runtime.sendStreamCredit(requestID, 64<<10); err != nil {
		runtime.stopTransport(err)
		return RuntimeBashResult{}, runtimeOutcomeUnknownError()
	}
	accepted := false
	processID := ""
	sequence := uint64(0)
	output := make([]byte, 0, 64<<10)
	cancelSent := false
	ctxDone := ctx.Done()
	for {
		select {
		case response := <-result:
			if response.err != nil {
				return RuntimeBashResult{}, runtimeOutcomeUnknownError()
			}
			frame := response.frame
			switch frame.Envelope.Kind {
			case remoteprotocol.KindEvent:
				switch frame.Envelope.Method {
				case remoteprotocol.MethodProcessAccepted:
					var event remoteprotocol.ProcessAccepted
					if accepted || len(frame.Blob) != 0 || remoteprotocol.DecodePayload(frame.Envelope.Payload, &event) != nil || !validRuntimeIdentity("process-", event.ProcessID) {
						_ = runtime.Kill()
						return RuntimeBashResult{}, runtimeOutcomeUnknownError()
					}
					accepted = true
					processID = event.ProcessID
				case remoteprotocol.MethodStreamData:
					var event remoteprotocol.StreamData
					if !accepted || remoteprotocol.DecodePayload(frame.Envelope.Payload, &event) != nil || event.Stream != "combined" || event.Sequence != sequence+1 || len(frame.Blob) == 0 || len(frame.Blob) > 64<<10 || len(output)+len(frame.Blob) > maxRuntimeBashOutputBytes {
						_ = runtime.Kill()
						return RuntimeBashResult{}, runtimeOutcomeUnknownError()
					}
					sequence = event.Sequence
					output = append(output, frame.Blob...)
					if err := runtime.sendStreamCredit(requestID, uint32(len(frame.Blob))); err != nil {
						runtime.stopTransport(err)
						return RuntimeBashResult{}, runtimeOutcomeUnknownError()
					}
				}
			case remoteprotocol.KindResponse:
				var terminal remotehelper.BashRunResponse
				if !accepted || len(frame.Blob) != 0 || remoteprotocol.DecodePayload(frame.Envelope.Payload, &terminal) != nil || terminal.OutputBytes != int64(len(output)) {
					_ = runtime.Kill()
					return RuntimeBashResult{}, runtimeOutcomeUnknownError()
				}
				projected := strings.ToValidUTF8(string(output), "\uFFFD")
				truncated := len(projected) > maxRuntimeBashOutputBytes
				if truncated {
					projected = projected[:maxRuntimeBashOutputBytes]
					for !utf8.ValidString(projected) {
						projected = projected[:len(projected)-1]
					}
				}
				return RuntimeBashResult{ProcessID: processID, ExitCode: terminal.ExitCode, Output: projected, OutputBytes: terminal.OutputBytes, OutputTruncated: truncated}, nil
			case remoteprotocol.KindError:
				if frame.Envelope.Error == nil || frame.Envelope.Error.OutcomeUnknown != (frame.Envelope.Error.Code == "REMOTE_OUTCOME_UNKNOWN") {
					_ = runtime.Kill()
					return RuntimeBashResult{}, runtimeOutcomeUnknownError()
				}
				switch frame.Envelope.Error.Code {
				case "REMOTE_CANCELLED":
					return RuntimeBashResult{}, lifecycleError(FailureCancelled, ReasonCancelled, context.Canceled)
				case "REMOTE_OUTPUT_LIMIT":
					return RuntimeBashResult{}, runtimeFileError(FailureOutputLimit, ReasonOutputLimit, ErrRuntimeFileOutputLimit)
				case "REMOTE_INVALID_REQUEST":
					return RuntimeBashResult{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeBashInvalid)
				case "REMOTE_BASH_START_FAILED":
					return RuntimeBashResult{}, runtimeFileError(FailureConnect, ReasonUnknown, ErrRuntimeBashStart)
				case "REMOTE_RESOURCE_LIMIT":
					return RuntimeBashResult{}, runtimeResourceError(ErrHelperRuntimeLimit)
				case "REMOTE_OUTCOME_UNKNOWN":
					return RuntimeBashResult{}, runtimeOutcomeUnknownError()
				default:
					_ = runtime.Kill()
					return RuntimeBashResult{}, runtimeOutcomeUnknownError()
				}
			}
		case <-ctxDone:
			ctxDone = nil
			if !cancelSent {
				cancelSent = true
				if err := runtime.writer.Write(remoteprotocol.Envelope{Version: remoteprotocol.Version, Kind: remoteprotocol.KindCancel, ID: requestID, Generation: runtime.generation}, nil); err != nil {
					runtime.stopTransport(err)
					return RuntimeBashResult{}, runtimeOutcomeUnknownError()
				}
			}
		case <-runtime.readDone:
			return RuntimeBashResult{}, runtimeOutcomeUnknownError()
		}
	}
}

func (runtime *installedHelperGeneration) sendStreamCredit(requestID string, value uint32) error {
	payload, err := remoteprotocol.EncodePayload(remoteprotocol.StreamCredit{Bytes: value})
	if err != nil {
		return err
	}
	return runtime.writer.Write(remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindEvent,
		ID: requestID, Generation: runtime.generation, Method: remoteprotocol.MethodStreamCredit, Payload: payload,
	}, nil)
}

func (runtime *installedHelperGeneration) ReadGit(ctx context.Context, rootHandle string, request RuntimeGitReadRequest) (RuntimeGitReadResult, error) {
	payload, err := remoteprotocol.EncodePayload(remotehelper.GitReadRequest{RootHandle: rootHandle, Operation: request.Operation, Path: request.Path})
	if err != nil {
		return RuntimeGitReadResult{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeGitInvalid)
	}
	frame, requestID, err := runtime.call(ctx, "git", remotehelper.MethodGitRead, payload, false)
	if err != nil {
		return RuntimeGitReadResult{}, err
	}
	envelope := frame.Envelope
	if envelope.ID != requestID || envelope.Generation != runtime.generation {
		_ = runtime.Kill()
		return RuntimeGitReadResult{}, ErrHelperProtocolMismatch
	}
	if envelope.Kind == remoteprotocol.KindError {
		if envelope.Error == nil || envelope.Error.OutcomeUnknown {
			_ = runtime.Kill()
			return RuntimeGitReadResult{}, ErrHelperProtocolMismatch
		}
		switch envelope.Error.Code {
		case "REMOTE_INVALID_REQUEST":
			return RuntimeGitReadResult{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeGitInvalid)
		case "REMOTE_GIT_UNAVAILABLE":
			return RuntimeGitReadResult{}, runtimeFileError(FailureGitUnavailable, ReasonGitUnavailable, ErrRuntimeGitUnavailable)
		case "REMOTE_GIT_CONFIG_UNSAFE":
			return RuntimeGitReadResult{}, runtimeFileError(FailureGitConfigUnsafe, ReasonGitConfigUnsafe, ErrRuntimeGitUnsafe)
		case "REMOTE_OUTPUT_LIMIT":
			return RuntimeGitReadResult{}, runtimeFileError(FailureOutputLimit, ReasonOutputLimit, ErrRuntimeFileOutputLimit)
		case "REMOTE_CANCELLED":
			return RuntimeGitReadResult{}, lifecycleError(FailureCancelled, ReasonCancelled, context.Canceled)
		default:
			_ = runtime.Kill()
			return RuntimeGitReadResult{}, ErrHelperProtocolMismatch
		}
	}
	var response remotehelper.GitReadResponse
	if envelope.Kind != remoteprotocol.KindResponse || remoteprotocol.DecodePayload(envelope.Payload, &response) != nil {
		_ = runtime.Kill()
		return RuntimeGitReadResult{}, ErrHelperProtocolMismatch
	}
	result := RuntimeGitReadResult{Operation: response.Operation, Parts: make([]RuntimeGitOutputPart, len(response.Parts)), Blob: frame.Blob}
	for index, part := range response.Parts {
		result.Parts[index] = RuntimeGitOutputPart{Name: part.Name, Offset: part.Offset, Size: part.Size, SHA256: part.SHA256}
	}
	return result, nil
}

func (runtime *installedHelperGeneration) FindFiles(ctx context.Context, rootHandle string, request RuntimeSearchFindRequest) (RuntimeSearchFindResult, error) {
	var response remotehelper.SearchFindResponse
	err := runtime.requestSearch(ctx, remotehelper.MethodSearchFind, remotehelper.SearchFindRequest{
		RootHandle: rootHandle, Path: request.Path, Pattern: request.Pattern, Limit: request.Limit,
	}, &response)
	if err != nil {
		return RuntimeSearchFindResult{}, err
	}
	return RuntimeSearchFindResult{
		Paths: response.Paths, SkippedUnsupportedPaths: response.SkippedUnsupportedPaths,
		BudgetReached: response.BudgetReached,
	}, nil
}

func (runtime *installedHelperGeneration) GrepFiles(ctx context.Context, rootHandle string, request RuntimeSearchGrepRequest) (RuntimeSearchGrepResult, error) {
	var response remotehelper.SearchGrepResponse
	err := runtime.requestSearch(ctx, remotehelper.MethodSearchGrep, remotehelper.SearchGrepRequest{
		RootHandle: rootHandle, Path: request.Path, Pattern: request.Pattern, Glob: request.Glob, Limit: request.Limit,
	}, &response)
	if err != nil {
		return RuntimeSearchGrepResult{}, err
	}
	result := RuntimeSearchGrepResult{
		Matches:                 make([]RuntimeSearchGrepMatch, len(response.Matches)),
		SkippedUnsupportedPaths: response.SkippedUnsupportedPaths, BudgetReached: response.BudgetReached,
	}
	for index, match := range response.Matches {
		result.Matches[index] = RuntimeSearchGrepMatch{
			Path: match.Path, Line: match.Line, Text: match.Text, LineTruncated: match.LineTruncated,
		}
	}
	return result, nil
}

func (runtime *installedHelperGeneration) requestSearch(ctx context.Context, method string, payloadValue, output any) error {
	payload, err := remoteprotocol.EncodePayload(payloadValue)
	if err != nil {
		return runtimeSearchError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeSearchInvalid)
	}
	frame, requestID, err := runtime.call(ctx, "search", method, payload, false)
	if err != nil {
		return err
	}
	err = decodeSearchResponse(frame, requestID, runtime.generation, output)
	if err != nil && !errors.Is(err, ErrRuntimeSearchInvalid) && !errors.Is(err, ErrRuntimeGitUnavailable) && !errors.Is(err, context.Canceled) {
		_ = runtime.Kill()
	}
	return err
}

func decodeSearchResponse(frame remoteprotocol.Frame, requestID string, generation uint64, output any) error {
	envelope := frame.Envelope
	if envelope.ID != requestID || envelope.Generation != generation || len(frame.Blob) != 0 {
		return ErrHelperProtocolMismatch
	}
	if envelope.Kind == remoteprotocol.KindError {
		if envelope.Error == nil || envelope.Error.OutcomeUnknown {
			return ErrHelperProtocolMismatch
		}
		switch envelope.Error.Code {
		case "REMOTE_INVALID_REQUEST":
			return runtimeSearchError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeSearchInvalid)
		case "REMOTE_GIT_UNAVAILABLE":
			return runtimeSearchError(FailureGitUnavailable, ReasonGitUnavailable, ErrRuntimeGitUnavailable)
		case "REMOTE_CANCELLED":
			return lifecycleError(FailureCancelled, ReasonCancelled, context.Canceled)
		default:
			return ErrHelperProtocolMismatch
		}
	}
	if envelope.Kind != remoteprotocol.KindResponse || remoteprotocol.DecodePayload(envelope.Payload, output) != nil {
		return ErrHelperProtocolMismatch
	}
	return nil
}

func (runtime *installedHelperGeneration) requestFile(ctx context.Context, method string, payloadValue, output any) error {
	payload, err := remoteprotocol.EncodePayload(payloadValue)
	if err != nil {
		return runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeFileInvalid)
	}
	frame, requestID, err := runtime.call(ctx, "file", method, payload, false)
	if err != nil {
		return err
	}
	err = decodeFileResponse(frame, requestID, runtime.generation, output)
	if err != nil && !errors.Is(err, ErrRuntimeFileInvalid) && !errors.Is(err, ErrRuntimeFileNotFound) && !errors.Is(err, ErrRuntimeFileUnsupported) && !errors.Is(err, ErrRuntimeFileOutputLimit) && !errors.Is(err, context.Canceled) {
		_ = runtime.Kill()
	}
	return err
}

func decodeFileResponse(frame remoteprotocol.Frame, requestID string, generation uint64, output any) error {
	envelope := frame.Envelope
	if envelope.ID != requestID || envelope.Generation != generation || len(frame.Blob) != 0 {
		return ErrHelperProtocolMismatch
	}
	if envelope.Kind == remoteprotocol.KindError {
		return decodeFileError(envelope.Error)
	}
	if envelope.Kind != remoteprotocol.KindResponse || remoteprotocol.DecodePayload(envelope.Payload, output) != nil {
		return ErrHelperProtocolMismatch
	}
	return nil
}

func decodeFileError(remoteError *remoteprotocol.RemoteError) error {
	if remoteError == nil || remoteError.OutcomeUnknown {
		return ErrHelperProtocolMismatch
	}
	switch remoteError.Code {
	case "REMOTE_INVALID_REQUEST":
		return runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeFileInvalid)
	case "REMOTE_FILE_NOT_FOUND":
		return runtimeFileError(FailureFileNotFound, ReasonFileNotFound, ErrRuntimeFileNotFound)
	case "REMOTE_UNSUPPORTED_FILE_LAYOUT":
		return runtimeFileError(FailureUnsupportedFileLayout, ReasonUnsupportedFileLayout, ErrRuntimeFileUnsupported)
	case "REMOTE_OUTPUT_LIMIT":
		return runtimeFileError(FailureOutputLimit, ReasonOutputLimit, ErrRuntimeFileOutputLimit)
	case "REMOTE_CANCELLED":
		return lifecycleError(FailureCancelled, ReasonCancelled, context.Canceled)
	default:
		return ErrHelperProtocolMismatch
	}
}

func projectRuntimeFileInfo(info remotehelper.FileInfoResponse) RuntimeFileInfo {
	return RuntimeFileInfo{
		Path: info.Path, Kind: info.Kind, Size: info.Size,
		Mode: info.Mode, ModTime: info.ModTime,
	}
}

func (runtime *installedHelperGeneration) startReader() {
	runtime.stateMu.Lock()
	if runtime.pending == nil {
		runtime.pending = make(map[string]chan helperCallResult)
	}
	if runtime.readDone == nil {
		runtime.readDone = make(chan struct{})
	}
	readDone := runtime.readDone
	runtime.stateMu.Unlock()
	go func() {
		defer close(readDone)
		for {
			frame, err := runtime.reader.Read()
			if err != nil {
				runtime.stateMu.Lock()
				shuttingDown := runtime.shutdown
				runtime.stateMu.Unlock()
				if !shuttingDown || !errors.Is(err, io.EOF) {
					runtime.stopTransport(err)
				}
				return
			}
			envelope := frame.Envelope
			terminal := envelope.Kind == remoteprotocol.KindResponse || envelope.Kind == remoteprotocol.KindError
			streamEvent := envelope.Kind == remoteprotocol.KindEvent && (envelope.Method == remoteprotocol.MethodProcessAccepted || envelope.Method == remoteprotocol.MethodStreamData)
			if envelope.Generation != runtime.generation || !terminal && !streamEvent || envelope.ID == "" {
				runtime.stopTransport(ErrHelperProtocolMismatch)
				return
			}
			runtime.stateMu.Lock()
			result := runtime.pending[envelope.ID]
			if result != nil && terminal {
				delete(runtime.pending, envelope.ID)
			}
			runtime.stateMu.Unlock()
			if result == nil {
				runtime.stopTransport(ErrHelperProtocolMismatch)
				return
			}
			result <- helperCallResult{frame: frame}
		}
	}()
}

func (runtime *installedHelperGeneration) call(ctx context.Context, prefix, method string, payload []byte, allowShutdown bool) (remoteprotocol.Frame, string, error) {
	frame, requestID, _, err := runtime.callFrame(ctx, prefix, method, payload, nil, allowShutdown)
	return frame, requestID, err
}

func (runtime *installedHelperGeneration) callFrame(ctx context.Context, prefix, method string, payload, blob []byte, allowShutdown bool) (remoteprotocol.Frame, string, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return remoteprotocol.Frame{}, "", false, lifecycleError(FailureCancelled, ReasonCancelled, err)
	}
	runtime.stateMu.Lock()
	if runtime.shutdown && !allowShutdown {
		runtime.stateMu.Unlock()
		return remoteprotocol.Frame{}, "", false, runtimeLifecycleError(ErrHelperRuntimeStopping)
	}
	if runtime.transportErr != nil {
		err := runtime.transportErr
		runtime.stateMu.Unlock()
		return remoteprotocol.Frame{}, "", false, err
	}
	if len(runtime.pending) >= maxHelperPendingRequests {
		runtime.stateMu.Unlock()
		return remoteprotocol.Frame{}, "", false, runtimeResourceError(ErrHelperRuntimeLimit)
	}
	runtime.nextRequest++
	requestID := fmt.Sprintf("%s-%d", prefix, runtime.nextRequest)
	result := make(chan helperCallResult, 2)
	runtime.pending[requestID] = result
	runtime.stateMu.Unlock()

	envelope := remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindRequest,
		ID: requestID, Generation: runtime.generation, Method: method, Payload: payload,
		TimeoutMillis: helperTimeoutMillis(ctx),
	}
	dispatched := true
	if err := runtime.writer.Write(envelope, blob); err != nil {
		runtime.stopTransport(err)
		return remoteprotocol.Frame{}, requestID, dispatched, err
	}
	select {
	case response := <-result:
		return response.frame, requestID, dispatched, response.err
	case <-ctx.Done():
		select {
		case response := <-result:
			return response.frame, requestID, dispatched, response.err
		default:
		}
		runtime.stateMu.Lock()
		_, pending := runtime.pending[requestID]
		runtime.stateMu.Unlock()
		if pending {
			if err := runtime.writer.Write(remoteprotocol.Envelope{
				Version: remoteprotocol.Version, Kind: remoteprotocol.KindCancel,
				ID: requestID, Generation: runtime.generation,
			}, nil); err != nil {
				runtime.stopTransport(err)
			}
		}
		if allowShutdown {
			return remoteprotocol.Frame{}, requestID, dispatched, lifecycleError(FailureCancelled, ReasonCancelled, ctx.Err())
		}
		select {
		case response := <-result:
			return response.frame, requestID, dispatched, response.err
		case <-runtime.readDone:
			select {
			case response := <-result:
				return response.frame, requestID, dispatched, response.err
			default:
				return remoteprotocol.Frame{}, requestID, dispatched, runtime.transportFailure()
			}
		}
	case <-runtime.readDone:
		select {
		case response := <-result:
			return response.frame, requestID, dispatched, response.err
		default:
			return remoteprotocol.Frame{}, requestID, dispatched, runtime.transportFailure()
		}
	}
}

func helperTimeoutMillis(ctx context.Context) uint32 {
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 1
	}
	const maxTimeout = 24 * time.Hour
	if remaining > maxTimeout {
		return 0
	}
	millis := (remaining + time.Millisecond - 1) / time.Millisecond
	return uint32(millis)
}

func (runtime *installedHelperGeneration) stopTransport(err error) {
	if err == nil {
		err = ErrHelperProtocolMismatch
	}
	runtime.transportOnce.Do(func() {
		runtime.stateMu.Lock()
		runtime.transportErr = err
		pending := make([]chan helperCallResult, 0, len(runtime.pending))
		for id, result := range runtime.pending {
			delete(runtime.pending, id)
			pending = append(pending, result)
		}
		runtime.stateMu.Unlock()
		for _, result := range pending {
			result <- helperCallResult{err: err}
		}
		runtime.cancel()
	})
}

func (runtime *installedHelperGeneration) transportFailure() error {
	runtime.stateMu.Lock()
	defer runtime.stateMu.Unlock()
	if runtime.transportErr != nil {
		return runtime.transportErr
	}
	return runtimeLifecycleError(ErrConnectionGenerationRevoked)
}

func (runtime *installedHelperGeneration) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	runtime.stateMu.Lock()
	if runtime.shutdown {
		runtime.stateMu.Unlock()
		return runtime.waitResult()
	}
	runtime.shutdown = true
	runtime.stateMu.Unlock()
	frame, requestID, err := runtime.call(ctx, "shutdown", remotehelper.MethodShutdown, nil, true)
	if err != nil {
		_ = runtime.Kill()
		return runtimeLifecycleError(ErrHelperRuntimeShutdown)
	}
	var response struct{}
	if err := validateHelperResponse(frame, requestID, runtime.generation, &response); err != nil {
		_ = runtime.Kill()
		return runtimeLifecycleError(ErrHelperRuntimeShutdown)
	}
	_ = runtime.stdin.Close()
	select {
	case <-runtime.done:
		return runtime.waitResult()
	case <-ctx.Done():
		_ = runtime.Kill()
		return runtimeLifecycleError(ErrHelperRuntimeShutdown)
	}
}

func (runtime *installedHelperGeneration) Kill() error {
	runtime.cancel()
	return nil
}

func (runtime *installedHelperGeneration) waitResult() error {
	select {
	case <-runtime.done:
		runtime.waitMu.Lock()
		defer runtime.waitMu.Unlock()
		if runtime.waitErr != nil || runtime.stderr.overflow {
			return runtimeLifecycleError(ErrHelperRuntimeShutdown)
		}
		return nil
	default:
		return runtimeLifecycleError(ErrHelperRuntimeShutdown)
	}
}

func (runtime *installedHelperGeneration) waitBriefly() {
	select {
	case <-runtime.done:
	case <-time.After(250 * time.Millisecond):
	}
}

func (runtime *installedHelperGeneration) startFailure(ctx context.Context, cause error) error {
	if errors.Is(cause, ErrHelperRuntimeIdentity) || errors.Is(cause, ErrHelperProtocolMismatch) {
		return ErrHelperRuntimeIdentity
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ctx.Err()
	}
	if runtime.stderr != nil && runtime.stderr.overflow {
		return lifecycleError(FailureOutputLimit, ReasonOutputLimit, ErrHelperRuntimeStart)
	}
	if runtime.stderr != nil {
		failure := ClassifyOpenSSHFailure(runtime.stderr.buffer.Bytes())
		if failure.Reason != ReasonUnknown {
			return &ConnectionProbeError{Failure: failure, cause: ErrHelperRuntimeStart}
		}
	}
	return fmt.Errorf("%w: handshake", ErrHelperRuntimeStart)
}
