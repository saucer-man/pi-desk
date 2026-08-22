package remotehelper

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	ptylib "github.com/aymanbagabas/go-pty"
	"pi-desk/internal/remoteprotocol"
)

const (
	MethodTerminalRun     = "terminal.run"
	maxTerminalInputBytes = 64 << 10
	terminalChunkBytes    = 32 << 10
)

var (
	ErrTerminalInvalid = errors.New("remote Terminal request is invalid")
	ErrTerminalStart   = errors.New("remote Terminal could not start")
)

type TerminalRunRequest struct {
	RootHandle string `json:"rootHandle"`
	Columns    int    `json:"columns"`
	Rows       int    `json:"rows"`
}

type TerminalRunResponse struct {
	ExitCode    int   `json:"exitCode"`
	OutputBytes int64 `json:"outputBytes"`
}

type terminalControl struct {
	input   []byte
	columns int
	rows    int
}

func (request *helperRequest) pushTerminal(control terminalControl) error {
	control.input = append([]byte(nil), control.input...)
	select {
	case request.terminal <- control:
		return nil
	default:
		return ErrUnexpectedMessage
	}
}

func (server *Server) runTerminal(request *helperRequest, envelope remoteprotocol.Envelope) requestTerminal {
	var input TerminalRunRequest
	if remoteprotocol.DecodePayload(envelope.Payload, &input) != nil || !validTerminalDimensions(input.Columns, input.Rows) {
		return terminalError(ErrTerminalInvalid)
	}
	roots, ok := server.roots.(*rootManager)
	if !ok {
		return terminalError(ErrTerminalStart)
	}
	capability, err := roots.lookup(input.RootHandle)
	if err != nil || !sameCapabilityRoot(capability) {
		return terminalError(ErrTerminalInvalid)
	}
	pseudoterminal, err := ptylib.New()
	if err != nil {
		return terminalError(ErrTerminalStart)
	}
	defer pseudoterminal.Close()
	if err := pseudoterminal.Resize(input.Columns, input.Rows); err != nil {
		return terminalError(ErrTerminalStart)
	}
	shell := remoteLoginShell()
	command := pseudoterminal.Command(shell)
	command.Dir = capability.canonical
	command.Env = append(remoteProcessEnvironment(), "TERM=xterm-256color")
	if err := command.Start(); err != nil {
		return terminalError(ErrTerminalStart)
	}
	if unixPTY, ok := pseudoterminal.(ptylib.UnixPty); ok {
		_ = unixPTY.Slave().Close()
	}
	processID, err := newProcessID()
	if err != nil {
		terminateTerminalGroup(command.Process.Pid)
		_ = command.Wait()
		return terminalError(ErrTerminalStart)
	}
	if err := server.writeEvent(envelope, remoteprotocol.MethodProcessAccepted, remoteprotocol.ProcessAccepted{ProcessID: processID}, nil); err != nil {
		terminateTerminalGroup(command.Process.Pid)
		_ = command.Wait()
		return requestTerminal{code: "REMOTE_OUTCOME_UNKNOWN", message: ErrMutationOutcomeUnknown.Error(), outcomeUnknown: true}
	}
	stop := make(chan struct{})
	controlDone := make(chan error, 1)
	go func() {
		for {
			select {
			case control := <-request.terminal:
				if len(control.input) > 0 {
					if _, err := pseudoterminal.Write(control.input); err != nil {
						controlDone <- err
						return
					}
				} else if err := pseudoterminal.Resize(control.columns, control.rows); err != nil {
					controlDone <- err
					return
				}
			case <-stop:
				controlDone <- nil
				return
			case <-request.ctx.Done():
				controlDone <- request.ctx.Err()
				return
			}
		}
	}()
	stopProcess := context.AfterFunc(request.ctx, func() {
		terminateTerminalGroup(command.Process.Pid)
	})
	defer stopProcess()
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	buffer := make([]byte, terminalChunkBytes)
	sequence := uint64(0)
	var outputBytes int64
	for {
		count, readErr := pseudoterminal.Read(buffer)
		if count > 0 {
			outputBytes += int64(count)
			if err := request.takeCredit(request.ctx, count); err != nil {
				terminateTerminalGroup(command.Process.Pid)
				<-wait
				close(stop)
				<-controlDone
				return cancelledTerminal()
			}
			sequence++
			if err := server.writeEvent(envelope, remoteprotocol.MethodStreamData, remoteprotocol.StreamData{Stream: "terminal", Sequence: sequence}, buffer[:count]); err != nil {
				terminateTerminalGroup(command.Process.Pid)
				<-wait
				close(stop)
				<-controlDone
				return requestTerminal{code: "REMOTE_OUTCOME_UNKNOWN", message: ErrMutationOutcomeUnknown.Error(), outcomeUnknown: true}
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) && !errors.Is(readErr, syscall.EIO) && request.ctx.Err() == nil {
				terminateTerminalGroup(command.Process.Pid)
			}
			break
		}
	}
	waitErr := <-wait
	close(stop)
	controlErr := <-controlDone
	if request.ctx.Err() != nil {
		return cancelledTerminal()
	}
	if controlErr != nil {
		return terminalError(ErrTerminalStart)
	}
	exitCode := 0
	if waitErr != nil {
		var exitError interface{ ExitCode() int }
		if !errors.As(waitErr, &exitError) {
			return terminalError(ErrTerminalStart)
		}
		exitCode = exitError.ExitCode()
	}
	if !sameCapabilityRoot(capability) {
		return requestTerminal{code: "REMOTE_OUTCOME_UNKNOWN", message: ErrMutationOutcomeUnknown.Error(), outcomeUnknown: true}
	}
	return requestTerminal{value: TerminalRunResponse{ExitCode: exitCode, OutputBytes: outputBytes}}
}

func validTerminalDimensions(columns, rows int) bool {
	return columns >= 20 && columns <= 500 && rows >= 5 && rows <= 300
}

func remoteLoginShell() string {
	value := os.Getenv("SHELL")
	if filepath.IsAbs(value) && len(value) <= 4096 && utf8.ValidString(value) && !strings.ContainsRune(value, 0) {
		if info, err := os.Stat(value); err == nil && info.Mode().IsRegular() {
			return value
		}
	}
	return "/bin/sh"
}

func terminalError(err error) requestTerminal {
	code := "REMOTE_TERMINAL_START_FAILED"
	if errors.Is(err, ErrTerminalInvalid) {
		code = "REMOTE_INVALID_REQUEST"
	}
	return requestTerminal{code: code, message: err.Error()}
}
