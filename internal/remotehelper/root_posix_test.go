//go:build linux || darwin

package remotehelper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"unicode/utf8"
)

func TestRootManagerOpensCanonicalDirectoryAndReusesIdentity(t *testing.T) {
	base := t.TempDir()
	repository := filepath.Join(base, "repository")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(repository, alias); err != nil {
		t.Fatal(err)
	}
	manager := newRootManager()
	t.Cleanup(func() { _ = manager.Close() })
	first, err := manager.Open(context.Background(), filepath.ToSlash(alias))
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Open(context.Background(), filepath.ToSlash(repository))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(repository)
	if err != nil {
		t.Fatal(err)
	}
	if first.Handle == "" || first != second || first.CanonicalPath != filepath.ToSlash(canonical) || first.Device == 0 || first.Inode == 0 {
		t.Fatalf("root responses: first=%#v second=%#v", first, second)
	}
}

func TestRootManagerReadOnlyFileOperationsStayWithinRoot(t *testing.T) {
	base := t.TempDir()
	repository := filepath.Join(base, "repository")
	outside := filepath.Join(base, "outside.txt")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "alpha\nbeta\ngamma\n"
	if err := os.WriteFile(filepath.Join(repository, "notes.txt"), []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	imageContent := []byte("\x89PNG\r\n\x1a\nfixture")
	if err := os.WriteFile(filepath.Join(repository, "image.png"), imageContent, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repository, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("notes.txt", filepath.Join(repository, "inside-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repository, "outside-link")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(repository, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, string([]byte{'b', 'a', 'd', 0xff})), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	manager := newRootManager()
	defer manager.Close()
	opened, err := manager.Open(context.Background(), filepath.ToSlash(repository))
	if err != nil {
		t.Fatal(err)
	}
	stat, err := manager.Stat(context.Background(), opened.Handle, "notes.txt")
	if err != nil || stat.Kind != "file" || stat.Size != int64(len(content)) || stat.Mode != 0o640 {
		t.Fatalf("file stat=%#v err=%v", stat, err)
	}
	link, err := manager.Stat(context.Background(), opened.Handle, "inside-link")
	if err != nil || link.Kind != "symlink" {
		t.Fatalf("link stat=%#v err=%v", link, err)
	}
	listing, err := manager.List(context.Background(), opened.Handle, ".")
	if err != nil || listing.Path != "." || listing.SkippedUnsupportedPaths != 2 {
		t.Fatalf("listing=%#v err=%v", listing, err)
	}
	for _, entry := range listing.Entries {
		if entry.Path == "pipe" || !utf8.ValidString(entry.Path) {
			t.Fatalf("unsafe entry was actionable: %#v", entry)
		}
	}
	read, err := manager.Read(context.Background(), FileReadRequest{RootHandle: opened.Handle, Path: "notes.txt", StartLine: 2, MaxLines: 1})
	if err != nil || read.Content != "beta" || read.StartLine != 2 || read.EndLine != 2 || !read.Truncated || read.NextLine != 3 {
		t.Fatalf("read=%#v err=%v", read, err)
	}
	digest := sha256.Sum256([]byte(content))
	full, fullBlob, err := manager.Content(context.Background(), opened.Handle, "notes.txt")
	if err != nil || full.SHA256 != hex.EncodeToString(digest[:]) || string(fullBlob) != content {
		t.Fatalf("content=%#v blob=%q err=%v", full, fullBlob, err)
	}
	hash, err := manager.Hash(context.Background(), opened.Handle, "notes.txt")
	if err != nil || hash.SHA256 != hex.EncodeToString(digest[:]) || hash.Size != int64(len(content)) {
		t.Fatalf("hash=%#v err=%v", hash, err)
	}
	imageDigest := sha256.Sum256(imageContent)
	image, blob, err := manager.Image(context.Background(), opened.Handle, "image.png")
	if err != nil || image.Path != "image.png" || image.MIME != "image/png" || image.Size != int64(len(imageContent)) || image.SHA256 != hex.EncodeToString(imageDigest[:]) || string(blob) != string(imageContent) {
		t.Fatalf("image=%#v blob=%q err=%v", image, blob, err)
	}
	if _, err := manager.Read(context.Background(), FileReadRequest{RootHandle: opened.Handle, Path: "outside-link", StartLine: 1, MaxLines: 10}); err == nil {
		t.Fatal("read followed symlink outside root")
	}
	if _, err := manager.Hash(context.Background(), opened.Handle, "pipe"); !errors.Is(err, ErrFileUnsupported) {
		t.Fatalf("special file hash error=%v", err)
	}
	if _, _, err := manager.Content(context.Background(), opened.Handle, "outside-link"); err == nil {
		t.Fatal("content followed symlink outside root")
	}
	if _, _, err := manager.Image(context.Background(), opened.Handle, "outside-link"); err == nil {
		t.Fatal("image followed symlink outside root")
	}
	if _, _, err := manager.Image(context.Background(), opened.Handle, "pipe"); !errors.Is(err, ErrFileUnsupported) {
		t.Fatalf("special file image error=%v", err)
	}
}

func TestRootManagerConditionalAtomicWrite(t *testing.T) {
	repository := t.TempDir()
	manager := newRootManager()
	defer manager.Close()
	opened, err := manager.Open(context.Background(), filepath.ToSlash(repository))
	if err != nil {
		t.Fatal(err)
	}
	request := FileWriteRequest{RootHandle: opened.Handle, Path: "file.txt", ExpectedAbsent: true}
	created, err := manager.Write(context.Background(), request, []byte("first"))
	firstDigest := sha256.Sum256([]byte("first"))
	if err != nil || !created.Created || created.SHA256 != hex.EncodeToString(firstDigest[:]) {
		t.Fatalf("created=%#v err=%v", created, err)
	}
	if err := os.Chmod(filepath.Join(repository, "file.txt"), 0o640); err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Write(context.Background(), FileWriteRequest{
		RootHandle: opened.Handle, Path: "file.txt", ExpectedSHA256: hex.EncodeToString(firstDigest[:]),
	}, []byte("second"))
	secondDigest := sha256.Sum256([]byte("second"))
	info, statErr := os.Stat(filepath.Join(repository, "file.txt"))
	content, readErr := os.ReadFile(filepath.Join(repository, "file.txt"))
	if err != nil || statErr != nil || readErr != nil {
		t.Fatalf("write checks: err=%v stat=%v read=%v", err, statErr, readErr)
	}
	if updated.Created || !updated.ExtendedMetadataNotPreserved || updated.SHA256 != hex.EncodeToString(secondDigest[:]) || string(content) != "second" || info.Mode().Perm() != 0o640 {
		t.Fatalf("updated=%#v content=%q mode=%o", updated, content, info.Mode().Perm())
	}
	if _, err := manager.Write(context.Background(), request, []byte("overwrite")); !errors.Is(err, ErrFileConflict) {
		t.Fatalf("expected-absent conflict=%v", err)
	}
	if _, err := manager.Write(context.Background(), FileWriteRequest{
		RootHandle: opened.Handle, Path: "file.txt", ExpectedSHA256: strings.Repeat("0", 64),
	}, []byte("overwrite")); !errors.Is(err, ErrFileConflict) {
		t.Fatalf("hash conflict=%v", err)
	}
	content, _ = os.ReadFile(filepath.Join(repository, "file.txt"))
	if string(content) != "second" {
		t.Fatalf("conflict changed content to %q", content)
	}
	if err := os.Symlink("file.txt", filepath.Join(repository, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Write(context.Background(), FileWriteRequest{
		RootHandle: opened.Handle, Path: "link", ExpectedSHA256: hex.EncodeToString(secondDigest[:]),
	}, []byte("link")); !errors.Is(err, ErrFileUnsupported) {
		t.Fatalf("symlink write error=%v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.Write(cancelled, FileWriteRequest{RootHandle: opened.Handle, Path: "cancelled", ExpectedAbsent: true}, []byte("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(repository, "cancelled")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled write created a file: %v", err)
	}
	entries, err := os.ReadDir(repository)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".pi-desk-write-") {
			t.Fatalf("temporary file leaked: %s", entry.Name())
		}
	}
}

func TestRootManagerMkdirCreatesOnlyDirectoriesInsideRoot(t *testing.T) {
	root := t.TempDir()
	manager := newRootManager()
	opened, err := manager.Open(context.Background(), filepath.ToSlash(root))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	response, err := manager.Mkdir(context.Background(), FileMkdirRequest{RootHandle: opened.Handle, Path: "nested/deep"})
	if err != nil || !slices.Equal(response.Created, []string{"nested", "nested/deep"}) {
		t.Fatalf("mkdir=%#v err=%v", response, err)
	}
	response, err = manager.Mkdir(context.Background(), FileMkdirRequest{RootHandle: opened.Handle, Path: "nested/deep"})
	if err != nil || len(response.Created) != 0 {
		t.Fatalf("idempotent mkdir=%#v err=%v", response, err)
	}
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Mkdir(context.Background(), FileMkdirRequest{RootHandle: opened.Handle, Path: "file/deep"}); !errors.Is(err, ErrFileConflict) {
		t.Fatalf("file parent mkdir error=%v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Mkdir(context.Background(), FileMkdirRequest{RootHandle: opened.Handle, Path: "link/deep"}); !errors.Is(err, ErrFileConflict) {
		t.Fatalf("symlink parent mkdir error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "deep")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mkdir escaped root: %v", err)
	}
}

func TestRootManagerSerializesConditionalWrites(t *testing.T) {
	repository := t.TempDir()
	if err := os.WriteFile(filepath.Join(repository, "file.txt"), []byte("base"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := newRootManager()
	defer manager.Close()
	opened, err := manager.Open(context.Background(), filepath.ToSlash(repository))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("base"))
	request := FileWriteRequest{RootHandle: opened.Handle, Path: "file.txt", ExpectedSHA256: hex.EncodeToString(digest[:])}
	results := make(chan error, 2)
	for _, content := range []string{"left", "right"} {
		go func(content string) {
			_, err := manager.Write(context.Background(), request, []byte(content))
			results <- err
		}(content)
	}
	succeeded, conflicted := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrFileConflict):
			conflicted++
		default:
			t.Fatalf("concurrent write error=%v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent writes succeeded=%d conflicted=%d", succeeded, conflicted)
	}
}

func TestRootManagerReadLimitsAndEncoding(t *testing.T) {
	repository := t.TempDir()
	if err := os.WriteFile(filepath.Join(repository, "binary"), []byte{0xff, 0xfe}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "long-line"), []byte(strings.Repeat("x", maxReadLineBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "large"), make([]byte, maxReadableFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	largeImage := make([]byte, maxImageFileBytes+1)
	copy(largeImage, "\x89PNG\r\n\x1a\n")
	if err := os.WriteFile(filepath.Join(repository, "large.png"), largeImage, 0o600); err != nil {
		t.Fatal(err)
	}
	manager := newRootManager()
	defer manager.Close()
	opened, err := manager.Open(context.Background(), filepath.ToSlash(repository))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"binary", "long-line"} {
		if _, err := manager.Read(context.Background(), FileReadRequest{RootHandle: opened.Handle, Path: name, StartLine: 1, MaxLines: 10}); !errors.Is(err, ErrFileUnsupported) {
			t.Fatalf("read %s error=%v", name, err)
		}
	}
	if _, err := manager.Read(context.Background(), FileReadRequest{RootHandle: opened.Handle, Path: "large", StartLine: 1, MaxLines: 10}); !errors.Is(err, ErrFileOutputLimit) {
		t.Fatalf("large read error=%v", err)
	}
	if _, err := manager.Hash(context.Background(), opened.Handle, "large"); !errors.Is(err, ErrFileOutputLimit) {
		t.Fatalf("large hash error=%v", err)
	}
	if _, _, err := manager.Image(context.Background(), opened.Handle, "large.png"); !errors.Is(err, ErrFileOutputLimit) {
		t.Fatalf("large image error=%v", err)
	}
	if _, _, err := manager.Image(context.Background(), opened.Handle, "binary"); !errors.Is(err, ErrFileUnsupported) {
		t.Fatalf("non-image error=%v", err)
	}
}

func TestRootManagerRejectsFilesAndCapsResources(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := newRootManager()
	defer manager.Close()
	if _, err := manager.Open(context.Background(), filepath.ToSlash(file)); !errors.Is(err, ErrRootOpen) {
		t.Fatalf("file root error = %v", err)
	}
	for index := 0; index < maxRootCapabilities; index++ {
		directory := filepath.Join(base, fmt.Sprintf("root-%02d", index))
		if err := os.Mkdir(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := manager.Open(context.Background(), filepath.ToSlash(directory)); err != nil {
			t.Fatalf("open root %d: %v", index, err)
		}
	}
	extra := filepath.Join(base, "extra")
	if err := os.Mkdir(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Open(context.Background(), filepath.ToSlash(extra)); !errors.Is(err, ErrRootResourceLimit) {
		t.Fatalf("root limit error = %v", err)
	}
}
