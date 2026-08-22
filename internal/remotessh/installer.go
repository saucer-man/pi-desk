package remotessh

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	remoteHelperCacheRelative       = ".cache/pi-desk/remote-helper"
	maxRemoteHomeBytes              = 4096
	concurrentCacheCreateWait       = 5 * time.Second
	concurrentCacheCreateRetryDelay = 10 * time.Millisecond
)

var (
	ErrHelperInstall     = errors.New("remote helper installation failed")
	ErrHelperCacheUnsafe = errors.New("remote helper cache is unsafe")
)

// HelperInstallResult is an in-memory bootstrap result. RemotePath is needed
// only to verify the fixed cache location and must not be persisted in target
// state or diagnostics.
type HelperInstallResult struct {
	RemotePath string
	Reused     bool
}

type sftpOpener func(context.Context, string) (remoteCacheFS, error)

// HelperInstaller installs one exact artifact into the fixed cache of one
// target supervisor. It serializes mutation so two callers cannot race a
// conditional replacement within this process.
type HelperInstaller struct {
	locator    *Locator
	supervisor *ConnectionSupervisor
	open       sftpOpener
	mu         sync.Mutex
}

type helperInstallKey struct {
	hostAlias string
	sha256    string
}

type helperInstallLock struct {
	gate chan struct{}
	refs int
}

var helperInstallLockRegistry = struct {
	sync.Mutex
	locks map[helperInstallKey]*helperInstallLock
}{locks: make(map[helperInstallKey]*helperInstallLock)}

func acquireHelperInstallLock(ctx context.Context, key helperInstallKey) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	helperInstallLockRegistry.Lock()
	lock := helperInstallLockRegistry.locks[key]
	if lock == nil {
		lock = &helperInstallLock{gate: make(chan struct{}, 1)}
		lock.gate <- struct{}{}
		helperInstallLockRegistry.locks[key] = lock
	}
	lock.refs++
	helperInstallLockRegistry.Unlock()

	select {
	case <-lock.gate:
		var once sync.Once
		return func() {
			once.Do(func() {
				lock.gate <- struct{}{}
				helperInstallLockRegistry.Lock()
				lock.refs--
				if lock.refs == 0 && helperInstallLockRegistry.locks[key] == lock {
					delete(helperInstallLockRegistry.locks, key)
				}
				helperInstallLockRegistry.Unlock()
			})
		}, nil
	case <-ctx.Done():
		helperInstallLockRegistry.Lock()
		lock.refs--
		if lock.refs == 0 && helperInstallLockRegistry.locks[key] == lock {
			delete(helperInstallLockRegistry.locks, key)
		}
		helperInstallLockRegistry.Unlock()
		return nil, ctx.Err()
	}
}

func NewHelperInstaller(locator *Locator, supervisor *ConnectionSupervisor) (*HelperInstaller, error) {
	if locator == nil || supervisor == nil {
		return nil, errors.New("SSH locator and connection supervisor are required")
	}
	if supervisor.locator != locator {
		return nil, errors.New("helper installer must use the supervisor OpenSSH locator")
	}
	return &HelperInstaller{
		locator:    locator,
		supervisor: supervisor,
		open:       locator.openSFTP,
	}, nil
}

func newHelperInstaller(supervisor *ConnectionSupervisor, opener sftpOpener) (*HelperInstaller, error) {
	if supervisor == nil || opener == nil {
		return nil, errors.New("connection supervisor and SFTP opener are required")
	}
	return &HelperInstaller{supervisor: supervisor, open: opener}, nil
}

// Install verifies local content before network activity, binds all SFTP work
// to the current generation, and returns only after a remote readback proves
// the exact artifact hash and final mode.
func (installer *HelperInstaller) Install(ctx context.Context, generation uint64, artifact HelperArtifact, content []byte) (HelperInstallResult, error) {
	if err := artifact.VerifyContent(content); err != nil {
		return HelperInstallResult{}, err
	}
	generationContext, release, err := installer.supervisor.bindGenerationContext(ctx, generation)
	if err != nil {
		return HelperInstallResult{}, err
	}
	defer release()
	installer.mu.Lock()
	defer installer.mu.Unlock()
	succeeded := false
	defer func() {
		if !succeeded {
			generationShutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			installer.supervisor.Disconnect()
			release()
			_ = installer.supervisor.WaitForGenerationIdle(generationShutdownContext, generation)
		}
	}()
	if err := generationContext.Err(); err != nil {
		return HelperInstallResult{}, lifecycleError(FailureDisconnected, ReasonDisconnected, err)
	}
	filesystem, err := installer.open(generationContext, installer.supervisor.hostAlias)
	if err != nil {
		if errors.Is(generationContext.Err(), context.Canceled) {
			return HelperInstallResult{}, lifecycleError(FailureDisconnected, ReasonDisconnected, ErrConnectionGenerationRevoked)
		}
		return HelperInstallResult{}, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = filesystem.Close()
		}
	}()

	result, err := installHelperContent(generationContext, filesystem, artifact, content, helperInstallKey{
		hostAlias: installer.supervisor.hostAlias,
		sha256:    artifact.SHA256,
	})
	if err != nil {
		if errors.Is(generationContext.Err(), context.Canceled) {
			return HelperInstallResult{}, lifecycleError(FailureDisconnected, ReasonDisconnected, ErrConnectionGenerationRevoked)
		}
		return HelperInstallResult{}, err
	}
	if closeErr := filesystem.Close(); closeErr != nil {
		closed = true
		return HelperInstallResult{}, closeErr
	}
	closed = true
	if err := installer.supervisor.ValidateGeneration(generation); err != nil {
		return HelperInstallResult{}, err
	}
	succeeded = true
	return result, nil
}

func installHelperContent(ctx context.Context, filesystem remoteCacheFS, artifact HelperArtifact, content []byte, lockKey helperInstallKey) (HelperInstallResult, error) {
	home, err := filesystem.RealPath(".")
	if err != nil {
		return HelperInstallResult{}, fmt.Errorf("%w: resolve remote home", ErrHelperInstall)
	}
	if err := validateRemoteHome(home); err != nil {
		return HelperInstallResult{}, err
	}
	homeEntry, err := filesystem.Lstat(home)
	if err != nil || !homeEntry.isDir() {
		return HelperInstallResult{}, ErrHelperCacheUnsafe
	}
	owner := homeEntry.Owner

	cacheRoot := path.Join(home, remoteHelperCacheRelative)
	cacheVersion := path.Join(cacheRoot, strconv.Itoa(int(artifact.ProtocolVersion)))
	cacheHash := path.Join(cacheVersion, artifact.SHA256)
	finalPath := path.Join(cacheHash, "helper")

	if err := ensureCacheParents(ctx, filesystem, home, owner, cacheVersion, cacheHash); err != nil {
		return HelperInstallResult{}, fmt.Errorf("ensure helper cache parents: %w", err)
	}
	if reused, err := validateExistingHelper(ctx, filesystem, finalPath, owner, artifact); err != nil {
		return HelperInstallResult{}, fmt.Errorf("validate existing helper: %w", err)
	} else if reused {
		return HelperInstallResult{RemotePath: finalPath, Reused: true}, nil
	}

	temporaryPath, err := createTemporaryHelper(ctx, filesystem, cacheHash, owner, artifact, content)
	if err != nil {
		return HelperInstallResult{}, fmt.Errorf("create temporary helper: %w", err)
	}
	defer func() { _ = filesystem.Remove(temporaryPath) }()
	installLockRelease, err := acquireHelperInstallLock(ctx, lockKey)
	if err != nil {
		return HelperInstallResult{}, err
	}
	defer installLockRelease()

	if existing, statErr := filesystem.Lstat(finalPath); statErr == nil {
		if !existing.isRegular() || existing.Owner != owner {
			return HelperInstallResult{}, ErrHelperCacheUnsafe
		}
		if err := filesystem.Rename(temporaryPath, finalPath); err != nil {
			return HelperInstallResult{}, fmt.Errorf("%w: replace helper: %w", ErrHelperInstall, err)
		}
	} else if errors.Is(statErr, os.ErrNotExist) {
		if err := filesystem.Link(temporaryPath, finalPath); err != nil {
			if reused, validationErr := validateExistingHelper(ctx, filesystem, finalPath, owner, artifact); validationErr != nil {
				return HelperInstallResult{}, fmt.Errorf("%w: publish helper: %w; validate concurrent result: %w", ErrHelperInstall, err, validationErr)
			} else if reused {
				return HelperInstallResult{RemotePath: finalPath, Reused: true}, nil
			}
			return HelperInstallResult{}, fmt.Errorf("%w: publish helper: %w", ErrHelperInstall, err)
		}
	} else {
		return HelperInstallResult{}, fmt.Errorf("%w: inspect helper destination: %w", ErrHelperInstall, statErr)
	}
	if err := validateInstalledHelper(ctx, filesystem, finalPath, owner, artifact); err != nil {
		return HelperInstallResult{}, fmt.Errorf("validate published helper: %w", err)
	}
	return HelperInstallResult{RemotePath: finalPath}, nil
}

func ensureCacheParents(ctx context.Context, filesystem remoteCacheFS, home string, owner uint32, cachePaths ...string) error {
	if err := ensureRemoteDirectory(ctx, filesystem, path.Join(home, ".cache"), owner, false); err != nil {
		return fmt.Errorf("ensure base cache: %w", err)
	}
	privatePaths := append([]string{
		path.Join(home, ".cache", "pi-desk"),
		path.Join(home, remoteHelperCacheRelative),
	}, cachePaths...)
	for index, cachePath := range privatePaths {
		if err := ensureRemoteDirectory(ctx, filesystem, cachePath, owner, true); err != nil {
			return fmt.Errorf("ensure private cache level %d: %w", index+1, err)
		}
	}
	return nil
}

func ensureRemoteDirectory(ctx context.Context, filesystem remoteCacheFS, directory string, owner uint32, private bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entry, err := filesystem.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		if err := filesystem.Mkdir(directory); err != nil {
			return waitForConcurrentCacheDirectory(ctx, filesystem, directory, owner, private)
		}
		if err := filesystem.Chmod(directory, 0o700); err != nil {
			return fmt.Errorf("%w: protect cache directory", ErrHelperInstall)
		}
		entry, err = filesystem.Lstat(directory)
	}
	validationErr := validateRemoteCacheDirectory(entry, err, owner, private)
	if validationErr != nil && err == nil && private && entry.isDir() && entry.Owner == owner {
		return waitForConcurrentCacheDirectory(ctx, filesystem, directory, owner, private)
	}
	return validationErr
}

func waitForConcurrentCacheDirectory(ctx context.Context, filesystem remoteCacheFS, directory string, owner uint32, private bool) error {
	deadline := time.Now().Add(concurrentCacheCreateWait)
	for {
		entry, err := filesystem.Lstat(directory)
		if err == nil {
			if validationErr := validateRemoteCacheDirectory(entry, nil, owner, private); validationErr == nil {
				return nil
			} else if !entry.isDir() || entry.Owner != owner {
				return validationErr
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return ErrHelperCacheUnsafe
		}
		if time.Now().After(deadline) {
			return ErrHelperCacheUnsafe
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(concurrentCacheCreateRetryDelay):
		}
	}
}

func validateRemoteCacheDirectory(entry remoteCacheEntry, err error, owner uint32, private bool) error {
	if err != nil || !entry.isDir() || entry.Owner != owner {
		return ErrHelperCacheUnsafe
	}
	permissions := entry.Mode.Perm()
	if permissions&0o022 != 0 || private && permissions != 0o700 {
		return ErrHelperCacheUnsafe
	}
	return nil
}

func validateExistingHelper(ctx context.Context, filesystem remoteCacheFS, helperPath string, owner uint32, artifact HelperArtifact) (bool, error) {
	entry, err := filesystem.Lstat(helperPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%w: inspect cached helper", ErrHelperInstall)
	}
	if !entry.isRegular() || entry.Owner != owner {
		return false, ErrHelperCacheUnsafe
	}
	if entry.Mode.Perm() != 0o700 || entry.Size != artifact.Size {
		return false, nil
	}
	matches, err := remoteFileMatches(ctx, filesystem, helperPath, artifact)
	return matches, err
}

func createTemporaryHelper(ctx context.Context, filesystem remoteCacheFS, directory string, owner uint32, artifact HelperArtifact, content []byte) (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("%w: create upload nonce", ErrHelperInstall)
	}
	temporaryPath := path.Join(directory, ".helper-upload-"+hex.EncodeToString(nonce[:]))
	file, err := filesystem.OpenFile(temporaryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("%w: create exclusive upload", ErrHelperInstall)
	}
	writeErr := writeAll(ctx, file, content)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		_ = filesystem.Remove(temporaryPath)
		return "", fmt.Errorf("%w: upload helper", ErrHelperInstall)
	}
	if closeErr != nil {
		_ = filesystem.Remove(temporaryPath)
		return "", fmt.Errorf("%w: close helper upload", ErrHelperInstall)
	}
	if err := filesystem.Chmod(temporaryPath, 0o700); err != nil {
		_ = filesystem.Remove(temporaryPath)
		return "", fmt.Errorf("%w: make helper executable", ErrHelperInstall)
	}
	entry, err := filesystem.Lstat(temporaryPath)
	if err != nil || !entry.isRegular() || entry.Owner != owner || entry.Mode.Perm() != 0o700 || entry.Size != artifact.Size {
		_ = filesystem.Remove(temporaryPath)
		return "", ErrHelperCacheUnsafe
	}
	if matches, err := remoteFileMatches(ctx, filesystem, temporaryPath, artifact); err != nil || !matches {
		_ = filesystem.Remove(temporaryPath)
		if err != nil {
			return "", err
		}
		return "", ErrHelperArtifactIntegrity
	}
	return temporaryPath, nil
}

func validateInstalledHelper(ctx context.Context, filesystem remoteCacheFS, helperPath string, owner uint32, artifact HelperArtifact) error {
	entry, err := filesystem.Lstat(helperPath)
	if err != nil || !entry.isRegular() || entry.Owner != owner || entry.Mode.Perm() != 0o700 || entry.Size != artifact.Size {
		return ErrHelperCacheUnsafe
	}
	matches, err := remoteFileMatches(ctx, filesystem, helperPath, artifact)
	if err != nil {
		return err
	}
	if !matches {
		return ErrHelperArtifactIntegrity
	}
	return nil
}

var errRemoteFileTooLarge = errors.New("remote helper file exceeds expected size")

func remoteFileMatches(ctx context.Context, filesystem remoteCacheFS, filename string, artifact HelperArtifact) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	file, err := filesystem.Open(filename)
	if err != nil {
		return false, fmt.Errorf("%w: open helper for verification", ErrHelperInstall)
	}
	defer file.Close()
	writer := &boundedHashWriter{hash: sha256.New(), remaining: artifact.Size + 1}
	var written int64
	if optimized, ok := file.(interface {
		WriteTo(io.Writer) (int64, error)
	}); ok {
		written, err = optimized.WriteTo(writer)
	} else {
		written, err = io.Copy(writer, io.LimitReader(file, artifact.Size+1))
	}
	if err != nil && !errors.Is(err, errRemoteFileTooLarge) {
		return false, fmt.Errorf("%w: read helper for verification: %w", ErrHelperInstall, err)
	}
	if written != artifact.Size {
		return false, nil
	}
	return hex.EncodeToString(writer.hash.Sum(nil)) == artifact.SHA256, nil
}

type boundedHashWriter struct {
	hash      hash.Hash
	remaining int64
}

func (writer *boundedHashWriter) Write(value []byte) (int, error) {
	if writer.remaining <= 0 {
		return 0, errRemoteFileTooLarge
	}
	if int64(len(value)) > writer.remaining {
		value = value[:writer.remaining]
		_, _ = writer.hash.Write(value)
		writer.remaining = 0
		return len(value), errRemoteFileTooLarge
	}
	_, _ = writer.hash.Write(value)
	writer.remaining -= int64(len(value))
	return len(value), nil
}

func writeAll(ctx context.Context, writer io.Writer, content []byte) error {
	for len(content) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		written, err := writer.Write(content)
		if err != nil {
			return err
		}
		if written <= 0 || written > len(content) {
			return io.ErrShortWrite
		}
		content = content[written:]
	}
	return nil
}

func validateRemoteHome(value string) error {
	if value == "" || value == "/" || len(value) > maxRemoteHomeBytes || !utf8.ValidString(value) || !strings.HasPrefix(value, "/") || path.Clean(value) != value {
		return ErrHelperCacheUnsafe
	}
	for _, character := range value {
		if character == 0 || unicode.IsControl(character) {
			return ErrHelperCacheUnsafe
		}
	}
	return nil
}
