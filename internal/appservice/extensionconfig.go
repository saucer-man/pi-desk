package appservice

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"pi-desk/internal/domain"
	"pi-desk/internal/piruntime"
	"pi-desk/internal/workspace"

	"github.com/natefinch/atomic"
)

const (
	piDeskTodoExtensionName   = "pi-desk-todo.ts"
	legacyTodoExtensionName   = "pi-deck-todo.ts"
	maxExtensionSettingsBytes = 4 << 20
	maxGlobalExtensionEntries = 1000
	maxPackageSourceBytes     = 2048
	piPackageCommandTimeout   = 10 * time.Minute
)

//go:embed resources/pi-desk-todo.ts
var bundledPiDeskTodoExtension []byte

type PiExtensionService struct {
	agentDirectory string
	directoryErr   error
	todoSource     []byte
	workspaces     interface {
		ResolvePath(string) (workspace.Record, error)
	}
	packageRunner interface {
		Run(context.Context, string, ...string) (string, error)
	}

	mu sync.Mutex
}

func NewPiExtensionService(catalog *workspace.Catalog, locator *piruntime.Locator) *PiExtensionService {
	directory, err := defaultPiAgentDirectory()
	return &PiExtensionService{
		agentDirectory: directory, directoryErr: err, todoSource: bundledPiDeskTodoExtension,
		workspaces: catalog, packageRunner: locatorPiPackageRunner{locator: locator},
	}
}

func newPiExtensionService(agentDirectory string, todoSource []byte) *PiExtensionService {
	return &PiExtensionService{agentDirectory: agentDirectory, todoSource: todoSource}
}

type locatorPiPackageRunner struct{ locator *piruntime.Locator }

func (runner locatorPiPackageRunner) Run(ctx context.Context, directory string, args ...string) (string, error) {
	if runner.locator == nil {
		return "", errors.New("Pi package management is unavailable")
	}
	invocation, err := runner.locator.Invocation(args...)
	if err != nil {
		return "", err
	}
	invocation.Directory = directory
	output, runErr := runner.locator.Run(ctx, invocation)
	return strings.TrimSpace(strings.ToValidUTF8(string(output), "?")), runErr
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

func (service *PiExtensionService) ListPackages(request domain.ListPiPackagesRequest) (domain.PiPackageSnapshot, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	globalPath, err := service.globalSettingsPath()
	if err != nil {
		return domain.PiPackageSnapshot{}, err
	}
	global, err := listPiPackages(globalPath, domain.PiPackageScopeGlobal)
	if err != nil {
		return domain.PiPackageSnapshot{}, err
	}
	snapshot := domain.PiPackageSnapshot{GlobalSettingsPath: globalPath, Packages: global}
	projectPath, notice, enabled := service.projectSettingsPath(request.WorkspacePath)
	snapshot.ProjectSettingsPath, snapshot.ProjectNotice, snapshot.ProjectEnabled = projectPath, notice, enabled
	if !enabled {
		return snapshot, nil
	}
	project, err := listPiPackages(projectPath, domain.PiPackageScopeProject)
	if err != nil {
		return domain.PiPackageSnapshot{}, err
	}
	snapshot.Packages = append(snapshot.Packages, project...)
	sort.Slice(snapshot.Packages, func(left, right int) bool {
		if snapshot.Packages[left].Scope != snapshot.Packages[right].Scope {
			return snapshot.Packages[left].Scope < snapshot.Packages[right].Scope
		}
		return strings.ToLower(snapshot.Packages[left].Source) < strings.ToLower(snapshot.Packages[right].Source)
	})
	return snapshot, nil
}

func (service *PiExtensionService) InstallPackage(request domain.PiPackageRequest) (domain.PiPackageCommandResult, error) {
	args, directory, err := service.packageCommand(request, "install")
	if err != nil {
		return domain.PiPackageCommandResult{}, err
	}
	return service.runPackageCommand(directory, args...)
}

func (service *PiExtensionService) UpdatePackage(request domain.PiPackageRequest) (domain.PiPackageCommandResult, error) {
	args, directory, err := service.packageCommand(request, "update")
	if err != nil {
		return domain.PiPackageCommandResult{}, err
	}
	return service.runPackageCommand(directory, args...)
}

func (service *PiExtensionService) RemovePackage(request domain.PiPackageRequest) (domain.PiPackageCommandResult, error) {
	args, directory, err := service.packageCommand(request, "remove")
	if err != nil {
		return domain.PiPackageCommandResult{}, err
	}
	return service.runPackageCommand(directory, args...)
}

func (service *PiExtensionService) SetPackageEnabled(request domain.SetPiPackageEnabledRequest) error {
	service.mu.Lock()
	defer service.mu.Unlock()

	source, err := validPiPackageSource(request.Source)
	if err != nil {
		return err
	}
	settingsPath, _, err := service.packageSettingsPath(request.Scope, request.WorkspacePath)
	if err != nil {
		return err
	}
	settings, packages, err := readPiPackageSettings(settingsPath)
	if err != nil {
		return err
	}
	changed := false
	for index, raw := range packages {
		if packageSource(raw) != source {
			continue
		}
		changed = true
		if request.Enabled {
			packages[index], _ = json.Marshal(source)
			continue
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil || object == nil {
			object = map[string]json.RawMessage{}
			object["source"], _ = json.Marshal(source)
		}
		empty, _ := json.Marshal([]string{})
		for _, key := range []string{"extensions", "skills", "prompts", "themes"} {
			object[key] = empty
		}
		packages[index], _ = json.Marshal(object)
	}
	if !changed {
		return errors.New("Pi package is not configured in the selected scope")
	}
	encodedPackages, _ := json.Marshal(packages)
	settings["packages"] = encodedPackages
	return writePiPackageSettings(settingsPath, settings, request.Scope)
}

func (service *PiExtensionService) packageCommand(request domain.PiPackageRequest, action string) ([]string, string, error) {
	source, err := validPiPackageSource(request.Source)
	if err != nil {
		return nil, "", err
	}
	_, directory, err := service.packageSettingsPath(request.Scope, request.WorkspacePath)
	if err != nil {
		return nil, "", err
	}
	args := []string{action, source}
	if request.Scope == domain.PiPackageScopeProject && (action == "install" || action == "remove") {
		args = append(args, "-l")
	}
	return args, directory, nil
}

func (service *PiExtensionService) runPackageCommand(directory string, args ...string) (domain.PiPackageCommandResult, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.packageRunner == nil {
		return domain.PiPackageCommandResult{}, errors.New("Pi package management is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), piPackageCommandTimeout)
	defer cancel()
	output, err := service.packageRunner.Run(ctx, directory, args...)
	result := domain.PiPackageCommandResult{Output: output}
	if err != nil {
		if output != "" {
			return result, fmt.Errorf("Pi package command failed: %s", truncateMaintenanceOutput(output))
		}
		return result, err
	}
	return result, nil
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

func (service *PiExtensionService) globalSettingsPath() (string, error) {
	if service.directoryErr != nil {
		return "", service.directoryErr
	}
	if strings.TrimSpace(service.agentDirectory) == "" {
		return "", errors.New("locate Pi agent directory")
	}
	return filepath.Join(filepath.Clean(service.agentDirectory), "settings.json"), nil
}

func (service *PiExtensionService) projectSettingsPath(workspacePath string) (string, string, bool) {
	if strings.TrimSpace(workspacePath) == "" {
		return "", "select a workspace to manage project packages", false
	}
	if service.workspaces == nil {
		return "", "workspace catalog is unavailable", false
	}
	record, err := service.workspaces.ResolvePath(strings.TrimSpace(workspacePath))
	if err != nil || record.Location.Kind != workspace.KindLocal {
		return "", "project packages require a registered local workspace", false
	}
	settingsPath := filepath.Join(filepath.Clean(record.Path), ".pi", "settings.json")
	if record.Trust != "approve" {
		return settingsPath, "trust project resources before managing project packages", false
	}
	if err := validateProjectSettingsPath(settingsPath); err != nil {
		return settingsPath, err.Error(), false
	}
	return settingsPath, "", true
}

func validateProjectSettingsPath(settingsPath string) error {
	directory := filepath.Dir(settingsPath)
	if info, err := os.Lstat(directory); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("project package directory must be a real directory inside the workspace")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect project package directory: %w", err)
	}
	if info, err := os.Lstat(settingsPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("project package settings must be a regular file inside the workspace")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect project package settings: %w", err)
	}
	return nil
}

func (service *PiExtensionService) packageSettingsPath(scope domain.PiPackageScope, workspacePath string) (string, string, error) {
	switch scope {
	case domain.PiPackageScopeGlobal:
		path, err := service.globalSettingsPath()
		return path, filepath.Dir(path), err
	case domain.PiPackageScopeProject:
		path, notice, enabled := service.projectSettingsPath(workspacePath)
		if !enabled {
			return "", "", errors.New(notice)
		}
		return path, filepath.Dir(filepath.Dir(path)), nil
	default:
		return "", "", errors.New("Pi package scope must be global or project")
	}
}

func validPiPackageSource(value string) (string, error) {
	source := strings.TrimSpace(value)
	if source == "" || len(source) > maxPackageSourceBytes {
		return "", fmt.Errorf("Pi package source must contain 1 to %d bytes", maxPackageSourceBytes)
	}
	if strings.HasPrefix(source, "-") || strings.ContainsFunc(source, func(character rune) bool {
		return character == 0 || character == '\r' || character == '\n' || character == '\t' || character < 0x20
	}) {
		return "", errors.New("Pi package source is invalid")
	}
	return source, nil
}

func listPiPackages(settingsPath string, scope domain.PiPackageScope) ([]domain.PiPackageSummary, error) {
	_, packages, err := readPiPackageSettings(settingsPath)
	if err != nil {
		return nil, err
	}
	result := make([]domain.PiPackageSummary, 0, len(packages))
	for _, raw := range packages {
		source := packageSource(raw)
		if source == "" {
			continue
		}
		result = append(result, domain.PiPackageSummary{Source: source, Scope: scope, Enabled: !piPackageDisabled(raw)})
	}
	return result, nil
}

func readPiPackageSettings(settingsPath string) (map[string]json.RawMessage, []json.RawMessage, error) {
	content, err := os.ReadFile(settingsPath)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, []json.RawMessage{}, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read Pi settings: %w", err)
	}
	if len(content) > maxExtensionSettingsBytes {
		return nil, nil, errors.New("Pi settings exceed the 4 MiB safety limit")
	}
	settings := map[string]json.RawMessage{}
	if err := json.Unmarshal(content, &settings); err != nil {
		return nil, nil, fmt.Errorf("parse Pi settings: %w", err)
	}
	var packages []json.RawMessage
	if raw := settings["packages"]; len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &packages); err != nil {
			return nil, nil, errors.New("Pi settings packages must be an array")
		}
	}
	return settings, packages, nil
}

func piPackageDisabled(raw json.RawMessage) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return false
	}
	for _, key := range []string{"extensions", "skills", "prompts", "themes"} {
		var values []string
		if json.Unmarshal(object[key], &values) != nil || len(values) != 0 {
			return false
		}
	}
	return true
}

func writePiPackageSettings(settingsPath string, settings map[string]json.RawMessage, scope domain.PiPackageScope) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Pi settings: %w", err)
	}
	data = append(data, '\n')
	directoryMode, fileMode := os.FileMode(0o700), os.FileMode(0o600)
	if scope == domain.PiPackageScopeProject {
		directoryMode, fileMode = 0o755, 0o644
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), directoryMode); err != nil {
		return fmt.Errorf("create Pi settings directory: %w", err)
	}
	if err := atomic.WriteFile(settingsPath, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("write Pi settings: %w", err)
	}
	if err := os.Chmod(settingsPath, fileMode); err != nil {
		return fmt.Errorf("protect Pi settings: %w", err)
	}
	return nil
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
