package sessionindex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"pi-desk/internal/workspace"
)

const (
	maxConcurrentLoads          = 10
	maxSessionBytes             = 64 << 20
	maxLineBytes                = 8 << 20
	transcriptPageTargetBytes   = 5 << 20
	transcriptPageTargetEntries = 1000
	maxTitleRunes               = 80
)

type Summary struct {
	ID                string
	Path              string
	CWD               string
	SSHAnchor         bool
	AnchorWorkspaceID string
	Name              string
	Title             string
	FirstMessage      string
	CreatedAt         time.Time
	ModifiedAt        time.Time
	MessageCount      int
	ParentSessionPath string
}

type Model struct {
	Provider string
	ID       string
}

type Snapshot struct {
	Messages     []json.RawMessage
	Model        *Model
	Before       string
	HasMore      bool
	MessageCount int
}

type TokenUsage struct {
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
	Reasoning  int64
	Total      int64
}

type ModelUsage struct {
	Provider          string
	Model             string
	AssistantMessages int
	Tokens            TokenUsage
	Cost              float64
}

type UsageSummary struct {
	Sessions          int
	Messages          int
	UserMessages      int
	AssistantMessages int
	ToolResults       int
	Tokens            TokenUsage
	Cost              float64
	Models            []ModelUsage
}

type Index struct {
	root       string
	anchorRoot string
	mutationMu sync.Mutex
}

func DefaultRoot() (string, error) {
	if root := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_SESSION_DIR")); root != "" {
		return expandHome(root)
	}
	if agentDir := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR")); agentDir != "" {
		root, err := expandHome(agentDir)
		if err != nil {
			return "", err
		}
		return filepath.Join(root, "sessions"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".pi", "agent", "sessions"), nil
}

func New(root string) *Index {
	return &Index{root: filepath.Clean(root)}
}

// NewWithAnchorRoot enables local-only SSH anchor projection. The root is not
// scanned by itself; marker reads occur only for a session CWD that is one
// direct child below this configured directory.
func NewWithAnchorRoot(root, anchorRoot string) *Index {
	cleanAnchorRoot := ""
	if strings.TrimSpace(anchorRoot) != "" {
		cleanAnchorRoot = filepath.Clean(anchorRoot)
	}
	return &Index{root: filepath.Clean(root), anchorRoot: cleanAnchorRoot}
}

// ListOrphanSSH returns only anchor transcripts whose immutable WorkspaceID is
// absent from the caller's current catalog projection. It performs local JSONL
// and marker reads only and cannot establish an SSH connection.
func (index *Index) ListOrphanSSH(ctx context.Context, knownWorkspaceIDs map[string]struct{}) ([]Summary, error) {
	summaries, err := index.List(ctx, "")
	if err != nil {
		return nil, err
	}
	orphans := make([]Summary, 0)
	for _, summary := range summaries {
		if !summary.SSHAnchor {
			continue
		}
		if _, known := knownWorkspaceIDs[summary.AnchorWorkspaceID]; !known {
			orphans = append(orphans, summary)
		}
	}
	return orphans, nil
}

func (index *Index) projectSSHAnchor(summary *Summary) bool {
	if summary == nil || strings.TrimSpace(index.anchorRoot) == "" || strings.TrimSpace(summary.CWD) == "" {
		return true
	}
	relative, err := filepath.Rel(index.anchorRoot, summary.CWD)
	if err != nil || filepath.IsAbs(relative) {
		// Different Windows volumes cannot share an anchor boundary.
		return true
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return true
	}
	if relative == "." || strings.Contains(relative, string(filepath.Separator)) {
		return false
	}
	marker, err := workspace.ReadSSHAnchor(index.anchorRoot, summary.CWD)
	if err != nil {
		return false
	}
	summary.SSHAnchor = true
	summary.AnchorWorkspaceID = marker.WorkspaceID
	return true
}

func (index *Index) List(ctx context.Context, workspacePath string) ([]Summary, error) {
	root, err := index.canonicalRoot()
	if errors.Is(err, os.ErrNotExist) {
		return []Summary{}, nil
	}
	if err != nil {
		return nil, err
	}

	workspaceKey := ""
	if strings.TrimSpace(workspacePath) != "" {
		canonical, err := canonicalDirectory(workspacePath)
		if err != nil {
			return nil, err
		}
		workspaceKey = pathKey(canonical)
	}
	files, err := sessionFiles(root)
	if err != nil {
		return nil, err
	}

	jobs := make(chan string)
	results := make(chan Summary)
	workerCount := min(maxConcurrentLoads, len(files))
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for path := range jobs {
				summary, ok := readSummary(path)
				if !ok || !index.projectSSHAnchor(&summary) || (workspaceKey != "" && pathKey(summary.CWD) != workspaceKey) {
					continue
				}
				select {
				case results <- summary:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, path := range files {
			select {
			case jobs <- path:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	summaries := make([]Summary, 0, len(files))
	for summary := range results {
		summaries = append(summaries, summary)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(summaries, func(a, b Summary) int { return b.ModifiedAt.Compare(a.ModifiedAt) })
	return summaries, nil
}

// Usage aggregates Pi's persisted assistant usage on each session's active
// branch. Forked alternatives are excluded, while pre-compaction usage remains
// included because those provider requests already consumed tokens and cost.
func (index *Index) Usage(ctx context.Context, workspacePath string) (UsageSummary, error) {
	root, err := index.canonicalRoot()
	if errors.Is(err, os.ErrNotExist) {
		return UsageSummary{Models: []ModelUsage{}}, nil
	}
	if err != nil {
		return UsageSummary{}, err
	}
	workspaceKey := ""
	if strings.TrimSpace(workspacePath) != "" {
		canonical, err := canonicalDirectory(workspacePath)
		if err != nil {
			return UsageSummary{}, err
		}
		workspaceKey = pathKey(canonical)
	}
	files, err := sessionFiles(root)
	if err != nil {
		return UsageSummary{}, err
	}

	type usageResult struct {
		usage UsageSummary
		ok    bool
	}
	jobs := make(chan string)
	results := make(chan usageResult)
	workerCount := min(maxConcurrentLoads, len(files))
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for path := range jobs {
				usage, cwd, ok := readUsage(path)
				if !ok || (workspaceKey != "" && pathKey(cwd) != workspaceKey) {
					continue
				}
				select {
				case results <- usageResult{usage: usage, ok: true}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, path := range files {
			select {
			case jobs <- path:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	total := UsageSummary{Models: []ModelUsage{}}
	models := make(map[string]ModelUsage)
	for result := range results {
		if !result.ok {
			continue
		}
		mergeUsage(&total, result.usage)
		for _, model := range result.usage.Models {
			key := model.Provider + "\x00" + model.Model
			merged := models[key]
			merged.Provider, merged.Model = model.Provider, model.Model
			merged.AssistantMessages += model.AssistantMessages
			mergeTokens(&merged.Tokens, model.Tokens)
			merged.Cost += model.Cost
			models[key] = merged
		}
	}
	if err := ctx.Err(); err != nil {
		return UsageSummary{}, err
	}
	total.Models = make([]ModelUsage, 0, len(models))
	for _, model := range models {
		total.Models = append(total.Models, model)
	}
	slices.SortFunc(total.Models, func(a, b ModelUsage) int {
		if a.Tokens.Total < b.Tokens.Total {
			return 1
		}
		if a.Tokens.Total > b.Tokens.Total {
			return -1
		}
		if order := strings.Compare(a.Provider, b.Provider); order != 0 {
			return order
		}
		return strings.Compare(a.Model, b.Model)
	})
	return total, nil
}

func (index *Index) ValidatePath(path string) (string, error) {
	root, err := index.canonicalRoot()
	if err != nil {
		return "", err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("session path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve session path: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve session path links: %w", err)
	}
	relative, err := filepath.Rel(root, canonical)
	if err != nil {
		return "", fmt.Errorf("compare session path to root: %w", err)
	}
	if relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("session path is outside the configured Pi sessions directory")
	}
	if !strings.EqualFold(filepath.Ext(canonical), ".jsonl") {
		return "", errors.New("session path must reference a JSONL file")
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("inspect session path: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("session path is not a regular file")
	}
	return filepath.Clean(canonical), nil
}

// CopyValidated copies one stable regular session file after rechecking that
// its opened handle still matches the canonical directory entry. It is used by
// local export so a path swap cannot make Pi read outside the session root.
func (index *Index) CopyValidated(path string, destination io.Writer) error {
	if destination == nil {
		return errors.New("session copy destination is required")
	}
	canonical, err := index.ValidatePath(path)
	if err != nil {
		return err
	}
	file, err := os.Open(canonical)
	if err != nil {
		return fmt.Errorf("open session path: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Size() > maxSessionBytes {
		return errors.New("session path is not a bounded regular file")
	}
	current, err := os.Lstat(canonical)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(opened, current) {
		return errors.New("session path changed during validation")
	}
	written, err := io.Copy(destination, io.LimitReader(file, maxSessionBytes+1))
	if err != nil {
		return fmt.Errorf("copy session path: %w", err)
	}
	if written > maxSessionBytes {
		return errors.New("session file exceeds the safety limit")
	}
	return nil
}

func (index *Index) Resolve(path string) (Summary, error) {
	canonical, err := index.ValidatePath(path)
	if err != nil {
		return Summary{}, err
	}
	summary, ok := readSummary(canonical)
	if !ok || !index.projectSSHAnchor(&summary) {
		return Summary{}, errors.New("session file or SSH anchor is malformed or exceeds safety limits")
	}
	return summary, nil
}

// Header validates a session path and reads only its bounded first line. It is
// intended for frequent ownership checks where scanning the transcript would
// be unnecessarily expensive.
func (index *Index) Header(path string) (Summary, error) {
	canonical, err := index.ValidatePath(path)
	if err != nil {
		return Summary{}, err
	}
	summary, ok := readHeader(canonical)
	if !ok || !index.projectSSHAnchor(&summary) {
		return Summary{}, errors.New("session header or SSH anchor is malformed or exceeds safety limits")
	}
	return summary, nil
}

// Messages reads the complete active message branch directly from a validated
// Pi JSONL session. Forked alternatives are excluded, but compaction only
// changes model context and must not hide earlier conversation history.
func (index *Index) Messages(path string) ([]json.RawMessage, error) {
	snapshot, err := index.Snapshot(path)
	if err != nil {
		return nil, err
	}
	return snapshot.Messages, nil
}

// Snapshot reads the complete active transcript and its most recent model selection.
func (index *Index) Snapshot(path string) (Snapshot, error) {
	canonical, err := index.ValidatePath(path)
	if err != nil {
		return Snapshot{}, err
	}
	entries, err := readTranscriptEntries(canonical)
	if err != nil {
		return Snapshot{}, err
	}
	pathEntries := addCompactionEstimates(activeTranscriptPath(entries))
	return Snapshot{
		Messages:     transcriptMessages(pathEntries),
		Model:        sessionModel(pathEntries),
		MessageCount: transcriptMessageCount(pathEntries),
	}, nil
}

// SnapshotPage reads the most recent bounded page before an optional stable
// entry ID. The byte and entry targets are soft so one large turn remains
// intact and a single valid message is never rejected for display.
func (index *Index) SnapshotPage(path string, before string) (Snapshot, error) {
	canonical, err := index.ValidatePath(path)
	if err != nil {
		return Snapshot{}, err
	}
	entries, err := readTranscriptEntries(canonical)
	if err != nil {
		return Snapshot{}, err
	}
	pathEntries := addCompactionEstimates(activeTranscriptPath(entries))
	end := len(pathEntries)
	if before = strings.TrimSpace(before); before != "" {
		end = -1
		for position, entry := range pathEntries {
			if entry.ID == before {
				end = position
				break
			}
		}
		if end < 0 {
			return Snapshot{}, errors.New("session transcript cursor is no longer on the active branch")
		}
	}

	messages := make([]json.RawMessage, 0, min(transcriptPageTargetEntries, end))
	totalBytes := 0
	firstPosition := end
	for position := end - 1; position >= 0; position-- {
		message, role, ok := transcriptMessage(pathEntries[position])
		if !ok {
			continue
		}
		messages = append(messages, message)
		totalBytes += len(message)
		firstPosition = position
		if (totalBytes >= transcriptPageTargetBytes || len(messages) >= transcriptPageTargetEntries) && role == "user" {
			break
		}
	}
	slices.Reverse(messages)

	hasMore := false
	for position := firstPosition - 1; position >= 0; position-- {
		if transcriptEntryVisible(pathEntries[position]) {
			hasMore = true
			break
		}
	}
	cursor := ""
	if hasMore && firstPosition < len(pathEntries) {
		cursor = pathEntries[firstPosition].ID
	}
	return Snapshot{
		Messages:     messages,
		Model:        sessionModel(pathEntries),
		Before:       cursor,
		HasMore:      hasMore,
		MessageCount: transcriptMessageCount(pathEntries),
	}, nil
}

func addCompactionEstimates(entries []rawEntry) []rawEntry {
	positions := make(map[string]int, len(entries))
	for position, entry := range entries {
		positions[entry.ID] = position
	}
	for position := range entries {
		entry := &entries[position]
		if entry.Type != "compaction" || entry.EstimatedTokensAfter != nil {
			continue
		}
		estimate := estimateContextEntryTokens(*entry)
		if firstKept, ok := positions[entry.FirstKeptEntryID]; ok && firstKept < position {
			for keptPosition := firstKept; keptPosition < position; keptPosition++ {
				estimate += estimateContextEntryTokens(entries[keptPosition])
			}
		}
		entry.EstimatedTokensAfter = &estimate
	}
	return entries
}

func estimateContextEntryTokens(entry rawEntry) int64 {
	chars := int64(0)
	switch entry.Type {
	case "compaction", "branch_summary":
		chars = utf16Length(entry.Summary)
	case "custom_message":
		chars = textAndImageContentChars(entry.Content)
	case "message":
		var message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
			Command string          `json:"command"`
			Output  string          `json:"output"`
		}
		if json.Unmarshal(entry.Message, &message) != nil {
			return 0
		}
		switch message.Role {
		case "user", "toolResult", "custom":
			chars = textAndImageContentChars(message.Content)
		case "assistant":
			chars = assistantContentChars(message.Content)
		case "bashExecution":
			chars = utf16Length(message.Command) + utf16Length(message.Output)
		case "branchSummary", "compactionSummary":
			var summary struct {
				Summary string `json:"summary"`
			}
			if json.Unmarshal(entry.Message, &summary) == nil {
				chars = utf16Length(summary.Summary)
			}
		}
	}
	return (chars + 3) / 4
}

func textAndImageContentChars(content json.RawMessage) int64 {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return utf16Length(text)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return 0
	}
	chars := int64(0)
	for _, block := range blocks {
		switch block.Type {
		case "text":
			chars += utf16Length(block.Text)
		case "image":
			chars += 4800
		}
	}
	return chars
}

func assistantContentChars(content json.RawMessage) int64 {
	var blocks []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		Thinking  string          `json:"thinking"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return 0
	}
	chars := int64(0)
	for _, block := range blocks {
		switch block.Type {
		case "text":
			chars += utf16Length(block.Text)
		case "thinking":
			chars += utf16Length(block.Thinking)
		case "toolCall":
			chars += utf16Length(block.Name) + compactJSONChars(block.Arguments)
		}
	}
	return chars
}

func compactJSONChars(value json.RawMessage) int64 {
	if len(value) == 0 {
		return 0
	}
	var compact bytes.Buffer
	if json.Compact(&compact, value) != nil {
		return utf16Length(string(value))
	}
	return utf16Length(compact.String())
}

func utf16Length(value string) int64 {
	length := int64(0)
	for _, char := range value {
		length++
		if char > 0xffff {
			length++
		}
	}
	return length
}

func transcriptMessageCount(entries []rawEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.Type == "message" && transcriptEntryVisible(entry) {
			count++
		}
	}
	return count
}

func transcriptMessages(entries []rawEntry) []json.RawMessage {
	messages := make([]json.RawMessage, 0, len(entries))
	for _, entry := range entries {
		message, _, ok := transcriptMessage(entry)
		if ok {
			messages = append(messages, message)
		}
	}
	return messages
}

func transcriptEntryVisible(entry rawEntry) bool {
	if entry.Type == "compaction" {
		return true
	}
	if entry.Type != "message" || len(entry.Message) == 0 {
		return false
	}
	var envelope struct {
		Role string `json:"role"`
	}
	if json.Unmarshal(entry.Message, &envelope) != nil {
		return false
	}
	switch envelope.Role {
	case "user", "assistant", "toolResult", "bashExecution":
		return true
	default:
		return false
	}
}

func transcriptMessage(entry rawEntry) (json.RawMessage, string, bool) {
	if entry.Type == "compaction" {
		marker, err := compactionWithEntryID(entry)
		return marker, "piDeskCompaction", err == nil
	}
	if entry.Type != "message" || len(entry.Message) == 0 {
		return nil, "", false
	}
	var envelope struct {
		Role string `json:"role"`
	}
	if json.Unmarshal(entry.Message, &envelope) != nil {
		return nil, "", false
	}
	switch envelope.Role {
	case "user", "assistant", "toolResult", "bashExecution":
	default:
		return nil, "", false
	}
	message, err := messageWithEntryID(entry.Message, entry.ID, !entry.SyntheticID, entry.Timestamp)
	return message, envelope.Role, err == nil
}

func compactionWithEntryID(entry rawEntry) (json.RawMessage, error) {
	marker := map[string]any{
		"role":         "piDeskCompaction",
		"summary":      entry.Summary,
		"timestamp":    entry.Timestamp,
		"tokensBefore": entry.TokensBefore,
	}
	if entry.EstimatedTokensAfter != nil {
		marker["estimatedTokensAfter"] = *entry.EstimatedTokensAfter
	}
	if entry.ID != "" {
		marker["piDeskDisplayId"] = entry.ID
		marker["piDeskEntryId"] = entry.ID
	}
	encoded, err := json.Marshal(marker)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func messageWithEntryID(message json.RawMessage, entryID string, persisted bool, entryTimestamp string) (json.RawMessage, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(message, &envelope); err != nil {
		return nil, err
	}
	if len(envelope["timestamp"]) == 0 && entryTimestamp != "" {
		encodedTimestamp, err := json.Marshal(entryTimestamp)
		if err != nil {
			return nil, err
		}
		envelope["timestamp"] = encodedTimestamp
	}
	if entryID != "" {
		encodedID, err := json.Marshal(entryID)
		if err != nil {
			return nil, err
		}
		envelope["piDeskDisplayId"] = encodedID
	}
	if persisted {
		encodedID, err := json.Marshal(entryID)
		if err != nil {
			return nil, err
		}
		envelope["piDeskEntryId"] = encodedID
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func (index *Index) canonicalRoot() (string, error) {
	root, err := filepath.Abs(index.root)
	if err != nil {
		return "", fmt.Errorf("resolve Pi sessions directory: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve Pi sessions directory links: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("inspect Pi sessions directory: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("Pi sessions path is not a directory")
	}
	return filepath.Clean(root), nil
}

func sessionFiles(root string) ([]string, error) {
	directories, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read Pi sessions directory: %w", err)
	}
	var files []string
	for _, directory := range directories {
		if directory.Type()&os.ModeSymlink != 0 || !directory.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(root, directory.Name()))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jsonl") {
				continue
			}
			files = append(files, filepath.Join(root, directory.Name(), entry.Name()))
		}
	}
	return files, nil
}

type rawEntry struct {
	Type                 string          `json:"type"`
	Version              int             `json:"version"`
	ID                   string          `json:"id"`
	ParentID             *string         `json:"parentId"`
	Timestamp            string          `json:"timestamp"`
	CWD                  string          `json:"cwd"`
	ParentSession        string          `json:"parentSession"`
	Name                 string          `json:"name"`
	Provider             string          `json:"provider"`
	ModelID              string          `json:"modelId"`
	Message              json.RawMessage `json:"message"`
	Content              json.RawMessage `json:"content"`
	Summary              string          `json:"summary"`
	TokensBefore         int64           `json:"tokensBefore"`
	EstimatedTokensAfter *int64          `json:"estimatedTokensAfter"`
	FirstKeptEntryID     string          `json:"firstKeptEntryId"`
	FirstKeptEntryIndex  *int            `json:"firstKeptEntryIndex"`
	SyntheticID          bool            `json:"-"`
}

type rawMessage struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Timestamp json.Number     `json:"timestamp"`
	Provider  string          `json:"provider"`
	Model     string          `json:"model"`
	Usage     *rawUsage       `json:"usage"`
}

type rawUsage struct {
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	CacheRead  int64   `json:"cacheRead"`
	CacheWrite int64   `json:"cacheWrite"`
	Reasoning  int64   `json:"reasoning"`
	Total      int64   `json:"totalTokens"`
	Cost       rawCost `json:"cost"`
}

type rawCost struct {
	Total float64 `json:"total"`
}

func readUsage(path string) (UsageSummary, string, bool) {
	entries, err := readTranscriptEntries(path)
	if err != nil {
		return UsageSummary{}, "", false
	}
	header, ok := readHeader(path)
	if !ok {
		return UsageSummary{}, "", false
	}
	usage := UsageSummary{Sessions: 1, Models: []ModelUsage{}}
	models := make(map[string]ModelUsage)
	for _, entry := range activeTranscriptPath(entries) {
		if entry.Type != "message" || len(entry.Message) == 0 {
			continue
		}
		var message rawMessage
		if json.Unmarshal(entry.Message, &message) != nil {
			continue
		}
		usage.Messages++
		switch message.Role {
		case "user":
			usage.UserMessages++
		case "assistant":
			usage.AssistantMessages++
			if message.Usage == nil {
				continue
			}
			tokens := tokensFromRaw(*message.Usage)
			mergeTokens(&usage.Tokens, tokens)
			usage.Cost += message.Usage.Cost.Total
			provider := strings.TrimSpace(message.Provider)
			modelID := strings.TrimSpace(message.Model)
			key := provider + "\x00" + modelID
			model := models[key]
			model.Provider, model.Model = provider, modelID
			model.AssistantMessages++
			mergeTokens(&model.Tokens, tokens)
			model.Cost += message.Usage.Cost.Total
			models[key] = model
		case "toolResult":
			usage.ToolResults++
		}
	}
	usage.Models = make([]ModelUsage, 0, len(models))
	for _, model := range models {
		usage.Models = append(usage.Models, model)
	}
	return usage, header.CWD, true
}

func tokensFromRaw(usage rawUsage) TokenUsage {
	total := usage.Total
	if total == 0 {
		total = usage.Input + usage.Output + usage.CacheRead + usage.CacheWrite
	}
	return TokenUsage{
		Input: usage.Input, Output: usage.Output, CacheRead: usage.CacheRead,
		CacheWrite: usage.CacheWrite, Reasoning: usage.Reasoning, Total: total,
	}
}

func mergeUsage(target *UsageSummary, source UsageSummary) {
	target.Sessions += source.Sessions
	target.Messages += source.Messages
	target.UserMessages += source.UserMessages
	target.AssistantMessages += source.AssistantMessages
	target.ToolResults += source.ToolResults
	mergeTokens(&target.Tokens, source.Tokens)
	target.Cost += source.Cost
}

func mergeTokens(target *TokenUsage, source TokenUsage) {
	target.Input += source.Input
	target.Output += source.Output
	target.CacheRead += source.CacheRead
	target.CacheWrite += source.CacheWrite
	target.Reasoning += source.Reasoning
	target.Total += source.Total
}

func readHeader(path string) (Summary, bool) {
	file, err := os.Open(path)
	if err != nil {
		return Summary{}, false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), maxLineBytes)
	if !scanner.Scan() {
		return Summary{}, false
	}
	line := bytes.TrimSpace(scanner.Bytes())
	if len(line) == 0 || !utf8.Valid(line) {
		return Summary{}, false
	}
	var entry rawEntry
	if json.Unmarshal(line, &entry) != nil || entry.Type != "session" || strings.TrimSpace(entry.ID) == "" {
		return Summary{}, false
	}
	summary := Summary{ID: entry.ID, Path: path, ParentSessionPath: entry.ParentSession}
	if strings.TrimSpace(entry.CWD) != "" {
		summary.CWD = filepath.Clean(entry.CWD)
	}
	summary.CreatedAt, _ = time.Parse(time.RFC3339Nano, entry.Timestamp)
	return summary, true
}

func readSummary(path string) (Summary, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxSessionBytes {
		return Summary{}, false
	}
	file, err := os.Open(path)
	if err != nil {
		return Summary{}, false
	}
	defer file.Close()

	scanner := bufio.NewScanner(io.LimitReader(file, maxSessionBytes+1))
	scanner.Buffer(make([]byte, 64<<10), maxLineBytes)
	var summary Summary
	var headerTime time.Time
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || !utf8.Valid(line) {
			if lineNumber == 1 {
				return Summary{}, false
			}
			continue
		}
		var entry rawEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			if lineNumber == 1 {
				return Summary{}, false
			}
			continue
		}
		if lineNumber == 1 {
			if entry.Type != "session" || strings.TrimSpace(entry.ID) == "" {
				return Summary{}, false
			}
			summary.ID = entry.ID
			summary.Path = path
			if strings.TrimSpace(entry.CWD) != "" {
				summary.CWD = filepath.Clean(entry.CWD)
			}
			summary.ParentSessionPath = entry.ParentSession
			headerTime, _ = time.Parse(time.RFC3339Nano, entry.Timestamp)
			summary.CreatedAt = headerTime
			continue
		}
		switch entry.Type {
		case "session_info":
			summary.Name = strings.TrimSpace(entry.Name)
		case "message":
			summary.MessageCount++
			var message rawMessage
			if err := json.Unmarshal(entry.Message, &message); err != nil || (message.Role != "user" && message.Role != "assistant") {
				continue
			}
			if activity := messageTime(message.Timestamp, entry.Timestamp); activity.After(summary.ModifiedAt) {
				summary.ModifiedAt = activity
			}
			if summary.FirstMessage == "" && message.Role == "user" {
				summary.FirstMessage = sessionTitleText(extractText(message.Content))
			}
		}
	}
	if err := scanner.Err(); err != nil || lineNumber == 0 {
		return Summary{}, false
	}
	if summary.CreatedAt.IsZero() {
		summary.CreatedAt = info.ModTime().UTC()
	}
	if summary.ModifiedAt.IsZero() {
		summary.ModifiedAt = headerTime
		if summary.ModifiedAt.IsZero() {
			summary.ModifiedAt = info.ModTime().UTC()
		}
	}
	summary.FirstMessage = compactText(summary.FirstMessage, maxTitleRunes)
	summary.Title = compactText(summary.Name, maxTitleRunes)
	if corruptedAutomaticName(summary.Title, summary.FirstMessage) {
		summary.Name = ""
		summary.Title = ""
	}
	if summary.Title == "" {
		summary.Title = summary.FirstMessage
	}
	if summary.Title == "" {
		summary.Title = "Empty session"
	}
	return summary, true
}

func readTranscriptEntries(path string) ([]rawEntry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect session transcript: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxSessionBytes {
		return nil, errors.New("session transcript exceeds the file safety limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open session transcript: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(io.LimitReader(file, maxSessionBytes+1))
	scanner.Buffer(make([]byte, 64<<10), maxLineBytes)
	entries := make([]rawEntry, 0, 256)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || !utf8.Valid(line) {
			if lineNumber == 1 {
				return nil, errors.New("session transcript header is malformed")
			}
			continue
		}
		var entry rawEntry
		if json.Unmarshal(line, &entry) != nil {
			if lineNumber == 1 {
				return nil, errors.New("session transcript header is malformed")
			}
			continue
		}
		if lineNumber == 1 && (entry.Type != "session" || strings.TrimSpace(entry.ID) == "") {
			return nil, errors.New("session transcript header is malformed")
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read session transcript: %w", err)
	}
	if len(entries) == 0 || entries[0].Type != "session" {
		return nil, errors.New("session transcript is empty")
	}
	if entries[0].Version < 2 {
		migrateLegacyTranscriptEntries(entries)
	}
	return entries[1:], nil
}

func migrateLegacyTranscriptEntries(entries []rawEntry) {
	previousID := ""
	for index := 1; index < len(entries); index++ {
		entry := &entries[index]
		entry.ID = fmt.Sprintf("legacy-%08d", index)
		entry.SyntheticID = true
		if previousID == "" {
			entry.ParentID = nil
		} else {
			parentID := previousID
			entry.ParentID = &parentID
		}
		previousID = entry.ID
	}
	for index := 1; index < len(entries); index++ {
		entry := &entries[index]
		if entry.Type != "compaction" || entry.FirstKeptEntryID != "" || entry.FirstKeptEntryIndex == nil {
			continue
		}
		target := *entry.FirstKeptEntryIndex
		if target > 0 && target < len(entries) && entries[target].Type != "session" {
			entry.FirstKeptEntryID = entries[target].ID
		}
	}
}

func activeTranscriptPath(entries []rawEntry) []rawEntry {
	byID := make(map[string]rawEntry, len(entries))
	leafID := ""
	for _, entry := range entries {
		if strings.TrimSpace(entry.ID) == "" {
			continue
		}
		byID[entry.ID] = entry
		leafID = entry.ID
	}
	if leafID == "" {
		return []rawEntry{}
	}
	path := make([]rawEntry, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for leafID != "" {
		if _, exists := seen[leafID]; exists {
			break
		}
		entry, exists := byID[leafID]
		if !exists {
			break
		}
		seen[leafID] = struct{}{}
		path = append(path, entry)
		if entry.ParentID == nil {
			break
		}
		leafID = strings.TrimSpace(*entry.ParentID)
	}
	slices.Reverse(path)
	return path
}

func sessionModel(path []rawEntry) *Model {
	var current *Model
	for _, entry := range path {
		provider := ""
		modelID := ""
		switch entry.Type {
		case "model_change":
			provider = strings.TrimSpace(entry.Provider)
			modelID = strings.TrimSpace(entry.ModelID)
		case "message":
			var message rawMessage
			if json.Unmarshal(entry.Message, &message) != nil || message.Role != "assistant" {
				continue
			}
			provider = strings.TrimSpace(message.Provider)
			modelID = strings.TrimSpace(message.Model)
		}
		if provider != "" && modelID != "" {
			current = &Model{Provider: provider, ID: modelID}
		}
	}
	return current
}

func messageTime(milliseconds json.Number, fallback string) time.Time {
	if milliseconds != "" {
		if value, err := strconv.ParseFloat(string(milliseconds), 64); err == nil {
			seconds, remainder := int64(value/1000), int64(value)%1000
			return time.Unix(seconds, remainder*int64(time.Millisecond)).UTC()
		}
	}
	parsed, _ := time.Parse(time.RFC3339Nano, fallback)
	return parsed
}

func extractText(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, block := range blocks {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, " ")
}

var expandedSkillMessagePattern = regexp.MustCompile(`(?s)^<skill name="([a-z0-9-]{1,64})" location="[^"\r\n]+">\r?\n.*?\r?\n</skill>(?:\r?\n\r?\n([\s\S]*))?$`)

func sessionTitleText(value string) string {
	match := expandedSkillMessagePattern.FindStringSubmatch(value)
	if match == nil {
		return value
	}
	if userMessage := strings.TrimSpace(match[2]); userMessage != "" {
		return userMessage
	}
	return "/skill:" + match[1]
}

func compactText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:limit-3])) + "..."
}

func corruptedAutomaticName(name, firstMessage string) bool {
	leadingQuotes := len(name) - len(strings.TrimLeft(name, `"`))
	trailingQuotes := len(name) - len(strings.TrimRight(name, `"`))
	if leadingQuotes == 0 || leadingQuotes <= trailingQuotes {
		return false
	}
	unquoted := strings.Trim(name, `"`)
	return unquoted != "" && strings.HasPrefix(firstMessage, unquoted)
}

func canonicalDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", fmt.Errorf("resolve workspace path: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve workspace links: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("inspect workspace: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("workspace path is not a directory")
	}
	return filepath.Clean(canonical), nil
}

func pathKey(path string) string {
	clean := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}

func expandHome(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home directory: %w", err)
		}
		if path == "~" {
			return home, nil
		}
		path = filepath.Join(home, path[2:])
	}
	return filepath.Clean(path), nil
}
