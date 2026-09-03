import { Dialogs, Events } from "@wailsio/runtime";
import { AgentService } from "../../bindings/pi-desk/internal/appservice";
import type {
  BashRequest,
  CommandResult,
  CompactRequest,
  ExportSessionRequest,
  ExtensionUIResponseRequest,
  LiveSession,
  ModelRequest,
  PromptRequest,
  QueueModeRequest,
  SessionBranchEntry,
  SessionBranches,
  SessionNameRequest,
  SessionForkRequest,
  SessionMessageRequest,
  StartSessionRequest,
  ThinkingRequest,
  ThreadRequest,
  ToggleRequest,
} from "../../bindings/pi-desk/internal/domain";

export interface PiEvent {
  generation: number;
  type: string;
  payload?: Record<string, unknown>;
  record?: string;
  error?: string;
}

export interface PiSessionEvent {
  threadId: string;
  event: PiEvent;
}

function parseResult<T>(result: CommandResult): T {
  if (!result.dataJson) return undefined as T;
  return JSON.parse(result.dataJson) as T;
}

export const agentService = {
  startSession(request: StartSessionRequest): Promise<LiveSession> {
    return AgentService.StartSession(request);
  },
  stopSession(threadId: string): Promise<void> {
    return AgentService.StopSession({ threadId });
  },
  sendPrompt(request: PromptRequest): Promise<CommandResult> {
    return AgentService.SendPrompt(request);
  },
  abort(threadId: string): Promise<CommandResult> {
    return AgentService.Abort({ threadId });
  },
  setAutoRetry(request: ToggleRequest): Promise<CommandResult> {
    return AgentService.SetAutoRetry(request);
  },
  setAutoCompaction(request: ToggleRequest): Promise<CommandResult> {
    return AgentService.SetAutoCompaction(request);
  },
  setSteeringMode(request: QueueModeRequest): Promise<CommandResult> {
    return AgentService.SetSteeringMode(request);
  },
  setFollowUpMode(request: QueueModeRequest): Promise<CommandResult> {
    return AgentService.SetFollowUpMode(request);
  },
  abortRetry(threadId: string): Promise<CommandResult> {
    return AgentService.AbortRetry({ threadId });
  },
  bash<T>(request: BashRequest): Promise<T> {
    return AgentService.Bash(request).then(parseResult<T>);
  },
  abortBash(threadId: string): Promise<CommandResult> {
    return AgentService.AbortBash({ threadId });
  },
  getState<T>(threadId: string): Promise<T> {
    return AgentService.GetState({ threadId }).then(parseResult<T>);
  },
  getMessages<T>(threadId: string): Promise<T> {
    return AgentService.GetMessages({ threadId }).then(parseResult<T>);
  },
  getSessionStats<T>(threadId: string): Promise<T> {
    return AgentService.GetSessionStats({ threadId }).then(parseResult<T>);
  },
  getAvailableModels<T>(threadId: string): Promise<T> {
    return AgentService.GetAvailableModels({ threadId }).then(parseResult<T>);
  },
  getAvailableThinkingLevels<T>(threadId: string): Promise<T> {
    return AgentService.GetAvailableThinkingLevels({ threadId }).then(parseResult<T>);
  },
  getCommands<T>(threadId: string): Promise<T> {
    return AgentService.GetCommands({ threadId }).then(parseResult<T>);
  },
  getForkMessages<T>(threadId: string): Promise<T> {
    return AgentService.GetForkMessages({ threadId }).then(parseResult<T>);
  },
  getSessionBranches(threadId: string): Promise<SessionBranches> {
    return AgentService.GetSessionBranches({ threadId });
  },
  cloneSession<T>(threadId: string): Promise<T> {
    return AgentService.CloneSession({ threadId }).then(parseResult<T>);
  },
  forkSession<T>(request: SessionForkRequest): Promise<T> {
    return AgentService.ForkSession(request).then(parseResult<T>);
  },
  forkSessionAt<T>(request: SessionMessageRequest): Promise<T> {
    return AgentService.ForkSessionAt(request).then(parseResult<T>);
  },
  editSessionMessage<T>(request: SessionMessageRequest): Promise<T> {
    return AgentService.EditSessionMessage(request).then(parseResult<T>);
  },
  replaySessionMessage<T>(request: SessionMessageRequest): Promise<T> {
    return AgentService.ReplaySessionMessage(request).then(parseResult<T>);
  },
  deleteSessionMessage<T>(request: SessionMessageRequest): Promise<T> {
    return AgentService.DeleteSessionMessage(request).then(parseResult<T>);
  },
  async exportSession<T>(threadId: string, title: string, directory: string): Promise<T | undefined> {
    const filename = `${title.replace(/[<>:"/\\|?*\u0000-\u001f]/g, "-").trim() || "pi-session"}.html`;
    const outputPath = await Dialogs.SaveFile({
      Title: "Export Pi session",
      Filename: filename,
      Directory: directory,
      CanCreateDirectories: true,
      AllowsOtherFiletypes: false,
      Filters: [{ DisplayName: "HTML document", Pattern: "*.html" }],
    });
    if (!outputPath) return undefined;
    return AgentService.ExportSession({ threadId, outputPath }).then(parseResult<T>);
  },
  setModel(request: ModelRequest): Promise<CommandResult> {
    return AgentService.SetModel(request);
  },
  setThinkingLevel(request: ThinkingRequest): Promise<CommandResult> {
    return AgentService.SetThinkingLevel(request);
  },
  compact(request: CompactRequest): Promise<CommandResult> {
    return AgentService.Compact(request);
  },
  setSessionName(request: SessionNameRequest): Promise<CommandResult> {
    return AgentService.SetSessionName(request);
  },
  respondExtensionUI(request: ExtensionUIResponseRequest): Promise<void> {
    return AgentService.RespondExtensionUI(request);
  },
  getDiagnostics(threadId: string): Promise<string> {
    return AgentService.GetDiagnostics({ threadId });
  },
};

export function onPiEvent(callback: (event: PiSessionEvent) => void): () => void {
  return Events.On("pi:event", (event) => callback(event.data as PiSessionEvent));
}

export type {
  BashRequest,
  CompactRequest,
  ExportSessionRequest,
  ExtensionUIResponseRequest,
  ModelRequest,
  PromptRequest,
  QueueModeRequest,
  SessionBranchEntry,
  SessionBranches,
  SessionNameRequest,
  SessionForkRequest,
  SessionMessageRequest,
  StartSessionRequest,
  ThinkingRequest,
  ThreadRequest,
  ToggleRequest,
};
