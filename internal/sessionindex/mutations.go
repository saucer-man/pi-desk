package sessionindex

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/natefinch/atomic"
)

const (
	maxEditedMessageBytes = 1 << 20
	maxMutationBackups    = 3
)

type Mutation struct {
	Path       string
	BackupPath string
	afterHash  [32]byte
}

func (index *Index) ForkAt(path, entryID string) (string, error) {
	result, err := index.ForkMessage(path, entryID, false)
	return result.Path, err
}

type ForkResult struct {
	Path      string
	Text      string
	Images    []json.RawMessage
	SessionID string
}

// ForkBefore creates a persisted branch ending immediately before a user
// message and returns that message text for the new composer draft.
func (index *Index) ForkBefore(path, entryID string) (ForkResult, error) {
	return index.ForkMessage(path, entryID, true)
}

func (index *Index) ForkMessage(path, entryID string, before bool) (ForkResult, error) {
	index.mutationMu.Lock()
	defer index.mutationMu.Unlock()

	canonical, err := index.ValidatePath(path)
	if err != nil {
		return ForkResult{}, err
	}
	lines, err := readMutationLines(canonical)
	if err != nil {
		return ForkResult{}, err
	}
	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return ForkResult{}, errors.New("entry id is required")
	}
	type indexedEntry struct {
		decoded map[string]json.RawMessage
		id      string
		parent  string
	}
	byID := make(map[string]indexedEntry, len(lines))
	lastEntryID := ""
	for _, line := range lines[1:] {
		var entry map[string]json.RawMessage
		if json.Unmarshal(line, &entry) != nil {
			continue
		}
		var id, parent string
		_ = json.Unmarshal(entry["id"], &id)
		_ = json.Unmarshal(entry["parentId"], &parent)
		if id != "" {
			byID[id] = indexedEntry{decoded: entry, id: id, parent: parent}
			lastEntryID = id
		}
	}
	current, found := byID[entryID]
	if !found {
		return ForkResult{}, errors.New("message entry was not found in the session file")
	}
	var targetType string
	_ = json.Unmarshal(current.decoded["type"], &targetType)
	if targetType != "message" {
		return ForkResult{}, errors.New("only message entries can be forked")
	}
	selectedText := ""
	var selectedImages []json.RawMessage
	if before {
		if entryRole(current.decoded) != "user" {
			return ForkResult{}, errors.New("only user messages can be forked from before the entry")
		}
		selectedText, err = forkPromptText(current.decoded)
		if err != nil {
			return ForkResult{}, err
		}
		var message struct {
			Content []json.RawMessage `json:"content"`
		}
		if json.Unmarshal(current.decoded["message"], &message) == nil {
			for _, block := range message.Content {
				var part struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(block, &part) == nil && part.Type == "image" {
					selectedImages = append(selectedImages, block)
				}
			}
		}
		if current.parent == "" {
			current = indexedEntry{}
		} else {
			parent, exists := byID[current.parent]
			if !exists {
				return ForkResult{}, errors.New("session branch contains a missing parent")
			}
			current = parent
		}
	}

	activeBranch := make([]indexedEntry, 0, 64)
	activeSeen := make(map[string]struct{}, 64)
	for leaf, exists := byID[lastEntryID]; exists; leaf, exists = byID[leaf.parent] {
		if _, duplicate := activeSeen[leaf.id]; duplicate {
			return ForkResult{}, errors.New("session branch contains a parent cycle")
		}
		activeSeen[leaf.id] = struct{}{}
		activeBranch = append(activeBranch, leaf)
		if leaf.parent == "" {
			break
		}
	}
	for left, right := 0, len(activeBranch)-1; left < right; left, right = left+1, right-1 {
		activeBranch[left], activeBranch[right] = activeBranch[right], activeBranch[left]
	}
	if !before {
		for position, item := range activeBranch {
			if item.id != entryID {
				continue
			}
			for next := position + 1; next < len(activeBranch); next++ {
				role := entryRole(activeBranch[next].decoded)
				if role != "" && role != "toolResult" {
					break
				}
				// Metadata may be inserted between a tool call and its results.
				if role == "toolResult" {
					current = activeBranch[next]
				}
			}
			break
		}
	}
	branch := make([]indexedEntry, 0, 64)
	seen := make(map[string]struct{}, 64)
	for current.id != "" {
		if _, duplicate := seen[current.id]; duplicate {
			return ForkResult{}, errors.New("session branch contains a parent cycle")
		}
		seen[current.id] = struct{}{}
		branch = append(branch, current)
		if current.parent == "" {
			break
		}
		parent, exists := byID[current.parent]
		if !exists {
			return ForkResult{}, errors.New("session branch contains a missing parent")
		}
		current = parent
	}
	for left, right := 0, len(branch)-1; left < right; left, right = left+1, right-1 {
		branch[left], branch[right] = branch[right], branch[left]
	}

	var header map[string]json.RawMessage
	if json.Unmarshal(lines[0], &header) != nil {
		return ForkResult{}, errors.New("session header is malformed")
	}
	sessionID, err := randomSessionID()
	if err != nil {
		return ForkResult{}, err
	}
	now := time.Now().UTC()
	header["id"], _ = json.Marshal(sessionID)
	header["timestamp"], _ = json.Marshal(now.Format(time.RFC3339Nano))
	header["parentSession"], _ = json.Marshal(canonical)
	delete(header, "_reloadMarker")
	output := make([]json.RawMessage, 0, len(branch)+1)
	encodedHeader, err := json.Marshal(header)
	if err != nil {
		return ForkResult{}, fmt.Errorf("encode forked session header: %w", err)
	}
	output = append(output, encodedHeader)
	parentID := ""
	for _, item := range branch {
		var entryType string
		_ = json.Unmarshal(item.decoded["type"], &entryType)
		if entryType == "label" {
			continue
		}
		if parentID == "" {
			item.decoded["parentId"] = json.RawMessage("null")
		} else {
			item.decoded["parentId"], _ = json.Marshal(parentID)
		}
		encoded, encodeErr := json.Marshal(item.decoded)
		if encodeErr != nil {
			return ForkResult{}, fmt.Errorf("encode forked session entry: %w", encodeErr)
		}
		output = append(output, encoded)
		parentID = item.id
	}
	fileTimestamp := strings.NewReplacer(":", "-", ".", "-").Replace(now.Format(time.RFC3339Nano))
	filename := fmt.Sprintf("%s_%s.jsonl", fileTimestamp, sessionID)
	destination := filepath.Join(filepath.Dir(canonical), filename)
	var contents bytes.Buffer
	for _, line := range output {
		contents.Write(line)
		contents.WriteByte('\n')
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ForkResult{}, fmt.Errorf("create forked session: %w", err)
	}
	if _, err = file.Write(contents.Bytes()); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(destination)
		return ForkResult{}, fmt.Errorf("write forked session: %w", err)
	}
	return ForkResult{Path: destination, Text: selectedText, Images: selectedImages, SessionID: sessionID}, nil
}

func forkPromptText(entry map[string]json.RawMessage) (string, error) {
	var message struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(entry["message"], &message); err != nil {
		return "", errors.New("session message is malformed")
	}
	var text string
	if json.Unmarshal(message.Content, &text) == nil {
		return text, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(message.Content, &parts); err != nil {
		return "", errors.New("user message content is malformed")
	}
	var result strings.Builder
	for _, part := range parts {
		if part.Type == "text" {
			result.WriteString(part.Text)
		}
	}
	return result.String(), nil
}

func randomSessionID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

// EditMessage replaces only text content. Image, reasoning, and tool-call
// blocks remain attached to the original Pi message entry.
func (index *Index) EditMessage(path, entryID, text string) (Mutation, error) {
	if strings.TrimSpace(text) == "" {
		return Mutation{}, errors.New("message text is required")
	}
	if len(text) > maxEditedMessageBytes {
		return Mutation{}, errors.New("message text exceeds the 1 MiB limit")
	}
	return index.mutateMessage(path, entryID, func(lines []json.RawMessage, target int, entry map[string]json.RawMessage) ([]json.RawMessage, error) {
		var message map[string]json.RawMessage
		if err := json.Unmarshal(entry["message"], &message); err != nil {
			return nil, errors.New("session message is malformed")
		}
		var role string
		if err := json.Unmarshal(message["role"], &role); err != nil || (role != "user" && role != "assistant") {
			return nil, errors.New("only user and assistant messages can be edited")
		}
		content, err := replaceMessageText(message["content"], text)
		if err != nil {
			return nil, err
		}
		message["content"] = content
		entry["message"], err = json.Marshal(message)
		if err != nil {
			return nil, fmt.Errorf("encode edited message: %w", err)
		}
		lines[target], err = json.Marshal(entry)
		if err != nil {
			return nil, fmt.Errorf("encode edited session entry: %w", err)
		}
		return lines, nil
	})
}

// RewindBefore removes the latest user turn from the active branch so it can
// be replayed in the same session file.
func (index *Index) RewindBefore(path, entryID string) (Mutation, error) {
	return index.mutateMessage(path, entryID, func(lines []json.RawMessage, _ int, entry map[string]json.RawMessage) ([]json.RawMessage, error) {
		if entryRole(entry) != "user" {
			return nil, errors.New("only user messages can be replayed")
		}
		type branchEntry struct {
			id     string
			parent string
			entry  map[string]json.RawMessage
		}
		byID := make(map[string]branchEntry, len(lines))
		leafID := ""
		for _, line := range lines[1:] {
			var decoded map[string]json.RawMessage
			if json.Unmarshal(line, &decoded) != nil {
				continue
			}
			var id, parent string
			_ = json.Unmarshal(decoded["id"], &id)
			_ = json.Unmarshal(decoded["parentId"], &parent)
			if id != "" {
				byID[id] = branchEntry{id: id, parent: parent, entry: decoded}
				leafID = id
			}
		}
		current, exists := byID[leafID]
		seen := make(map[string]struct{}, 64)
		for exists && current.id != entryID {
			if _, duplicate := seen[current.id]; duplicate {
				return nil, errors.New("session branch contains a parent cycle")
			}
			seen[current.id] = struct{}{}
			if entryRole(current.entry) == "user" {
				return nil, errors.New("only the latest user message can be replayed")
			}
			current, exists = byID[current.parent]
		}
		if !exists || current.id != entryID {
			return nil, errors.New("message is not on the active session branch")
		}

		seen[current.id] = struct{}{} // Only the selected turn's active suffix is removable.
		protected := make(map[string]struct{})
		var references []string
		collectReferences := func(item map[string]json.RawMessage) {
			for _, field := range []string{"parentId", "targetId", "firstKeptEntryId", "fromId"} {
				var ref string
				if json.Unmarshal(item[field], &ref) == nil && ref != "" {
					references = append(references, ref)
				}
			}
		}
		for id, item := range byID {
			if _, removing := seen[id]; removing {
				continue
			}
			// Other branches and their metadata may still refer to this suffix.
			collectReferences(item.entry)
		}
		for position := 0; position < len(references); position++ {
			ref := references[position]
			if _, removing := seen[ref]; !removing {
				continue
			}
			if _, kept := protected[ref]; kept {
				continue
			}
			protected[ref] = struct{}{}
			collectReferences(byID[ref].entry)
		}
		result := []json.RawMessage{lines[0]}
		for _, line := range lines[1:] {
			var item struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(line, &item)
			_, removing := seen[item.ID]
			_, keep := protected[item.ID]
			if removing && !keep {
				continue
			}
			result = append(result, line)
		}
		return resumeMutationAt(result, current.parent)
	})
}

// DeleteMessage removes one entry and reconnects each direct child to the
// deleted entry's parent. This preserves later branches without introducing a
// non-Pi tombstone entry into the session tree.
func (index *Index) DeleteMessage(path, entryID string) (Mutation, error) {
	return index.mutateMessage(path, entryID, func(lines []json.RawMessage, target int, entry map[string]json.RawMessage) ([]json.RawMessage, error) {
		var message struct {
			Role string `json:"role"`
		}
		if err := json.Unmarshal(entry["message"], &message); err != nil || (message.Role != "user" && message.Role != "assistant") {
			return nil, errors.New("only user and assistant messages can be deleted")
		}
		parents := make(map[string]string, len(lines))
		byID := make(map[string]map[string]json.RawMessage, len(lines))
		decoded := make([]map[string]json.RawMessage, len(lines))
		for lineIndex, line := range lines {
			if lineIndex == 0 {
				continue
			}
			var relationship struct {
				ID       string  `json:"id"`
				ParentID *string `json:"parentId"`
			}
			if json.Unmarshal(line, &relationship) != nil || relationship.ID == "" {
				continue
			}
			_ = json.Unmarshal(line, &decoded[lineIndex])
			byID[relationship.ID] = decoded[lineIndex]
			if relationship.ParentID != nil {
				parents[relationship.ID] = *relationship.ParentID
			} else {
				parents[relationship.ID] = ""
			}
		}
		removeIDs := map[string]struct{}{entryID: {}}
		for changed := true; changed; {
			changed = false
			for lineIndex, child := range decoded {
				if lineIndex == target || child == nil {
					continue
				}
				var id, childParent string
				_ = json.Unmarshal(child["id"], &id)
				_ = json.Unmarshal(child["parentId"], &childParent)
				if id == "" {
					continue
				}
				if _, alreadyRemoved := removeIDs[id]; alreadyRemoved {
					continue
				}
				var kind, targetID string
				_ = json.Unmarshal(child["type"], &kind)
				_ = json.Unmarshal(child["targetId"], &targetID)
				_, targetRemoved := removeIDs[targetID]
				remove := kind == "label" && targetRemoved
				if entryRole(child) == "toolResult" {
					for ancestor := childParent; ancestor != ""; ancestor = parents[ancestor] {
						if _, removed := removeIDs[ancestor]; removed {
							remove = true
							break
						}
						role := entryRole(byID[ancestor])
						if role != "" && role != "toolResult" {
							break
						}
					}
				}
				if remove {
					removeIDs[id] = struct{}{}
					changed = true
				}
			}
		}
		result := make([]json.RawMessage, 0, len(lines)-len(removeIDs))
		for lineIndex, line := range lines {
			child := decoded[lineIndex]
			if child == nil {
				result = append(result, line)
				continue
			}
			var childID string
			_ = json.Unmarshal(child["id"], &childID)
			if _, remove := removeIDs[childID]; remove {
				continue
			}
			var childParent string
			changed := false
			if json.Unmarshal(child["parentId"], &childParent) == nil {
				if _, removeParent := removeIDs[childParent]; removeParent {
					for childParent != "" {
						if _, removeAncestor := removeIDs[childParent]; !removeAncestor {
							break
						}
						childParent = parents[childParent]
					}
					if childParent == "" {
						child["parentId"] = json.RawMessage("null")
					} else {
						child["parentId"], _ = json.Marshal(childParent)
					}
					changed = true
				}
			}
			var firstKeptEntryID string
			if json.Unmarshal(child["firstKeptEntryId"], &firstKeptEntryID) == nil {
				if _, removeBoundary := removeIDs[firstKeptEntryID]; removeBoundary {
					replacement := firstRetainedDescendantOnPath(parents, removeIDs, childParent)
					child["firstKeptEntryId"], _ = json.Marshal(replacement)
					changed = true
				}
			}
			if changed {
				encoded, err := json.Marshal(child)
				if err != nil {
					return nil, fmt.Errorf("encode updated session entry: %w", err)
				}
				line = encoded
			}
			result = append(result, line)
		}
		var leaf string
		_ = json.Unmarshal(decoded[len(decoded)-1]["id"], &leaf)
		for leaf != "" {
			if _, removed := removeIDs[leaf]; !removed {
				break
			}
			leaf = parents[leaf]
		}
		return resumeMutationAt(result, leaf)
	})
}

// Pi reopens the last entry as its active leaf, not the last timestamp.
// Removing a leaf must not silently activate a neighboring, unrelated branch.
func resumeMutationAt(lines []json.RawMessage, leafID string) ([]json.RawMessage, error) {
	if leafID == "" {
		if len(lines) > 1 {
			return nil, errors.New("cannot leave an empty active branch while retaining other branches; fork this message instead")
		}
		return lines, nil
	}
	for position := 1; position < len(lines); position++ {
		var entry struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(lines[position], &entry)
		if entry.ID != leafID {
			continue
		}
		leaf := lines[position]
		lines = append(lines[:position], lines[position+1:]...)
		return append(lines, leaf), nil
	}
	return nil, errors.New("active session leaf was not retained")
}

func entryRole(entry map[string]json.RawMessage) string {
	var message struct {
		Role string `json:"role"`
	}
	_ = json.Unmarshal(entry["message"], &message)
	return message.Role
}

func firstRetainedDescendantOnPath(parents map[string]string, removed map[string]struct{}, leaf string) string {
	path := make([]string, 0, 16)
	seen := make(map[string]struct{}, 16)
	for leaf != "" {
		if _, duplicate := seen[leaf]; duplicate {
			return ""
		}
		seen[leaf] = struct{}{}
		path = append(path, leaf)
		parent, exists := parents[leaf]
		if !exists {
			break
		}
		leaf = parent
	}
	for left, right := 0, len(path)-1; left < right; left, right = left+1, right-1 {
		path[left], path[right] = path[right], path[left]
	}
	seenRemoved := false
	candidate := ""
	for _, id := range path {
		if _, remove := removed[id]; remove {
			seenRemoved = true
			candidate = ""
			continue
		}
		if seenRemoved && candidate == "" {
			candidate = id
		}
	}
	return candidate
}

func (index *Index) RestoreMutation(mutation Mutation) error {
	index.mutationMu.Lock()
	defer index.mutationMu.Unlock()
	path, err := index.ValidatePath(mutation.Path)
	if err != nil {
		return err
	}
	backup, err := filepath.Abs(strings.TrimSpace(mutation.BackupPath))
	if err != nil {
		return fmt.Errorf("resolve session backup: %w", err)
	}
	if filepath.Dir(backup) != filepath.Dir(path) || !strings.HasPrefix(filepath.Base(backup), filepath.Base(path)+".") || !strings.HasSuffix(backup, ".pi-desk-backup") {
		return errors.New("session backup path is invalid")
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		return fmt.Errorf("read session backup: %w", err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read session before restore: %w", err)
	}
	if sha256.Sum256(current) != mutation.afterHash {
		return errors.New("session changed after mutation; backup was not restored")
	}
	if err := atomic.WriteFile(path, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("restore session backup: %w", err)
	}
	return nil
}

func (index *Index) mutateMessage(path, entryID string, change func([]json.RawMessage, int, map[string]json.RawMessage) ([]json.RawMessage, error)) (Mutation, error) {
	index.mutationMu.Lock()
	defer index.mutationMu.Unlock()

	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return Mutation{}, errors.New("entry id is required")
	}
	canonical, err := index.ValidatePath(path)
	if err != nil {
		return Mutation{}, err
	}
	lines, err := readMutationLines(canonical)
	if err != nil {
		return Mutation{}, err
	}
	target := -1
	var targetEntry map[string]json.RawMessage
	for lineIndex, line := range lines {
		if lineIndex == 0 {
			continue
		}
		var entry map[string]json.RawMessage
		if json.Unmarshal(line, &entry) != nil {
			continue
		}
		var id string
		if json.Unmarshal(entry["id"], &id) == nil && id == entryID {
			target, targetEntry = lineIndex, entry
			break
		}
	}
	if target < 0 {
		return Mutation{}, errors.New("message entry was not found in the session file")
	}
	var entryType string
	if json.Unmarshal(targetEntry["type"], &entryType) != nil || entryType != "message" {
		return Mutation{}, errors.New("entry is not a session message")
	}
	changed, err := change(lines, target, targetEntry)
	if err != nil {
		return Mutation{}, err
	}
	if err := validateMutationTree(changed); err != nil {
		return Mutation{}, err
	}
	backup, err := backupSession(canonical)
	if err != nil {
		return Mutation{}, err
	}
	if err := writeMutationLines(canonical, changed); err != nil {
		_ = os.Remove(backup)
		return Mutation{}, err
	}
	hash := sha256.New()
	for _, line := range changed {
		hash.Write(line)
		hash.Write([]byte{'\n'})
	}
	var afterHash [32]byte
	copy(afterHash[:], hash.Sum(nil))
	return Mutation{Path: canonical, BackupPath: backup, afterHash: afterHash}, nil
}

func replaceMessageText(content json.RawMessage, text string) (json.RawMessage, error) {
	var scalar string
	if json.Unmarshal(content, &scalar) == nil {
		return json.Marshal(text)
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(content, &blocks); err != nil {
		return nil, errors.New("message content is not editable")
	}
	encodedText, _ := json.Marshal(text)
	found := false
	filtered := make([]map[string]json.RawMessage, 0, len(blocks)+1)
	for _, block := range blocks {
		var blockType string
		_ = json.Unmarshal(block["type"], &blockType)
		if blockType == "text" {
			if found {
				continue
			}
			block["text"] = encodedText
			delete(block, "textSignature") // Provider signatures no longer describe edited text.
			found = true
		}
		filtered = append(filtered, block)
	}
	if !found {
		filtered = append(filtered, map[string]json.RawMessage{
			"type": json.RawMessage(`"text"`),
			"text": encodedText,
		})
	}
	return json.Marshal(filtered)
}

func readMutationLines(path string) ([]json.RawMessage, error) {
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
	lines := make([]json.RawMessage, 0, 256)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || !utf8.Valid(line) {
			return nil, fmt.Errorf("session transcript contains an invalid line at line %d", lineNumber)
		}
		var valid json.RawMessage
		if json.Unmarshal(line, &valid) != nil {
			return nil, fmt.Errorf("session transcript contains malformed JSON at line %d; each line must contain exactly one JSON object", lineNumber)
		}
		lines = append(lines, append(json.RawMessage(nil), line...))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read session transcript: %w", err)
	}
	if len(lines) == 0 {
		return nil, errors.New("session transcript is empty")
	}
	if err := validateMutationTree(lines); err != nil {
		return nil, err
	}
	return lines, nil
}

// Reject ambiguous IDs and broken parent chains before any backup or write.
// Parents need not precede children: replay may move the resume entry to EOF.
func validateMutationTree(lines []json.RawMessage) error {
	parents := make(map[string]string, len(lines))
	for position, line := range lines {
		var entry struct {
			Type     string  `json:"type"`
			ID       string  `json:"id"`
			ParentID *string `json:"parentId"`
		}
		if json.Unmarshal(line, &entry) != nil || entry.Type == "" || entry.ID == "" {
			return fmt.Errorf("invalid session entry at line %d", position+1)
		}
		if position == 0 {
			if entry.Type != "session" {
				return errors.New("session header is missing")
			}
			continue
		}
		if entry.Type == "session" {
			return fmt.Errorf("unexpected session header at line %d", position+1)
		}
		if _, exists := parents[entry.ID]; exists {
			return fmt.Errorf("duplicate session entry id %q at line %d", entry.ID, position+1)
		}
		parent := ""
		if entry.ParentID != nil {
			parent = *entry.ParentID
		}
		parents[entry.ID] = parent
	}
	visited := make(map[string]uint8, len(parents))
	for id := range parents {
		path := make([]string, 0, 16)
		for current := id; current != "" && visited[current] != 2; current = parents[current] {
			if _, exists := parents[current]; !exists {
				return fmt.Errorf("session branch contains a missing parent %q", current)
			}
			if visited[current] == 1 {
				return errors.New("session branch contains a parent cycle")
			}
			visited[current] = 1
			path = append(path, current)
		}
		for _, current := range path {
			visited[current] = 2
		}
	}
	return nil
}

func writeMutationLines(path string, lines []json.RawMessage) error {
	var output bytes.Buffer
	for _, line := range lines {
		if len(line) > maxLineBytes {
			return errors.New("edited session entry exceeds the line safety limit")
		}
		if output.Len()+len(line)+1 > maxSessionBytes {
			return errors.New("edited session exceeds the file safety limit")
		}
		output.Write(line)
		output.WriteByte('\n')
	}
	if err := atomic.WriteFile(path, &output); err != nil {
		return fmt.Errorf("write session transcript: %w", err)
	}
	return nil
}

func backupSession(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read session for backup: %w", err)
	}
	backup := fmt.Sprintf("%s.%d.pi-desk-backup", path, time.Now().UnixNano())
	if err := os.WriteFile(backup, data, 0o600); err != nil {
		return "", fmt.Errorf("back up session: %w", err)
	}
	entries, _ := filepath.Glob(path + ".*.pi-desk-backup")
	sort.Strings(entries)
	for len(entries) > maxMutationBackups {
		_ = os.Remove(entries[0])
		entries = entries[1:]
	}
	return backup, nil
}
