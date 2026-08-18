package appservice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"pi-desk/internal/domain"
	"pi-desk/internal/sessionindex"
	"pi-desk/internal/workspace"
	"pi-desk/internal/workspaceapp"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const sessionListTimeout = 20 * time.Second

type sessionLister interface {
	List(context.Context, string) ([]sessionindex.Summary, error)
	Usage(context.Context, string) (sessionindex.UsageSummary, error)
	Resolve(string) (sessionindex.Summary, error)
	Header(string) (sessionindex.Summary, error)
	SnapshotPage(string, string) (sessionindex.Snapshot, error)
}

type folderPicker func(initialPath string) (string, error)
type sessionTrasher func(path string) (string, error)
type workspaceOpener func(path string) error

type workspaceApplicationManager interface {
	List() []workspaceapp.Application
	Open(applicationID, workspacePath string) error
}

type CatalogService struct {
	catalog               *workspace.Catalog
	index                 sessionLister
	picker                folderPicker
	trash                 sessionTrasher
	openWorkspace         workspaceOpener
	workspaceApplications workspaceApplicationManager
}

func NewCatalogService(catalog *workspace.Catalog, index *sessionindex.Index) *CatalogService {
	return &CatalogService{
		catalog: catalog, index: index, picker: pickWorkspaceFolder, trash: trashSessionFile,
		workspaceApplications: workspaceapp.NewManager(),
		openWorkspace: func(path string) error {
			app := application.Get()
			if app == nil || app.Env == nil {
				return errors.New("desktop file manager is unavailable")
			}
			return app.Env.OpenFileManager(path, false)
		},
	}
}

func newCatalogService(catalog *workspace.Catalog, index sessionLister, picker folderPicker) *CatalogService {
	return &CatalogService{catalog: catalog, index: index, picker: picker, trash: trashSessionFile}
}

func (service *CatalogService) PickWorkspace(request domain.PickWorkspaceRequest) (string, error) {
	if service.picker == nil {
		return "", errors.New("folder picker is unavailable")
	}
	return service.picker(strings.TrimSpace(request.InitialPath))
}

func (service *CatalogService) ListWorkspaces() ([]domain.WorkspaceSummary, error) {
	records, err := service.catalog.List()
	if err != nil {
		return nil, err
	}
	result := make([]domain.WorkspaceSummary, 0, len(records))
	for _, record := range records {
		result = append(result, workspaceSummary(record))
	}
	return result, nil
}

func (service *CatalogService) AddWorkspace(request domain.AddWorkspaceRequest) (domain.WorkspaceSummary, error) {
	record, err := service.catalog.Add(strings.TrimSpace(request.Path), strings.TrimSpace(request.Trust))
	if err != nil {
		return domain.WorkspaceSummary{}, err
	}
	return workspaceSummary(record), nil
}

func (service *CatalogService) RemoveWorkspace(request domain.WorkspaceRequest) error {
	return service.catalog.Remove(strings.TrimSpace(request.ID))
}

func (service *CatalogService) OpenWorkspace(request domain.WorkspaceRequest) error {
	if service.openWorkspace == nil {
		return errors.New("desktop file manager is unavailable")
	}
	record, err := service.approvedWorkspace(request.ID)
	if err != nil {
		return err
	}
	return service.openWorkspace(record.Path)
}

func (service *CatalogService) ListWorkspaceApplications() []domain.WorkspaceApplication {
	if service.workspaceApplications == nil {
		return nil
	}
	applications := service.workspaceApplications.List()
	result := make([]domain.WorkspaceApplication, 0, len(applications))
	for _, application := range applications {
		result = append(result, domain.WorkspaceApplication{ID: application.ID, Name: application.Name, IconDataURL: application.IconDataURL})
	}
	return result
}

func (service *CatalogService) OpenWorkspaceWith(request domain.OpenWorkspaceWithRequest) error {
	if service.workspaceApplications == nil {
		return errors.New("workspace application manager is unavailable")
	}
	record, err := service.approvedWorkspace(request.WorkspaceID)
	if err != nil {
		return err
	}
	return service.workspaceApplications.Open(strings.TrimSpace(request.ApplicationID), record.Path)
}

func (service *CatalogService) approvedWorkspace(id string) (workspace.Record, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return workspace.Record{}, errors.New("workspace id is required")
	}
	records, err := service.catalog.List()
	if err != nil {
		return workspace.Record{}, err
	}
	for _, record := range records {
		if record.ID != id {
			continue
		}
		if record.Trust != "approve" {
			return workspace.Record{}, errors.New("workspace must be trusted before opening it")
		}
		canonicalPath, err := workspace.CanonicalDirectory(record.Path)
		if err != nil {
			return workspace.Record{}, err
		}
		if sessionPathKey(canonicalPath) != sessionPathKey(record.Path) {
			return workspace.Record{}, errors.New("registered workspace path now resolves outside its original boundary")
		}
		record.Path = canonicalPath
		return record, nil
	}
	return workspace.Record{}, errors.New("workspace not found")
}

func (service *CatalogService) ListSessions(request domain.ListSessionsRequest) ([]domain.SessionSummary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sessionListTimeout)
	defer cancel()
	summaries, err := service.index.List(ctx, strings.TrimSpace(request.WorkspacePath))
	if err != nil {
		return nil, err
	}
	result := make([]domain.SessionSummary, 0, len(summaries))
	for _, summary := range summaries {
		result = append(result, domain.SessionSummary{
			ID:                summary.ID,
			Path:              summary.Path,
			CWD:               summary.CWD,
			Name:              summary.Name,
			Title:             summary.Title,
			FirstMessage:      summary.FirstMessage,
			CreatedAt:         summary.CreatedAt,
			ModifiedAt:        summary.ModifiedAt,
			MessageCount:      summary.MessageCount,
			ParentSessionPath: summary.ParentSessionPath,
		})
	}
	return result, nil
}

func (service *CatalogService) GetSessionSnapshot(request domain.SessionSnapshotRequest) (domain.SessionSnapshot, error) {
	snapshot, err := service.index.SnapshotPage(strings.TrimSpace(request.Path), strings.TrimSpace(request.Before))
	if err != nil {
		return domain.SessionSnapshot{}, err
	}
	result := domain.SessionSnapshot{
		Messages:     snapshot.Messages,
		Before:       snapshot.Before,
		HasMore:      snapshot.HasMore,
		MessageCount: snapshot.MessageCount,
	}
	if snapshot.Model != nil {
		result.Model = &domain.SessionModel{Provider: snapshot.Model.Provider, ID: snapshot.Model.ID}
	}
	return result, nil
}

func (service *CatalogService) GetSessionUsage(request domain.ListSessionsRequest) (domain.SessionUsageSummary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sessionListTimeout)
	defer cancel()
	usage, err := service.index.Usage(ctx, strings.TrimSpace(request.WorkspacePath))
	if err != nil {
		return domain.SessionUsageSummary{}, err
	}
	result := domain.SessionUsageSummary{
		Sessions: usage.Sessions, Messages: usage.Messages, UserMessages: usage.UserMessages,
		AssistantMessages: usage.AssistantMessages, ToolResults: usage.ToolResults,
		Tokens: sessionTokenUsage(usage.Tokens), Cost: usage.Cost,
		Models: make([]domain.SessionModelUsage, 0, len(usage.Models)),
	}
	for _, model := range usage.Models {
		result.Models = append(result.Models, domain.SessionModelUsage{
			Provider: model.Provider, Model: model.Model, AssistantMessages: model.AssistantMessages,
			Tokens: sessionTokenUsage(model.Tokens), Cost: model.Cost,
		})
	}
	return result, nil
}

func (service *CatalogService) DeleteSession(request domain.DeleteSessionRequest) (domain.DeletedSession, error) {
	summary, err := service.index.Resolve(strings.TrimSpace(request.Path))
	if err != nil {
		return domain.DeletedSession{}, err
	}
	if service.trash == nil {
		return domain.DeletedSession{}, errors.New("session trash is unavailable")
	}
	recoveryPath, err := service.trash(summary.Path)
	if err != nil {
		return domain.DeletedSession{}, err
	}
	if err := service.catalog.ForgetSession(summary.Path); err != nil {
		if rollbackErr := os.Rename(recoveryPath, summary.Path); rollbackErr != nil {
			return domain.DeletedSession{}, fmt.Errorf("update catalog after moving session: %w; rollback failed: %v", err, rollbackErr)
		}
		return domain.DeletedSession{}, err
	}
	return domain.DeletedSession{RecoveryPath: recoveryPath}, nil
}

func (service *CatalogService) GetDesktopState() (domain.DesktopState, error) {
	record, err := service.catalog.Desktop()
	if err != nil {
		return domain.DesktopState{}, err
	}
	result := domain.DesktopState{ActiveThreadID: record.ActiveThreadID, Threads: make([]domain.DesktopThreadState, 0, len(record.Threads))}
	if record.Preferences != nil {
		result.Preferences = &domain.DesktopPreferences{
			Appearance: record.Preferences.Appearance, Language: record.Preferences.Language, OfflineMode: record.Preferences.OfflineMode, ProxyEnabled: record.Preferences.ProxyEnabled,
			ProxyURL: record.Preferences.ProxyURL, StreamingBehavior: record.Preferences.StreamingBehavior,
			SidebarCollapsed: record.Preferences.SidebarCollapsed, SidebarWidth: record.Preferences.SidebarWidth,
			InspectorOpen: record.Preferences.InspectorOpen, InspectorWidth: record.Preferences.InspectorWidth,
			InspectorTab:         record.Preferences.InspectorTab,
			NotificationsEnabled: record.Preferences.NotificationsEnabled, UpdateChecksEnabled: record.Preferences.UpdateChecksEnabled,
			CloseToTray: record.Preferences.CloseToTray, WorkspaceApplication: record.Preferences.WorkspaceApplication,
		}
	}
	for _, thread := range record.Threads {
		result.Threads = append(result.Threads, domain.DesktopThreadState{
			ID: thread.ID, Title: thread.Title, WorkspacePath: thread.WorkspacePath, Trust: thread.Trust,
			Status: thread.Status, SessionPath: thread.SessionPath, Draft: thread.Draft,
			CreatedAt: thread.CreatedAt, UpdatedAt: thread.UpdatedAt, Unread: thread.Unread,
		})
	}
	return result, nil
}

func (service *CatalogService) SaveDesktopState(state domain.DesktopState) error {
	record := workspace.DesktopRecord{ActiveThreadID: strings.TrimSpace(state.ActiveThreadID), Threads: make([]workspace.ThreadRecord, 0, len(state.Threads))}
	if state.Preferences != nil {
		record.Preferences = &workspace.PreferencesRecord{
			Appearance: strings.TrimSpace(state.Preferences.Appearance), Language: strings.TrimSpace(state.Preferences.Language), OfflineMode: state.Preferences.OfflineMode, ProxyEnabled: state.Preferences.ProxyEnabled,
			ProxyURL: strings.TrimSpace(state.Preferences.ProxyURL), StreamingBehavior: strings.TrimSpace(state.Preferences.StreamingBehavior),
			SidebarCollapsed: state.Preferences.SidebarCollapsed, SidebarWidth: state.Preferences.SidebarWidth,
			InspectorOpen: state.Preferences.InspectorOpen, InspectorWidth: state.Preferences.InspectorWidth,
			InspectorTab:         strings.TrimSpace(state.Preferences.InspectorTab),
			NotificationsEnabled: state.Preferences.NotificationsEnabled, UpdateChecksEnabled: state.Preferences.UpdateChecksEnabled,
			CloseToTray: state.Preferences.CloseToTray, WorkspaceApplication: strings.TrimSpace(state.Preferences.WorkspaceApplication),
		}
	}
	for _, thread := range state.Threads {
		sessionPath := strings.TrimSpace(thread.SessionPath)
		if sessionPath != "" {
			summary, err := service.index.Header(sessionPath)
			if err != nil {
				return err
			}
			workspacePath, err := workspace.CanonicalDirectory(thread.WorkspacePath)
			if err != nil {
				return err
			}
			if sessionPathKey(summary.CWD) != sessionPathKey(workspacePath) {
				return errors.New("session working directory does not match the thread workspace")
			}
			sessionPath = summary.Path
		}
		mapped := workspace.ThreadRecord{
			ID: strings.TrimSpace(thread.ID), Title: strings.TrimSpace(thread.Title), WorkspacePath: strings.TrimSpace(thread.WorkspacePath),
			Trust: strings.TrimSpace(thread.Trust), Status: strings.TrimSpace(thread.Status), SessionPath: sessionPath,
			Draft: thread.Draft, CreatedAt: strings.TrimSpace(thread.CreatedAt), UpdatedAt: strings.TrimSpace(thread.UpdatedAt), Unread: thread.Unread,
		}
		record.Threads = append(record.Threads, mapped)
	}
	return service.catalog.SaveDesktop(record)
}

func workspaceSummary(record workspace.Record) domain.WorkspaceSummary {
	return domain.WorkspaceSummary{
		ID:           record.ID,
		Name:         record.Name,
		Path:         record.Path,
		Trust:        record.Trust,
		AddedAt:      record.AddedAt,
		LastOpenedAt: record.LastOpenedAt,
	}
}

func pickWorkspaceFolder(initialPath string) (string, error) {
	dialog := application.Get().Dialog.OpenFile().
		CanChooseDirectories(true).
		CanChooseFiles(false).
		CanCreateDirectories(true).
		SetTitle("Choose workspace folder").
		SetButtonText("Choose folder")
	if initialPath != "" {
		if path, err := workspace.CanonicalDirectory(initialPath); err == nil {
			dialog.SetDirectory(path)
		}
	}
	return dialog.PromptForSingleSelection()
}

func sessionPathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func sessionTokenUsage(usage sessionindex.TokenUsage) domain.SessionTokenUsage {
	return domain.SessionTokenUsage{
		Input: usage.Input, Output: usage.Output, CacheRead: usage.CacheRead,
		CacheWrite: usage.CacheWrite, Reasoning: usage.Reasoning, Total: usage.Total,
	}
}

func trashSessionFile(path string) (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("create session recovery name: %w", err)
	}
	recoveryPath := path + ".deleted-" + hex.EncodeToString(random)
	if err := os.Rename(path, recoveryPath); err != nil {
		return "", fmt.Errorf("move session to recovery file: %w", err)
	}
	return recoveryPath, nil
}
