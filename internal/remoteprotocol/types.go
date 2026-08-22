package remoteprotocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"
)

const (
	Version                = 1
	DefaultMaxControlBytes = 1 << 20
	DefaultMaxBlobBytes    = 16 << 20
	maxIDBytes             = 256
	maxMethodBytes         = 128
	maxErrorCodeBytes      = 128
	maxErrorMessageBytes   = 8 << 10
	maxTimeoutMillis       = 24 * 60 * 60 * 1000
)

var (
	ErrInvalidEnvelope    = errors.New("invalid remote protocol envelope")
	ErrUnsupportedVersion = errors.New("unsupported remote protocol version")
	ErrControlTooLarge    = errors.New("remote protocol control frame exceeds the safety limit")
	ErrBlobTooLarge       = errors.New("remote protocol blob exceeds the safety limit")
)

type Kind string

const (
	KindRequest  Kind = "request"
	KindResponse Kind = "response"
	KindError    Kind = "error"
	KindEvent    Kind = "event"
	KindCancel   Kind = "cancel"
)

type Envelope struct {
	Version       uint16          `json:"version"`
	Kind          Kind            `json:"kind"`
	ID            string          `json:"id,omitempty"`
	Generation    uint64          `json:"generation"`
	Method        string          `json:"method,omitempty"`
	TimeoutMillis uint32          `json:"timeoutMillis,omitempty"`
	Payload       json.RawMessage `json:"payload,omitempty"`
	BlobLength    uint32          `json:"blobLength,omitempty"`
	Error         *RemoteError    `json:"error,omitempty"`
}

type RemoteError struct {
	Code           string `json:"code"`
	Message        string `json:"message"`
	OutcomeUnknown bool   `json:"outcomeUnknown,omitempty"`
}

type Frame struct {
	Envelope Envelope
	Blob     []byte
}

type VersionError struct {
	Got  uint16
	Want uint16
}

func (err *VersionError) Error() string {
	return fmt.Sprintf("remote protocol version %d is not supported; expected %d", err.Got, err.Want)
}

func (err *VersionError) Unwrap() error {
	return ErrUnsupportedVersion
}

func ValidateEnvelope(envelope Envelope) error {
	if envelope.Version != Version {
		return &VersionError{Got: envelope.Version, Want: Version}
	}
	if envelope.Generation == 0 {
		return fmt.Errorf("%w: generation is required", ErrInvalidEnvelope)
	}
	if envelope.TimeoutMillis > maxTimeoutMillis {
		return fmt.Errorf("%w: timeout exceeds the safety limit", ErrInvalidEnvelope)
	}
	if err := validateToken("id", envelope.ID, maxIDBytes); err != nil {
		return err
	}
	if err := validateToken("method", envelope.Method, maxMethodBytes); err != nil {
		return err
	}
	if len(envelope.Payload) > 0 && !json.Valid(envelope.Payload) {
		return fmt.Errorf("%w: payload is not valid JSON", ErrInvalidEnvelope)
	}

	switch envelope.Kind {
	case KindRequest:
		if envelope.ID == "" || envelope.Method == "" || envelope.Error != nil {
			return fmt.Errorf("%w: request requires id and method", ErrInvalidEnvelope)
		}
	case KindResponse:
		if envelope.ID == "" || envelope.Method != "" || envelope.Error != nil || envelope.TimeoutMillis != 0 {
			return fmt.Errorf("%w: response requires only an id", ErrInvalidEnvelope)
		}
	case KindError:
		if envelope.ID == "" || envelope.Method != "" || envelope.Error == nil || envelope.BlobLength != 0 || envelope.TimeoutMillis != 0 {
			return fmt.Errorf("%w: error requires id and error details without a blob", ErrInvalidEnvelope)
		}
		if err := validateRemoteError(*envelope.Error); err != nil {
			return err
		}
	case KindEvent:
		if envelope.Method == "" || envelope.Error != nil || envelope.TimeoutMillis != 0 {
			return fmt.Errorf("%w: event requires a method", ErrInvalidEnvelope)
		}
	case KindCancel:
		if envelope.ID == "" || envelope.Method != "" || envelope.Error != nil || len(envelope.Payload) != 0 || envelope.BlobLength != 0 || envelope.TimeoutMillis != 0 {
			return fmt.Errorf("%w: cancel requires only an id", ErrInvalidEnvelope)
		}
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidEnvelope, envelope.Kind)
	}
	return nil
}

func validateRemoteError(remoteError RemoteError) error {
	if err := validateRequiredToken("error code", remoteError.Code, maxErrorCodeBytes); err != nil {
		return err
	}
	if remoteError.Message == "" {
		return fmt.Errorf("%w: error message is required", ErrInvalidEnvelope)
	}
	if !utf8.ValidString(remoteError.Message) || len(remoteError.Message) > maxErrorMessageBytes {
		return fmt.Errorf("%w: error message is invalid or too large", ErrInvalidEnvelope)
	}
	return nil
}

func validateRequiredToken(name, value string, maxBytes int) error {
	if value == "" {
		return fmt.Errorf("%w: %s is required", ErrInvalidEnvelope, name)
	}
	return validateToken(name, value, maxBytes)
}

func validateToken(name, value string, maxBytes int) error {
	if value == "" {
		return nil
	}
	if !utf8.ValidString(value) || len(value) > maxBytes {
		return fmt.Errorf("%w: %s is invalid or too large", ErrInvalidEnvelope, name)
	}
	for _, char := range value {
		if unicode.IsControl(char) || unicode.IsSpace(char) {
			return fmt.Errorf("%w: %s contains whitespace or control characters", ErrInvalidEnvelope, name)
		}
	}
	return nil
}
