package remotehelper

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MethodFileStat    = "file.stat"
	MethodFileList    = "file.list"
	MethodFileRead    = "file.read"
	MethodFileImage   = "file.image"
	MethodFileHash    = "file.hash"
	MethodFileContent = "file.content"

	maxRelativePathBytes  = 4096
	maxPathComponentBytes = 255
	maxPathDepth          = 64
	maxListEntries        = 5000
	maxListPathBytes      = 256 << 10
	maxReadLines          = 2000
	maxReadOutputBytes    = 50 << 10
	maxReadLineBytes      = 1 << 20
	maxReadableFileBytes  = 16 << 20
	maxImageFileBytes     = 10 << 20
)

var (
	ErrFileInvalidPath = errors.New("remote file path is invalid")
	ErrFileNotFound    = errors.New("remote file was not found")
	ErrFileUnsupported = errors.New("remote file type, encoding, or layout is unsupported")
	ErrFileOutputLimit = errors.New("remote file exceeds the read safety limit")
)

type FileRequest struct {
	RootHandle string `json:"rootHandle"`
	Path       string `json:"path"`
}

type FileReadRequest struct {
	RootHandle string `json:"rootHandle"`
	Path       string `json:"path"`
	StartLine  int    `json:"startLine"`
	MaxLines   int    `json:"maxLines"`
}

type FileInfoResponse struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Size    int64  `json:"size"`
	Mode    uint32 `json:"mode"`
	ModTime int64  `json:"modTimeMillis"`
}

type FileListResponse struct {
	Path                    string             `json:"path"`
	Entries                 []FileInfoResponse `json:"entries"`
	SkippedUnsupportedPaths int                `json:"skippedUnsupportedPaths"`
	Truncated               bool               `json:"truncated"`
}

type FileReadResponse struct {
	Path          string `json:"path"`
	Content       string `json:"content"`
	StartLine     int    `json:"startLine"`
	EndLine       int    `json:"endLine"`
	NextLine      int    `json:"nextLine,omitempty"`
	Truncated     bool   `json:"truncated"`
	LineTruncated bool   `json:"lineTruncated"`
}

type FileImageResponse struct {
	Path   string `json:"path"`
	MIME   string `json:"mime"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type FileContentResponse struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type FileHashResponse struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func (manager *rootManager) Stat(ctx context.Context, handle, logicalPath string) (FileInfoResponse, error) {
	if err := ctx.Err(); err != nil {
		return FileInfoResponse{}, err
	}
	capability, err := manager.lookup(handle)
	if err != nil {
		return FileInfoResponse{}, err
	}
	if err := validateRelativePath(logicalPath, true); err != nil {
		return FileInfoResponse{}, err
	}
	info, err := capability.root.Lstat(logicalPath)
	if err != nil {
		return FileInfoResponse{}, ErrFileNotFound
	}
	response, ok := projectFileInfo(logicalPath, info.Mode(), info.Size(), info.ModTime().UnixMilli())
	if !ok {
		return FileInfoResponse{}, ErrFileUnsupported
	}
	return response, nil
}

func (manager *rootManager) List(ctx context.Context, handle, logicalPath string) (FileListResponse, error) {
	if err := ctx.Err(); err != nil {
		return FileListResponse{}, err
	}
	capability, err := manager.lookup(handle)
	if err != nil {
		return FileListResponse{}, err
	}
	if err := validateRelativePath(logicalPath, true); err != nil {
		return FileListResponse{}, err
	}
	directory, err := openRootRead(capability.root, logicalPath)
	if err != nil {
		return FileListResponse{}, ErrFileNotFound
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil || !info.IsDir() {
		return FileListResponse{}, ErrFileUnsupported
	}
	entries, err := directory.ReadDir(maxListEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return FileListResponse{}, ErrFileNotFound
	}
	response := FileListResponse{Path: logicalPath, Entries: make([]FileInfoResponse, 0, min(len(entries), maxListEntries))}
	if len(entries) > maxListEntries {
		entries = entries[:maxListEntries]
		response.Truncated = true
	}
	pathBytes := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return FileListResponse{}, err
		}
		name := entry.Name()
		if !validPathComponent(name) {
			response.SkippedUnsupportedPaths++
			continue
		}
		entryInfo, err := entry.Info()
		if err != nil {
			response.SkippedUnsupportedPaths++
			continue
		}
		entryPath := name
		if logicalPath != "." {
			entryPath = path.Join(logicalPath, name)
		}
		projected, ok := projectFileInfo(entryPath, entryInfo.Mode(), entryInfo.Size(), entryInfo.ModTime().UnixMilli())
		if !ok {
			response.SkippedUnsupportedPaths++
			continue
		}
		if pathBytes+len(entryPath) > maxListPathBytes {
			response.Truncated = true
			break
		}
		pathBytes += len(entryPath)
		response.Entries = append(response.Entries, projected)
	}
	slices.SortFunc(response.Entries, func(left, right FileInfoResponse) int {
		return strings.Compare(left.Path, right.Path)
	})
	return response, nil
}

func (manager *rootManager) Read(ctx context.Context, request FileReadRequest) (FileReadResponse, error) {
	if err := ctx.Err(); err != nil {
		return FileReadResponse{}, err
	}
	capability, err := manager.lookup(request.RootHandle)
	if err != nil {
		return FileReadResponse{}, err
	}
	if err := validateRelativePath(request.Path, false); err != nil || request.StartLine < 1 || request.MaxLines < 1 || request.MaxLines > maxReadLines {
		return FileReadResponse{}, ErrFileInvalidPath
	}
	file, err := openRootRead(capability.root, request.Path)
	if err != nil {
		return FileReadResponse{}, ErrFileNotFound
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return FileReadResponse{}, ErrFileUnsupported
	}
	if info.Size() > maxReadableFileBytes {
		return FileReadResponse{}, ErrFileOutputLimit
	}

	response := FileReadResponse{Path: request.Path, StartLine: request.StartLine}
	scanner := bufio.NewScanner(io.LimitReader(file, maxReadableFileBytes+1))
	scanner.Buffer(make([]byte, 64<<10), maxReadLineBytes+1)
	var builder strings.Builder
	lineNumber := 0
	included := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return FileReadResponse{}, err
		}
		lineNumber++
		line := scanner.Bytes()
		if !utf8.Valid(line) {
			return FileReadResponse{}, ErrFileUnsupported
		}
		if lineNumber < request.StartLine {
			continue
		}
		if included >= request.MaxLines {
			response.Truncated = true
			response.NextLine = lineNumber
			break
		}
		separatorBytes := 0
		if included > 0 {
			separatorBytes = 1
		}
		available := maxReadOutputBytes - builder.Len() - separatorBytes
		if available <= 0 {
			response.Truncated = true
			response.NextLine = lineNumber
			break
		}
		lineText := string(line)
		if len(lineText) > available {
			lineText = truncateUTF8(lineText, available)
			response.Truncated = true
			response.LineTruncated = true
		}
		if included > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(lineText)
		included++
		response.EndLine = lineNumber
		if response.LineTruncated {
			response.NextLine = lineNumber + 1
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return FileReadResponse{}, ErrFileUnsupported
	}
	response.Content = builder.String()
	return response, nil
}

func (manager *rootManager) Image(ctx context.Context, handle, logicalPath string) (FileImageResponse, []byte, error) {
	if err := ctx.Err(); err != nil {
		return FileImageResponse{}, nil, err
	}
	capability, err := manager.lookup(handle)
	if err != nil {
		return FileImageResponse{}, nil, err
	}
	if err := validateRelativePath(logicalPath, false); err != nil {
		return FileImageResponse{}, nil, err
	}
	file, err := openRootRead(capability.root, logicalPath)
	if err != nil {
		return FileImageResponse{}, nil, ErrFileNotFound
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return FileImageResponse{}, nil, ErrFileUnsupported
	}
	if info.Size() < 0 || info.Size() > maxImageFileBytes {
		return FileImageResponse{}, nil, ErrFileOutputLimit
	}
	content, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: file}, maxImageFileBytes+1))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return FileImageResponse{}, nil, err
		}
		return FileImageResponse{}, nil, ErrFileUnsupported
	}
	if int64(len(content)) != info.Size() || len(content) > maxImageFileBytes {
		return FileImageResponse{}, nil, ErrFileOutputLimit
	}
	mime := imageMIME(content)
	if mime == "" {
		return FileImageResponse{}, nil, ErrFileUnsupported
	}
	digest := sha256.Sum256(content)
	return FileImageResponse{
		Path: logicalPath, MIME: mime, Size: info.Size(), SHA256: hex.EncodeToString(digest[:]),
	}, content, nil
}

func imageMIME(content []byte) string {
	switch {
	case len(content) >= 8 && string(content[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(content) >= 3 && content[0] == 0xff && content[1] == 0xd8 && content[2] == 0xff:
		return "image/jpeg"
	case len(content) >= 6 && (string(content[:6]) == "GIF87a" || string(content[:6]) == "GIF89a"):
		return "image/gif"
	case len(content) >= 12 && string(content[:4]) == "RIFF" && string(content[8:12]) == "WEBP":
		return "image/webp"
	case len(content) >= 2 && string(content[:2]) == "BM":
		return "image/bmp"
	default:
		return ""
	}
}

func (manager *rootManager) Content(ctx context.Context, handle, logicalPath string) (FileContentResponse, []byte, error) {
	if err := ctx.Err(); err != nil {
		return FileContentResponse{}, nil, err
	}
	capability, err := manager.lookup(handle)
	if err != nil {
		return FileContentResponse{}, nil, err
	}
	if err := validateRelativePath(logicalPath, false); err != nil {
		return FileContentResponse{}, nil, err
	}
	file, err := openRootRead(capability.root, logicalPath)
	if err != nil {
		return FileContentResponse{}, nil, ErrFileNotFound
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 {
		return FileContentResponse{}, nil, ErrFileUnsupported
	}
	if info.Size() > maxReadableFileBytes {
		return FileContentResponse{}, nil, ErrFileOutputLimit
	}
	content, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: file}, maxReadableFileBytes+1))
	if err != nil {
		if isContextError(err) {
			return FileContentResponse{}, nil, err
		}
		return FileContentResponse{}, nil, ErrFileUnsupported
	}
	if int64(len(content)) != info.Size() || len(content) > maxReadableFileBytes {
		return FileContentResponse{}, nil, ErrFileOutputLimit
	}
	digest := sha256.Sum256(content)
	return FileContentResponse{Path: logicalPath, Size: info.Size(), SHA256: hex.EncodeToString(digest[:])}, content, nil
}

func (manager *rootManager) Hash(ctx context.Context, handle, logicalPath string) (FileHashResponse, error) {
	if err := ctx.Err(); err != nil {
		return FileHashResponse{}, err
	}
	capability, err := manager.lookup(handle)
	if err != nil {
		return FileHashResponse{}, err
	}
	if err := validateRelativePath(logicalPath, false); err != nil {
		return FileHashResponse{}, err
	}
	file, err := openRootRead(capability.root, logicalPath)
	if err != nil {
		return FileHashResponse{}, ErrFileNotFound
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return FileHashResponse{}, ErrFileUnsupported
	}
	if info.Size() > maxReadableFileBytes {
		return FileHashResponse{}, ErrFileOutputLimit
	}
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(&contextReader{ctx: ctx, reader: file}, maxReadableFileBytes+1))
	if err != nil || written != info.Size() {
		return FileHashResponse{}, ErrFileOutputLimit
	}
	return FileHashResponse{Path: logicalPath, Size: info.Size(), SHA256: hex.EncodeToString(digest.Sum(nil))}, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

func (manager *rootManager) lookup(handle string) (*rootCapability, error) {
	if !validRootHandle(handle) {
		return nil, ErrFileInvalidPath
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	capability := manager.byHandle[handle]
	if capability == nil {
		return nil, ErrFileInvalidPath
	}
	return capability, nil
}

func validateRelativePath(value string, allowRoot bool) error {
	if value == "." {
		if allowRoot {
			return nil
		}
		return ErrFileInvalidPath
	}
	if value == "" || len(value) > maxRelativePathBytes || !utf8.ValidString(value) || path.IsAbs(value) || path.Clean(value) != value || strings.Contains(value, "\\") {
		return ErrFileInvalidPath
	}
	components := strings.Split(value, "/")
	if len(components) > maxPathDepth {
		return ErrFileInvalidPath
	}
	for _, component := range components {
		if !validPathComponent(component) {
			return ErrFileInvalidPath
		}
	}
	return nil
}

func validPathComponent(value string) bool {
	if value == "" || value == "." || value == ".." || len(value) > maxPathComponentBytes || !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if char == 0 || char == '/' || unicode.IsControl(char) || unicode.Is(unicode.Cf, char) {
			return false
		}
	}
	return true
}

func validRootHandle(value string) bool {
	if !strings.HasPrefix(value, "root-") || len(value) != len("root-")+32 {
		return false
	}
	for _, char := range value[len("root-"):] {
		if char < '0' || (char > '9' && char < 'a') || char > 'f' {
			return false
		}
	}
	return true
}

func projectFileInfo(logicalPath string, mode fs.FileMode, size, modTime int64) (FileInfoResponse, bool) {
	kind := ""
	switch {
	case mode.IsRegular():
		kind = "file"
	case mode.IsDir():
		kind = "directory"
	case mode&fs.ModeSymlink != 0:
		kind = "symlink"
	default:
		return FileInfoResponse{}, false
	}
	return FileInfoResponse{Path: logicalPath, Kind: kind, Size: size, Mode: uint32(mode.Perm()), ModTime: modTime}, true
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
