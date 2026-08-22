package remotehelper

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

	"pi-desk/internal/remoteprotocol"
)

const (
	MethodBashRun       = "bash.run"
	maxBashCommandBytes = 64 << 10
	maxBashOutputBytes  = 16 << 20
	maxBashChunkBytes   = 64 << 10
)

var (
	ErrBashInvalid     = errors.New("remote Bash request is invalid")
	ErrBashStart       = errors.New("remote Bash process could not start")
	ErrBashOutputLimit = errors.New("remote Bash output exceeds the safety limit")
)

type BashRunRequest struct {
	RootHandle string `json:"rootHandle"`
	Command    string `json:"command"`
}

type BashRunResponse struct {
	ExitCode    int   `json:"exitCode"`
	OutputBytes int64 `json:"outputBytes"`
}

func (server *Server) runBash(request *helperRequest, envelope remoteprotocol.Envelope) requestTerminal {
	var input BashRunRequest
	if remoteprotocol.DecodePayload(envelope.Payload, &input) != nil || !validBashCommand(input.Command) {
		return bashErrorTerminal(ErrBashInvalid)
	}
	roots, ok := server.roots.(*rootManager)
	if !ok {
		return bashErrorTerminal(ErrBashStart)
	}
	capability, err := roots.lookup(input.RootHandle)
	if err != nil || !sameCapabilityRoot(capability) {
		return bashErrorTerminal(ErrBashInvalid)
	}
	shell, args := bashCommand(input.Command)
	command := exec.Command(shell, args...)
	command.Dir = capability.canonical
	command.Env = remoteProcessEnvironment()
	configureProcessGroup(command)
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		return bashErrorTerminal(ErrBashStart)
	}
	defer readPipe.Close()
	command.Stdout = writePipe
	command.Stderr = writePipe
	if err := command.Start(); err != nil {
		_ = writePipe.Close()
		return bashErrorTerminal(ErrBashStart)
	}
	_ = writePipe.Close()
	processID, err := newProcessID()
	if err != nil {
		terminateProcessGroup(command.Process.Pid)
		_ = command.Wait()
		return bashErrorTerminal(ErrBashStart)
	}
	if err := server.writeEvent(envelope, remoteprotocol.MethodProcessAccepted, remoteprotocol.ProcessAccepted{ProcessID: processID}, nil); err != nil {
		terminateProcessGroup(command.Process.Pid)
		_ = command.Wait()
		return requestTerminal{code: "REMOTE_OUTCOME_UNKNOWN", message: ErrMutationOutcomeUnknown.Error(), outcomeUnknown: true}
	}
	stopRead := context.AfterFunc(request.ctx, func() {
		_ = readPipe.Close()
		terminateProcessGroup(command.Process.Pid)
	})
	defer stopRead()
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	buffer := make([]byte, maxBashChunkBytes)
	var outputBytes int64
	sequence := uint64(0)
	for {
		count, readErr := readPipe.Read(buffer)
		if count > 0 {
			outputBytes += int64(count)
			if outputBytes > maxBashOutputBytes {
				terminateProcessGroup(command.Process.Pid)
				<-wait
				return bashErrorTerminal(ErrBashOutputLimit)
			}
			if err := request.takeCredit(request.ctx, count); err != nil {
				terminateProcessGroup(command.Process.Pid)
				<-wait
				return cancelledTerminal()
			}
			sequence++
			if err := server.writeEvent(envelope, remoteprotocol.MethodStreamData, remoteprotocol.StreamData{Stream: "combined", Sequence: sequence}, buffer[:count]); err != nil {
				terminateProcessGroup(command.Process.Pid)
				<-wait
				return requestTerminal{code: "REMOTE_OUTCOME_UNKNOWN", message: ErrMutationOutcomeUnknown.Error(), outcomeUnknown: true}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) && request.ctx.Err() == nil {
				terminateProcessGroup(command.Process.Pid)
				<-wait
				return bashErrorTerminal(ErrBashStart)
			}
			break
		}
	}
	waitErr := <-wait
	if request.ctx.Err() != nil {
		terminateProcessGroup(command.Process.Pid)
		return cancelledTerminal()
	}
	exitCode := 0
	if waitErr != nil {
		var exitError *exec.ExitError
		if !errors.As(waitErr, &exitError) {
			return bashErrorTerminal(ErrBashStart)
		}
		exitCode = exitError.ExitCode()
	}
	if !sameCapabilityRoot(capability) {
		return requestTerminal{code: "REMOTE_OUTCOME_UNKNOWN", message: ErrMutationOutcomeUnknown.Error(), outcomeUnknown: true}
	}
	return requestTerminal{value: BashRunResponse{ExitCode: exitCode, OutputBytes: outputBytes}}
}

func validBashCommand(value string) bool {
	return value != "" && len(value) <= maxBashCommandBytes && utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func bashCommand(command string) (string, []string) {
	if info, err := os.Stat("/bin/bash"); err == nil && info.Mode().IsRegular() {
		return "/bin/bash", []string{"--noprofile", "--norc", "-c", command}
	}
	return "/bin/sh", []string{"-c", command}
}

func remoteProcessEnvironment() []string {
	environment := []string{"PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin", "LC_ALL=C.UTF-8", "LANG=C.UTF-8"}
	for _, name := range []string{"HOME", "USER", "LOGNAME", "SHELL", "TMPDIR"} {
		if value := os.Getenv(name); value != "" && !strings.ContainsRune(value, 0) {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}

func newProcessID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "process-" + hex.EncodeToString(value[:]), nil
}

func (server *Server) writeEvent(request remoteprotocol.Envelope, method string, value any, blob []byte) error {
	payload, err := remoteprotocol.EncodePayload(value)
	if err != nil {
		return err
	}
	return server.writer.Write(remoteprotocol.Envelope{
		Version: remoteprotocol.Version, Kind: remoteprotocol.KindEvent,
		ID: request.ID, Generation: request.Generation, Method: method, Payload: payload,
	}, blob)
}

func bashErrorTerminal(err error) requestTerminal {
	code := "REMOTE_BASH_START_FAILED"
	switch {
	case errors.Is(err, ErrBashInvalid):
		code = "REMOTE_INVALID_REQUEST"
	case errors.Is(err, ErrBashOutputLimit):
		code = "REMOTE_OUTPUT_LIMIT"
	}
	return requestTerminal{code: code, message: err.Error()}
}

func (request *helperRequest) pushCredit(value int64) error {
	request.creditMu.Lock()
	if value <= 0 || request.creditBalance > int64(remoteprotocol.MaxStreamChunkBytes)-value {
		request.creditMu.Unlock()
		return ErrUnexpectedMessage
	}
	request.creditBalance += value
	request.creditMu.Unlock()
	select {
	case request.creditReady <- struct{}{}:
	default:
	}
	return nil
}

func (request *helperRequest) takeCredit(ctx context.Context, bytes int) error {
	needed := int64(bytes)
	for {
		request.creditMu.Lock()
		if request.creditBalance >= needed {
			request.creditBalance -= needed
			request.creditMu.Unlock()
			return nil
		}
		request.creditMu.Unlock()
		select {
		case <-request.creditReady:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
