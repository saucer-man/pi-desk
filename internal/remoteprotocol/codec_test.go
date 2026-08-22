package remoteprotocol

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

func requestEnvelope() Envelope {
	return Envelope{
		Version:    Version,
		Kind:       KindRequest,
		ID:         "request-1",
		Generation: 7,
		Method:     "system.hello",
		Payload:    json.RawMessage(`{"nonce":"abc"}`),
	}
}

func TestFrameRoundTripWithBlob(t *testing.T) {
	var transport bytes.Buffer
	writer := NewWriter(&transport, Limits{})
	blob := []byte{0, 1, 2, 3, 255}
	if err := writer.Write(requestEnvelope(), blob); err != nil {
		t.Fatalf("Write returned an error: %v", err)
	}

	frame, err := NewReader(&transport, Limits{}).Read()
	if err != nil {
		t.Fatalf("Read returned an error: %v", err)
	}
	if frame.Envelope.ID != "request-1" || frame.Envelope.BlobLength != uint32(len(blob)) {
		t.Fatalf("unexpected envelope: %#v", frame.Envelope)
	}
	if !bytes.Equal(frame.Blob, blob) {
		t.Fatalf("blob = %v, want %v", frame.Blob, blob)
	}
}

func TestFrameRoundTripHandlesShortReadsAndWrites(t *testing.T) {
	var transport bytes.Buffer
	writer := NewWriter(&shortWriter{writer: &transport, maximum: 2}, Limits{})
	if err := writer.Write(requestEnvelope(), []byte("blob")); err != nil {
		t.Fatalf("Write returned an error: %v", err)
	}

	frame, err := NewReader(&shortReader{reader: bytes.NewReader(transport.Bytes()), maximum: 1}, Limits{}).Read()
	if err != nil {
		t.Fatalf("Read returned an error: %v", err)
	}
	if string(frame.Blob) != "blob" {
		t.Fatalf("blob = %q", frame.Blob)
	}
}

func TestReaderRejectsOversizedControlBeforeAllocation(t *testing.T) {
	var transport bytes.Buffer
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], 65)
	transport.Write(header[:])

	_, err := NewReader(&transport, Limits{MaxControlBytes: 64, MaxBlobBytes: 64}).Read()
	if !errors.Is(err, ErrControlTooLarge) {
		t.Fatalf("Read error = %v, want ErrControlTooLarge", err)
	}
}

func TestReaderRejectsOversizedBlobBeforeReadingIt(t *testing.T) {
	envelope := requestEnvelope()
	envelope.BlobLength = 65
	transport := rawFrame(t, envelope, nil)

	_, err := NewReader(bytes.NewReader(transport), Limits{MaxControlBytes: 1024, MaxBlobBytes: 64}).Read()
	if !errors.Is(err, ErrBlobTooLarge) {
		t.Fatalf("Read error = %v, want ErrBlobTooLarge", err)
	}
}

func TestReaderRejectsTruncatedFrame(t *testing.T) {
	var transport bytes.Buffer
	if err := NewWriter(&transport, Limits{}).Write(requestEnvelope(), []byte("blob")); err != nil {
		t.Fatal(err)
	}
	value := transport.Bytes()

	_, err := NewReader(bytes.NewReader(value[:len(value)-1]), Limits{}).Read()
	if err == nil || !strings.Contains(err.Error(), "blob") {
		t.Fatalf("Read error = %v, want a truncated blob error", err)
	}
}

func TestReaderRejectsUnknownEnvelopeFields(t *testing.T) {
	control := []byte(`{"version":1,"kind":"event","generation":1,"method":"status","unknown":true}`)
	transport := rawControl(control)

	_, err := NewReader(bytes.NewReader(transport), Limits{}).Read()
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Read error = %v, want an unknown field error", err)
	}
}

func TestReaderRejectsUnsupportedVersion(t *testing.T) {
	envelope := requestEnvelope()
	envelope.Version++
	transport := rawFrame(t, envelope, nil)

	_, err := NewReader(bytes.NewReader(transport), Limits{}).Read()
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("Read error = %v, want ErrUnsupportedVersion", err)
	}
}

func TestWriterRejectsLimitsBeforeWriting(t *testing.T) {
	tests := []struct {
		name   string
		limits Limits
		value  Envelope
		blob   []byte
		want   error
	}{
		{
			name:   "control",
			limits: Limits{MaxControlBytes: 64, MaxBlobBytes: 64},
			value:  requestEnvelope(),
			want:   ErrControlTooLarge,
		},
		{
			name:   "blob",
			limits: Limits{MaxControlBytes: 1024, MaxBlobBytes: 3},
			value:  requestEnvelope(),
			blob:   []byte("four"),
			want:   ErrBlobTooLarge,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var transport bytes.Buffer
			err := NewWriter(&transport, test.limits).Write(test.value, test.blob)
			if !errors.Is(err, test.want) {
				t.Fatalf("Write error = %v, want %v", err, test.want)
			}
			if transport.Len() != 0 {
				t.Fatalf("Write emitted %d bytes before validation failed", transport.Len())
			}
		})
	}
}

func TestWriterSerializesConcurrentFrames(t *testing.T) {
	const frameCount = 64
	var transport bytes.Buffer
	writer := NewWriter(&transport, Limits{})
	errorsChannel := make(chan error, frameCount)
	var wait sync.WaitGroup
	for index := 0; index < frameCount; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			payload, _ := json.Marshal(index)
			errorsChannel <- writer.Write(Envelope{
				Version:    Version,
				Kind:       KindEvent,
				Generation: 1,
				Method:     "stream.data",
				Payload:    payload,
			}, []byte{byte(index)})
		}(index)
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("concurrent Write returned an error: %v", err)
		}
	}

	reader := NewReader(bytes.NewReader(transport.Bytes()), Limits{})
	seen := make(map[int]bool, frameCount)
	for range frameCount {
		frame, err := reader.Read()
		if err != nil {
			t.Fatalf("Read concurrent frame: %v", err)
		}
		var index int
		if err := json.Unmarshal(frame.Envelope.Payload, &index); err != nil {
			t.Fatal(err)
		}
		if len(frame.Blob) != 1 || frame.Blob[0] != byte(index) {
			t.Fatalf("frame %d has mismatched blob %v", index, frame.Blob)
		}
		seen[index] = true
	}
	if len(seen) != frameCount {
		t.Fatalf("read %d unique frames, want %d", len(seen), frameCount)
	}
	if _, err := reader.Read(); !errors.Is(err, io.EOF) {
		t.Fatalf("final Read error = %v, want EOF", err)
	}
}

func TestValidateEnvelopeKinds(t *testing.T) {
	tests := []struct {
		name  string
		value Envelope
		valid bool
	}{
		{name: "request", value: requestEnvelope(), valid: true},
		{name: "response", value: Envelope{Version: Version, Kind: KindResponse, ID: "request-1", Generation: 1}, valid: true},
		{name: "error", value: Envelope{Version: Version, Kind: KindError, ID: "request-1", Generation: 1, Error: &RemoteError{Code: "REMOTE_FAILED", Message: "failed"}}, valid: true},
		{name: "event", value: Envelope{Version: Version, Kind: KindEvent, Generation: 1, Method: "stream.open"}, valid: true},
		{name: "cancel", value: Envelope{Version: Version, Kind: KindCancel, ID: "request-1", Generation: 1}, valid: true},
		{name: "missing generation", value: Envelope{Version: Version, Kind: KindEvent, Method: "status"}},
		{name: "timeout too large", value: Envelope{Version: Version, Kind: KindEvent, Generation: 1, Method: "status", TimeoutMillis: maxTimeoutMillis + 1}},
		{name: "request without id", value: Envelope{Version: Version, Kind: KindRequest, Generation: 1, Method: "ping"}},
		{name: "cancel with payload", value: Envelope{Version: Version, Kind: KindCancel, ID: "request-1", Generation: 1, Payload: json.RawMessage(`{}`)}},
		{name: "cancel with timeout", value: Envelope{Version: Version, Kind: KindCancel, ID: "request-1", Generation: 1, TimeoutMillis: 1}},
		{name: "response with timeout", value: Envelope{Version: Version, Kind: KindResponse, ID: "request-1", Generation: 1, TimeoutMillis: 1}},
		{name: "error without details", value: Envelope{Version: Version, Kind: KindError, ID: "request-1", Generation: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateEnvelope(test.value)
			if test.valid && err != nil {
				t.Fatalf("ValidateEnvelope returned an error: %v", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidEnvelope) {
				t.Fatalf("ValidateEnvelope error = %v, want ErrInvalidEnvelope", err)
			}
		})
	}
}

func TestWriteAllRejectsNoProgress(t *testing.T) {
	err := writeAll(zeroWriter{}, []byte("value"))
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("writeAll error = %v, want io.ErrShortWrite", err)
	}
}

func FuzzReaderNeverPanics(f *testing.F) {
	f.Add([]byte{})
	f.Add(rawControl([]byte(`{"version":1,"kind":"event","generation":1,"method":"status"}`)))
	f.Add([]byte{0, 0, 4, 0})
	f.Fuzz(func(t *testing.T, value []byte) {
		_, _ = NewReader(bytes.NewReader(value), Limits{MaxControlBytes: 1024, MaxBlobBytes: 1024}).Read()
	})
}

func rawFrame(t *testing.T, envelope Envelope, blob []byte) []byte {
	t.Helper()
	control, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	result := rawControl(control)
	return append(result, blob...)
}

func rawControl(control []byte) []byte {
	result := make([]byte, 4, 4+len(control))
	binary.BigEndian.PutUint32(result, uint32(len(control)))
	return append(result, control...)
}

type shortReader struct {
	reader  io.Reader
	maximum int
}

func (reader *shortReader) Read(value []byte) (int, error) {
	if len(value) > reader.maximum {
		value = value[:reader.maximum]
	}
	return reader.reader.Read(value)
}

type shortWriter struct {
	writer  io.Writer
	maximum int
}

func (writer *shortWriter) Write(value []byte) (int, error) {
	if len(value) > writer.maximum {
		value = value[:writer.maximum]
	}
	return writer.writer.Write(value)
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) {
	return 0, nil
}
