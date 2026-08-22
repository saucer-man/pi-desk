import * as RemoteWorkspaceService from "../../bindings/pi-desk/internal/appservice/remoteworkspaceservice";
import type {
  RemoteAliasSummary,
  RemoteRootCandidate,
  RemoteTargetSummary,
  WorkspaceSummary,
} from "../../bindings/pi-desk/internal/domain";

export const remoteWorkspaceService = {
  discover(): Promise<RemoteAliasSummary[]> {
    return RemoteWorkspaceService.DiscoverRemoteTargets().then((aliases) => aliases ?? []);
  },
  listTargets(): Promise<RemoteTargetSummary[]> {
    return RemoteWorkspaceService.ListRemoteTargets().then((targets) => targets ?? []);
  },
  connectNew(name: string, hostAlias: string): Promise<string> {
    return RemoteWorkspaceService.ConnectRemoteTarget({ name, hostAlias });
  },
  connect(targetId: string): Promise<string> {
    return RemoteWorkspaceService.ConnectRemoteTarget({ targetId });
  },
  prepareRoot(targetId: string, name: string, requestedRoot: string): Promise<RemoteRootCandidate> {
    return RemoteWorkspaceService.PrepareRemoteRoot({ targetId, name, requestedRoot });
  },
  decideRoot(token: string, trust: "approve" | "deny"): Promise<WorkspaceSummary> {
    return RemoteWorkspaceService.DecideRemoteRoot({ token, trust });
  },
  resume(workspaceId: string): Promise<WorkspaceSummary> {
    return RemoteWorkspaceService.ResumeRemoteWorkspace({ workspaceId });
  },
  open(workspaceId: string): Promise<WorkspaceSummary> {
    return RemoteWorkspaceService.OpenRemoteWorkspace({ id: workspaceId });
  },
  removeTarget(targetId: string): Promise<void> {
    return RemoteWorkspaceService.RemoveRemoteTarget({ targetId });
  },
  disconnect(targetId: string): Promise<void> {
    return RemoteWorkspaceService.DisconnectRemoteTarget({ targetId });
  },
};

export type { RemoteAliasSummary, RemoteRootCandidate, RemoteTargetSummary } from "../../bindings/pi-desk/internal/domain";
