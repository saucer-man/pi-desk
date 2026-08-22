import { OrphanSessionService } from "../../bindings/pi-desk/internal/appservice";
import type { OrphanSessionSummary, SessionSnapshot } from "../../bindings/pi-desk/internal/domain";

export const orphanSessionService = {
  async list(): Promise<OrphanSessionSummary[]> {
    return (await OrphanSessionService.ListOrphanSessions()) ?? [];
  },
  async snapshot(path: string): Promise<SessionSnapshot> {
    return await OrphanSessionService.GetOrphanSessionSnapshot({ path });
  },
  async restore(path: string, workspaceId: string): Promise<void> {
    await OrphanSessionService.RestoreOrphanSession({ path, workspaceId });
  },
};
