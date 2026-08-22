package remoteprotocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

func DecodePayload(payload json.RawMessage, target any) error {
	if len(payload) == 0 {
		return fmt.Errorf("%w: payload is required", ErrInvalidEnvelope)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: decode payload: %v", ErrInvalidEnvelope, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%w: payload contains trailing JSON", ErrInvalidEnvelope)
		}
		return fmt.Errorf("%w: decode payload trailing data: %v", ErrInvalidEnvelope, err)
	}
	return nil
}

func EncodePayload(value any) (json.RawMessage, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode remote protocol payload: %w", err)
	}
	return payload, nil
}
