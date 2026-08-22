package appservice

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"pi-desk/internal/domain"
	"pi-desk/internal/workspace"

	"github.com/natefinch/atomic"
)

const (
	maxSkillBytes       = 1 << 20
	maxSkillNameLength  = 64
	maxSkillDescription = 1024
	maxSkillDepth       = 12
	maxSkillEntries     = 1000
)

// ManagedSkillService edits only Pi-owned global and project skill roots. Other
// harness directories, including ~/.agents/skills, remain read-only to Pi Desk.
type ManagedSkillService struct {
	agentDirectory       string
	agentDirectoryErr    error
	sharedSkillDirectory string
	workspaces           promptWorkspaceResolver

	mu sync.Mutex
}

func NewManagedSkillService(catalog *workspace.Catalog) *ManagedSkillService {
	directory, err := defaultPiAgentDirectory()
	sharedDirectory, sharedErr := defaultSharedSkillDirectory()
	if err == nil && sharedErr != nil {
		err = sharedErr
	}
	return &ManagedSkillService{agentDirectory: directory, agentDirectoryErr: err, sharedSkillDirectory: sharedDirectory, workspaces: catalog}
}

func newManagedSkillService(agentDirectory string, workspaces promptWorkspaceResolver) *ManagedSkillService {
	return &ManagedSkillService{agentDirectory: agentDirectory, sharedSkillDirectory: filepath.Join(filepath.Dir(filepath.Dir(agentDirectory)), ".agents", "skills"), workspaces: workspaces}
}

func (service *ManagedSkillService) ListManagedSkills(request domain.ListManagedSkillsRequest) (domain.ManagedSkillSnapshot, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	globalDirectories, err := service.globalDirectories()
	if err != nil {
		return domain.ManagedSkillSnapshot{}, err
	}
	snapshot := domain.ManagedSkillSnapshot{
		GlobalDirectory:   globalDirectories[0],
		GlobalDirectories: globalDirectories,
		Skills:            make([]domain.ManagedSkillSummary, 0),
	}
	for index, directory := range globalDirectories {
		skills, err := listManagedSkills(directory, domain.SkillScopeGlobal, index == 0)
		if err != nil {
			return domain.ManagedSkillSnapshot{}, err
		}
		snapshot.Skills = append(snapshot.Skills, skills...)
	}
	projectDirectory, notice, enabled := service.projectDirectory(request.WorkspacePath)
	snapshot.ProjectDirectory = projectDirectory
	snapshot.ProjectNotice = notice
	snapshot.ProjectEnabled = enabled
	if enabled {
		projectSkills, err := listManagedSkills(projectDirectory, domain.SkillScopeProject, false)
		if err != nil {
			return domain.ManagedSkillSnapshot{}, err
		}
		snapshot.Skills = append(snapshot.Skills, projectSkills...)
	}
	sortManagedSkills(snapshot.Skills)
	return snapshot, nil
}

func (service *ManagedSkillService) GetManagedSkill(request domain.ManagedSkillRequest) (domain.ManagedSkill, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	directory, err := service.directoryFor(request.Scope, request.WorkspacePath, request.RootDirectory)
	if err != nil {
		return domain.ManagedSkill{}, err
	}
	path, relativePath, err := managedSkillPath(directory, request.RelativePath)
	if err != nil {
		return domain.ManagedSkill{}, err
	}
	content, err := readManagedSkill(path)
	if err != nil {
		return domain.ManagedSkill{}, err
	}
	return makeManagedSkillSummary(directory, request.Scope, relativePath, content), nil
}

func (service *ManagedSkillService) CreateManagedSkill(request domain.CreateManagedSkillRequest) (domain.ManagedSkill, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	directory, err := service.directoryFor(request.Scope, request.WorkspacePath, "")
	if err != nil {
		return domain.ManagedSkill{}, err
	}
	name, err := validManagedSkillName(request.Name)
	if err != nil {
		return domain.ManagedSkill{}, err
	}
	description := strings.TrimSpace(request.Description)
	if description == "" || len(description) > maxSkillDescription || strings.ContainsAny(description, "\r\n") {
		return domain.ManagedSkill{}, fmt.Errorf("skill description must contain 1 to %d single-line characters", maxSkillDescription)
	}
	if err := os.MkdirAll(directory, skillDirectoryMode(request.Scope)); err != nil {
		return domain.ManagedSkill{}, fmt.Errorf("create skill directory: %w", err)
	}
	skillDirectory := filepath.Join(directory, name)
	if _, err := os.Stat(skillDirectory); err == nil {
		return domain.ManagedSkill{}, fmt.Errorf("skill %q already exists", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return domain.ManagedSkill{}, fmt.Errorf("check skill %q: %w", name, err)
	}
	if err := os.Mkdir(skillDirectory, skillDirectoryMode(request.Scope)); err != nil {
		return domain.ManagedSkill{}, fmt.Errorf("create skill: %w", err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n\n## Instructions\n\nDescribe when and how Pi should use this skill.\n"
	path := filepath.Join(skillDirectory, "SKILL.md")
	if err := atomic.WriteFile(path, strings.NewReader(content)); err != nil {
		_ = os.Remove(skillDirectory)
		return domain.ManagedSkill{}, fmt.Errorf("write skill: %w", err)
	}
	return makeManagedSkillSummary(directory, request.Scope, filepath.Join(name, "SKILL.md"), content), nil
}

func (service *ManagedSkillService) UpdateManagedSkill(request domain.UpdateManagedSkillRequest) (domain.ManagedSkill, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	directory, err := service.directoryFor(request.Scope, request.WorkspacePath, request.RootDirectory)
	if err != nil {
		return domain.ManagedSkill{}, err
	}
	path, relativePath, err := managedSkillPath(directory, request.RelativePath)
	if err != nil {
		return domain.ManagedSkill{}, err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.ManagedSkill{}, errors.New("skill was not found")
		}
		return domain.ManagedSkill{}, fmt.Errorf("check skill: %w", err)
	}
	if len(request.Content) > maxSkillBytes {
		return domain.ManagedSkill{}, fmt.Errorf("skill exceeds the %d MiB safety limit", maxSkillBytes>>20)
	}
	if !utf8.ValidString(request.Content) {
		return domain.ManagedSkill{}, errors.New("skill must be valid UTF-8")
	}
	if err := atomic.WriteFile(path, strings.NewReader(request.Content)); err != nil {
		return domain.ManagedSkill{}, fmt.Errorf("write skill: %w", err)
	}
	return makeManagedSkillSummary(directory, request.Scope, relativePath, request.Content), nil
}

func (service *ManagedSkillService) DeleteManagedSkill(request domain.ManagedSkillRequest) error {
	service.mu.Lock()
	defer service.mu.Unlock()

	directory, err := service.directoryFor(request.Scope, request.WorkspacePath, request.RootDirectory)
	if err != nil {
		return err
	}
	path, relativePath, err := managedSkillPath(directory, request.RelativePath)
	if err != nil {
		return err
	}
	target := path
	if filepath.Base(relativePath) == "SKILL.md" {
		target = filepath.Dir(path)
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("delete skill: %w", err)
	}
	return nil
}

func defaultSharedSkillDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate user home directory: %w", err)
	}
	return filepath.Join(home, ".agents", "skills"), nil
}

func samePath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if filepath.Separator == '\\' {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func (service *ManagedSkillService) globalDirectories() ([]string, error) {
	primary, err := service.globalDirectory()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(service.sharedSkillDirectory) == "" {
		return nil, errors.New("locate shared agent skill directory")
	}
	return []string{primary, filepath.Clean(service.sharedSkillDirectory)}, nil
}

func (service *ManagedSkillService) globalDirectory() (string, error) {
	if service.agentDirectoryErr != nil {
		return "", service.agentDirectoryErr
	}
	if strings.TrimSpace(service.agentDirectory) == "" {
		return "", errors.New("locate Pi agent directory")
	}
	return filepath.Join(filepath.Clean(service.agentDirectory), "skills"), nil
}

func (service *ManagedSkillService) directoryFor(scope domain.SkillScope, workspacePath, rootDirectory string) (string, error) {
	switch scope {
	case domain.SkillScopeGlobal:
		if strings.TrimSpace(rootDirectory) == "" {
			return service.globalDirectory()
		}
		requested := filepath.Clean(rootDirectory)
		directories, err := service.globalDirectories()
		if err != nil {
			return "", err
		}
		for _, directory := range directories {
			if samePath(directory, requested) {
				return directory, nil
			}
		}
		return "", errors.New("skill root must be a Pi global skill directory")
	case domain.SkillScopeProject:
		directory, notice, enabled := service.projectDirectory(workspacePath)
		if !enabled {
			if notice == "" {
				notice = "project skills are unavailable"
			}
			return "", errors.New(notice)
		}
		return directory, nil
	default:
		return "", errors.New("skill scope must be global or project")
	}
}

func (service *ManagedSkillService) projectDirectory(workspacePath string) (string, string, bool) {
	if strings.TrimSpace(workspacePath) == "" {
		return "", "select a workspace to manage project skills", false
	}
	if service.workspaces == nil {
		return "", "workspace catalog is unavailable", false
	}
	record, err := service.workspaces.ResolvePath(strings.TrimSpace(workspacePath))
	if err != nil {
		return "", "project skills require a registered workspace", false
	}
	directory := filepath.Join(filepath.Clean(record.Path), ".pi", "skills")
	if record.Trust != "approve" {
		return directory, "trust project resources before managing project skills", false
	}
	return directory, "", true
}

func skillDirectoryMode(scope domain.SkillScope) os.FileMode {
	if scope == domain.SkillScopeGlobal {
		return 0o700
	}
	return 0o755
}

func listManagedSkills(directory string, scope domain.SkillScope, includeRootMarkdown bool) ([]domain.ManagedSkillSummary, error) {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []domain.ManagedSkillSummary{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read skill directory: %w", err)
	}
	result := make([]domain.ManagedSkillSummary, 0)
	visited := 0
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		if entry.IsDir() {
			if err := collectManagedSkills(directory, scope, path, 1, &visited, &result); err != nil {
				return nil, err
			}
			continue
		}
		if !includeRootMarkdown || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		content, err := readManagedSkill(path)
		if err != nil {
			return nil, err
		}
		relativePath, err := filepath.Rel(directory, path)
		if err != nil {
			return nil, err
		}
		result = append(result, makeManagedSkillSummary(directory, scope, relativePath, content).ManagedSkillSummary)
	}
	sortManagedSkills(result)
	return result, nil
}

func collectManagedSkills(root string, scope domain.SkillScope, directory string, depth int, visited *int, result *[]domain.ManagedSkillSummary) error {
	if depth > maxSkillDepth {
		return nil
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read skill directory: %w", err)
	}
	*visited += len(entries)
	if *visited > maxSkillEntries {
		return errors.New("skill directory exceeds the 1000 entry safety limit")
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || entry.Name() != "SKILL.md" || entry.IsDir() {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		content, err := readManagedSkill(path)
		if err != nil {
			return err
		}
		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		*result = append(*result, makeManagedSkillSummary(root, scope, relativePath, content).ManagedSkillSummary)
		return nil
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		if err := collectManagedSkills(root, scope, filepath.Join(directory, entry.Name()), depth+1, visited, result); err != nil {
			return err
		}
	}
	return nil
}

func managedSkillPath(directory, rawRelativePath string) (string, string, error) {
	relativePath := filepath.Clean(strings.TrimSpace(rawRelativePath))
	if relativePath == "." || filepath.IsAbs(relativePath) || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) || relativePath == ".." {
		return "", "", errors.New("skill path must remain inside its Pi skill directory")
	}
	base := filepath.Base(relativePath)
	if base != "SKILL.md" && (filepath.Dir(relativePath) != "." || !strings.HasSuffix(strings.ToLower(base), ".md")) {
		return "", "", errors.New("skill path must reference SKILL.md or a root Markdown skill")
	}
	path := filepath.Join(directory, relativePath)
	rel, err := filepath.Rel(directory, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", errors.New("skill path must remain inside its Pi skill directory")
	}
	return path, relativePath, nil
}

func validManagedSkillName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if len(name) == 0 || len(name) > maxSkillNameLength {
		return "", fmt.Errorf("skill name must contain 1 to %d characters", maxSkillNameLength)
	}
	for index, character := range name {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			continue
		}
		if character == '-' && index > 0 && index < len(name)-1 && name[index-1] != '-' && name[index+1] != '-' {
			continue
		}
		return "", errors.New("skill name must use lowercase letters, numbers, and single hyphens")
	}
	return name, nil
}

func readManagedSkill(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.New("skill was not found")
		}
		return "", fmt.Errorf("read skill: %w", err)
	}
	if len(content) > maxSkillBytes {
		return "", fmt.Errorf("skill exceeds the %d MiB safety limit", maxSkillBytes>>20)
	}
	if !utf8.Valid(content) {
		return "", errors.New("skill must be valid UTF-8")
	}
	return string(content), nil
}

func makeManagedSkillSummary(root string, scope domain.SkillScope, relativePath, content string) domain.ManagedSkill {
	metadata := parseSkillMetadata(content)
	name := metadata["name"]
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(relativePath), filepath.Ext(relativePath))
		if filepath.Base(relativePath) == "SKILL.md" {
			name = filepath.Base(filepath.Dir(relativePath))
		}
	}
	description := metadata["description"]
	warnings := validateManagedSkillMetadata(metadata)
	path := filepath.Join(root, relativePath)
	kind := "markdown"
	directory := filepath.Dir(path)
	if filepath.Base(relativePath) == "SKILL.md" {
		kind = "directory"
	}
	return domain.ManagedSkill{ManagedSkillSummary: domain.ManagedSkillSummary{
		Scope: scope, Name: name, Description: description, RootDirectory: root, RelativePath: relativePath, Path: path, Directory: directory,
		Kind: kind, Enabled: metadata["disable-model-invocation"] != "true", Warnings: warnings,
	}, Content: content}
}

func parseSkillMetadata(content string) map[string]string {
	result := map[string]string{}
	lines := strings.Split(strings.ReplaceAll(strings.TrimPrefix(content, "\ufeff"), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return result
	}
	for index := 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "---" {
			break
		}
		key, value, found := strings.Cut(lines[index], ":")
		if !found {
			continue
		}
		result[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), "\"'")
	}
	return result
}

func validateManagedSkillMetadata(metadata map[string]string) []string {
	warnings := make([]string, 0, 3)
	name := metadata["name"]
	if _, err := validManagedSkillName(name); err != nil {
		warnings = append(warnings, "Name must use lowercase letters, numbers, and single hyphens.")
	}
	description := metadata["description"]
	if description == "" {
		warnings = append(warnings, "Description is required before Pi can load this skill.")
	} else if len(description) > maxSkillDescription {
		warnings = append(warnings, "Description exceeds Pi's 1024 character limit.")
	}
	return warnings
}

func sortManagedSkills(skills []domain.ManagedSkillSummary) {
	sort.Slice(skills, func(left, right int) bool {
		if skills[left].Scope != skills[right].Scope {
			return skills[left].Scope < skills[right].Scope
		}
		return strings.ToLower(skills[left].Name) < strings.ToLower(skills[right].Name)
	})
}
