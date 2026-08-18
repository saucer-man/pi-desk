package pirpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
)

type Process interface {
	Stdin() io.WriteCloser
	Stdout() io.Reader
	Stderr() io.Reader
	Wait() error
	Kill() error
}

type EventSink func(Event)

const maxAbandonedRequestIDs = 256

type Client struct {
	process    Process
	generation uint64
	maxRecord  int
	sink       EventSink

	requestSequence atomic.Uint64
	writeMu         sync.Mutex
	pendingMu       sync.Mutex
	pending         map[string]chan callResult
	abandoned       map[string]struct{}
	abandonedOrder  []string

	stderr     *tailBuffer
	done       chan struct{}
	stdoutDone chan struct{}

	closeOnce sync.Once
	closeMu   sync.RWMutex
	closeErr  error
}

func NewClient(process Process, generation uint64, sink EventSink) *Client {
	return NewClientWithLimits(process, generation, sink, DefaultMaxRecordBytes, DefaultStderrBytes)
}

func NewClientWithLimits(process Process, generation uint64, sink EventSink, maxRecordBytes, stderrBytes int) *Client {
	if sink == nil {
		sink = func(Event) {}
	}
	client := &Client{
		process:    process,
		generation: generation,
		maxRecord:  maxRecordBytes,
		sink:       sink,
		pending:    make(map[string]chan callResult),
		abandoned:  make(map[string]struct{}),
		stderr:     newTailBuffer(stderrBytes),
		done:       make(chan struct{}),
		stdoutDone: make(chan struct{}),
	}

	go client.readStdout()
	go client.readStderr()
	go client.wait()
	return client
}

func (client *Client) Call(ctx context.Context, command map[string]any) (Response, error) {
	commandType, ok := command["type"].(string)
	if !ok || commandType == "" {
		return Response{}, ErrInvalidCommand
	}

	request := make(map[string]any, len(command)+1)
	for key, value := range command {
		request[key] = value
	}
	requestID := fmt.Sprintf("req-%d", client.requestSequence.Add(1))
	request["id"] = requestID

	record, err := EncodeRecord(request, client.maxRecord)
	if err != nil {
		return Response{}, err
	}

	resultChannel := make(chan callResult, 1)
	client.pendingMu.Lock()
	if client.isClosed() {
		client.pendingMu.Unlock()
		return Response{}, client.closedError()
	}
	client.pending[requestID] = resultChannel
	client.pendingMu.Unlock()

	client.writeMu.Lock()
	_, writeErr := client.process.Stdin().Write(record)
	client.writeMu.Unlock()
	if writeErr != nil {
		client.removePending(requestID)
		return Response{}, fmt.Errorf("write pi RPC command: %w", writeErr)
	}

	select {
	case result := <-resultChannel:
		if result.err != nil {
			return Response{}, result.err
		}
		if !result.response.Success {
			return result.response, &RemoteError{Command: result.response.Command, Message: result.response.Error}
		}
		return result.response, nil
	case <-ctx.Done():
		client.abandonPending(requestID)
		return Response{}, ctx.Err()
	case <-client.done:
		client.removePending(requestID)
		return Response{}, client.closedError()
	}
}

func (client *Client) Send(command map[string]any) error {
	commandType, ok := command["type"].(string)
	if !ok || commandType == "" {
		return ErrInvalidCommand
	}
	record, err := EncodeRecord(command, client.maxRecord)
	if err != nil {
		return err
	}
	if client.isClosed() {
		return client.closedError()
	}
	client.writeMu.Lock()
	defer client.writeMu.Unlock()
	if _, err := client.process.Stdin().Write(record); err != nil {
		return fmt.Errorf("write pi RPC message: %w", err)
	}
	return nil
}

func (client *Client) Diagnostics() string {
	return client.stderr.String()
}

func (client *Client) Done() <-chan struct{} {
	return client.done
}

func (client *Client) Err() error {
	if !client.isClosed() {
		return nil
	}
	return client.closedError()
}

func (client *Client) Close() error {
	client.finish(ErrClientClosed)
	_ = client.process.Stdin().Close()
	if err := client.process.Kill(); err != nil && !errors.Is(err, ErrClientClosed) {
		return fmt.Errorf("kill pi RPC process: %w", err)
	}
	return nil
}

func (client *Client) readStdout() {
	defer close(client.stdoutDone)
	err := ReadRecords(client.process.Stdout(), client.maxRecord, func(record []byte) error {
		client.handleRecord(record)
		return nil
	})
	if err != nil {
		// A process exit can close the stdout handle before Wait returns. The
		// supervisor reports that exit once; it is not an RPC framing failure.
		if !errors.Is(err, os.ErrClosed) && !errors.Is(err, io.ErrClosedPipe) {
			client.emitProtocolError(err, nil)
		}
		client.finish(err)
		_ = client.process.Kill()
	}
}

func (client *Client) readStderr() {
	_, _ = io.Copy(client.stderr, client.process.Stderr())
}

func (client *Client) wait() {
	err := client.process.Wait()
	<-client.stdoutDone
	if err == nil {
		err = io.EOF
	}
	client.finish(fmt.Errorf("pi RPC process exited: %w", err))
}

func (client *Client) handleRecord(record []byte) {
	var envelope struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(record, &envelope); err != nil {
		client.emitProtocolError(fmt.Errorf("decode pi RPC record: %w", err), record)
		return
	}
	if envelope.Type == "" {
		client.emitProtocolError(errors.New("pi RPC record has no type"), record)
		return
	}

	if envelope.Type != "response" {
		client.sink(Event{
			Generation: client.generation,
			Type:       envelope.Type,
			Payload:    append(json.RawMessage(nil), record...),
		})
		return
	}

	var response Response
	if err := json.Unmarshal(record, &response); err != nil {
		client.emitProtocolError(fmt.Errorf("decode pi RPC response: %w", err), record)
		return
	}
	if response.ID == "" {
		client.emitProtocolError(errors.New("pi RPC response has no id"), record)
		return
	}

	client.pendingMu.Lock()
	resultChannel, found := client.pending[response.ID]
	if found {
		delete(client.pending, response.ID)
	}
	_, abandoned := client.abandoned[response.ID]
	client.pendingMu.Unlock()
	if abandoned {
		return
	}
	if !found {
		client.emitProtocolError(fmt.Errorf("pi RPC response has unknown id %q", response.ID), record)
		return
	}
	resultChannel <- callResult{response: response}
}

func (client *Client) emitProtocolError(err error, record []byte) {
	event := Event{
		Generation: client.generation,
		Type:       "protocol_error",
		Error:      err.Error(),
	}
	if json.Valid(record) {
		event.Payload = append(json.RawMessage(nil), record...)
	} else {
		const maxDiagnosticBytes = 1024
		if len(record) > maxDiagnosticBytes {
			record = record[:maxDiagnosticBytes]
		}
		event.Record = string(record)
	}
	client.sink(event)
}

func (client *Client) finish(err error) {
	client.closeOnce.Do(func() {
		client.closeMu.Lock()
		client.closeErr = err
		client.closeMu.Unlock()
		close(client.done)

		client.pendingMu.Lock()
		pending := client.pending
		client.pending = make(map[string]chan callResult)
		client.pendingMu.Unlock()
		for _, resultChannel := range pending {
			resultChannel <- callResult{err: err}
		}
	})
}

func (client *Client) removePending(requestID string) {
	client.pendingMu.Lock()
	delete(client.pending, requestID)
	client.pendingMu.Unlock()
}

func (client *Client) abandonPending(requestID string) {
	// Keep a bounded tombstone so a valid response that races cancellation is not reported as a protocol error.
	client.pendingMu.Lock()
	defer client.pendingMu.Unlock()
	if _, found := client.pending[requestID]; !found {
		return
	}
	delete(client.pending, requestID)
	client.abandoned[requestID] = struct{}{}
	client.abandonedOrder = append(client.abandonedOrder, requestID)
	if len(client.abandonedOrder) > maxAbandonedRequestIDs {
		oldest := client.abandonedOrder[0]
		client.abandonedOrder = client.abandonedOrder[1:]
		delete(client.abandoned, oldest)
	}
}

func (client *Client) isClosed() bool {
	select {
	case <-client.done:
		return true
	default:
		return false
	}
}

func (client *Client) closedError() error {
	client.closeMu.RLock()
	defer client.closeMu.RUnlock()
	if client.closeErr != nil {
		return client.closeErr
	}
	return ErrClientClosed
}

type tailBuffer struct {
	mu    sync.RWMutex
	limit int
	data  []byte
}

func newTailBuffer(limit int) *tailBuffer {
	if limit <= 0 {
		limit = DefaultStderrBytes
	}
	return &tailBuffer{limit: limit}
}

func (buffer *tailBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	written := len(data)
	if len(data) >= buffer.limit {
		buffer.data = append(buffer.data[:0], data[len(data)-buffer.limit:]...)
		return written, nil
	}
	if overflow := len(buffer.data) + len(data) - buffer.limit; overflow > 0 {
		copy(buffer.data, buffer.data[overflow:])
		buffer.data = buffer.data[:len(buffer.data)-overflow]
	}
	buffer.data = append(buffer.data, data...)
	return written, nil
}

func (buffer *tailBuffer) String() string {
	buffer.mu.RLock()
	defer buffer.mu.RUnlock()
	return string(append([]byte(nil), buffer.data...))
}
