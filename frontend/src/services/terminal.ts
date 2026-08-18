import { Events } from "@wailsio/runtime";
import { TerminalService } from "../../bindings/pi-desk/internal/appservice";
import type { TerminalState } from "../../bindings/pi-desk/internal/domain";

export interface TerminalEvent {
  threadId: string;
  type: "output" | "error" | "exit";
  sequence: number;
  dataB64?: string;
  exitCode?: number;
  error?: string;
}

export const terminalService = {
  start(threadId: string, workspacePath: string, columns: number, rows: number): Promise<TerminalState> {
    return TerminalService.Start({ threadId, workspacePath, columns, rows });
  },
  snapshot(threadId: string): Promise<TerminalState> {
    return TerminalService.Snapshot({ threadId });
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
