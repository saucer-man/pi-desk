package remotehelper

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	MethodRootOpen      = "root.open"
	maxRootPathBytes    = 4096
	maxRootCapabilities = 32
)

var (
	ErrRootInvalid       = errors.New("remote root request is invalid")
	ErrRootOpen          = errors.New("remote root could not be opened")
	ErrRootUnsupported   = errors.New("remote root identity is unsupported")
	ErrRootResourceLimit = errors.New("remote root capability limit reached")
)

type RootOpenRequest struct {
	Path string `json:"path"`
}

type RootOpenResponse struct {
	Handle        string `json:"handle"`
	CanonicalPath string `json:"canonicalPath"`
	Device        uint64 `json:"device"`
	Inode         uint64 `json:"inode"`
}

type rootCapabilityManager interface {
	Open(context.Context, string) (RootOpenResponse, error)
	Stat(context.Context, string, string) (FileInfoResponse, error)
	List(context.Context, string, string) (FileListResponse, error)
	Read(context.Context, FileReadRequest) (FileReadResponse, error)
	Image(context.Context, string, string) (FileImageResponse, []byte, error)
	Content(context.Context, string, string) (FileContentResponse, []byte, error)
	Hash(context.Context, string, string) (FileHashResponse, error)
	Write(context.Context, FileWriteRequest, []byte) (FileWriteResponse, error)
	Mkdir(context.Context, FileMkdirRequest) (FileMkdirResponse, error)
	Find(context.Context, SearchFindRequest) (SearchFindResponse, error)
	Grep(context.Context, SearchGrepRequest) (SearchGrepResponse, error)
	Git(context.Context, GitReadRequest) (GitReadResponse, []byte, error)
	Close() error
}

type rootManager struct {
	mu       sync.Mutex
	gitMu    sync.Mutex
	byID     map[rootFileIdentity]*rootCapability
	byHandle map[string]*rootCapability
}

type rootCapability struct {
	handle     string
	canonical  string
	identity   rootFileIdentity
	root       *os.Root
	info       os.FileInfo
	mutationMu sync.Mutex
}

func newRootManager() *rootManager {
	return &rootManager{
		byID:     make(map[rootFileIdentity]*rootCapability),
		byHandle: make(map[string]*rootCapability),
	}
}

func (manager *rootManager) Open(ctx context.Context, requested string) (RootOpenResponse, error) {
	if err := ctx.Err(); err != nil {
		return RootOpenResponse{}, err
	}
	if err := validateRootPath(requested); err != nil {
		return RootOpenResponse{}, err
	}
	canonical, err := filepath.EvalSymlinks(requested)
	if err != nil {
		return RootOpenResponse{}, ErrRootOpen
	}
	canonical = filepath.Clean(canonical)
	if err := validateRootPath(filepath.ToSlash(canonical)); err != nil {
		return RootOpenResponse{}, ErrRootUnsupported
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return RootOpenResponse{}, ErrRootOpen
	}
	identity, err := rootIdentity(info)
	if err != nil || identity.Device == 0 || identity.Inode == 0 {
		return RootOpenResponse{}, ErrRootUnsupported
	}
	if err := ctx.Err(); err != nil {
		return RootOpenResponse{}, err
	}

	manager.mu.Lock()
	defer manager.mu.Unlock()
	if existing := manager.byID[identity]; existing != nil {
		current, err := os.Stat(existing.canonical)
		if err != nil || !os.SameFile(existing.info, current) {
			return RootOpenResponse{}, ErrRootOpen
		}
		return existing.response(), nil
	}
	if len(manager.byHandle) >= maxRootCapabilities {
		return RootOpenResponse{}, ErrRootResourceLimit
	}
	root, err := os.OpenRoot(canonical)
	if err != nil {
		return RootOpenResponse{}, ErrRootOpen
	}
	openedInfo, err := root.Stat(".")
	if err != nil || !os.SameFile(info, openedInfo) {
		_ = root.Close()
		return RootOpenResponse{}, ErrRootOpen
	}
	openedIdentity, err := rootIdentity(openedInfo)
	if err != nil || openedIdentity != identity {
		_ = root.Close()
		return RootOpenResponse{}, ErrRootUnsupported
	}
	handle := ""
	for range 4 {
		candidate, err := newRootHandle()
		if err != nil {
			_ = root.Close()
			return RootOpenResponse{}, ErrRootOpen
		}
		if manager.byHandle[candidate] == nil {
			handle = candidate
			break
		}
	}
	if handle == "" {
		_ = root.Close()
		return RootOpenResponse{}, ErrRootOpen
	}
	capability := &rootCapability{
		handle: handle, canonical: filepath.ToSlash(canonical), identity: identity,
		root: root, info: openedInfo,
	}
	manager.byID[identity] = capability
	manager.byHandle[handle] = capability
	return capability.response(), nil
}

func (manager *rootManager) Close() error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	var result error
	for _, capability := range manager.byHandle {
		if err := capability.root.Close(); err != nil {
			result = errors.Join(result, err)
		}
	}
	manager.byID = make(map[rootFileIdentity]*rootCapability)
	manager.byHandle = make(map[string]*rootCapability)
	return result
}

func (capability *rootCapability) response() RootOpenResponse {
	return RootOpenResponse{
		Handle: capability.handle, CanonicalPath: capability.canonical,
		Device: capability.identity.Device, Inode: capability.identity.Inode,
	}
}

func validateRootPath(value string) error {
	if value == "" || len(value) > maxRootPathBytes || !utf8.ValidString(value) || !path.IsAbs(value) || path.Clean(value) != value {
		return ErrRootInvalid
	}
	for _, char := range value {
		if char == 0 || unicode.IsControl(char) || unicode.Is(unicode.Cf, char) {
			return ErrRootInvalid
		}
	}
	if strings.Contains(value, "\\") {
		return ErrRootInvalid
	}
	return nil
}

func newRootHandle() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "root-" + hex.EncodeToString(value[:]), nil
}
