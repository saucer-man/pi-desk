package appservice

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"pi-desk/internal/remotessh"
)

const (
	remoteBrokerMaxFrame       = 32 << 20
	remoteBrokerMaxConcurrent  = 16
	remoteBrokerMaxGrepContext = 10
	remoteBrokerMaxGrepOutput  = 256 << 10
	remoteBrokerMaxContext     = 256 << 10
)

//go:embed resources/pi-desk-remote.ts
var remoteAdapterSource []byte

type remoteBrokerRequest struct {
	ID         string                             `json:"id"`
	Token      string                             `json:"token"`
	Operation  string                             `json:"operation"`
	Path       string                             `json:"path,omitempty"`
	Pattern    string                             `json:"pattern,omitempty"`
	Glob       string                             `json:"glob,omitempty"`
	Content    string                             `json:"content,omitempty"`
	Edits      []remotessh.RuntimeFileReplacement `json:"edits,omitempty"`
	Offset     int                                `json:"offset,omitempty"`
	Limit      int                                `json:"limit,omitempty"`
	Context    int                                `json:"context,omitempty"`
	Timeout    int                                `json:"timeout,omitempty"`
	IgnoreCase bool                               `json:"ignoreCase,omitempty"`
	Literal    bool                               `json:"literal,omitempty"`
	Protocol   string                             `json:"protocol,omitempty"`
	Coverage   []string                           `json:"coverage,omitempty"`
}

type remoteBrokerResponse struct {
	ID     string             `json:"id"`
	OK     bool               `json:"ok"`
	Result any                `json:"result,omitempty"`
	Error  *remoteBrokerError `json:"error,omitempty"`
}

type remoteBrokerError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type remoteTaskBroker struct {
	runtime     *remotessh.RuntimeLeaseSupervisor
	lease       *remotessh.RuntimeLease
	root        string
	token       string
	dir         string
	socket      string
	adapter     string
	manifest    remoteAdapterManifest
	contextHash string
	contextText string

	ctx           context.Context
	cancel        context.CancelFunc
	listener      net.Listener
	sem           chan struct{}
	mu            sync.Mutex
	conns         map[net.Conn]struct{}
	wg            sync.WaitGroup
	close         sync.Once
	handshakeOnce sync.Once
	handshakeDone chan struct{}
	handshakeErr  error
	handshakePeer int
	peerMu        sync.RWMutex
	launcherPID   int
}

func newRemoteTaskBroker(runtime *remotessh.RuntimeLeaseSupervisor, lease *remotessh.RuntimeLease, canonicalRoot, piVersion string) (*remoteTaskBroker, error) {
	manifest, err := verifyRemoteAdapterBundle(piVersion)
	if err != nil {
		return nil, err
	}
	if runtime == nil || lease == nil || lease.Kind() != remotessh.RuntimeTaskLease || lease.Context().Err() != nil || !validCanonicalRemoteRoot(canonicalRoot) {
		return nil, errors.New("remote task broker requires a live task lease and canonical root")
	}
	directory, err := os.MkdirTemp("", "pi-desk-remote-")
	if err != nil {
		return nil, fmt.Errorf("create remote broker directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	if err := os.Chmod(directory, 0o700); err != nil {
		cleanup()
		return nil, fmt.Errorf("protect remote broker directory: %w", err)
	}
	adapter := directory + string(os.PathSeparator) + "adapter.ts"
	if err := os.WriteFile(adapter, remoteAdapterSource, 0o600); err != nil {
		cleanup()
		return nil, fmt.Errorf("write remote adapter: %w", err)
	}
	if err := verifyRemoteAdapterFile(adapter, manifest); err != nil {
		cleanup()
		return nil, err
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		cleanup()
		return nil, err
	}
	token := hex.EncodeToString(tokenBytes)
	listener, socket, err := listenRemoteBroker(directory, token[:32])
	if err != nil {
		cleanup()
		return nil, err
	}
	ctx, cancel := context.WithCancel(lease.Context())
	broker := &remoteTaskBroker{
		runtime: runtime, lease: lease, root: canonicalRoot, token: token,
		dir: directory, socket: socket, adapter: adapter, manifest: manifest, ctx: ctx, cancel: cancel,
		listener: listener, sem: make(chan struct{}, remoteBrokerMaxConcurrent), conns: make(map[net.Conn]struct{}),
		handshakeDone: make(chan struct{}),
	}
	contextValue, contextHash, err := broker.loadRemoteContext(ctx)
	if err != nil {
		cancel()
		_ = listener.Close()
		cleanup()
		return nil, err
	}
	broker.contextText, broker.contextHash = contextValue, contextHash
	broker.wg.Add(1)
	go broker.serve()
	return broker, nil
}

func (broker *remoteTaskBroker) Environment() []string {
	return []string{"PI_DESK_REMOTE_SOCKET=" + broker.socket, "PI_DESK_REMOTE_TOKEN=" + broker.token, "PI_DESK_REMOTE_ROOT=" + broker.root}
}

func (broker *remoteTaskBroker) AdapterPath() string { return broker.adapter }

func (broker *remoteTaskBroker) Close(ctx context.Context) error {
	broker.close.Do(func() {
		broker.cancel()
		_ = broker.listener.Close()
		broker.mu.Lock()
		for connection := range broker.conns {
			_ = connection.Close()
		}
		broker.mu.Unlock()
	})
	done := make(chan struct{})
	go func() { broker.wg.Wait(); close(done) }()
	select {
	case <-done:
		return os.RemoveAll(broker.dir)
	case <-ctx.Done():
		go func() {
			broker.wg.Wait()
			_ = os.RemoveAll(broker.dir)
		}()
		return ctx.Err()
	}
}

func (broker *remoteTaskBroker) serve() {
	defer broker.wg.Done()
	for {
		connection, err := broker.listener.Accept()
		if err != nil {
			return
		}
		select {
		case broker.sem <- struct{}{}:
		case <-broker.ctx.Done():
			_ = connection.Close()
			return
		default:
			_ = connection.Close()
			continue
		}
		broker.mu.Lock()
		broker.conns[connection] = struct{}{}
		broker.mu.Unlock()
		broker.wg.Add(1)
		go broker.handle(connection)
	}
}

func (broker *remoteTaskBroker) handle(connection net.Conn) {
	defer broker.wg.Done()
	defer func() {
		<-broker.sem
		_ = connection.Close()
		broker.mu.Lock()
		delete(broker.conns, connection)
		broker.mu.Unlock()
	}()
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	peerPID, err := remoteBrokerPeerPID(connection)
	if err != nil {
		return
	}
	request, err := readRemoteBrokerRequest(connection)
	if err != nil {
		_ = writeRemoteBrokerResponse(connection, remoteBrokerResponse{OK: false, Error: &remoteBrokerError{Code: "REMOTE_INVALID_REQUEST", Message: "Remote tool request is invalid"}})
		return
	}
	if subtle.ConstantTimeCompare([]byte(request.Token), []byte(broker.token)) != 1 || request.ID == "" || len(request.ID) > 256 {
		_ = writeRemoteBrokerResponse(connection, remoteBrokerResponse{ID: request.ID, OK: false, Error: &remoteBrokerError{Code: "REMOTE_INVALID_REQUEST", Message: "Remote tool request is invalid"}})
		return
	}
	_ = connection.SetReadDeadline(time.Time{})
	requestContext, cancel := context.WithTimeout(broker.ctx, 120*time.Second)
	defer cancel()
	go func() {
		var scratch [1]byte
		_, _ = connection.Read(scratch[:])
		cancel()
	}()
	result, err := broker.execute(requestContext, request, peerPID)
	response := remoteBrokerResponse{ID: request.ID, OK: err == nil, Result: result}
	if err != nil {
		response.Result = nil
		response.Error = classifyRemoteBrokerError(err)
	}
	if writeErr := writeRemoteBrokerResponse(connection, response); writeErr != nil && remoteBrokerMutation(request.Operation) {
		_ = broker.runtime.Disconnect(context.Background())
	}
}

func remoteBrokerMutation(operation string) bool {
	return operation == "write" || operation == "edit" || operation == "bash"
}

func (broker *remoteTaskBroker) execute(ctx context.Context, request remoteBrokerRequest, peerPID int) (any, error) {
	if peerPID <= 0 {
		return nil, errors.New("remote broker peer identity is invalid")
	}
	if request.Operation != "hello" {
		select {
		case <-broker.handshakeDone:
			if broker.handshakeErr != nil {
				return nil, broker.handshakeErr
			}
			broker.peerMu.RLock()
			launcherPID := broker.launcherPID
			broker.peerMu.RUnlock()
			if !remoteBrokerPeerMatches(peerPID, launcherPID) {
				return nil, errors.New("remote broker peer process is not the active Pi task")
			}
		default:
			return nil, errors.New("remote adapter coverage handshake is required")
		}
	}
	switch request.Operation {
	case "hello":
		if !validRemoteAdapterHandshake(broker.manifest, request.Protocol, request.Coverage) {
			err := errors.New("remote adapter coverage handshake is invalid")
			broker.completeHandshake(err, 0)
			return nil, err
		}
		select {
		case <-broker.handshakeDone:
			if broker.handshakeErr != nil {
				return nil, broker.handshakeErr
			}
			if broker.handshakePeer != peerPID {
				return nil, errors.New("remote adapter handshake peer changed")
			}
		default:
		}
		root, err := broker.runtime.StatFile(ctx, broker.lease, ".")
		if err != nil || root.Kind != "directory" {
			if err == nil {
				err = errors.New("remote adapter root handshake is invalid")
			}
			broker.completeHandshake(err, 0)
			return nil, err
		}
		broker.completeHandshake(nil, peerPID)
		return map[string]any{"protocol": broker.manifest.Protocol, "coverage": broker.manifest.Coverage, "contextHash": broker.contextHash}, nil
	case "context":
		value, hash, err := broker.loadRemoteContext(ctx)
		if err != nil {
			return nil, err
		}
		if hash != broker.contextHash {
			return nil, ErrRemoteContextChanged
		}
		return map[string]any{"content": value, "hash": hash}, nil
	case "stat":
		return broker.runtime.StatFile(ctx, broker.lease, request.Path)
	case "list":
		return broker.runtime.ListFiles(ctx, broker.lease, request.Path)
	case "read":
		read, err := broker.runtime.ReadFile(ctx, broker.lease, request.Path, request.Offset, request.Limit)
		if !errors.Is(err, remotessh.ErrRuntimeFileUnsupported) {
			return read, err
		}
		image, imageErr := broker.runtime.ReadImage(ctx, broker.lease, request.Path)
		if imageErr != nil {
			return nil, imageErr
		}
		return map[string]any{"path": image.Path, "mime": image.MIME, "size": image.Size, "sha256": image.SHA256, "base64": base64.StdEncoding.EncodeToString(image.Content)}, nil
	case "write":
		if !utf8.ValidString(request.Content) || len(request.Content) > 16<<20 {
			return nil, remotessh.ErrRuntimeFileInvalid
		}
		if parent := path.Dir(request.Path); parent != "." {
			if _, err := broker.runtime.EnsureDirectory(ctx, broker.lease, parent); err != nil {
				return nil, err
			}
		}
		hash, err := broker.runtime.HashFile(ctx, broker.lease, request.Path)
		write := remotessh.RuntimeFileWriteRequest{Path: request.Path, Content: []byte(request.Content)}
		if errors.Is(err, remotessh.ErrRuntimeFileNotFound) {
			write.ExpectedAbsent = true
		} else if err != nil {
			return nil, err
		} else {
			write.ExpectedSHA256 = hash.SHA256
		}
		return broker.runtime.WriteFile(ctx, broker.lease, write)
	case "edit":
		return broker.runtime.EditFileAll(ctx, broker.lease, request.Path, request.Edits)
	case "find":
		return broker.runtime.FindFiles(ctx, broker.lease, remotessh.RuntimeSearchFindRequest{Path: request.Path, Pattern: request.Pattern, Limit: request.Limit})
	case "grep":
		return broker.grep(ctx, request)
	case "bash":
		if request.Timeout < 1 || request.Timeout > 120 {
			return nil, remotessh.ErrRuntimeBashInvalid
		}
		bashContext, cancel := context.WithTimeout(ctx, time.Duration(request.Timeout)*time.Second)
		defer cancel()
		return broker.runtime.RunBash(bashContext, broker.lease, request.Content)
	default:
		return nil, remotessh.ErrRuntimeFileInvalid
	}
}

func (broker *remoteTaskBroker) completeHandshake(err error, peerPID int) {
	broker.handshakeOnce.Do(func() {
		broker.handshakeErr, broker.handshakePeer = err, peerPID
		close(broker.handshakeDone)
	})
}

func (broker *remoteTaskBroker) waitHandshake(ctx context.Context, launcherPID int) error {
	select {
	case <-broker.handshakeDone:
		if broker.handshakeErr != nil {
			return broker.handshakeErr
		}
		if !remoteBrokerPeerMatches(broker.handshakePeer, launcherPID) {
			return errors.New("remote adapter peer is not the launched Pi process")
		}
		broker.peerMu.Lock()
		broker.launcherPID = launcherPID
		broker.peerMu.Unlock()
		return nil
	case <-broker.ctx.Done():
		return errors.New("remote adapter broker closed before coverage handshake")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (broker *remoteTaskBroker) validateContext(ctx context.Context) error {
	_, hash, err := broker.loadRemoteContext(ctx)
	if err != nil {
		return err
	}
	if hash != broker.contextHash {
		return ErrRemoteContextChanged
	}
	return nil
}

func (broker *remoteTaskBroker) loadRemoteContext(ctx context.Context) (string, string, error) {
	for _, logicalPath := range []string{"AGENTS.override.md", "AGENTS.md"} {
		start := 1
		var content strings.Builder
		for {
			read, err := broker.runtime.ReadFile(ctx, broker.lease, logicalPath, start, 2000)
			if errors.Is(err, remotessh.ErrRuntimeFileNotFound) {
				break
			}
			if err != nil {
				return "", "", err
			}
			if read.LineTruncated {
				return "", "", remotessh.ErrRuntimeFileOutputLimit
			}
			if content.Len() > 0 && read.Content != "" {
				content.WriteByte('\n')
			}
			if content.Len()+len(read.Content) > remoteBrokerMaxContext {
				return "", "", remotessh.ErrRuntimeFileOutputLimit
			}
			content.WriteString(read.Content)
			if !read.Truncated || read.NextLine == 0 {
				value := content.String()
				digest := sha256.Sum256([]byte(logicalPath + "\x00" + value))
				return value, hex.EncodeToString(digest[:]), nil
			}
			if read.NextLine <= start {
				return "", "", remotessh.ErrRuntimeFileUnsupported
			}
			start = read.NextLine
		}
	}
	digest := sha256.Sum256(nil)
	return "", hex.EncodeToString(digest[:]), nil
}

func (broker *remoteTaskBroker) grep(ctx context.Context, request remoteBrokerRequest) (map[string]any, error) {
	if request.Context < 0 || request.Context > remoteBrokerMaxGrepContext {
		return nil, remotessh.ErrRuntimeSearchInvalid
	}
	pattern := request.Pattern
	if request.Literal {
		pattern = regexp.QuoteMeta(pattern)
	}
	if request.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	result, err := broker.runtime.GrepFiles(ctx, broker.lease, remotessh.RuntimeSearchGrepRequest{Path: request.Path, Pattern: pattern, Glob: request.Glob, Limit: request.Limit})
	if err != nil {
		return nil, err
	}
	var output strings.Builder
	truncated := false
	for _, match := range result.Matches {
		lines := []string{match.Text}
		start := match.Line
		if request.Context > 0 {
			start = max(1, match.Line-request.Context)
			read, readErr := broker.runtime.ReadFile(ctx, broker.lease, match.Path, start, request.Context*2+1)
			if readErr != nil {
				return nil, readErr
			}
			lines = strings.Split(strings.TrimSuffix(strings.ReplaceAll(read.Content, "\r\n", "\n"), "\n"), "\n")
		}
		displayPath := strings.TrimPrefix(strings.TrimPrefix(match.Path, request.Path), "/")
		if displayPath == "" {
			displayPath = path.Base(match.Path)
		}
		for index, line := range lines {
			lineNumber := start + index
			separator := "-"
			if lineNumber == match.Line {
				separator = ":"
			}
			piece := fmt.Sprintf("%s%s%d%s %s\n", displayPath, separator, lineNumber, separator, line)
			if output.Len()+len(piece) > remoteBrokerMaxGrepOutput {
				truncated = true
				break
			}
			output.WriteString(piece)
		}
		if truncated {
			break
		}
	}
	text := strings.TrimSuffix(output.String(), "\n")
	if len(result.Matches) == 0 {
		text = "No matches found"
	}
	return map[string]any{"output": text, "budgetReached": result.BudgetReached || truncated, "lineTruncated": slicesContainTruncated(result.Matches)}, nil
}

func validCanonicalRemoteRoot(value string) bool {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) || !path.IsAbs(value) || path.Clean(value) != value || strings.Contains(value, "\\") {
		return false
	}
	for _, character := range value {
		if character == 0 || unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return false
		}
	}
	return true
}

func slicesContainTruncated(matches []remotessh.RuntimeSearchGrepMatch) bool {
	for _, match := range matches {
		if match.LineTruncated {
			return true
		}
	}
	return false
}

func readRemoteBrokerRequest(reader io.Reader) (remoteBrokerRequest, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return remoteBrokerRequest{}, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > remoteBrokerMaxFrame {
		return remoteBrokerRequest{}, errors.New("remote broker frame exceeds limit")
	}
	content := make([]byte, size)
	if _, err := io.ReadFull(reader, content); err != nil {
		return remoteBrokerRequest{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	var request remoteBrokerRequest
	if err := decoder.Decode(&request); err != nil {
		return remoteBrokerRequest{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return remoteBrokerRequest{}, errors.New("remote broker frame has trailing content")
	}
	return request, nil
}

func writeRemoteBrokerResponse(writer io.Writer, response remoteBrokerResponse) error {
	content, err := json.Marshal(response)
	if err != nil {
		return err
	}
	if len(content) > remoteBrokerMaxFrame {
		content, _ = json.Marshal(remoteBrokerResponse{ID: response.ID, OK: false, Error: &remoteBrokerError{Code: "REMOTE_OUTPUT_LIMIT", Message: "Remote tool output exceeds the safety limit"}})
	}
	buffer := bufio.NewWriter(writer)
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(content)))
	if _, err := buffer.Write(header[:]); err != nil {
		return err
	}
	if _, err := buffer.Write(content); err != nil {
		return err
	}
	return buffer.Flush()
}

func classifyRemoteBrokerError(err error) *remoteBrokerError {
	code, message := "REMOTE_DISCONNECTED", "Remote workspace is disconnected or stale"
	switch {
	case errors.Is(err, ErrRemoteContextChanged):
		code, message = "REMOTE_CONTEXT_CHANGED_WAIT_FOR_IDLE", "Remote workspace context changed; stop or resume the task before sending another prompt"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code, message = "REMOTE_CANCELLED", "Remote tool operation was cancelled"
	case errors.Is(err, remotessh.ErrRuntimeFileInvalid), errors.Is(err, remotessh.ErrRuntimeSearchInvalid), errors.Is(err, remotessh.ErrRuntimeBashInvalid):
		code, message = "REMOTE_INVALID_REQUEST", "Remote tool request is invalid"
	case errors.Is(err, remotessh.ErrRuntimeFileNotFound):
		code, message = "REMOTE_FILE_NOT_FOUND", "Remote file was not found"
	case errors.Is(err, remotessh.ErrRuntimeFileUnsupported):
		code, message = "REMOTE_UNSUPPORTED_FILE_LAYOUT", "Remote file type or encoding is unsupported"
	case errors.Is(err, remotessh.ErrRuntimeFileOutputLimit):
		code, message = "REMOTE_OUTPUT_LIMIT", "Remote tool output exceeds the safety limit"
	case errors.Is(err, remotessh.ErrRuntimeFileConflict):
		code, message = "REMOTE_FILE_CONFLICT", "Remote file changed before the operation completed"
	case errors.Is(err, remotessh.ErrRuntimeFileWrite):
		code, message = "REMOTE_FILE_WRITE_FAILED", "Remote file could not be written atomically"
	case errors.Is(err, remotessh.ErrRuntimeOutcomeUnknown):
		code, message = "REMOTE_OUTCOME_UNKNOWN", "Remote mutation outcome is unknown; inspect the workspace before retrying"
	case errors.Is(err, remotessh.ErrRuntimeBashStart):
		code, message = "REMOTE_TERMINAL_START_FAILED", "Remote command could not start"
	}
	return &remoteBrokerError{Code: code, Message: message}
}
