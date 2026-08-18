package pirpc

import (
	"encoding/json"
	"errors"
	"fmt"
)

const (
	DefaultMaxRecordBytes = 8 << 20
	DefaultStderrBytes    = 64 << 10
)

var (
	ErrClientClosed   = errors.New("pi RPC client is closed")
	ErrRecordTooLarge = errors.New("pi RPC record exceeds the configured limit")
	ErrInvalidCommand = errors.New("pi RPC command requires a non-empty type")
)

type Response struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Command string          `json:"command"`
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
}

type RemoteError struct {
	Command string
	Message string
}

func (err *RemoteError) Error() string {
	if err.Message == "" {
		return fmt.Sprintf("pi RPC command %q failed", err.Command)
	}
	return fmt.Sprintf("pi RPC command %q failed: %s", err.Command, err.Message)
}

type Event struct {
	Generation uint64          `json:"generation"`
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	Record     string          `json:"record,omitempty"`
	Error      string          `json:"error,omitempty"`
}

type callResult struct {
	response Response
	err      error
}
