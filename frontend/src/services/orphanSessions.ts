import { Dialogs } from "@wailsio/runtime";
import { OrphanSessionService } from "../../bindings/pi-desk/internal/appservice";
import type { DeletedSession, OrphanSessionSummary, SessionSnapshot } from "../../bindings/pi-desk/internal/domain";

export const orphanSessionService = {
  async list(): Promise<OrphanSessionSummary[]> {
    return (await OrphanSessionService.ListOrphanSessions()) ?? [];
  },
  async snapshot(path: string, before?: string): Promise<SessionSnapshot> {
    return await OrphanSessionService.GetOrphanSessionSnapshot({ path, before });
  },
  async remove(path: string): Promise<DeletedSession> {
    return await OrphanSessionService.DeleteOrphanSession({ path });
  },
  async exportHTML(path: string, title: string): Promise<string | undefined> {
    const filename = `${title.replace(/[<>:"/\\|?*\u0000-\u001f]/g, "-").trim() || "orphan-session"}.html`;
    const outputPath = await Dialogs.SaveFile({
      Title: "Export orphan SSH session",
      Filename: filename,
      CanCreateDirectories: true,
      AllowsOtherFiletypes: false,
      Filters: [{ DisplayName: "HTML document", Pattern: "*.html" }],
    });
    if (!outputPath) return undefined;
    await OrphanSessionService.ExportOrphanSession({ path, outputPath });
    return outputPath;
  },
};
