import { CatalogService } from "../../bindings/pi-desk/internal/appservice";
import type { DeletedSession, DesktopState, SessionSnapshot, SessionSummary, SessionUsageSummary, WorkspaceApplication, WorkspaceSummary } from "../../bindings/pi-desk/internal/domain";

const MAX_WORKSPACE_ICON_DATA_URL = 256 * 1024;
const PNG_DATA_URL_PATTERN = /^data:image\/png;base64,[A-Za-z0-9+/]+={0,2}$/;

function validWorkspaceApplication(application: WorkspaceApplication): boolean {
  return /^[a-z0-9-]{1,64}$/.test(application.id)
    && Boolean(application.name.trim())
    && application.iconDataUrl.length <= MAX_WORKSPACE_ICON_DATA_URL
    && PNG_DATA_URL_PATTERN.test(application.iconDataUrl);
}

export const catalogService = {
  async listWorkspaces(): Promise<WorkspaceSummary[]> {
    return (await CatalogService.ListWorkspaces()) ?? [];
  },
  async addWorkspace(path: string, trust: "approve" | "deny"): Promise<WorkspaceSummary> {
    return await CatalogService.AddWorkspace({ path, trust });
  },
  async renameWorkspace(id: string, name: string): Promise<WorkspaceSummary> {
    return await CatalogService.RenameWorkspace({ id, name });
  },
  async removeWorkspace(id: string): Promise<void> {
    await CatalogService.RemoveWorkspace({ id });
  },
  async deleteWorkspaceSessions(id: string): Promise<void> {
    await CatalogService.DeleteWorkspaceSessions({ id });
  },
  async openWorkspace(id: string): Promise<void> {
    await CatalogService.OpenWorkspace({ id });
  },
  async listWorkspaceApplications(): Promise<WorkspaceApplication[]> {
    return ((await CatalogService.ListWorkspaceApplications()) ?? []).filter(validWorkspaceApplication);
  },
  async openWorkspaceWith(workspaceId: string, applicationId: string): Promise<void> {
    await CatalogService.OpenWorkspaceWith({ workspaceId, applicationId });
  },
  async pickWorkspace(initialPath?: string): Promise<string> {
    return await CatalogService.PickWorkspace({ initialPath });
  },
  async listSessions(workspacePath?: string): Promise<SessionSummary[]> {
    return (await CatalogService.ListSessions({ workspacePath })) ?? [];
  },
  async getSessionSnapshot(path: string): Promise<SessionSnapshot> {
    return await CatalogService.GetSessionSnapshot({ path });
  },
  async getSessionUsage(workspacePath?: string): Promise<SessionUsageSummary> {
    return await CatalogService.GetSessionUsage({ workspacePath });
  },
  async deleteSession(path: string): Promise<DeletedSession> {
    return await CatalogService.DeleteSession({ path });
  },
  async getDesktopState(): Promise<DesktopState> {
    return await CatalogService.GetDesktopState();
  },
  async saveDesktopState(state: DesktopState): Promise<void> {
    await CatalogService.SaveDesktopState(state);
  },
};
