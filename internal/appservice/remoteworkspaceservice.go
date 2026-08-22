package appservice

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"pi-desk/internal/domain"
	"pi-desk/internal/piruntime"
	"pi-desk/internal/remotessh"
	"pi-desk/internal/workspace"
)

const (
	remoteConnectTimeout = 45 * time.Second
	remoteSetupTimeout   = 150 * time.Second
)

// RemoteWorkspaceService is the Wails-safe facade for explicit SSH connect and
// root trust. It accepts no executable path, SSH option, command, or platform.
type RemoteWorkspaceService struct {
	catalog   *workspace.Catalog
	lifecycle *RemoteWorkspaceLifecycle
	pi        *piruntime.Locator

	mu             sync.Mutex
	globalEpoch    uint64
	targetEpoch    map[string]uint64
	pendingTargets map[string]string
}

func NewRemoteWorkspaceService(catalog *workspace.Catalog, lifecycle *RemoteWorkspaceLifecycle, pi *piruntime.Locator) (*RemoteWorkspaceService, error) {
	if catalog == nil || lifecycle == nil || pi == nil {
		return nil, errors.New("remote workspace service dependencies are required")
	}
	return &RemoteWorkspaceService{
		catalog: catalog, lifecycle: lifecycle, pi: pi,
		targetEpoch: make(map[string]uint64), pendingTargets: make(map[string]string),
	}, nil
}

func (service *RemoteWorkspaceService) DiscoverRemoteTargets() ([]domain.RemoteAliasSummary, error) {
	aliases, err := remotessh.DiscoverSSHConfig(remotessh.DiscoveryOptions{})
	if err != nil {
		return nil, errors.New("SSH config discovery failed")
	}
	result := make([]domain.RemoteAliasSummary, 0, len(aliases))
	for _, alias := range aliases {
		risk := alias.Risk
		result = append(result, domain.RemoteAliasSummary{
			Name:  alias.Name,
			Risky: risk.HasMatchExec || risk.HasProxyCommand || risk.HasProxyJump || risk.HasLocalCommand || risk.HasRemoteCommand || risk.HasSetEnv,
		})
	}
	return result, nil
}

func (service *RemoteWorkspaceService) ListRemoteTargets() ([]domain.RemoteTargetSummary, error) {
	targets, err := service.catalog.ListTargets()
	if err != nil {
		return nil, err
	}
	result := make([]domain.RemoteTargetSummary, 0, len(targets))
	for _, target := range targets {
		result = append(result, domain.RemoteTargetSummary{
			ID: target.ID, Name: target.Name, HostAlias: target.HostAlias,
		})
	}
	return result, nil
}

func (service *RemoteWorkspaceService) ConnectRemoteTarget(request domain.ConnectRemoteTargetRequest) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), remoteConnectTimeout)
	defer cancel()
	targetID := strings.TrimSpace(request.TargetID)
	if targetID != "" {
		if strings.TrimSpace(request.Name) != "" || strings.TrimSpace(request.HostAlias) != "" {
			return "", errors.New("existing SSH target cannot include a name or host alias")
		}
		epoch := service.beginTargetOperation(targetID)
		if err := service.lifecycle.ConnectTarget(ctx, targetID); err != nil {
			return "", err
		}
		if err := service.finishTargetOperation(ctx, targetID, epoch); err != nil {
			return "", err
		}
		return targetID, nil
	}

	epoch := service.beginNewTargetOperation()
	targetID, err := service.lifecycle.ConnectNewTarget(ctx, request.Name, request.HostAlias)
	if err != nil {
		return "", err
	}
	if err := service.finishNewTargetOperation(ctx, targetID, epoch); err != nil {
		return "", err
	}
	return targetID, nil
}

func (service *RemoteWorkspaceService) PrepareRemoteRoot(request domain.PrepareRemoteRootRequest) (domain.RemoteRootCandidate, error) {
	targetID := strings.TrimSpace(request.TargetID)
	epoch := service.beginTargetOperation(targetID)
	version, err := service.piVersion()
	if err != nil {
		return domain.RemoteRootCandidate{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteSetupTimeout)
	defer cancel()
	candidate, err := service.lifecycle.PrepareRootTrust(ctx, targetID, request.Name, request.RequestedRoot, version)
	if err != nil {
		return domain.RemoteRootCandidate{}, err
	}
	service.trackPendingTarget(candidate.Token, candidate.TargetID)
	if err := service.finishTargetOperation(ctx, targetID, epoch); err != nil {
		return domain.RemoteRootCandidate{}, err
	}
	target, err := service.catalog.ResolveTarget(candidate.TargetID)
	if err != nil {
		service.takePendingTarget(candidate.Token)
		return domain.RemoteRootCandidate{}, err
	}
	return domain.RemoteRootCandidate{
		Token: candidate.Token, TargetID: candidate.TargetID,
		HostAlias: target.HostAlias, HostKeyAlgorithm: target.HostKey.Algorithm, HostKeySHA256: target.HostKey.SHA256,
		CanonicalRoot: candidate.Root.CanonicalPath,
		Device:        strconv.FormatUint(candidate.Root.Device, 10), Inode: strconv.FormatUint(candidate.Root.Inode, 10),
	}, nil
}

func (service *RemoteWorkspaceService) DecideRemoteRoot(request domain.DecideRemoteRootRequest) (domain.WorkspaceSummary, error) {
	token, trust := strings.TrimSpace(request.Token), strings.TrimSpace(request.Trust)
	if !validRemoteTrustToken(token) || trust != "approve" && trust != "deny" {
		return domain.WorkspaceSummary{}, errors.New("remote root trust decision is invalid")
	}
	targetID := service.takePendingTarget(token)
	var epoch uint64
	if trust == "approve" {
		epoch = service.beginTargetOperation(targetID)
	} else if targetID != "" {
		service.revokeTargetOperations(targetID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteSetupTimeout)
	defer cancel()
	record, err := service.lifecycle.DecideRootTrust(ctx, token, trust)
	if err != nil {
		return domain.WorkspaceSummary{}, err
	}
	if trust == "approve" && targetID != "" {
		if err := service.finishTargetOperation(ctx, targetID, epoch); err != nil {
			return domain.WorkspaceSummary{}, err
		}
	}
	return workspaceSummary(record), nil
}

func (service *RemoteWorkspaceService) ResumeRemoteWorkspace(request domain.ResumeRemoteWorkspaceRequest) (domain.WorkspaceSummary, error) {
	workspaceID := strings.TrimSpace(request.WorkspaceID)
	record, err := service.catalog.ResolveID(workspaceID)
	if err != nil {
		return domain.WorkspaceSummary{}, err
	}
	if record.Trust != "approve" || record.Location.Kind != workspace.KindSSH {
		return domain.WorkspaceSummary{}, errors.New("remote workspace requires trust approval")
	}
	targetID := record.Location.SSH.TargetID
	epoch := service.beginTargetOperation(targetID)
	version, err := service.piVersion()
	if err != nil {
		return domain.WorkspaceSummary{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteSetupTimeout)
	defer cancel()
	wasReady := service.lifecycle.TargetState(targetID) == remotessh.ConnectionReady
	if err := service.lifecycle.ConnectTarget(ctx, targetID); err != nil {
		return domain.WorkspaceSummary{}, err
	}
	if err := service.finishTargetOperation(ctx, targetID, epoch); err != nil {
		return domain.WorkspaceSummary{}, err
	}
	if err := service.lifecycle.OpenWorkspace(ctx, workspaceID, version); err != nil {
		if !wasReady {
			_ = service.lifecycle.DisconnectTarget(ctx, targetID)
		}
		return domain.WorkspaceSummary{}, err
	}
	if err := service.finishTargetOperation(ctx, targetID, epoch); err != nil {
		return domain.WorkspaceSummary{}, err
	}
	return workspaceSummary(record), nil
}

func (service *RemoteWorkspaceService) OpenRemoteWorkspace(request domain.WorkspaceRequest) (domain.WorkspaceSummary, error) {
	workspaceID := strings.TrimSpace(request.ID)
	record, err := service.catalog.ResolveID(workspaceID)
	if err != nil || record.Location.Kind != workspace.KindSSH {
		return domain.WorkspaceSummary{}, errors.New("remote workspace is unavailable")
	}
	epoch := service.beginTargetOperation(record.Location.SSH.TargetID)
	version, err := service.piVersion()
	if err != nil {
		return domain.WorkspaceSummary{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteSetupTimeout)
	defer cancel()
	if err := service.lifecycle.OpenWorkspace(ctx, workspaceID, version); err != nil {
		return domain.WorkspaceSummary{}, err
	}
	if err := service.finishTargetOperation(ctx, record.Location.SSH.TargetID, epoch); err != nil {
		return domain.WorkspaceSummary{}, err
	}
	record, err = service.catalog.ResolveID(workspaceID)
	if err != nil {
		return domain.WorkspaceSummary{}, err
	}
	return workspaceSummary(record), nil
}

func (service *RemoteWorkspaceService) RemoveRemoteTarget(request domain.RemoteTargetRequest) error {
	targetID := strings.TrimSpace(request.TargetID)
	if _, err := service.catalog.ResolveTarget(targetID); err != nil {
		return err
	}
	service.revokeTargetOperations(targetID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return service.lifecycle.RemoveTarget(ctx, targetID)
}

func (service *RemoteWorkspaceService) DisconnectRemoteTarget(request domain.RemoteTargetRequest) error {
	targetID := strings.TrimSpace(request.TargetID)
	if _, err := service.catalog.ResolveTarget(targetID); err != nil {
		return err
	}
	service.revokeTargetOperations(targetID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return service.lifecycle.DisconnectTarget(ctx, targetID)
}

func (service *RemoteWorkspaceService) beginTargetOperation(targetID string) uint64 {
	targetID = strings.TrimSpace(targetID)
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.targetEpoch[targetID]
}

func (service *RemoteWorkspaceService) beginNewTargetOperation() uint64 {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.globalEpoch
}

func (service *RemoteWorkspaceService) revokeTargetOperations(targetID string) {
	targetID = strings.TrimSpace(targetID)
	service.mu.Lock()
	if service.targetEpoch == nil {
		service.targetEpoch = make(map[string]uint64)
	}
	service.globalEpoch++
	service.targetEpoch[targetID]++
	for token, pendingTargetID := range service.pendingTargets {
		if pendingTargetID == targetID {
			delete(service.pendingTargets, token)
		}
	}
	service.mu.Unlock()
}

func (service *RemoteWorkspaceService) trackPendingTarget(token, targetID string) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.pendingTargets == nil {
		service.pendingTargets = make(map[string]string)
	}
	for pendingToken, pendingTargetID := range service.pendingTargets {
		if pendingTargetID == targetID {
			delete(service.pendingTargets, pendingToken)
		}
	}
	service.pendingTargets[token] = targetID
}

func (service *RemoteWorkspaceService) takePendingTarget(token string) string {
	service.mu.Lock()
	defer service.mu.Unlock()
	targetID := service.pendingTargets[token]
	delete(service.pendingTargets, token)
	return targetID
}

func (service *RemoteWorkspaceService) clearPendingTargets(targetID string) {
	service.mu.Lock()
	defer service.mu.Unlock()
	for token, pendingTargetID := range service.pendingTargets {
		if pendingTargetID == targetID {
			delete(service.pendingTargets, token)
		}
	}
}

func (service *RemoteWorkspaceService) finishTargetOperation(ctx context.Context, targetID string, epoch uint64) error {
	service.mu.Lock()
	current := service.targetEpoch[targetID]
	service.mu.Unlock()
	if current == epoch {
		return nil
	}
	service.clearPendingTargets(targetID)
	_ = service.lifecycle.DisconnectTarget(ctx, targetID)
	return errors.New("REMOTE_DISCONNECTED: remote target operation was revoked")
}

func (service *RemoteWorkspaceService) finishNewTargetOperation(ctx context.Context, targetID string, epoch uint64) error {
	service.mu.Lock()
	current := service.globalEpoch
	service.mu.Unlock()
	if current == epoch {
		return nil
	}
	service.revokeTargetOperations(targetID)
	_ = service.lifecycle.DisconnectTarget(ctx, targetID)
	return errors.New("REMOTE_DISCONNECTED: remote target operation was revoked")
}

func (service *RemoteWorkspaceService) piVersion() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status := service.pi.Probe(ctx)
	if status.State != domain.RuntimeReady || strings.TrimSpace(status.Version) == "" {
		return "", errors.New("compatible Pi runtime version could not be verified")
	}
	return status.Version, nil
}
