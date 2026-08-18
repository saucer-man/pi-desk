package pirpc

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestReadRecordsUsesLFOnlyFraming(t *testing.T) {
	input := "{\"text\":\"before\u2028after\"}\r\n{\"ok\":true}"
	var records []string

	err := ReadRecords(strings.NewReader(input), 1024, func(record []byte) error {
		records = append(records, string(record))
		return nil
	})

	if err != nil {
		t.Fatalf("ReadRecords returned an error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0] != "{\"text\":\"before\u2028after\"}" {
		t.Fatalf("unexpected first record: %q", records[0])
	}
	if records[1] != "{\"ok\":true}" {
		t.Fatalf("unexpected second record: %q", records[1])
	}
}

func TestReadRecordsRejectsOversizedRecordAcrossBufferFragments(t *testing.T) {
	input := bytes.NewBufferString(strings.Repeat("x", readerBufferBytes+20) + "\n")

	err := ReadRecords(input, readerBufferBytes, func([]byte) error {
		t.Fatal("oversized record reached the handler")
		return nil
	})

	if !errors.Is(err, ErrRecordTooLarge) {
		t.Fatalf("expected ErrRecordTooLarge, got %v", err)
	}
}

func TestReadRecordsPreservesEmptyRecordForProtocolValidation(t *testing.T) {
	called := false
	err := ReadRecords(strings.NewReader("\n"), 16, func(record []byte) error {
		called = true
		if len(record) != 0 {
			t.Fatalf("expected an empty record, got %q", record)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ReadRecords returned an error: %v", err)
	}
	if !called {
		t.Fatal("empty record was not passed to protocol validation")
	}
}

func TestEncodeRecordAppendsSingleLFAndEnforcesLimit(t *testing.T) {
	record, err := EncodeRecord(map[string]string{"type": "get_state"}, 128)
	if err != nil {
		t.Fatalf("EncodeRecord returned an error: %v", err)
	}
	if !bytes.HasSuffix(record, []byte{'\n'}) || bytes.HasSuffix(record, []byte("\n\n")) {
		t.Fatalf("record is not single-LF terminated: %q", record)
	}

	_, err = EncodeRecord(map[string]string{"message": strings.Repeat("x", 128)}, 32)
	if !errors.Is(err, ErrRecordTooLarge) {
		t.Fatalf("expected ErrRecordTooLarge, got %v", err)
	}
}
