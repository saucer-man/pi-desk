package appservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pi-desk/internal/domain"
	"pi-desk/internal/piruntime"
	"pi-desk/internal/sessionindex"
	"pi-desk/internal/workspace"

	githubatomic "github.com/natefinch/atomic"
)

const (
	orphanExportTimeout  = 2 * time.Minute
	maxOrphanExportBytes = 128 << 20
)

type orphanSessionExporter func(context.Context, string, string) error

// OrphanSessionService is the local-only read/delete/export path for SSH-anchor transcripts and owns no
// SSH connection, workspace capability, Repository, Terminal, or Pi session.
type OrphanSessionService struct {
	catalog  *workspace.Catalog
	index    *sessionindex.Index
	trash    sessionTrasher
	exporter orphanSessionExporter
}

func NewOrphanSessionService(catalog *workspace.Catalog, index *sessionindex.Index, locator *piruntime.Locator) (*OrphanSessionService, error) {
	if catalog == nil || index == nil || locator == nil {
		return nil, errors.New("orphan session service dependencies are required")
	}
	return &OrphanSessionService{catalog: catalog, index: index, trash: trashSessionFile, exporter: piOrphanExporter(locator, index)}, nil
}

func newOrphanSessionService(catalog *workspace.Catalog, index *sessionindex.Index, trash sessionTrasher, exporter orphanSessionExporter) *OrphanSessionService {
	return &OrphanSessionService{catalog: catalog, index: index, trash: trash, exporter: exporter}
}

func (service *OrphanSessionService) ListOrphanSessions() ([]domain.OrphanSessionSummary, error) {
	known, err := service.knownWorkspaceIDs()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sessionListTimeout)
	defer cancel()
	summaries, err := service.index.ListOrphanSSH(ctx, known)
	if err != nil {
		return nil, err
	}
	result := make([]domain.OrphanSessionSummary, 0, len(summaries))
	for _, summary := range summaries {
		result = append(result, orphanSessionSummary(summary))
	}
	return result, nil
}

func (service *OrphanSessionService) GetOrphanSessionSnapshot(request domain.SessionSnapshotRequest) (domain.SessionSnapshot, error) {
	if _, err := service.resolveOrphan(request.Path); err != nil {
		return domain.SessionSnapshot{}, err
	}
	snapshot, err := service.index.Snapshot(strings.TrimSpace(request.Path))
	if err != nil {
		return domain.SessionSnapshot{}, err
	}
	result := domain.SessionSnapshot{Messages: snapshot.Messages, MessageCount: snapshot.MessageCount}
	if snapshot.Model != nil {
		result.Model = &domain.SessionModel{Provider: snapshot.Model.Provider, ID: snapshot.Model.ID}
	}
	return result, nil
}

func (service *OrphanSessionService) RestoreOrphanSession(request domain.RestoreOrphanSessionRequest) error {
	summary, err := service.resolveOrphan(request.Path)
	if err != nil {
		return err
	}
	record, err := service.catalog.ResolveID(strings.TrimSpace(request.WorkspaceID))
	if err != nil {
		return err
	}
	if record.Location.Kind != workspace.KindSSH || record.Trust != "approve" {
		return errors.New("restore requires an approved SSH workspace")
	}
	ssh := record.Location.SSH
	if summary.AnchorTargetID == "" || summary.AnchorRemoteRoot == "" || ssh.TargetID != summary.AnchorTargetID || ssh.CanonicalRoot != summary.AnchorRemoteRoot {
		return errors.New("SSH target or remote root does not match the orphan session")
	}
	return service.index.RebindSSHAnchor(summary.Path, record.ID, ssh.TargetID, ssh.CanonicalRoot)
}

func (service *OrphanSessionService) DeleteOrphanSession(request domain.DeleteSessionRequest) (domain.DeletedSession, error) {
	summary, err := service.resolveOrphan(request.Path)
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
			return domain.DeletedSession{}, fmt.Errorf("update catalog after moving orphan session: %w; rollback failed: %v", err, rollbackErr)
		}
		return domain.DeletedSession{}, err
	}
	return domain.DeletedSession{RecoveryPath: recoveryPath}, nil
}

func (service *OrphanSessionService) ExportOrphanSession(request domain.ExportOrphanSessionRequest) error {
	summary, err := service.resolveOrphan(request.Path)
	if err != nil {
		return err
	}
	outputPath, err := validateOrphanExportPath(request.OutputPath)
	if err != nil {
		return err
	}
	if service.exporter == nil {
		return errors.New("orphan session export is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), orphanExportTimeout)
	defer cancel()
	return service.exporter(ctx, summary.Path, outputPath)
}

func (service *OrphanSessionService) resolveOrphan(path string) (sessionindex.Summary, error) {
	summary, err := service.index.Resolve(strings.TrimSpace(path))
	if err != nil {
		return sessionindex.Summary{}, err
	}
	if !summary.SSHAnchor || summary.AnchorWorkspaceID == "" {
		return sessionindex.Summary{}, errors.New("session is not an SSH orphan transcript")
	}
	known, err := service.knownWorkspaceIDs()
	if err != nil {
		return sessionindex.Summary{}, err
	}
	if _, exists := known[summary.AnchorWorkspaceID]; exists {
		return sessionindex.Summary{}, errors.New("session is no longer an orphan transcript")
	}
	return summary, nil
}

func (service *OrphanSessionService) knownWorkspaceIDs() (map[string]struct{}, error) {
	records, err := service.catalog.List()
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{}, len(records))
	for _, record := range records {
		known[record.ID] = struct{}{}
	}
	return known, nil
}

func orphanSessionSummary(summary sessionindex.Summary) domain.OrphanSessionSummary {
	return domain.OrphanSessionSummary{
		ID: summary.ID, Path: summary.Path, AnchorWorkspaceID: summary.AnchorWorkspaceID,
		TargetID: summary.AnchorTargetID, RemoteRoot: summary.AnchorRemoteRoot,
		Name: summary.Name, Title: summary.Title, FirstMessage: summary.FirstMessage,
		CreatedAt: summary.CreatedAt, ModifiedAt: summary.ModifiedAt, MessageCount: summary.MessageCount,
	}
}

func validateOrphanExportPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxOutputPathBytes || !filepath.IsAbs(value) {
		return "", errors.New("orphan export path must be an absolute local path")
	}
	if !strings.EqualFold(filepath.Ext(value), ".html") {
		return "", errors.New("orphan export path must use the .html extension")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(value))
	if err != nil {
		return "", fmt.Errorf("resolve orphan export directory: %w", err)
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return "", errors.New("orphan export directory is unavailable")
	}
	return filepath.Join(parent, filepath.Base(value)), nil
}

func piOrphanExporter(locator *piruntime.Locator, index *sessionindex.Index) orphanSessionExporter {
	return func(ctx context.Context, inputPath, outputPath string) error {
		transcript, err := os.CreateTemp("", "pi-desk-orphan-transcript-*.jsonl")
		if err != nil {
			return fmt.Errorf("create orphan transcript staging file: %w", err)
		}
		transcriptPath := transcript.Name()
		defer os.Remove(transcriptPath)
		if err := index.CopyValidated(inputPath, transcript); err != nil {
			_ = transcript.Close()
			return err
		}
		if err := transcript.Sync(); err != nil {
			_ = transcript.Close()
			return err
		}
		if err := transcript.Close(); err != nil {
			return err
		}

		temporary, err := os.CreateTemp(filepath.Dir(outputPath), ".pi-desk-orphan-export-*.html")
		if err != nil {
			return fmt.Errorf("create orphan export staging file: %w", err)
		}
		temporaryPath := temporary.Name()
		if err := temporary.Close(); err != nil {
			_ = os.Remove(temporaryPath)
			return err
		}
		_ = os.Remove(temporaryPath)
		defer os.Remove(temporaryPath)

		invocation, err := locator.Invocation("--offline", "--no-extensions", "--no-skills", "--no-prompt-templates", "--no-context-files", "--export", transcriptPath, temporaryPath)
		if err != nil {
			return errors.New("Pi CLI is unavailable for orphan export")
		}
		if _, err := locator.Run(ctx, invocation); err != nil {
			return errors.New("Pi could not export the orphan transcript")
		}
		info, err := os.Lstat(temporaryPath)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxOrphanExportBytes {
			return errors.New("Pi produced an invalid orphan transcript export")
		}
		source, err := os.Open(temporaryPath)
		if err != nil {
			return err
		}
		defer source.Close()
		if err := githubatomic.WriteFile(outputPath, source); err != nil {
			return fmt.Errorf("write orphan transcript export: %w", err)
		}
		return nil
	}
}
