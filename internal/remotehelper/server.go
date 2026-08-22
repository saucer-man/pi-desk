package remotehelper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"pi-desk/internal/remoteprotocol"
)

const (
	MethodHello    = "system.hello"
	MethodPing     = "system.ping"
	MethodShutdown = "system.shutdown"

	maxBuildIdentityBytes  = 128
	maxClientVersionBytes  = 128
	minNonceBytes          = 16
	maxNonceBytes          = 256
	maxConcurrentRequests  = 64
	maxConcurrentProcesses = 16
	maxRequestTombstones   = 1024
	requestTombstoneTTL    = 60 * time.Second
)

var (
	ErrHandshakeRequired  = errors.New("remote helper handshake is required")
	ErrHandshakeRejected  = errors.New("remote helper handshake was rejected")
	ErrGenerationMismatch = errors.New("remote helper generation mismatch")
	ErrUnexpectedMessage  = errors.New("remote helper received an unexpected message")
	ErrDuplicateRequest   = errors.New("remote helper received a duplicate request id")
	ErrUnknownCancel      = errors.New("remote helper received a cancel for an unknown request")
)

type Config struct {
	BuildHash string
	Limits    remoteprotocol.Limits
}

type HelloRequest struct {
	Nonce         string `json:"nonce"`
	ClientVersion string `json:"clientVersion"`
}

type HelloResponse struct {
	Nonce           string   `json:"nonce"`
	ProtocolVersion uint16   `json:"protocolVersion"`
	BuildHash       string   `json:"buildHash"`
	OS              string   `json:"os"`
	Architecture    string   `json:"architecture"`
	Capabilities    []string `json:"capabilities"`
}

type PingResponse struct {
	OK bool `json:"ok"`
}

type Server struct {
	reader       *remoteprotocol.Reader
	writer       *remoteprotocol.Writer
	inputCloser  io.Closer
	outputCloser io.Closer
	buildHash    string
	roots        rootCapabilityManager

	mu          sync.Mutex
	active      map[string]*helperRequest
	tombstones  map[string]time.Time
	tombstoneQ  []helperTombstone
	requests    sync.WaitGroup
	fatalOnce   sync.Once
	fatalErr    error
	serveCancel context.CancelFunc
	blobSlot    chan struct{}
}

type helperRequest struct {
	ctx      context.Context
	cancel   context.CancelFunc
	method   string
	terminal chan terminalControl

	creditMu      sync.Mutex
	creditReady   chan struct{}
	creditBalance int64
}

type helperTombstone struct {
	id      string
	expires time.Time
}

type requestTerminal struct {
	value          any
	blob           []byte
	release        func()
	code           string
	message        string
	outcomeUnknown bool
}

func NewServer(input io.Reader, output io.Writer, config Config) (*Server, error) {
	if input == nil || output == nil {
		return nil, errors.New("remote helper input and output are required")
	}
	if err := validateIdentity("build hash", config.BuildHash, maxBuildIdentityBytes); err != nil {
		return nil, err
	}
	server := &Server{
		reader:    remoteprotocol.NewReader(input, config.Limits),
		writer:    remoteprotocol.NewWriter(output, config.Limits),
		buildHash: config.BuildHash,
		roots:     newRootManager(),
		blobSlot:  make(chan struct{}, 1),
	}
	if closer, ok := input.(io.Closer); ok {
		server.inputCloser = closer
	}
	if closer, ok := output.(io.Closer); ok {
		server.outputCloser = closer
	}
	return server, nil
}

func (server *Server) Serve(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	serveCtx, cancel := context.WithCancel(ctx)
	server.mu.Lock()
	server.active = make(map[string]*helperRequest)
	server.tombstones = make(map[string]time.Time)
	server.serveCancel = cancel
	server.mu.Unlock()
	defer cancel()
	defer func() { _ = server.roots.Close() }()
	defer func() {
		server.cancelAll("")
		if server.outputCloser != nil {
			_ = server.outputCloser.Close()
		}
		server.requests.Wait()
	}()
	if server.inputCloser != nil {
		stopClose := context.AfterFunc(serveCtx, func() { _ = server.inputCloser.Close() })
		defer stopClose()
	}

	frame, err := server.reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return server.readError(ctx, err)
	}
	envelope := frame.Envelope
	if envelope.Kind != remoteprotocol.KindRequest || envelope.Method != MethodHello {
		_ = server.writeError(envelope, "REMOTE_HANDSHAKE_REQUIRED", ErrHandshakeRequired.Error(), false)
		return ErrHandshakeRequired
	}
	if err := server.handleHello(envelope, frame.Blob); err != nil {
		_ = server.writeError(envelope, "REMOTE_HANDSHAKE_REJECTED", err.Error(), false)
		return fmt.Errorf("%w: %v", ErrHandshakeRejected, err)
	}
	generation := envelope.Generation

	for {
		frame, err = server.reader.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return server.readError(ctx, err)
		}
		envelope = frame.Envelope
		if envelope.Generation != generation {
			return fmt.Errorf("%w: got %d, want %d", ErrGenerationMismatch, envelope.Generation, generation)
		}
		if envelope.Kind == remoteprotocol.KindCancel {
			if err := server.cancelRequest(envelope.ID); err != nil {
				return err
			}
			continue
		}
		if envelope.Kind == remoteprotocol.KindEvent {
			if err := server.handleClientEvent(frame); err != nil {
				return err
			}
			continue
		}
		if envelope.Kind != remoteprotocol.KindRequest {
			return fmt.Errorf("%w: %s", ErrUnexpectedMessage, envelope.Kind)
		}
		if envelope.Method == MethodShutdown {
			request, _, err := server.registerRequest(serveCtx, envelope)
			if err != nil {
				return err
			}
			if len(frame.Blob) != 0 || len(envelope.Payload) != 0 {
				if err := server.completeRequest(envelope, request, requestTerminal{code: "REMOTE_INVALID_REQUEST", message: "shutdown does not accept payload or blob data"}); err != nil {
					return err
				}
				continue
			}
			server.cancelAll(envelope.ID)
			server.requests.Wait()
			if err := server.completeRequest(envelope, request, requestTerminal{value: struct{}{}}); err != nil {
				return err
			}
			return nil
		}

		request, limited, err := server.registerRequest(serveCtx, envelope)
		if err != nil {
			return err
		}
		if limited {
			if err := server.completeRequest(envelope, request, requestTerminal{code: "REMOTE_RESOURCE_LIMIT", message: "remote helper concurrent request limit reached"}); err != nil {
				return err
			}
			continue
		}
		server.requests.Add(1)
		go func(frame remoteprotocol.Frame, request *helperRequest) {
			defer server.requests.Done()
			if err := server.completeRequest(frame.Envelope, request, server.handleRequest(request, frame)); err != nil {
				server.fail(err)
			}
		}(frame, request)
	}
}

func (server *Server) handleRequest(request *helperRequest, frame remoteprotocol.Frame) requestTerminal {
	ctx := request.ctx
	if err := ctx.Err(); err != nil {
		return cancelledTerminal()
	}
	envelope := frame.Envelope
	switch envelope.Method {
	case MethodTerminalRun:
		if len(frame.Blob) != 0 {
			return terminalError(ErrTerminalInvalid)
		}
		return server.runTerminal(request, envelope)
	case MethodBashRun:
		if len(frame.Blob) != 0 {
			return bashErrorTerminal(ErrBashInvalid)
		}
		return server.runBash(request, envelope)
	case MethodPing:
		if len(frame.Blob) != 0 || len(envelope.Payload) != 0 {
			return requestTerminal{code: "REMOTE_INVALID_REQUEST", message: "ping does not accept payload or blob data"}
		}
		return requestTerminal{value: PingResponse{OK: true}}
	case MethodRootOpen:
		if len(frame.Blob) != 0 {
			return requestTerminal{code: "REMOTE_INVALID_REQUEST", message: ErrRootInvalid.Error()}
		}
		var request RootOpenRequest
		if remoteprotocol.DecodePayload(envelope.Payload, &request) != nil {
			return requestTerminal{code: "REMOTE_INVALID_REQUEST", message: ErrRootInvalid.Error()}
		}
		response, err := server.roots.Open(ctx, request.Path)
		if err == nil {
			return requestTerminal{value: response}
		}
		if isContextError(err) {
			return cancelledTerminal()
		}
		code := "REMOTE_ROOT_OPEN_FAILED"
		switch {
		case errors.Is(err, ErrRootInvalid):
			code = "REMOTE_INVALID_REQUEST"
		case errors.Is(err, ErrRootUnsupported):
			code = "REMOTE_UNSUPPORTED_FILE_LAYOUT"
		case errors.Is(err, ErrRootResourceLimit):
			code = "REMOTE_RESOURCE_LIMIT"
		}
		return requestTerminal{code: code, message: err.Error()}
	case MethodFileMkdir:
		if len(frame.Blob) != 0 {
			return fileErrorTerminal(ErrFileInvalidPath)
		}
		var request FileMkdirRequest
		if remoteprotocol.DecodePayload(envelope.Payload, &request) != nil {
			return fileErrorTerminal(ErrFileInvalidPath)
		}
		response, operationErr := server.roots.Mkdir(ctx, request)
		if operationErr != nil {
			if errors.Is(operationErr, ErrMutationOutcomeUnknown) {
				return requestTerminal{code: "REMOTE_OUTCOME_UNKNOWN", message: operationErr.Error(), outcomeUnknown: true}
			}
			if isContextError(operationErr) {
				return cancelledTerminal()
			}
			return fileErrorTerminal(operationErr)
		}
		return requestTerminal{value: response}
	case MethodFileWrite:
		var request FileWriteRequest
		if remoteprotocol.DecodePayload(envelope.Payload, &request) != nil {
			return fileErrorTerminal(ErrFileInvalidPath)
		}
		response, operationErr := server.roots.Write(ctx, request, frame.Blob)
		if operationErr != nil {
			if isContextError(operationErr) {
				return cancelledTerminal()
			}
			return fileErrorTerminal(operationErr)
		}
		return requestTerminal{value: response}
	case MethodGitRead:
		if len(frame.Blob) != 0 {
			return gitErrorTerminal(ErrGitInvalid)
		}
		var request GitReadRequest
		if remoteprotocol.DecodePayload(envelope.Payload, &request) != nil {
			return gitErrorTerminal(ErrGitInvalid)
		}
		select {
		case server.blobSlot <- struct{}{}:
		case <-ctx.Done():
			return cancelledTerminal()
		}
		release := func() { <-server.blobSlot }
		response, blob, operationErr := server.roots.Git(ctx, request)
		if operationErr != nil {
			release()
			if isContextError(operationErr) {
				return cancelledTerminal()
			}
			return gitErrorTerminal(operationErr)
		}
		return requestTerminal{value: response, blob: blob, release: release}
	case MethodSearchFind, MethodSearchGrep:
		if len(frame.Blob) != 0 {
			return searchErrorTerminal(ErrSearchInvalid)
		}
		var response any
		var operationErr error
		if envelope.Method == MethodSearchFind {
			var request SearchFindRequest
			if remoteprotocol.DecodePayload(envelope.Payload, &request) != nil {
				operationErr = ErrSearchInvalid
			} else {
				response, operationErr = server.roots.Find(ctx, request)
			}
		} else {
			var request SearchGrepRequest
			if remoteprotocol.DecodePayload(envelope.Payload, &request) != nil {
				operationErr = ErrSearchInvalid
			} else {
				response, operationErr = server.roots.Grep(ctx, request)
			}
		}
		if operationErr != nil {
			if isContextError(operationErr) {
				return cancelledTerminal()
			}
			return searchErrorTerminal(operationErr)
		}
		return requestTerminal{value: response}
	case MethodFileStat, MethodFileList, MethodFileRead, MethodFileImage, MethodFileContent, MethodFileHash:
		if len(frame.Blob) != 0 {
			return fileErrorTerminal(ErrFileInvalidPath)
		}
		var response any
		var operationErr error
		if envelope.Method == MethodFileRead {
			var request FileReadRequest
			if remoteprotocol.DecodePayload(envelope.Payload, &request) != nil {
				operationErr = ErrFileInvalidPath
			} else {
				response, operationErr = server.roots.Read(ctx, request)
			}
		} else {
			var request FileRequest
			if remoteprotocol.DecodePayload(envelope.Payload, &request) != nil {
				operationErr = ErrFileInvalidPath
			} else {
				switch envelope.Method {
				case MethodFileStat:
					response, operationErr = server.roots.Stat(ctx, request.RootHandle, request.Path)
				case MethodFileList:
					response, operationErr = server.roots.List(ctx, request.RootHandle, request.Path)
				case MethodFileImage, MethodFileContent:
					// ponytail: one live outbound blob per helper caps memory; replace with a
					// bounded byte pool only if preview/edit throughput becomes measurable.
					select {
					case server.blobSlot <- struct{}{}:
					case <-ctx.Done():
						return cancelledTerminal()
					}
					release := func() { <-server.blobSlot }
					var blob []byte
					if envelope.Method == MethodFileImage {
						response, blob, operationErr = server.roots.Image(ctx, request.RootHandle, request.Path)
					} else {
						response, blob, operationErr = server.roots.Content(ctx, request.RootHandle, request.Path)
					}
					if operationErr == nil {
						return requestTerminal{value: response, blob: blob, release: release}
					}
					release()
				case MethodFileHash:
					response, operationErr = server.roots.Hash(ctx, request.RootHandle, request.Path)
				}
			}
		}
		if operationErr != nil {
			if isContextError(operationErr) {
				return cancelledTerminal()
			}
			return fileErrorTerminal(operationErr)
		}
		return requestTerminal{value: response}
	default:
		return requestTerminal{code: "REMOTE_METHOD_NOT_FOUND", message: "remote helper method is not supported"}
	}
}

func cancelledTerminal() requestTerminal {
	return requestTerminal{code: "REMOTE_CANCELLED", message: "remote request was cancelled"}
}

func fileErrorTerminal(operationErr error) requestTerminal {
	code := "REMOTE_FILE_NOT_FOUND"
	switch {
	case errors.Is(operationErr, ErrFileInvalidPath):
		code = "REMOTE_INVALID_REQUEST"
	case errors.Is(operationErr, ErrFileUnsupported):
		code = "REMOTE_UNSUPPORTED_FILE_LAYOUT"
	case errors.Is(operationErr, ErrFileOutputLimit):
		code = "REMOTE_OUTPUT_LIMIT"
	case errors.Is(operationErr, ErrFileConflict):
		code = "REMOTE_FILE_CONFLICT"
	case errors.Is(operationErr, ErrFileWrite):
		code = "REMOTE_FILE_WRITE_FAILED"
	}
	return requestTerminal{code: code, message: operationErr.Error()}
}

func gitErrorTerminal(operationErr error) requestTerminal {
	code := "REMOTE_GIT_UNAVAILABLE"
	switch {
	case errors.Is(operationErr, ErrGitInvalid):
		code = "REMOTE_INVALID_REQUEST"
	case errors.Is(operationErr, ErrGitUnsafe):
		code = "REMOTE_GIT_CONFIG_UNSAFE"
	case errors.Is(operationErr, ErrGitOutputLimit):
		code = "REMOTE_OUTPUT_LIMIT"
	}
	return requestTerminal{code: code, message: operationErr.Error()}
}

func searchErrorTerminal(operationErr error) requestTerminal {
	code := "REMOTE_INVALID_REQUEST"
	if errors.Is(operationErr, ErrSearchGitUnavailable) {
		code = "REMOTE_GIT_UNAVAILABLE"
	}
	return requestTerminal{code: code, message: operationErr.Error()}
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (server *Server) registerRequest(parent context.Context, envelope remoteprotocol.Envelope) (*helperRequest, bool, error) {
	now := time.Now()
	server.mu.Lock()
	defer server.mu.Unlock()
	server.pruneTombstonesLocked(now)
	if server.active[envelope.ID] != nil {
		return nil, false, ErrDuplicateRequest
	}
	if _, exists := server.tombstones[envelope.ID]; exists {
		return nil, false, ErrDuplicateRequest
	}
	requestContext := parent
	cancel := func() {}
	if envelope.TimeoutMillis > 0 {
		requestContext, cancel = context.WithTimeout(parent, time.Duration(envelope.TimeoutMillis)*time.Millisecond)
	} else {
		requestContext, cancel = context.WithCancel(parent)
	}
	request := &helperRequest{ctx: requestContext, cancel: cancel, method: envelope.Method, creditReady: make(chan struct{}, 1), terminal: make(chan terminalControl, 8)}
	limited := len(server.active) >= maxConcurrentRequests
	if envelope.Method == MethodBashRun || envelope.Method == MethodTerminalRun {
		processes := 0
		for _, active := range server.active {
			if active.method == MethodBashRun || active.method == MethodTerminalRun {
				processes++
			}
		}
		limited = limited || processes >= maxConcurrentProcesses
	}
	server.active[envelope.ID] = request
	return request, limited, nil
}

func (server *Server) handleClientEvent(frame remoteprotocol.Frame) error {
	now := time.Now()
	server.mu.Lock()
	server.pruneTombstonesLocked(now)
	request := server.active[frame.Envelope.ID]
	_, completed := server.tombstones[frame.Envelope.ID]
	server.mu.Unlock()
	if completed {
		return nil
	}
	if request == nil {
		return ErrUnexpectedMessage
	}
	switch frame.Envelope.Method {
	case remoteprotocol.MethodStreamCredit:
		if len(frame.Blob) != 0 || request.method != MethodBashRun && request.method != MethodTerminalRun {
			return ErrUnexpectedMessage
		}
		var credit remoteprotocol.StreamCredit
		if remoteprotocol.DecodePayload(frame.Envelope.Payload, &credit) != nil || credit.Bytes == 0 || credit.Bytes > remoteprotocol.MaxStreamChunkBytes {
			return ErrUnexpectedMessage
		}
		return request.pushCredit(int64(credit.Bytes))
	case remoteprotocol.MethodTerminalInput:
		if request.method != MethodTerminalRun || len(frame.Envelope.Payload) != 0 || len(frame.Blob) == 0 || len(frame.Blob) > maxTerminalInputBytes {
			return ErrUnexpectedMessage
		}
		return request.pushTerminal(terminalControl{input: frame.Blob})
	case remoteprotocol.MethodTerminalResize:
		if request.method != MethodTerminalRun || len(frame.Blob) != 0 {
			return ErrUnexpectedMessage
		}
		var resize remoteprotocol.TerminalResize
		if remoteprotocol.DecodePayload(frame.Envelope.Payload, &resize) != nil || !validTerminalDimensions(resize.Columns, resize.Rows) {
			return ErrUnexpectedMessage
		}
		return request.pushTerminal(terminalControl{columns: resize.Columns, rows: resize.Rows})
	default:
		return ErrUnexpectedMessage
	}
}

func (server *Server) cancelRequest(id string) error {
	now := time.Now()
	server.mu.Lock()
	server.pruneTombstonesLocked(now)
	request := server.active[id]
	_, completed := server.tombstones[id]
	server.mu.Unlock()
	if request != nil {
		request.cancel()
		return nil
	}
	if completed {
		return nil
	}
	return ErrUnknownCancel
}

func (server *Server) cancelAll(exceptID string) {
	server.mu.Lock()
	requests := make([]*helperRequest, 0, len(server.active))
	for id, request := range server.active {
		if id != exceptID {
			requests = append(requests, request)
		}
	}
	server.mu.Unlock()
	for _, request := range requests {
		request.cancel()
	}
}

func (server *Server) completeRequest(envelope remoteprotocol.Envelope, request *helperRequest, terminal requestTerminal) error {
	if terminal.release != nil {
		defer terminal.release()
	}
	server.mu.Lock()
	if server.active[envelope.ID] != request {
		server.mu.Unlock()
		return ErrUnexpectedMessage
	}
	delete(server.active, envelope.ID)
	request.cancel()
	server.addTombstoneLocked(envelope.ID, time.Now())
	server.mu.Unlock()
	if terminal.code != "" {
		return server.writeError(envelope, terminal.code, terminal.message, terminal.outcomeUnknown)
	}
	return server.writeResponseBlob(envelope, terminal.value, terminal.blob)
}

func (server *Server) addTombstoneLocked(id string, now time.Time) {
	server.pruneTombstonesLocked(now)
	for len(server.tombstones) >= maxRequestTombstones && len(server.tombstoneQ) > 0 {
		oldest := server.tombstoneQ[0]
		server.tombstoneQ = server.tombstoneQ[1:]
		delete(server.tombstones, oldest.id)
	}
	expires := now.Add(requestTombstoneTTL)
	server.tombstones[id] = expires
	server.tombstoneQ = append(server.tombstoneQ, helperTombstone{id: id, expires: expires})
}

func (server *Server) pruneTombstonesLocked(now time.Time) {
	for len(server.tombstoneQ) > 0 {
		oldest := server.tombstoneQ[0]
		if now.Before(oldest.expires) {
			break
		}
		server.tombstoneQ = server.tombstoneQ[1:]
		if server.tombstones[oldest.id] == oldest.expires {
			delete(server.tombstones, oldest.id)
		}
	}
}

func (server *Server) fail(err error) {
	server.fatalOnce.Do(func() {
		server.mu.Lock()
		server.fatalErr = err
		cancel := server.serveCancel
		server.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if server.inputCloser != nil {
			_ = server.inputCloser.Close()
		}
	})
}

func (server *Server) readError(ctx context.Context, readErr error) error {
	server.mu.Lock()
	fatalErr := server.fatalErr
	server.mu.Unlock()
	if fatalErr != nil {
		return fatalErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return readErr
}

func (server *Server) handleHello(envelope remoteprotocol.Envelope, blob []byte) error {
	if len(blob) != 0 {
		return errors.New("hello does not accept blob data")
	}
	var request HelloRequest
	if err := remoteprotocol.DecodePayload(envelope.Payload, &request); err != nil {
		return err
	}
	if len(request.Nonce) < minNonceBytes || len(request.Nonce) > maxNonceBytes {
		return errors.New("hello nonce has an invalid length")
	}
	if err := validateIdentity("hello nonce", request.Nonce, maxNonceBytes); err != nil {
		return err
	}
	if err := validateIdentity("client version", request.ClientVersion, maxClientVersionBytes); err != nil {
		return err
	}
	return server.writeResponse(envelope, HelloResponse{
		Nonce:           request.Nonce,
		ProtocolVersion: remoteprotocol.Version,
		BuildHash:       server.buildHash,
		OS:              runtime.GOOS,
		Architecture:    runtime.GOARCH,
		Capabilities: []string{
			MethodFileContent, MethodFileHash, MethodFileImage, MethodFileList, MethodFileMkdir, MethodFileRead, MethodFileStat, MethodFileWrite,
			MethodBashRun, MethodGitRead, MethodTerminalRun, MethodPing, MethodRootOpen, MethodSearchFind, MethodSearchGrep, MethodShutdown,
		},
	})
}

func (server *Server) writeResponse(request remoteprotocol.Envelope, value any) error {
	return server.writeResponseBlob(request, value, nil)
}

func (server *Server) writeResponseBlob(request remoteprotocol.Envelope, value any, blob []byte) error {
	payload, err := remoteprotocol.EncodePayload(value)
	if err != nil {
		return err
	}
	return server.writer.Write(remoteprotocol.Envelope{
		Version:    remoteprotocol.Version,
		Kind:       remoteprotocol.KindResponse,
		ID:         request.ID,
		Generation: request.Generation,
		Payload:    payload,
	}, blob)
}

func (server *Server) writeError(request remoteprotocol.Envelope, code, message string, outcomeUnknown bool) error {
	return server.writer.Write(remoteprotocol.Envelope{
		Version:    remoteprotocol.Version,
		Kind:       remoteprotocol.KindError,
		ID:         request.ID,
		Generation: request.Generation,
		Error: &remoteprotocol.RemoteError{
			Code:           code,
			Message:        message,
			OutcomeUnknown: outcomeUnknown,
		},
	}, nil)
}

func validateIdentity(name, value string, maxBytes int) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if !utf8.ValidString(value) || len(value) > maxBytes {
		return fmt.Errorf("%s is invalid or too large", name)
	}
	for _, char := range value {
		if unicode.IsControl(char) || unicode.IsSpace(char) {
			return fmt.Errorf("%s contains whitespace or control characters", name)
		}
	}
	return nil
}
