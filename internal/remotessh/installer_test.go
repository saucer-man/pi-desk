package remotessh

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"sync"
	"testing"
	"time"
)

type memoryCacheNode struct {
	entry remoteCacheEntry
	data  []byte
}

type memoryCacheFS struct {
	mu       sync.Mutex
	home     string
	owner    uint32
	nodes    map[string]*memoryCacheNode
	closed   bool
	closeErr error
}

func newMemoryCacheFS() *memoryCacheFS {
	filesystem := &memoryCacheFS{
		home:  "/home/test",
		owner: 1000,
		nodes: make(map[string]*memoryCacheNode),
	}
	filesystem.nodes[filesystem.home] = &memoryCacheNode{entry: remoteCacheEntry{Mode: fs.ModeDir | 0o700, Owner: filesystem.owner}}
	return filesystem
}

func (filesystem *memoryCacheFS) RealPath(value string) (string, error) {
	if value != "." {
		return "", errors.New("unexpected realpath")
	}
	return filesystem.home, nil
}

func (filesystem *memoryCacheFS) Lstat(filename string) (remoteCacheEntry, error) {
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	node := filesystem.nodes[filename]
	if node == nil {
		return remoteCacheEntry{}, os.ErrNotExist
	}
	entry := node.entry
	entry.Size = int64(len(node.data))
	return entry, nil
}

func (filesystem *memoryCacheFS) ReadDir(directory string) ([]remoteCacheDirEntry, error) {
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	if node := filesystem.nodes[directory]; node == nil || !node.entry.isDir() {
		return nil, os.ErrNotExist
	}
	var result []remoteCacheDirEntry
	for filename, node := range filesystem.nodes {
		if path.Dir(filename) == directory && filename != directory {
			entry := node.entry
			entry.Size = int64(len(node.data))
			result = append(result, remoteCacheDirEntry{Name: path.Base(filename), Entry: entry})
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result, nil
}

func (filesystem *memoryCacheFS) Mkdir(directory string) error {
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	if filesystem.nodes[directory] != nil {
		return os.ErrExist
	}
	parent := filesystem.nodes[path.Dir(directory)]
	if parent == nil || !parent.entry.isDir() {
		return os.ErrNotExist
	}
	filesystem.nodes[directory] = &memoryCacheNode{entry: remoteCacheEntry{Mode: fs.ModeDir | 0o755, Owner: filesystem.owner}}
	return nil
}

func (filesystem *memoryCacheFS) Chmod(filename string, mode fs.FileMode) error {
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	node := filesystem.nodes[filename]
	if node == nil {
		return os.ErrNotExist
	}
	node.entry.Mode = node.entry.Mode.Type() | mode.Perm()
	return nil
}

func (filesystem *memoryCacheFS) OpenFile(filename string, flags int, mode fs.FileMode) (remoteCacheFile, error) {
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	if filesystem.nodes[filename] != nil && flags&os.O_EXCL != 0 {
		return nil, os.ErrExist
	}
	if parent := filesystem.nodes[path.Dir(filename)]; parent == nil || !parent.entry.isDir() {
		return nil, os.ErrNotExist
	}
	node := &memoryCacheNode{entry: remoteCacheEntry{Mode: mode.Perm(), Owner: filesystem.owner}}
	filesystem.nodes[filename] = node
	return &memoryCacheFileHandle{filesystem: filesystem, filename: filename}, nil
}

func (filesystem *memoryCacheFS) Open(filename string) (io.ReadCloser, error) {
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	node := filesystem.nodes[filename]
	if node == nil || !node.entry.isRegular() {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), node.data...))), nil
}

func (filesystem *memoryCacheFS) Link(oldPath, newPath string) error {
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	if filesystem.nodes[newPath] != nil {
		return os.ErrExist
	}
	node := filesystem.nodes[oldPath]
	if node == nil {
		return os.ErrNotExist
	}
	filesystem.nodes[newPath] = node
	return nil
}

func (filesystem *memoryCacheFS) Rename(oldPath, newPath string) error {
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	node := filesystem.nodes[oldPath]
	if node == nil {
		return os.ErrNotExist
	}
	filesystem.nodes[newPath] = node
	delete(filesystem.nodes, oldPath)
	return nil
}

func (filesystem *memoryCacheFS) Remove(filename string) error {
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	node := filesystem.nodes[filename]
	if node == nil {
		return os.ErrNotExist
	}
	if node.entry.isDir() {
		return errors.New("is a directory")
	}
	delete(filesystem.nodes, filename)
	return nil
}

func (filesystem *memoryCacheFS) RemoveDirectory(directory string) error {
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	for filename := range filesystem.nodes {
		if filename != directory && path.Dir(filename) == directory {
			return errors.New("directory not empty")
		}
	}
	delete(filesystem.nodes, directory)
	return nil
}

func (filesystem *memoryCacheFS) Close() error {
	filesystem.mu.Lock()
	defer filesystem.mu.Unlock()
	filesystem.closed = true
	return filesystem.closeErr
}

type memoryCacheFileHandle struct {
	filesystem *memoryCacheFS
	filename   string
	offset     int
	closed     bool
}

func (file *memoryCacheFileHandle) Read(value []byte) (int, error) {
	file.filesystem.mu.Lock()
	defer file.filesystem.mu.Unlock()
	node := file.filesystem.nodes[file.filename]
	if node == nil || file.offset >= len(node.data) {
		return 0, io.EOF
	}
	count := copy(value, node.data[file.offset:])
	file.offset += count
	return count, nil
}

func (file *memoryCacheFileHandle) Write(value []byte) (int, error) {
	file.filesystem.mu.Lock()
	defer file.filesystem.mu.Unlock()
	if file.closed {
		return 0, fs.ErrClosed
	}
	node := file.filesystem.nodes[file.filename]
	node.data = append(node.data, value...)
	return len(value), nil
}

func (file *memoryCacheFileHandle) Sync() error { return nil }
func (file *memoryCacheFileHandle) Close() error {
	file.closed = true
	return nil
}

func connectedInstallerSupervisor(t *testing.T) (*ConnectionSupervisor, uint64) {
	t.Helper()
	supervisor := newTestSupervisor(t, connectionProberFunc(func(_ context.Context, _ string) (ConnectionPreflight, error) {
		return supervisorPreflight("config-a", "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), nil
	}))
	ready, err := supervisor.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return supervisor, ready.Generation
}

func TestWaitForConcurrentCacheDirectory(t *testing.T) {
	filesystem := newMemoryCacheFS()
	directory := path.Join(filesystem.home, ".cache")
	filesystem.nodes[directory] = &memoryCacheNode{entry: remoteCacheEntry{Mode: fs.ModeDir | 0o755, Owner: filesystem.owner}}
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = filesystem.Chmod(directory, 0o700)
	}()
	if err := waitForConcurrentCacheDirectory(context.Background(), filesystem, directory, filesystem.owner, true); err != nil {
		t.Fatalf("concurrent directory did not become safe: %v", err)
	}
	filesystem.nodes[directory].entry.Owner++
	if err := waitForConcurrentCacheDirectory(context.Background(), filesystem, directory, filesystem.owner, true); !errors.Is(err, ErrHelperCacheUnsafe) {
		t.Fatalf("unsafe concurrent directory error=%v", err)
	}
}

func TestHelperInstallerInstallsAndReusesExactArtifact(t *testing.T) {
	content := []byte("immutable remote helper binary")
	artifact := helperArtifactForTest("linux", "amd64", content)
	filesystem := newMemoryCacheFS()
	supervisor, generation := connectedInstallerSupervisor(t)
	openCalls := 0
	installer, err := newHelperInstaller(supervisor, func(_ context.Context, alias string) (remoteCacheFS, error) {
		openCalls++
		if alias != "build-prod" {
			t.Fatalf("SFTP alias = %q", alias)
		}
		return filesystem, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := installer.Install(context.Background(), generation, artifact, content)
	if err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	if first.Reused || first.RemotePath != "/home/test/.cache/pi-desk/remote-helper/1/"+artifact.SHA256+"/helper" {
		t.Fatalf("unexpected install result: %#v", first)
	}
	entry, err := filesystem.Lstat(first.RemotePath)
	if err != nil || !entry.isRegular() || entry.Mode.Perm() != 0o700 || entry.Owner != filesystem.owner || entry.Size != artifact.Size {
		t.Fatalf("installed helper metadata = %#v, %v", entry, err)
	}
	second, err := installer.Install(context.Background(), generation, artifact, content)
	if err != nil || !second.Reused || second.RemotePath != first.RemotePath || openCalls != 2 {
		t.Fatalf("reuse result=%#v err=%v openCalls=%d", second, err, openCalls)
	}
}

func TestHelperInstallerReplacesOnlyCorruptRegularHelper(t *testing.T) {
	content := []byte("immutable remote helper binary")
	artifact := helperArtifactForTest("linux", "amd64", content)
	filesystem := newMemoryCacheFS()
	supervisor, generation := connectedInstallerSupervisor(t)
	installer, _ := newHelperInstaller(supervisor, func(context.Context, string) (remoteCacheFS, error) { return filesystem, nil })
	first, err := installer.Install(context.Background(), generation, artifact, content)
	if err != nil {
		t.Fatal(err)
	}
	filesystem.mu.Lock()
	filesystem.nodes[first.RemotePath].data = bytes.Repeat([]byte{'x'}, len(content))
	filesystem.mu.Unlock()
	result, err := installer.Install(context.Background(), generation, artifact, content)
	if err != nil || result.Reused {
		t.Fatalf("corrupt helper replacement result=%#v err=%v", result, err)
	}
	matches, err := remoteFileMatches(context.Background(), filesystem, result.RemotePath, artifact)
	if err != nil || !matches {
		t.Fatalf("replacement content mismatch: matches=%t err=%v", matches, err)
	}
}

func TestHelperInstallerRejectsUnsafeCacheEntries(t *testing.T) {
	content := []byte("immutable remote helper binary")
	artifact := helperArtifactForTest("linux", "amd64", content)
	for _, test := range []struct {
		name   string
		mutate func(*memoryCacheFS)
	}{
		{
			name: "foreign cache owner",
			mutate: func(filesystem *memoryCacheFS) {
				filesystem.nodes["/home/test/.cache"] = &memoryCacheNode{entry: remoteCacheEntry{Mode: fs.ModeDir | 0o700, Owner: 2000}}
			},
		},
		{
			name: "symlink cache",
			mutate: func(filesystem *memoryCacheFS) {
				filesystem.nodes["/home/test/.cache"] = &memoryCacheNode{entry: remoteCacheEntry{Mode: fs.ModeSymlink | 0o777, Owner: 1000}}
			},
		},
		{
			name: "group writable cache",
			mutate: func(filesystem *memoryCacheFS) {
				filesystem.nodes["/home/test/.cache"] = &memoryCacheNode{entry: remoteCacheEntry{Mode: fs.ModeDir | 0o770, Owner: 1000}}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			filesystem := newMemoryCacheFS()
			test.mutate(filesystem)
			supervisor, generation := connectedInstallerSupervisor(t)
			installer, _ := newHelperInstaller(supervisor, func(context.Context, string) (remoteCacheFS, error) { return filesystem, nil })
			if _, err := installer.Install(context.Background(), generation, artifact, content); !errors.Is(err, ErrHelperCacheUnsafe) {
				t.Fatalf("Install error = %v, want ErrHelperCacheUnsafe", err)
			}
		})
	}
}

func TestHelperInstallerFailsClosedWhenSFTPShutdownFails(t *testing.T) {
	content := []byte("immutable remote helper binary")
	artifact := helperArtifactForTest("linux", "amd64", content)
	filesystem := newMemoryCacheFS()
	filesystem.closeErr = ErrSFTPTransport
	supervisor, generation := connectedInstallerSupervisor(t)
	installer, _ := newHelperInstaller(supervisor, func(context.Context, string) (remoteCacheFS, error) { return filesystem, nil })
	if _, err := installer.Install(context.Background(), generation, artifact, content); !errors.Is(err, ErrSFTPTransport) {
		t.Fatalf("Install close error = %v", err)
	}
	if snapshot := supervisor.Snapshot(); snapshot.State != ConnectionDisconnected || snapshot.Generation != 0 {
		t.Fatalf("failed SFTP shutdown left generation ready: %#v", snapshot)
	}
}

func TestHelperInstallerVerifiesLocalArtifactBeforeOpeningSFTP(t *testing.T) {
	content := []byte("immutable remote helper binary")
	artifact := helperArtifactForTest("linux", "amd64", content)
	supervisor, generation := connectedInstallerSupervisor(t)
	openCalls := 0
	installer, _ := newHelperInstaller(supervisor, func(context.Context, string) (remoteCacheFS, error) {
		openCalls++
		return newMemoryCacheFS(), nil
	})
	if _, err := installer.Install(context.Background(), generation, artifact, []byte("tampered")); !errors.Is(err, ErrHelperArtifactIntegrity) {
		t.Fatalf("Install error = %v", err)
	}
	if openCalls != 0 {
		t.Fatalf("invalid local artifact opened SFTP %d times", openCalls)
	}
	if err := supervisor.ValidateGeneration(generation); err != nil {
		t.Fatalf("pre-network integrity failure revoked generation: %v", err)
	}
}

func TestHelperInstallerDisconnectCancelsSFTPBootstrap(t *testing.T) {
	content := []byte("immutable remote helper binary")
	artifact := helperArtifactForTest("linux", "amd64", content)
	supervisor, generation := connectedInstallerSupervisor(t)
	opened := make(chan struct{})
	installer, _ := newHelperInstaller(supervisor, func(ctx context.Context, _ string) (remoteCacheFS, error) {
		close(opened)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	done := make(chan error, 1)
	go func() {
		_, err := installer.Install(context.Background(), generation, artifact, content)
		done <- err
	}()
	<-opened
	supervisor.Disconnect()
	select {
	case err := <-done:
		if !errors.Is(err, ErrConnectionGenerationRevoked) {
			t.Fatalf("cancelled install error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Disconnect did not stop SFTP bootstrap")
	}
}

func TestValidateRemoteHome(t *testing.T) {
	for _, value := range []string{"/home/test", "/Users/developer", "/root"} {
		if err := validateRemoteHome(value); err != nil {
			t.Fatalf("validateRemoteHome(%q) = %v", value, err)
		}
	}
	for _, value := range []string{"", "/", "relative", "/home/../root", "/home/test\nname", "//home/test"} {
		if err := validateRemoteHome(value); !errors.Is(err, ErrHelperCacheUnsafe) {
			t.Fatalf("validateRemoteHome(%q) = %v", value, err)
		}
	}
}
