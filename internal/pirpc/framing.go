package pirpc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const readerBufferBytes = 32 << 10

func EncodeRecord(value any, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxRecordBytes
	}

	record, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode pi RPC record: %w", err)
	}
	if len(record) > maxBytes {
		return nil, ErrRecordTooLarge
	}

	return append(record, '\n'), nil
}

// ReadRecords implements Pi's strict LF-only JSONL framing. Unicode line
// separators remain part of the JSON payload and a trailing CR is tolerated.
func ReadRecords(reader io.Reader, maxBytes int, handle func([]byte) error) error {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxRecordBytes
	}

	buffered := bufio.NewReaderSize(reader, readerBufferBytes)
	record := make([]byte, 0, readerBufferBytes)

	for {
		fragment, err := buffered.ReadSlice('\n')
		record = append(record, fragment...)

		if len(record) > maxBytes+1 {
			return ErrRecordTooLarge
		}

		switch {
		case err == nil:
			record = bytes.TrimSuffix(record, []byte{'\n'})
			record = bytes.TrimSuffix(record, []byte{'\r'})
			if len(record) > maxBytes {
				return ErrRecordTooLarge
			}
			if handleErr := handle(record); handleErr != nil {
				return handleErr
			}
			record = record[:0]
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if len(record) == 0 {
				return nil
			}
			record = bytes.TrimSuffix(record, []byte{'\r'})
			if len(record) > maxBytes {
				return ErrRecordTooLarge
			}
			if handleErr := handle(record); handleErr != nil {
				return handleErr
			}
			return nil
		default:
			return fmt.Errorf("read pi RPC stream: %w", err)
		}
	}
}
