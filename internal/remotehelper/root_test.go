package remotehelper

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateRootPath(t *testing.T) {
	for _, valid := range []string{"/", "/srv/repo", "/path with spaces/repo", "/é/repo"} {
		if err := validateRootPath(valid); err != nil {
			t.Fatalf("valid path %q: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", ".", "relative", "/srv/../etc", "/srv//repo", `C:\repo`, `/srv\repo`, "/bad\x00path", "/bad\npath", "/bad\u202erepo", "/" + strings.Repeat("a", maxRootPathBytes)} {
		if err := validateRootPath(invalid); !errors.Is(err, ErrRootInvalid) {
			t.Fatalf("invalid path %q: %v", invalid, err)
		}
	}
}

func FuzzValidateRootPath(f *testing.F) {
	for _, seed := range []string{"/", "/srv/repo", "../escape", `C:\\repo`, "/bad\x00path", "/é/repo"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if validateRootPath(value) == nil && (value == "" || len(value) > maxRootPathBytes || !strings.HasPrefix(value, "/") || strings.Contains(value, "\\")) {
			t.Fatalf("accepted invalid root path %q", value)
		}
	})
}

func TestValidateRelativePathAndHandle(t *testing.T) {
	for _, valid := range []string{".", "file.txt", "dir/file.txt", "path with spaces/é.txt"} {
		if err := validateRelativePath(valid, true); err != nil {
			t.Fatalf("valid relative path %q: %v", valid, err)
		}
	}
	if err := validateRelativePath(".", false); !errors.Is(err, ErrFileInvalidPath) {
		t.Fatalf("file root path error = %v", err)
	}
	for _, invalid := range []string{"", "/absolute", "../escape", "dir/../file", "dir//file", `dir\\file`, "bad\x00file", "bad\u202efile", strings.Repeat("a", maxPathComponentBytes+1)} {
		if err := validateRelativePath(invalid, true); !errors.Is(err, ErrFileInvalidPath) {
			t.Fatalf("invalid relative path %q: %v", invalid, err)
		}
	}
	if !validRootHandle("root-0123456789abcdef0123456789abcdef") || validRootHandle("root-ABC") {
		t.Fatal("root handle validation mismatch")
	}
}

func TestValidateWriteRequestRequiresOneCondition(t *testing.T) {
	validHandle := "root-0123456789abcdef0123456789abcdef"
	validHash := strings.Repeat("a", 64)
	for _, request := range []FileWriteRequest{
		{RootHandle: validHandle, Path: "new.txt", ExpectedAbsent: true},
		{RootHandle: validHandle, Path: "existing.txt", ExpectedSHA256: validHash},
	} {
		if err := validateWriteRequest(request, []byte("content")); err != nil {
			t.Fatalf("valid write rejected: %#v: %v", request, err)
		}
	}
	for _, request := range []FileWriteRequest{
		{RootHandle: validHandle, Path: "file.txt"},
		{RootHandle: validHandle, Path: "file.txt", ExpectedAbsent: true, ExpectedSHA256: validHash},
		{RootHandle: validHandle, Path: "file.txt", ExpectedSHA256: "ABC"},
		{RootHandle: validHandle, Path: "../file.txt", ExpectedAbsent: true},
	} {
		if err := validateWriteRequest(request, nil); !errors.Is(err, ErrFileInvalidPath) {
			t.Fatalf("invalid write accepted: %#v", request)
		}
	}
	if err := validateWriteRequest(FileWriteRequest{RootHandle: validHandle, Path: "large", ExpectedAbsent: true}, make([]byte, maxReadableFileBytes+1)); !errors.Is(err, ErrFileInvalidPath) {
		t.Fatalf("oversized write error=%v", err)
	}
}

func TestImageMIMEUsesFixedMagicBytes(t *testing.T) {
	for name, test := range map[string]struct {
		content []byte
		mime    string
	}{
		"png":       {content: []byte("\x89PNG\r\n\x1a\nrest"), mime: "image/png"},
		"jpeg":      {content: []byte{0xff, 0xd8, 0xff, 0x00}, mime: "image/jpeg"},
		"gif87":     {content: []byte("GIF87arest"), mime: "image/gif"},
		"gif89":     {content: []byte("GIF89arest"), mime: "image/gif"},
		"webp":      {content: []byte("RIFFxxxxWEBPrest"), mime: "image/webp"},
		"bmp":       {content: []byte("BMrest"), mime: "image/bmp"},
		"svg":       {content: []byte("<svg></svg>"), mime: ""},
		"truncated": {content: []byte("RIFF"), mime: ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := imageMIME(test.content); got != test.mime {
				t.Fatalf("imageMIME=%q, want %q", got, test.mime)
			}
		})
	}
}

func FuzzImageMIMENeverPanics(f *testing.F) {
	f.Add([]byte("\x89PNG\r\n\x1a\n"))
	f.Add([]byte("RIFFxxxxWEBP"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, content []byte) { _ = imageMIME(content) })
}

func FuzzValidateRelativePath(f *testing.F) {
	for _, seed := range []string{".", "file", "dir/file", "../escape", "/absolute", `C:\\repo`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if validateRelativePath(value, true) == nil && value != "." && (value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\\")) {
			t.Fatalf("accepted invalid relative path %q", value)
		}
	})
}

func TestNewRootHandleIsBoundedAndRandom(t *testing.T) {
	first, err := newRootHandle()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newRootHandle()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len("root-")+32 || first == second || strings.ToLower(first) != first {
		t.Fatalf("root handles: %q %q", first, second)
	}
}
