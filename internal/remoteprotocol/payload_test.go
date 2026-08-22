package remoteprotocol

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestPayloadRoundTripAndStrictDecoding(t *testing.T) {
	type payloadValue struct {
		Name string `json:"name"`
	}
	encoded, err := EncodePayload(payloadValue{Name: "helper"})
	if err != nil {
		t.Fatalf("EncodePayload returned an error: %v", err)
	}
	var decoded payloadValue
	if err := DecodePayload(encoded, &decoded); err != nil {
		t.Fatalf("DecodePayload returned an error: %v", err)
	}
	if decoded.Name != "helper" {
		t.Fatalf("decoded payload = %#v", decoded)
	}

	for _, payload := range []json.RawMessage{
		nil,
		json.RawMessage(`{"name":"helper","unknown":true}`),
		json.RawMessage(`{"name":"helper"} {}`),
	} {
		if err := DecodePayload(payload, &decoded); !errors.Is(err, ErrInvalidEnvelope) {
			t.Fatalf("DecodePayload(%q) error = %v, want ErrInvalidEnvelope", payload, err)
		}
	}
}
