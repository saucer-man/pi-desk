package appservice

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"pi-desk/internal/domain"

	"github.com/natefinch/atomic"
)

const (
	piDeskTodoExtensionName   = "pi-desk-todo.ts"
	legacyTodoExtensionName   = "pi-deck-todo.ts"
	maxExtensionSettingsBytes = 4 << 20
	maxGlobalExtensionEntries = 1000
)

//go:embed resources/pi-desk-todo.ts
var bundledPiDeskTodoExtension []byte

type PiExtensionService struct {
	agentDirectory string
	directoryErr   error
	todoSource     []byte

	mu sync.Mutex
}

func NewPiExtensionService() *PiExtensionService {
	directory, err := defaultPiAgentDirectory()
	return &PiExtensionService{agentDirectory: directory, directoryErr: err, todoSource: bundledPiDeskTodoExtension}
}

func newPiExtensionService(agentDirectory string, todoSource []byte) *PiExtensionService {
	return &PiExtensionService{agentDirectory: agentDirectory, todoSource: todoSource}
}

func (service *PiExtensionService) ListExtensions() (domain.PiExtensionSnapshot, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.snapshot()
}

func (service *PiExtensionService) InstallPiDeskTodo() (domain.PiDeskTodoInstallResult, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	directory, err := service.extensionDirectory()
	if err != nil {
		return domain.PiDeskTodoInstallResult{}, err
	}
	if len(service.todoSource) == 0 {
		return domain.PiDeskTodoInstallResult{}, errors.New("Pi Desk todo extension source is unavailable")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return domain.PiDeskTodoInstallResult{}, fmt.Errorf("create Pi extension directory: %w", err)
	}

	legacyPath := filepath.Join(directory, legacyTodoExtensionName)
	backupPath := legacyPath + ".disabled-by-pi-desk"
	replacedLegacy := false
	if _, statErr := os.Stat(legacyPath); statErr == nil {
		if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return domain.PiDeskTodoInstallResult{}, fmt.Errorf("replace legacy todo backup: %w", err)
		}
		if err := os.Rename(legacyPath, backupPath); err != nil {
			return domain.PiDeskTodoInstallResult{}, fmt.Errorf("disable legacy PiDeck todo extension: %w", err)
		}
		replacedLegacy = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return domain.PiDeskTodoInstallResult{}, fmt.Errorf("inspect legacy PiDeck todo extension: %w", statErr)
	}

	targetPath := filepath.Join(directory, piDeskTodoExtensionName)
	if err := atomic.WriteFile(targetPath, bytes.NewReader(service.todoSource)); err != nil {
		if replacedLegacy {
			_ = os.Rename(backupPath, legacyPath)
		}
		return domain.PiDeskTodoInstallResult{}, fmt.Errorf("install Pi Desk todo extension: %w", err)
	}
	if err := os.Chmod(targetPath, 0o600); err != nil {
		return domain.PiDeskTodoInstallResult{}, fmt.Errorf("protect Pi Desk todo extension: %w", err)
	}

	status, err := service.todoStatus(directory)
	if err != nil {
		return domain.PiDeskTodoInstallResult{}, err
	}
	return domain.PiDeskTodoInstallResult{Todo: status, ReplacedLegacy: replacedLegacy}, nil
}

func (service *PiExtensionService) RemovePiDeskTodo() error {
	service.mu.Lock()
	defer service.mu.Unlock()

	directory, err := service.extensionDirectory()
	if err != nil {
		return err
	}
	path := filepath.Join(directory, piDeskTodoExtensionName)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove Pi Desk todo extension: %w", err)
	}
	return nil
}

func (service *PiExtensionService) snapshot() (domain.PiExtensionSnapshot, error) {
	directory, err := service.extensionDirectory()
	if err != nil {
		return domain.PiExtensionSnapshot{}, err
	}
	settingsPath := filepath.Join(filepath.Clean(service.agentDirectory), "settings.json")
	extensions, err := listGlobalExtensions(directory)
	if err != nil {
		return domain.PiExtensionSnapshot{}, err
	}
	configured, err := listConfiguredExtensions(settingsPath)
	if err != nil {
		return domain.PiExtensionSnapshot{}, err
	}
	extensions = append(extensions, configured...)
	extensions = deduplicateExtensions(extensions)
	sort.Slice(extensions, func(left, right int) bool {
		if extensions[left].Origin != extensions[right].Origin {
			return extensions[left].Origin < extensions[right].Origin
		}
		return strings.ToLower(extensions[left].Name) < strings.ToLower(extensions[right].Name)
	})
	status, err := service.todoStatus(directory)
	if err != nil {
		return domain.PiExtensionSnapshot{}, err
	}
	return domain.PiExtensionSnapshot{
		GlobalDirectory: directory,
		SettingsPath:    settingsPath,
		Extensions:      extensions,
		Todo:            status,
	}, nil
}

func (service *PiExtensionService) extensionDirectory() (string, error) {
	if service.directoryErr != nil {
		return "", service.directoryErr
	}
	if strings.TrimSpace(service.agentDirectory) == "" {
		return "", errors.New("locate Pi agent directory")
	}
	return filepath.Join(filepath.Clean(service.agentDirectory), "extensions"), nil
}

func (service *PiExtensionService) todoStatus(directory string) (domain.PiDeskTodoExtensionStatus, error) {
	targetPath := filepath.Join(directory, piDeskTodoExtensionName)
	legacyPath := filepath.Join(directory, legacyTodoExtensionName)
	backupPath := legacyPath + ".disabled-by-pi-desk"
	status := domain.PiDeskTodoExtensionStatus{Path: targetPath, LegacyPath: legacyPath}
	content, err := os.ReadFile(targetPath)
	if err == nil {
		status.Installed = true
		status.UpdateAvailable = !bytes.Equal(content, service.todoSource)
	} else if !errors.Is(err, os.ErrNotExist) {
		return domain.PiDeskTodoExtensionStatus{}, fmt.Errorf("read Pi Desk todo extension: %w", err)
	}
	if _, err := os.Stat(legacyPath); err == nil {
		status.LegacyInstalled = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return domain.PiDeskTodoExtensionStatus{}, fmt.Errorf("inspect legacy PiDeck todo extension: %w", err)
	}
	if _, err := os.Stat(backupPath); err == nil {
		status.LegacyBackupPath = backupPath
	} else if !errors.Is(err, os.ErrNotExist) {
		return domain.PiDeskTodoExtensionStatus{}, fmt.Errorf("inspect legacy todo backup: %w", err)
	}
	return status, nil
}

func listGlobalExtensions(directory string) ([]domain.PiExtensionSummary, error) {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []domain.PiExtensionSummary{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Pi extension directory: %w", err)
	}
	if len(entries) > maxGlobalExtensionEntries {
		return nil, errors.New("Pi extension directory exceeds the 1000 entry safety limit")
	}
	result := make([]domain.PiExtensionSummary, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || strings.HasSuffix(entry.Name(), ".d.ts") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		info, statErr := os.Stat(path)
		if statErr != nil {
			continue
		}
		if !info.IsDir() && isPiExtensionFile(entry.Name()) {
			result = append(result, domain.PiExtensionSummary{
				Name:   strings.TrimSuffix(strings.TrimSuffix(entry.Name(), ".ts"), ".js"),
				Source: entry.Name(), Path: path, Origin: domain.PiExtensionOriginGlobal,
			})
			continue
		}
		if info.IsDir() && directoryLoadsAsExtension(path) {
			result = append(result, domain.PiExtensionSummary{
				Name: entry.Name(), Source: entry.Name(), Path: path, Origin: domain.PiExtensionOriginGlobal,
			})
		}
	}
	return result, nil
}

func isPiExtensionFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".ts") || strings.HasSuffix(lower, ".js")
}

func directoryLoadsAsExtension(directory string) bool {
	for _, name := range []string{"index.ts", "index.js"} {
		if info, err := os.Stat(filepath.Join(directory, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	content, err := os.ReadFile(filepath.Join(directory, "package.json"))
	if err != nil || len(content) > maxExtensionSettingsBytes {
		return false
	}
	var manifest struct {
		Pi struct {
			Extensions []string `json:"extensions"`
		} `json:"pi"`
	}
	return json.Unmarshal(content, &manifest) == nil && len(manifest.Pi.Extensions) > 0
}

func listConfiguredExtensions(settingsPath string) ([]domain.PiExtensionSummary, error) {
	content, err := os.ReadFile(settingsPath)
	if errors.Is(err, os.ErrNotExist) {
		return []domain.PiExtensionSummary{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Pi settings: %w", err)
	}
	if len(content) > maxExtensionSettingsBytes {
		return nil, errors.New("Pi settings exceed the 4 MiB safety limit")
	}
	var settings struct {
		Extensions []string          `json:"extensions"`
		Packages   []json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(content, &settings); err != nil {
		return nil, fmt.Errorf("parse Pi settings: %w", err)
	}
	result := make([]domain.PiExtensionSummary, 0, len(settings.Extensions)+len(settings.Packages))
	for _, source := range settings.Extensions {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		path := source
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(settingsPath), path)
		}
		result = append(result, domain.PiExtensionSummary{
			Name: extensionDisplayName(source), Source: source, Path: filepath.Clean(path), Origin: domain.PiExtensionOriginSettings,
		})
	}
	for _, raw := range settings.Packages {
		source := packageSource(raw)
		if source == "" {
			continue
		}
		result = append(result, domain.PiExtensionSummary{
			Name: extensionDisplayName(source), Source: source, Origin: domain.PiExtensionOriginPackage,
		})
	}
	return result, nil
}

func packageSource(raw json.RawMessage) string {
	var source string
	if json.Unmarshal(raw, &source) == nil {
		return strings.TrimSpace(source)
	}
	var object struct {
		Source string `json:"source"`
	}
	if json.Unmarshal(raw, &object) == nil {
		return strings.TrimSpace(object.Source)
	}
	return ""
}

func extensionDisplayName(source string) string {
	normalized := source
	for _, prefix := range []string{"npm:", "file:", "github:", "git:"} {
		normalized = strings.TrimPrefix(normalized, prefix)
	}
	name := filepath.Base(strings.ReplaceAll(normalized, "\\", "/"))
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".ts"), ".js")
	if at := strings.LastIndex(name, "@"); at > 0 {
		name = name[:at]
	}
	if name == "" || name == "." || name == "/" {
		return source
	}
	return name
}

func deduplicateExtensions(extensions []domain.PiExtensionSummary) []domain.PiExtensionSummary {
	result := make([]domain.PiExtensionSummary, 0, len(extensions))
	seen := make(map[string]struct{}, len(extensions))
	for _, extension := range extensions {
		key := extension.Path
		if key == "" {
			key = string(extension.Origin) + "\x00" + extension.Source
		}
		key = strings.ToLower(filepath.Clean(key))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, extension)
	}
	return result
}
