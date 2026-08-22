package remotessh

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxRuntimeRelativePathBytes  = 4096
	maxRuntimePathComponentBytes = 255
	maxRuntimePathDepth          = 64
	maxRuntimeListEntries        = 5000
	maxRuntimeReadLines          = 2000
	maxRuntimeReadBytes          = 50 << 10
	maxRuntimeImageBytes         = 10 << 20
	maxRuntimeWriteBytes         = 16 << 20
)

var (
	ErrRuntimeFileInvalid     = errors.New("remote file request is invalid")
	ErrRuntimeFileNotFound    = errors.New("remote file was not found")
	ErrRuntimeFileUnsupported = errors.New("remote file type, encoding, or layout is unsupported")
	ErrRuntimeFileOutputLimit = errors.New("remote file exceeds the output safety limit")
	ErrRuntimeFileConflict    = errors.New("remote file changed before conditional write")
	ErrRuntimeFileWrite       = errors.New("remote file could not be written atomically")
	ErrRuntimeOutcomeUnknown  = errors.New("remote mutation outcome is unknown")
)

type RuntimeFileInfo struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Size    int64  `json:"size"`
	Mode    uint32 `json:"mode"`
	ModTime int64  `json:"modTime"`
}

type RuntimeFileList struct {
	Path                    string            `json:"path"`
	Entries                 []RuntimeFileInfo `json:"entries"`
	SkippedUnsupportedPaths int               `json:"skippedUnsupportedPaths"`
	Truncated               bool              `json:"truncated"`
}

type RuntimeFileRead struct {
	Path          string `json:"path"`
	Content       string `json:"content"`
	StartLine     int    `json:"startLine"`
	EndLine       int    `json:"endLine"`
	NextLine      int    `json:"nextLine"`
	Truncated     bool   `json:"truncated"`
	LineTruncated bool   `json:"lineTruncated"`
}

type RuntimeFileImage struct {
	Path    string `json:"path"`
	MIME    string `json:"mime"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	Content []byte `json:"-"`
}

type RuntimeFileMkdirResult struct {
	Path    string   `json:"path"`
	Created []string `json:"created"`
}

type RuntimeFileContent struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	Content []byte `json:"-"`
}

type RuntimeFileEditRequest struct {
	Path    string
	OldText string
	NewText string
}

type RuntimeFileReplacement struct {
	OldText string
	NewText string
}

type RuntimeFileWriteRequest struct {
	Path           string
	Content        []byte
	ExpectedSHA256 string
	ExpectedAbsent bool
}

type RuntimeFileWriteResult struct {
	Path                         string `json:"path"`
	Size                         int64  `json:"size"`
	SHA256                       string `json:"sha256"`
	Created                      bool   `json:"created"`
	ExtendedMetadataNotPreserved bool   `json:"extendedMetadataNotPreserved"`
}

type RuntimeFileHash struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type runtimeFileGeneration interface {
	StatFile(context.Context, string, string) (RuntimeFileInfo, error)
	ListFiles(context.Context, string, string) (RuntimeFileList, error)
	ReadFile(context.Context, string, string, int, int) (RuntimeFileRead, error)
	ReadImage(context.Context, string, string) (RuntimeFileImage, error)
	HashFile(context.Context, string, string) (RuntimeFileHash, error)
	Content(context.Context, string, string) (RuntimeFileContent, error)
	WriteFile(context.Context, string, RuntimeFileWriteRequest) (RuntimeFileWriteResult, error)
	Mkdir(context.Context, string, string) (RuntimeFileMkdirResult, error)
}

func (supervisor *RuntimeLeaseSupervisor) StatFile(ctx context.Context, lease *RuntimeLease, logicalPath string) (RuntimeFileInfo, error) {
	if !validRemoteRelativePath(logicalPath, true) {
		return RuntimeFileInfo{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeFileInvalid)
	}
	generation, rootHandle, err := supervisor.authorizeFileLease(lease)
	if err != nil {
		return RuntimeFileInfo{}, err
	}
	response, err := generation.StatFile(ctx, rootHandle, logicalPath)
	if err != nil {
		return RuntimeFileInfo{}, err
	}
	if !validRuntimeFileInfo(response, logicalPath) {
		supervisor.Disconnect(context.Background())
		return RuntimeFileInfo{}, runtimeLifecycleError(ErrHelperProtocolMismatch)
	}
	return response, nil
}

func (supervisor *RuntimeLeaseSupervisor) ListFiles(ctx context.Context, lease *RuntimeLease, logicalPath string) (RuntimeFileList, error) {
	if !validRemoteRelativePath(logicalPath, true) {
		return RuntimeFileList{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeFileInvalid)
	}
	generation, rootHandle, err := supervisor.authorizeFileLease(lease)
	if err != nil {
		return RuntimeFileList{}, err
	}
	response, err := generation.ListFiles(ctx, rootHandle, logicalPath)
	if err != nil {
		return RuntimeFileList{}, err
	}
	if response.Path != logicalPath || response.SkippedUnsupportedPaths < 0 || len(response.Entries) > maxRuntimeListEntries {
		supervisor.Disconnect(context.Background())
		return RuntimeFileList{}, runtimeLifecycleError(ErrHelperProtocolMismatch)
	}
	for _, entry := range response.Entries {
		if !validRuntimeFileInfo(entry, entry.Path) || !directChildPath(logicalPath, entry.Path) {
			supervisor.Disconnect(context.Background())
			return RuntimeFileList{}, runtimeLifecycleError(ErrHelperProtocolMismatch)
		}
	}
	return response, nil
}

func (supervisor *RuntimeLeaseSupervisor) ReadFile(ctx context.Context, lease *RuntimeLease, logicalPath string, startLine, maxLines int) (RuntimeFileRead, error) {
	if !validRemoteRelativePath(logicalPath, false) || startLine < 1 || maxLines < 1 || maxLines > maxRuntimeReadLines {
		return RuntimeFileRead{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeFileInvalid)
	}
	generation, rootHandle, err := supervisor.authorizeFileLease(lease)
	if err != nil {
		return RuntimeFileRead{}, err
	}
	response, err := generation.ReadFile(ctx, rootHandle, logicalPath, startLine, maxLines)
	if err != nil {
		return RuntimeFileRead{}, err
	}
	lineCount := 0
	if response.EndLine >= response.StartLine {
		lineCount = response.EndLine - response.StartLine + 1
	}
	if response.Path != logicalPath || response.StartLine != startLine || response.EndLine < 0 || response.EndLine > 0 && response.EndLine < startLine || lineCount > maxLines || response.NextLine < 0 || len(response.Content) > maxRuntimeReadBytes || !utf8.ValidString(response.Content) {
		supervisor.Disconnect(context.Background())
		return RuntimeFileRead{}, runtimeLifecycleError(ErrHelperProtocolMismatch)
	}
	return response, nil
}

func (supervisor *RuntimeLeaseSupervisor) ReadImage(ctx context.Context, lease *RuntimeLease, logicalPath string) (RuntimeFileImage, error) {
	if !validRemoteRelativePath(logicalPath, false) {
		return RuntimeFileImage{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeFileInvalid)
	}
	generation, rootHandle, err := supervisor.authorizeFileLease(lease)
	if err != nil {
		return RuntimeFileImage{}, err
	}
	response, err := generation.ReadImage(ctx, rootHandle, logicalPath)
	if err != nil {
		return RuntimeFileImage{}, err
	}
	if !validRuntimeFileImage(response, logicalPath) {
		supervisor.Disconnect(context.Background())
		return RuntimeFileImage{}, runtimeLifecycleError(ErrHelperProtocolMismatch)
	}
	return response, nil
}

func validRuntimeFileImage(image RuntimeFileImage, expectedPath string) bool {
	if image.Path != expectedPath || image.Size < 0 || image.Size > maxRuntimeImageBytes || int64(len(image.Content)) != image.Size || !validLowerHex(image.SHA256, 64) || runtimeImageMIME(image.Content) != image.MIME {
		return false
	}
	digest := sha256.Sum256(image.Content)
	return hex.EncodeToString(digest[:]) == image.SHA256
}

func runtimeImageMIME(content []byte) string {
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

func (supervisor *RuntimeLeaseSupervisor) HashFile(ctx context.Context, lease *RuntimeLease, logicalPath string) (RuntimeFileHash, error) {
	if !validRemoteRelativePath(logicalPath, false) {
		return RuntimeFileHash{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeFileInvalid)
	}
	generation, rootHandle, err := supervisor.authorizeFileLease(lease)
	if err != nil {
		return RuntimeFileHash{}, err
	}
	response, err := generation.HashFile(ctx, rootHandle, logicalPath)
	if err != nil {
		return RuntimeFileHash{}, err
	}
	if response.Path != logicalPath || response.Size < 0 || !validLowerHex(response.SHA256, 64) {
		supervisor.Disconnect(context.Background())
		return RuntimeFileHash{}, runtimeLifecycleError(ErrHelperProtocolMismatch)
	}
	return response, nil
}

func (supervisor *RuntimeLeaseSupervisor) EnsureDirectory(ctx context.Context, lease *RuntimeLease, logicalPath string) (RuntimeFileMkdirResult, error) {
	if !validRemoteRelativePath(logicalPath, false) {
		return RuntimeFileMkdirResult{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeFileInvalid)
	}
	generation, rootHandle, err := supervisor.authorizeMutationLease(lease)
	if err != nil {
		return RuntimeFileMkdirResult{}, err
	}
	response, err := generation.Mkdir(ctx, rootHandle, logicalPath)
	if err != nil {
		supervisor.revokeOutcomeUnknown(err)
		return RuntimeFileMkdirResult{}, err
	}
	if response.Path != logicalPath || !validCreatedDirectories(response.Created, logicalPath) {
		supervisor.Disconnect(context.Background())
		return RuntimeFileMkdirResult{}, runtimeOutcomeUnknownError()
	}
	return response, nil
}

func validCreatedDirectories(created []string, target string) bool {
	previous := ""
	seen := make(map[string]struct{}, len(created))
	for _, directory := range created {
		if !validRemoteRelativePath(directory, false) || !strings.HasPrefix(target+"/", directory+"/") {
			return false
		}
		if _, duplicate := seen[directory]; duplicate {
			return false
		}
		if previous != "" && !strings.HasPrefix(directory+"/", previous+"/") {
			return false
		}
		seen[directory] = struct{}{}
		previous = directory
	}
	return true
}

func (supervisor *RuntimeLeaseSupervisor) EditFile(ctx context.Context, lease *RuntimeLease, request RuntimeFileEditRequest) (RuntimeFileWriteResult, error) {
	return supervisor.EditFileAll(ctx, lease, request.Path, []RuntimeFileReplacement{{OldText: request.OldText, NewText: request.NewText}})
}

func (supervisor *RuntimeLeaseSupervisor) EditFileAll(ctx context.Context, lease *RuntimeLease, logicalPath string, replacements []RuntimeFileReplacement) (RuntimeFileWriteResult, error) {
	if !validRemoteRelativePath(logicalPath, false) || len(replacements) == 0 || len(replacements) > 256 {
		return RuntimeFileWriteResult{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeFileInvalid)
	}
	for _, replacement := range replacements {
		if replacement.OldText == "" || len(replacement.OldText) > maxRuntimeWriteBytes || len(replacement.NewText) > maxRuntimeWriteBytes || !validRuntimeEditText(replacement.OldText) || !validRuntimeEditText(replacement.NewText) {
			return RuntimeFileWriteResult{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeFileInvalid)
		}
	}
	generation, rootHandle, err := supervisor.authorizeMutationLease(lease)
	if err != nil {
		return RuntimeFileWriteResult{}, err
	}
	current, err := generation.Content(ctx, rootHandle, logicalPath)
	if err != nil {
		return RuntimeFileWriteResult{}, err
	}
	if !validRuntimeFileContent(current, logicalPath) {
		supervisor.Disconnect(context.Background())
		return RuntimeFileWriteResult{}, runtimeLifecycleError(ErrHelperProtocolMismatch)
	}
	if !utf8.Valid(current.Content) || bytes.IndexByte(current.Content, 0) >= 0 {
		return RuntimeFileWriteResult{}, runtimeFileError(FailureUnsupportedFileLayout, ReasonUnsupportedFileLayout, ErrRuntimeFileUnsupported)
	}
	type span struct{ start, end, replacement int }
	spans := make([]span, 0, len(replacements))
	for index, replacement := range replacements {
		position := bytes.Index(current.Content, []byte(replacement.OldText))
		if position < 0 || bytes.Index(current.Content[position+len(replacement.OldText):], []byte(replacement.OldText)) >= 0 {
			return RuntimeFileWriteResult{}, runtimeFileError(FailureFileConflict, ReasonFileConflict, ErrRuntimeFileConflict)
		}
		spans = append(spans, span{start: position, end: position + len(replacement.OldText), replacement: index})
	}
	slices.SortFunc(spans, func(left, right span) int { return left.start - right.start })
	for index := 1; index < len(spans); index++ {
		if spans[index].start < spans[index-1].end {
			return RuntimeFileWriteResult{}, runtimeFileError(FailureFileConflict, ReasonFileConflict, ErrRuntimeFileConflict)
		}
	}
	var updated bytes.Buffer
	previous := 0
	for _, currentSpan := range spans {
		updated.Write(current.Content[previous:currentSpan.start])
		updated.WriteString(replacements[currentSpan.replacement].NewText)
		previous = currentSpan.end
		if updated.Len() > maxRuntimeWriteBytes {
			return RuntimeFileWriteResult{}, runtimeFileError(FailureOutputLimit, ReasonOutputLimit, ErrRuntimeFileOutputLimit)
		}
	}
	updated.Write(current.Content[previous:])
	if updated.Len() > maxRuntimeWriteBytes {
		return RuntimeFileWriteResult{}, runtimeFileError(FailureOutputLimit, ReasonOutputLimit, ErrRuntimeFileOutputLimit)
	}
	content := updated.Bytes()
	response, err := generation.WriteFile(ctx, rootHandle, RuntimeFileWriteRequest{Path: logicalPath, Content: content, ExpectedSHA256: current.SHA256})
	if err != nil {
		supervisor.revokeOutcomeUnknown(err)
		return RuntimeFileWriteResult{}, err
	}
	if !validRuntimeWriteResult(response, logicalPath, content, false) {
		supervisor.Disconnect(context.Background())
		return RuntimeFileWriteResult{}, runtimeOutcomeUnknownError()
	}
	return response, nil
}

func validRuntimeEditText(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func validRuntimeFileContent(content RuntimeFileContent, expectedPath string) bool {
	if content.Path != expectedPath || content.Size < 0 || content.Size > maxRuntimeWriteBytes || int64(len(content.Content)) != content.Size || !validLowerHex(content.SHA256, 64) {
		return false
	}
	digest := sha256.Sum256(content.Content)
	return hex.EncodeToString(digest[:]) == content.SHA256
}

func (supervisor *RuntimeLeaseSupervisor) WriteFile(ctx context.Context, lease *RuntimeLease, request RuntimeFileWriteRequest) (RuntimeFileWriteResult, error) {
	if !validRemoteRelativePath(request.Path, false) || len(request.Content) > maxRuntimeWriteBytes || request.ExpectedAbsent == (request.ExpectedSHA256 != "") || request.ExpectedSHA256 != "" && !validLowerHex(request.ExpectedSHA256, 64) {
		return RuntimeFileWriteResult{}, runtimeFileError(FailureInvalidRequest, ReasonInvalidRequest, ErrRuntimeFileInvalid)
	}
	generation, rootHandle, err := supervisor.authorizeMutationLease(lease)
	if err != nil {
		return RuntimeFileWriteResult{}, err
	}
	request.Content = append([]byte(nil), request.Content...)
	response, err := generation.WriteFile(ctx, rootHandle, request)
	if err != nil {
		supervisor.revokeOutcomeUnknown(err)
		return RuntimeFileWriteResult{}, err
	}
	if !validRuntimeWriteResult(response, request.Path, request.Content, request.ExpectedAbsent) {
		supervisor.Disconnect(context.Background())
		return RuntimeFileWriteResult{}, runtimeOutcomeUnknownError()
	}
	return response, nil
}

func validRuntimeWriteResult(response RuntimeFileWriteResult, expectedPath string, content []byte, expectedCreated bool) bool {
	digest := sha256.Sum256(content)
	return response.Path == expectedPath && response.Size == int64(len(content)) && response.SHA256 == hex.EncodeToString(digest[:]) && response.Created == expectedCreated && response.ExtendedMetadataNotPreserved == !response.Created
}

func (supervisor *RuntimeLeaseSupervisor) authorizeMutationLease(lease *RuntimeLease) (runtimeFileGeneration, string, error) {
	if lease == nil || lease.supervisor != supervisor || lease.kind != RuntimeTaskLease {
		return nil, "", runtimeLifecycleError(ErrHelperRuntimeInvalidLease)
	}
	supervisor.mu.Lock()
	record, exists := supervisor.leases[lease.id]
	runtime := supervisor.runtime
	valid := exists && record.kind == RuntimeTaskLease && supervisor.state == RuntimeReady && runtime != nil && runtime.Generation() == lease.generation && record.workspaceID == lease.workspaceID && record.rootHandle == lease.rootHandle
	generation, ok := runtime.(runtimeFileGeneration)
	supervisor.mu.Unlock()
	if !valid || !ok || lease.Context().Err() != nil {
		return nil, "", runtimeLifecycleError(ErrConnectionGenerationRevoked)
	}
	if err := supervisor.connection.ValidateGeneration(lease.generation); err != nil {
		return nil, "", err
	}
	return generation, lease.rootHandle, nil
}

func (supervisor *RuntimeLeaseSupervisor) authorizeFileLease(lease *RuntimeLease) (runtimeFileGeneration, string, error) {
	if lease == nil || lease.supervisor != supervisor {
		return nil, "", runtimeLifecycleError(ErrHelperRuntimeInvalidLease)
	}
	supervisor.mu.Lock()
	record, exists := supervisor.leases[lease.id]
	runtime := supervisor.runtime
	valid := exists && supervisor.state == RuntimeReady && runtime != nil && runtime.Generation() == lease.generation && record.workspaceID == lease.workspaceID && record.rootHandle == lease.rootHandle
	generation, ok := runtime.(runtimeFileGeneration)
	supervisor.mu.Unlock()
	if !valid || !ok || lease.Context().Err() != nil {
		return nil, "", runtimeLifecycleError(ErrConnectionGenerationRevoked)
	}
	if err := supervisor.connection.ValidateGeneration(lease.generation); err != nil {
		return nil, "", err
	}
	return generation, lease.rootHandle, nil
}

func validRuntimeFileInfo(info RuntimeFileInfo, expectedPath string) bool {
	if info.Path != expectedPath || info.Size < 0 || info.Mode > 0o777 {
		return false
	}
	switch info.Kind {
	case "file", "directory", "symlink":
		return validRemoteRelativePath(info.Path, true)
	default:
		return false
	}
}

func directChildPath(parent, child string) bool {
	if !validRemoteRelativePath(child, false) {
		return false
	}
	if parent == "." {
		return !strings.Contains(child, "/")
	}
	return path.Dir(child) == parent
}

func validRemoteRelativePath(value string, allowRoot bool) bool {
	if value == "." {
		return allowRoot
	}
	if value == "" || len(value) > maxRuntimeRelativePathBytes || !utf8.ValidString(value) || path.IsAbs(value) || path.Clean(value) != value || strings.Contains(value, "\\") {
		return false
	}
	components := strings.Split(value, "/")
	if len(components) > maxRuntimePathDepth {
		return false
	}
	for _, component := range components {
		if component == "" || component == "." || component == ".." || len(component) > maxRuntimePathComponentBytes || !utf8.ValidString(component) {
			return false
		}
		for _, char := range component {
			if char == 0 || char == '/' || unicode.IsControl(char) || unicode.Is(unicode.Cf, char) {
				return false
			}
		}
	}
	return true
}

func validLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, char := range value {
		if char < '0' || (char > '9' && char < 'a') || char > 'f' {
			return false
		}
	}
	return true
}

func runtimeFileError(code FailureCode, reason FailureReason, cause error) error {
	return &ConnectionSupervisorError{Failure: ConnectionFailure{Code: code, Reason: reason}, cause: cause}
}
