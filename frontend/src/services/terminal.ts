import { Events } from "@wailsio/runtime";
import { TerminalService } from "../../bindings/pi-desk/internal/appservice";
import type { TerminalState } from "../../bindings/pi-desk/internal/domain";

export interface TerminalEvent {
  threadId: string;
  type: "output" | "error" | "exit";
  generation?: number;
  sequence: number;
  dataB64?: string;
  exitCode?: number;
  error?: string;
}

export type TerminalWorkspaceReference = string | { workspaceId: string };

export const terminalService = {
  start(threadId: string, workspace: TerminalWorkspaceReference, columns: number, rows: number): Promise<TerminalState> {
    const reference = typeof workspace === "string" ? { workspacePath: workspace } : workspace;
    return TerminalService.Start({ threadId, ...reference, columns, rows });
  },
  snapshot(threadId: string, workspaceId?: string): Promise<TerminalState> {
    return TerminalService.Snapshot({ threadId, workspaceId });
  },
  write(threadId: string, data: string): Promise<void> {
    return TerminalService.Write({ threadId, data });
  },
  resize(threadId: string, columns: number, rows: number): Promise<void> {
    return TerminalService.Resize({ threadId, columns, rows });
  },
  stop(threadId: string): Promise<void> {
    return TerminalService.Stop({ threadId });
  },
};

export function onTerminalEvent(callback: (event: TerminalEvent) => void): () => void {
  return Events.On("terminal:event", (event) => callback(event.data as TerminalEvent));
}
