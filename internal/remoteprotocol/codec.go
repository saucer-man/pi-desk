package remoteprotocol

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
)

type Limits struct {
	MaxControlBytes int
	MaxBlobBytes    int
}

func (limits Limits) normalized() Limits {
	if limits.MaxControlBytes <= 0 {
		limits.MaxControlBytes = DefaultMaxControlBytes
	}
	if limits.MaxBlobBytes <= 0 {
		limits.MaxBlobBytes = DefaultMaxBlobBytes
	}
	return limits
}

type Reader struct {
	reader io.Reader
	limits Limits
	header [4]byte
}

func NewReader(reader io.Reader, limits Limits) *Reader {
	return &Reader{reader: reader, limits: limits.normalized()}
}

func (reader *Reader) Read() (Frame, error) {
	readBytes, err := io.ReadFull(reader.reader, reader.header[:])
	if err != nil {
		if errors.Is(err, io.EOF) && readBytes == 0 {
			return Frame{}, io.EOF
		}
		return Frame{}, fmt.Errorf("read remote protocol frame length: %w", err)
	}

	controlLength := binary.BigEndian.Uint32(reader.header[:])
	if int64(controlLength) > int64(reader.limits.MaxControlBytes) {
		return Frame{}, ErrControlTooLarge
	}
	if controlLength == 0 {
		return Frame{}, fmt.Errorf("%w: control frame is empty", ErrInvalidEnvelope)
	}

	control := make([]byte, int(controlLength))
	if _, err := io.ReadFull(reader.reader, control); err != nil {
		return Frame{}, fmt.Errorf("read remote protocol control frame: %w", err)
	}

	var envelope Envelope
	decoder := json.NewDecoder(bytes.NewReader(control))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return Frame{}, fmt.Errorf("decode remote protocol envelope: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Frame{}, fmt.Errorf("%w: control frame contains trailing JSON", ErrInvalidEnvelope)
		}
		return Frame{}, fmt.Errorf("decode remote protocol trailing data: %w", err)
	}
	if err := ValidateEnvelope(envelope); err != nil {
		return Frame{}, err
	}
	if int64(envelope.BlobLength) > int64(reader.limits.MaxBlobBytes) {
		return Frame{}, ErrBlobTooLarge
	}

	blob := make([]byte, int(envelope.BlobLength))
	if len(blob) > 0 {
		if _, err := io.ReadFull(reader.reader, blob); err != nil {
			return Frame{}, fmt.Errorf("read remote protocol blob: %w", err)
		}
	}
	return Frame{Envelope: envelope, Blob: blob}, nil
}

type Writer struct {
	writer io.Writer
	limits Limits
	mu     sync.Mutex
	header [4]byte
}

func NewWriter(writer io.Writer, limits Limits) *Writer {
	return &Writer{writer: writer, limits: limits.normalized()}
}

func (writer *Writer) Write(envelope Envelope, blob []byte) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	if len(blob) > writer.limits.MaxBlobBytes || uint64(len(blob)) > math.MaxUint32 {
		return ErrBlobTooLarge
	}
	envelope.BlobLength = uint32(len(blob))
	if err := ValidateEnvelope(envelope); err != nil {
		return err
	}
	control, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode remote protocol envelope: %w", err)
	}
	if len(control) > writer.limits.MaxControlBytes || uint64(len(control)) > math.MaxUint32 {
		return ErrControlTooLarge
	}

	binary.BigEndian.PutUint32(writer.header[:], uint32(len(control)))
	if err := writeAll(writer.writer, writer.header[:]); err != nil {
		return fmt.Errorf("write remote protocol frame length: %w", err)
	}
	if err := writeAll(writer.writer, control); err != nil {
		return fmt.Errorf("write remote protocol control frame: %w", err)
	}
	if len(blob) > 0 {
		if err := writeAll(writer.writer, blob); err != nil {
			return fmt.Errorf("write remote protocol blob: %w", err)
		}
	}
	return nil
}

func writeAll(writer io.Writer, value []byte) error {
	for len(value) > 0 {
		written, err := writer.Write(value)
		if err != nil {
			return err
		}
		if written <= 0 || written > len(value) {
			return io.ErrShortWrite
		}
		value = value[written:]
	}
	return nil
}
