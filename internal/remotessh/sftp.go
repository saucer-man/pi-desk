package remotessh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/pkg/sftp"

	"pi-desk/internal/processutil"
)

const maxSFTPPacketBytes = 32 << 10

const (
	sftpCommandCloseTimeout = 5 * time.Second
	sftpCommandKillTimeout  = 2 * time.Second
)

var ErrSFTPTransport = errors.New("OpenSSH SFTP subsystem failed")

// sftpInvocation is private because SFTP may only be opened by the helper
// bootstrap after a target generation has completed the strict connection
// probe. It has no caller-provided options, subsystem, or remote command.
func (locator *Locator) sftpInvocation(hostAlias string) (Invocation, error) {
	target, executable, err := locator.resolveTarget(hostAlias)
	if err != nil {
		return Invocation{}, err
	}
	args := []string{"-s"}
	args = append(args, connectionPolicyArgs()...)
	args = append(args, "--", target.HostAlias, "sftp")
	return Invocation{Executable: executable, Args: locator.withTestConfig(args)}, nil
}

// sftpOpenError exposes a stable, redacted reason while preserving the actual
// process error only for errors.Is. It never retains the OpenSSH stderr text.
type sftpOpenError struct {
	Failure ConnectionFailure
	cause   error
}

func (err *sftpOpenError) Error() string {
	return fmt.Sprintf("%s: %s", err.Failure.Code, err.Failure.Reason)
}

func (err *sftpOpenError) Unwrap() error {
	return err.cause
}

// remoteCacheFS is intentionally smaller than an SFTP client. It admits only
// the operations needed to install one fixed helper-cache schema and makes the
// installer independently testable without a real SSH server.
type remoteCacheFS interface {
	RealPath(string) (string, error)
	Lstat(string) (remoteCacheEntry, error)
	ReadDir(string) ([]remoteCacheDirEntry, error)
	Mkdir(string) error
	Chmod(string, fs.FileMode) error
	OpenFile(string, int, fs.FileMode) (remoteCacheFile, error)
	Open(string) (io.ReadCloser, error)
	Link(string, string) error
	Rename(string, string) error
	Remove(string) error
	RemoveDirectory(string) error
	Close() error
}

type remoteCacheEntry struct {
	Mode  fs.FileMode
	Size  int64
	Owner uint32
}

func (entry remoteCacheEntry) isDir() bool {
	return entry.Mode.IsDir() && entry.Mode&fs.ModeSymlink == 0
}

func (entry remoteCacheEntry) isRegular() bool {
	return entry.Mode.IsRegular() && entry.Mode&fs.ModeSymlink == 0
}

type remoteCacheDirEntry struct {
	Name  string
	Entry remoteCacheEntry
}

type remoteCacheFile interface {
	io.Reader
	io.Writer
	Sync() error
	Close() error
}

// openSFTP starts only the fixed OpenSSH sftp subsystem. Command stdout is the
// protocol pipe, stderr is bounded in memory for redacted classification, and
// the caller-provided context is expected to be generation-bound.
func (locator *Locator) openSFTP(ctx context.Context, hostAlias string) (remoteCacheFS, error) {
	invocation, err := locator.sftpInvocation(hostAlias)
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, invocation.Executable, invocation.Args...)
	processutil.ConfigureBackground(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, newSFTPOpenError(ctx, nil, err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, newSFTPOpenError(ctx, nil, err)
	}
	stderr := &boundedOutput{limit: maxConnectionProbeStderrBytes}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return nil, newSFTPOpenError(ctx, stderr.buffer.Bytes(), err)
	}

	client, err := sftp.NewClientPipe(stdout, stdin,
		sftp.MaxPacketChecked(maxSFTPPacketBytes),
		sftp.UseConcurrentReads(true),
		sftp.UseConcurrentWrites(true),
	)
	if err != nil {
		_ = stdin.Close()
		waitErr := command.Wait()
		if waitErr != nil {
			err = waitErr
		}
		return nil, newSFTPOpenError(ctx, stderr.buffer.Bytes(), err)
	}
	return &sftpCacheFS{client: client, command: command, context: ctx, stderr: stderr}, nil
}

func newSFTPOpenError(ctx context.Context, stderr []byte, cause error) *sftpOpenError {
	failure := ClassifyOpenSSHFailure(stderr)
	if errors.Is(cause, ErrProbeOutputTooLarge) {
		failure = ConnectionFailure{Code: FailureOutputLimit, Reason: ReasonOutputLimit}
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		failure = ConnectionFailure{Code: FailureCancelled, Reason: ReasonCancelled}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		failure = ConnectionFailure{Code: FailureConnect, Reason: ReasonConnectionTimeout}
	}
	return &sftpOpenError{Failure: failure, cause: fmt.Errorf("%w: %w", ErrSFTPTransport, cause)}
}

type sftpCacheFS struct {
	client   *sftp.Client
	command  *exec.Cmd
	context  context.Context
	stderr   *boundedOutput
	close    sync.Once
	closeErr error
}

func (filesystem *sftpCacheFS) RealPath(value string) (string, error) {
	return filesystem.client.RealPath(value)
}

func (filesystem *sftpCacheFS) Lstat(value string) (remoteCacheEntry, error) {
	info, err := filesystem.client.Lstat(value)
	if err != nil {
		return remoteCacheEntry{}, err
	}
	return sftpCacheEntry(info)
}

func (filesystem *sftpCacheFS) ReadDir(value string) ([]remoteCacheDirEntry, error) {
	entries, err := filesystem.client.ReadDir(value)
	if err != nil {
		return nil, err
	}
	result := make([]remoteCacheDirEntry, 0, len(entries))
	for _, entry := range entries {
		metadata, err := sftpCacheEntry(entry)
		if err != nil {
			return nil, err
		}
		result = append(result, remoteCacheDirEntry{Name: entry.Name(), Entry: metadata})
	}
	return result, nil
}

func (filesystem *sftpCacheFS) Mkdir(value string) error {
	return filesystem.client.Mkdir(value)
}

func (filesystem *sftpCacheFS) Chmod(value string, mode fs.FileMode) error {
	return filesystem.client.Chmod(value, mode)
}

func (filesystem *sftpCacheFS) OpenFile(value string, flags int, _ fs.FileMode) (remoteCacheFile, error) {
	return filesystem.client.OpenFile(value, flags)
}

func (filesystem *sftpCacheFS) Open(value string) (io.ReadCloser, error) {
	return filesystem.client.Open(value)
}

func (filesystem *sftpCacheFS) Link(oldPath, newPath string) error {
	return filesystem.client.Link(oldPath, newPath)
}

func (filesystem *sftpCacheFS) Rename(oldPath, newPath string) error {
	return filesystem.client.PosixRename(oldPath, newPath)
}

func (filesystem *sftpCacheFS) Remove(value string) error {
	return filesystem.client.Remove(value)
}

func (filesystem *sftpCacheFS) RemoveDirectory(value string) error {
	return filesystem.client.RemoveDirectory(value)
}

func (filesystem *sftpCacheFS) Close() error {
	filesystem.close.Do(func() {
		closeErr := filesystem.client.Close()
		waitDone := make(chan error, 1)
		go func() { waitDone <- filesystem.command.Wait() }()
		var waitErr error
		timer := time.NewTimer(sftpCommandCloseTimeout)
		select {
		case waitErr = <-waitDone:
			timer.Stop()
		case <-timer.C:
			_ = processutil.TerminateTree(filesystem.command)
			killTimer := time.NewTimer(sftpCommandKillTimeout)
			select {
			case waitErr = <-waitDone:
				killTimer.Stop()
			case <-killTimer.C:
				waitErr = context.DeadlineExceeded
			}
		}
		if closeErr != nil {
			filesystem.closeErr = newSFTPOpenError(filesystem.context, filesystem.stderr.buffer.Bytes(), closeErr)
		} else if filesystem.stderr.overflow {
			filesystem.closeErr = newSFTPOpenError(filesystem.context, filesystem.stderr.buffer.Bytes(), ErrProbeOutputTooLarge)
		} else if waitErr != nil {
			filesystem.closeErr = newSFTPOpenError(filesystem.context, filesystem.stderr.buffer.Bytes(), waitErr)
		}
	})
	return filesystem.closeErr
}

func sftpCacheEntry(info os.FileInfo) (remoteCacheEntry, error) {
	if info == nil {
		return remoteCacheEntry{}, ErrSFTPTransport
	}
	metadata, ok := info.Sys().(*sftp.FileStat)
	if !ok {
		return remoteCacheEntry{}, ErrSFTPTransport
	}
	return remoteCacheEntry{Mode: info.Mode(), Size: info.Size(), Owner: metadata.UID}, nil
}
