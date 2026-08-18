package appservice

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"pi-desk/internal/domain"
	"pi-desk/internal/workspace"

	"github.com/natefinch/atomic"
)

const (
	maxPromptTemplateBytes = 1 << 20
	maxPromptTemplateName  = 120
)

type promptWorkspaceResolver interface {
	ResolvePath(string) (workspace.Record, error)
}

// PromptTemplateService edits Pi's documented prompt template locations. It does
// not maintain a second registry, so Pi remains the source of truth for commands.
type PromptTemplateService struct {
	agentDirectory    string
	agentDirectoryErr error
	workspaces        promptWorkspaceResolver

	mu sync.Mutex
}

func NewPromptTemplateService(catalog *workspace.Catalog) *PromptTemplateService {
	directory, err := defaultPiAgentDirectory()
	return &PromptTemplateService{agentDirectory: directory, agentDirectoryErr: err, workspaces: catalog}
}

func newPromptTemplateService(agentDirectory string, workspaces promptWorkspaceResolver) *PromptTemplateService {
	return &PromptTemplateService{agentDirectory: agentDirectory, workspaces: workspaces}
}

func (service *PromptTemplateService) ListPromptTemplates(request domain.ListPromptTemplatesRequest) (domain.PromptTemplateSnapshot, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	globalDirectory, err := service.globalDirectory()
	if err != nil {
		return domain.PromptTemplateSnapshot{}, err
	}
	globalTemplates, err := listPromptTemplates(globalDirectory, domain.PromptTemplateScopeGlobal, true)
	if err != nil {
		return domain.PromptTemplateSnapshot{}, err
	}

	snapshot := domain.PromptTemplateSnapshot{
		GlobalDirectory: globalDirectory,
		ProjectEnabled:  false,
		Templates:       globalTemplates,
	}
	projectDirectory, notice, enabled := service.projectDirectory(request.WorkspacePath)
	if strings.TrimSpace(request.WorkspacePath) == "" {
		return snapshot, nil
	}
	snapshot.ProjectDirectory = projectDirectory
	snapshot.ProjectNotice = notice
	snapshot.ProjectEnabled = enabled
	if !enabled {
		return snapshot, nil
	}
	projectTemplates, err := listPromptTemplates(projectDirectory, domain.PromptTemplateScopeProject, false)
	if err != nil {
		return domain.PromptTemplateSnapshot{}, err
	}
	snapshot.Templates = append(snapshot.Templates, projectTemplates...)
	sortPromptTemplates(snapshot.Templates)
	return snapshot, nil
}

func (service *PromptTemplateService) GetPromptTemplate(request domain.PromptTemplateRequest) (domain.PromptTemplate, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	directory, err := service.directoryFor(request.Scope, request.WorkspacePath, false)
	if err != nil {
		return domain.PromptTemplate{}, err
	}
	name, err := validPromptTemplateName(request.Name)
	if err != nil {
		return domain.PromptTemplate{}, err
	}
	path := promptTemplatePath(directory, name)
	content, err := readPromptTemplate(path)
	if err != nil {
		return domain.PromptTemplate{}, err
	}
	description, argumentHint := promptTemplateMetadata(content)
	return domain.PromptTemplate{PromptTemplateSummary: domain.PromptTemplateSummary{
		Scope: request.Scope, Name: name, Description: description, ArgumentHint: argumentHint, Path: path,
	}, Content: content}, nil
}

func (service *PromptTemplateService) UpsertPromptTemplate(request domain.UpsertPromptTemplateRequest) (domain.PromptTemplate, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	directory, err := service.directoryFor(request.Scope, request.WorkspacePath, true)
	if err != nil {
		return domain.PromptTemplate{}, err
	}
	name, err := validPromptTemplateName(request.Name)
	if err != nil {
		return domain.PromptTemplate{}, err
	}
	if len(request.Content) > maxPromptTemplateBytes {
		return domain.PromptTemplate{}, fmt.Errorf("prompt template exceeds the %d MiB safety limit", maxPromptTemplateBytes>>20)
	}
	if !utf8.ValidString(request.Content) {
		return domain.PromptTemplate{}, errors.New("prompt template must be valid UTF-8")
	}
	if err := os.MkdirAll(directory, promptDirectoryMode(request.Scope)); err != nil {
		return domain.PromptTemplate{}, fmt.Errorf("create prompt template directory: %w", err)
	}

	targetPath := promptTemplatePath(directory, name)
	originalName := strings.TrimSpace(request.OriginalName)
	if originalName != "" {
		originalName, err = validPromptTemplateName(originalName)
		if err != nil {
			return domain.PromptTemplate{}, err
		}
		originalPath := promptTemplatePath(directory, originalName)
		if _, err := os.Stat(originalPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return domain.PromptTemplate{}, fmt.Errorf("prompt template %q was not found", originalName)
			}
			return domain.PromptTemplate{}, fmt.Errorf("read prompt template %q: %w", originalName, err)
		}
		if originalPath != targetPath {
			if _, err := os.Stat(targetPath); err == nil {
				return domain.PromptTemplate{}, fmt.Errorf("prompt template %q already exists", name)
			} else if !errors.Is(err, os.ErrNotExist) {
				return domain.PromptTemplate{}, fmt.Errorf("check prompt template %q: %w", name, err)
			}
			if err := os.Rename(originalPath, targetPath); err != nil {
				return domain.PromptTemplate{}, fmt.Errorf("rename prompt template: %w", err)
			}
		}
	} else if _, err := os.Stat(targetPath); err == nil {
		return domain.PromptTemplate{}, fmt.Errorf("prompt template %q already exists", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return domain.PromptTemplate{}, fmt.Errorf("check prompt template %q: %w", name, err)
	}

	if err := atomic.WriteFile(targetPath, strings.NewReader(request.Content)); err != nil {
		return domain.PromptTemplate{}, fmt.Errorf("write prompt template: %w", err)
	}
	description, argumentHint := promptTemplateMetadata(request.Content)
	return domain.PromptTemplate{PromptTemplateSummary: domain.PromptTemplateSummary{
		Scope: request.Scope, Name: name, Description: description, ArgumentHint: argumentHint, Path: targetPath,
	}, Content: request.Content}, nil
}

func (service *PromptTemplateService) DeletePromptTemplate(request domain.PromptTemplateRequest) error {
	service.mu.Lock()
	defer service.mu.Unlock()

	directory, err := service.directoryFor(request.Scope, request.WorkspacePath, false)
	if err != nil {
		return err
	}
	name, err := validPromptTemplateName(request.Name)
	if err != nil {
		return err
	}
	path := promptTemplatePath(directory, name)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("prompt template %q was not found", name)
		}
		return fmt.Errorf("delete prompt template: %w", err)
	}
	return nil
}

func (service *PromptTemplateService) globalDirectory() (string, error) {
	if service.agentDirectoryErr != nil {
		return "", service.agentDirectoryErr
	}
	if strings.TrimSpace(service.agentDirectory) == "" {
		return "", errors.New("locate Pi agent directory")
	}
	return filepath.Join(filepath.Clean(service.agentDirectory), "prompts"), nil
}

func (service *PromptTemplateService) directoryFor(scope domain.PromptTemplateScope, workspacePath string, create bool) (string, error) {
	switch scope {
	case domain.PromptTemplateScopeGlobal:
		return service.globalDirectory()
	case domain.PromptTemplateScopeProject:
		directory, notice, enabled := service.projectDirectory(workspacePath)
		if !enabled {
			if notice == "" {
				notice = "project prompt templates are unavailable"
			}
			return "", errors.New(notice)
		}
		if create {
			return directory, nil
		}
		return directory, nil
	default:
		return "", errors.New("prompt template scope must be global or project")
	}
}

func (service *PromptTemplateService) projectDirectory(workspacePath string) (string, string, bool) {
	if strings.TrimSpace(workspacePath) == "" {
		return "", "select a workspace to manage project prompt templates", false
	}
	if service.workspaces == nil {
		return "", "workspace catalog is unavailable", false
	}
	record, err := service.workspaces.ResolvePath(strings.TrimSpace(workspacePath))
	if err != nil {
		return "", "project prompt templates require a registered workspace", false
	}
	directory := filepath.Join(filepath.Clean(record.Path), ".pi", "prompts")
	if record.Trust != "approve" {
		return directory, "trust project resources before managing project prompt templates", false
	}
	return directory, "", true
}

func promptDirectoryMode(scope domain.PromptTemplateScope) os.FileMode {
	if scope == domain.PromptTemplateScopeGlobal {
		return 0o700
	}
	return 0o755
}

func listPromptTemplates(directory string, scope domain.PromptTemplateScope, create bool) ([]domain.PromptTemplateSummary, error) {
	if create {
		if err := os.MkdirAll(directory, promptDirectoryMode(scope)); err != nil {
			return nil, fmt.Errorf("create prompt template directory: %w", err)
		}
	}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []domain.PromptTemplateSummary{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read prompt template directory: %w", err)
	}
	templates := make([]domain.PromptTemplateSummary, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") || strings.HasSuffix(strings.ToLower(entry.Name()), ".d.md") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if _, err := validPromptTemplateName(name); err != nil {
			continue
		}
		path := promptTemplatePath(directory, name)
		content, err := readPromptTemplate(path)
		if err != nil {
			return nil, err
		}
		description, argumentHint := promptTemplateMetadata(content)
		templates = append(templates, domain.PromptTemplateSummary{
			Scope: scope, Name: name, Description: description, ArgumentHint: argumentHint, Path: path,
		})
	}
	sortPromptTemplates(templates)
	return templates, nil
}

func sortPromptTemplates(templates []domain.PromptTemplateSummary) {
	sort.Slice(templates, func(left, right int) bool {
		if templates[left].Scope != templates[right].Scope {
			return templates[left].Scope < templates[right].Scope
		}
		return strings.ToLower(templates[left].Name) < strings.ToLower(templates[right].Name)
	})
}

func readPromptTemplate(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.New("prompt template was not found")
		}
		return "", fmt.Errorf("read prompt template: %w", err)
	}
	if len(content) > maxPromptTemplateBytes {
		return "", fmt.Errorf("prompt template exceeds the %d MiB safety limit", maxPromptTemplateBytes>>20)
	}
	if !utf8.Valid(content) {
		return "", errors.New("prompt template must be valid UTF-8")
	}
	return string(content), nil
}

func validPromptTemplateName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || utf8.RuneCountInString(name) > maxPromptTemplateName {
		return "", fmt.Errorf("prompt template name must contain 1 to %d characters", maxPromptTemplateName)
	}
	for _, character := range name {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '-' || character == '_' {
			continue
		}
		return "", errors.New("prompt template name may contain only letters, numbers, hyphens, and underscores")
	}
	return name, nil
}

func promptTemplatePath(directory, name string) string {
	return filepath.Join(directory, name+".md")
}

func promptTemplateMetadata(content string) (description, argumentHint string) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimPrefix(content, "\ufeff"), "\r\n", "\n"), "\n")
	start := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for index := 1; index < len(lines); index++ {
			if strings.TrimSpace(lines[index]) == "---" {
				for _, line := range lines[1:index] {
					key, value, found := strings.Cut(line, ":")
					if !found {
						continue
					}
					value = strings.Trim(strings.TrimSpace(value), "\"'")
					switch strings.TrimSpace(key) {
					case "description":
						description = value
					case "argument-hint":
						argumentHint = value
					}
				}
				start = index + 1
				break
			}
		}
	}
	if description != "" {
		return description, argumentHint
	}
	for _, line := range lines[start:] {
		line = strings.TrimSpace(line)
		if line != "" {
			return line, argumentHint
		}
	}
	return "", argumentHint
}
