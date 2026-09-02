import { defineStore } from "pinia";
import { RuntimeState, type BootstrapState, type DesktopState, type SessionSnapshot, type WorkspaceApplication, type WorkspaceSummary as HostWorkspaceSummary } from "../../bindings/pi-desk/internal/domain";
import { agentService, onPiEvent, type PiSessionEvent, type SessionBranches } from "../services/agent";
import { catalogService } from "../services/catalog";
import { checkForUpdates, checkRuntime as checkRuntimeStatus, getBootstrapState, notifyDesktop } from "../services/desktop";
import { repositoryService, type GitBranchInventory, type RepositoryFileDiff, type RepositoryFilePreview, type RepositorySnapshot, type RepositoryWorkspaceReference } from "../services/repository";
import { remoteWorkspaceService } from "../services/remoteWorkspaces";
import { onTerminalEvent, type TerminalEvent } from "../services/terminal";
import { modelConfigService } from "../services/modelconfig";
import { BATCH_ASK_PLACEHOLDER, parseBatchAskEnvelope, type BatchAskQuestion } from "../utils/batchAsk";
import { formatFileMention } from "../utils/fileMentions";
import type { PreparedImage } from "../utils/imageAttachments";
import { skillInvocationCommandText, skillInvocationTitleText } from "../utils/skillInvocation";
import { runtimeErrorText } from "../utils/runtimeError";
import { buildToolDiff } from "../utils/toolDiff";
import { setAppLanguage, tr } from "../i18n";

export type ThreadStatus = "idle" | "starting" | "running" | "attention";
export type InspectorTab = "changes" | "context" | "terminal";
export type StreamingBehavior = "steer" | "followUp";
export type QueueMode = "all" | "one-at-a-time";
export type Appearance = "dark" | "light" | "system";
export type Language = "zh-CN" | "en";
export type InterfaceFont = "default" | "system" | "serif" | "mono";
type RemoteReconnectIntent = "start" | "prompt" | "bash" | "terminal";
export type RemoteReconnectProgressStatus = "pending" | "active" | "complete" | "error";
export interface RemoteReconnectProgressStep {
  id: string;
  label: string;
  status: RemoteReconnectProgressStatus;
}

const remoteReconnectProgressDefinitions: RemoteReconnectProgressStep[] = [
  { id: "stop", label: "remoteReconnect.stepStop", status: "active" },
  { id: "connect", label: "remoteReconnect.stepConnect", status: "pending" },
  { id: "restore", label: "remoteReconnect.stepRestore", status: "pending" },
];

export const MAX_PI_PROCESSES = 10;
export const DEFAULT_SIDEBAR_WIDTH = 272;
export const MIN_SIDEBAR_WIDTH = 180;
export const MAX_SIDEBAR_WIDTH = 560;
export const DEFAULT_INSPECTOR_WIDTH = 320;
export const MIN_INSPECTOR_WIDTH = 240;
export const MAX_INSPECTOR_WIDTH = 840;
const TODO_WIDGET_KEYS = ["pi-deck-todo", "pi-desk-todo"] as const;
const REMOTE_RECONNECT_CODES = ["REMOTE_DISCONNECTED", "REMOTE_CONTEXT_CHANGED_WAIT_FOR_IDLE", "REMOTE_OUTCOME_UNKNOWN"] as const;
const REMOTE_MUTATING_TOOLS = new Set(["write", "edit", "bash", "user_bash"]);

function hasRemoteCode(message: string, code: (typeof REMOTE_RECONNECT_CODES)[number]): boolean {
  return message === code || message.startsWith(`${code}:`);
}

function requiresRemoteReconnect(message: string): boolean {
  return REMOTE_RECONNECT_CODES.some((code) => hasRemoteCode(message, code));
}

export interface WorkspaceSummary {
  id: string;
  name: string;
  path: string;
  kind?: string;
  targetId?: string;
  remoteRoot?: string;
  trust: "approve" | "deny";
  addedAt?: string;
  lastOpenedAt?: string;
  discovered?: boolean;
}

export interface ThreadSummary {
  id: string;
  title: string;
  workspace: string;
  workspaceId?: string;
  workspacePath: string;
  trust: "approve" | "deny";
  status: ThreadStatus;
  started: boolean;
  generation: number;
  sessionId?: string;
  sessionFile?: string;
  createdAt?: string;
  modifiedAt?: string;
  messageCount?: number;
  firstMessage?: string;
  parentSessionFile?: string;
  unread?: boolean;
  error?: string;
}

export interface ToolExecution {
  id: string;
  name: string;
  arguments?: unknown;
  output: string;
  truncated?: boolean;
  status: "running" | "complete" | "error";
  startedAt?: number;
  durationMs?: number;
  diff?: ToolDiff;
}

export interface ToolDiff {
  path: string;
  text: string;
}

export interface ExecutionStep {
  id: string;
  kind: "thinking" | "tools" | "message";
  text?: string;
  tools?: ToolExecution[];
  active?: boolean;
}

export interface TimelineCompaction {
  summary: string;
  tokensBefore?: number;
  estimatedTokensAfter?: number;
}

export interface TimelineRunNotice {
  status: "retrying" | "retried" | "recovered" | "failed";
  error?: string;
  attempt?: number;
  maxAttempts?: number;
  delayMs?: number;
}

export interface TimelineMessage {
  id: string;
  entryId?: string;
  role: "user" | "assistant" | "system";
  text: string;
  thinking: string;
  timestamp: string;
  timestampMs?: number;
  durationMs?: number;
  thinkingCount?: number;
  executionSteps?: ExecutionStep[];
  activeExecution?: "thinking" | "tool" | "text";
  streaming: boolean;
  error?: string;
  delivery?: StreamingBehavior;
  images?: PreparedImage[];
  tools: ToolExecution[];
  compaction?: TimelineCompaction;
  runNotice?: TimelineRunNotice;
}

export interface QueuedMessages {
  steering: string[];
  followUp: string[];
}

export interface PendingPrompt {
  id: string;
  text: string;
  images: PreparedImage[];
  createdAt: string;
}

export interface RetryInfo {
  attempt: number;
  maxAttempts: number;
  delayMs: number;
  errorMessage?: string;
}

export interface ForkMessage {
  entryId: string;
  text: string;
}

export interface PiModel {
  id: string;
  name?: string;
  provider: string;
  contextWindow?: number;
  reasoning?: boolean;
}

export interface PiSessionState {
  model?: PiModel;
  thinkingLevel?: string;
  isStreaming?: boolean;
  isCompacting?: boolean;
  steeringMode?: "all" | "one-at-a-time";
  followUpMode?: "all" | "one-at-a-time";
  autoCompactionEnabled?: boolean;
  sessionFile?: string;
  sessionId?: string;
  sessionName?: string;
  pendingMessageCount?: number;
  messageCount?: number;
}

export interface SessionStats {
  cost?: number;
  totalMessages?: number;
  contextUsage?: { tokens: number | null; contextWindow: number; percent: number | null; estimated?: boolean };
  tokens?: { input: number; output: number; cacheRead: number; cacheWrite: number; total: number };
}

export interface SlashCommand {
  name: string;
  description?: string;
  source: "extension" | "prompt" | "skill";
  location?: "user" | "project" | "path";
  path?: string;
}

export type SettingsSection = "general" | "modelManagement" | "promptManagement" | "skillManagement" | "extensionManagement" | "mcpManagement" | "statistics" | "resources";

interface RpcSlashCommand extends SlashCommand {
  sourceInfo?: {
    path?: string;
    scope?: "user" | "project" | "temporary";
    source?: string;
  };
}

export interface ExtensionUIRequest {
  id: string;
  method: "select" | "confirm" | "input" | "editor" | "batch_ask" | "notify" | "setStatus" | "setWidget" | "setTitle" | "set_editor_text";
  title?: string;
  message?: string;
  options?: string[];
  placeholder?: string;
  prefill?: string;
  notifyType?: "info" | "warning" | "error";
  timeout?: number;
  statusKey?: string;
  statusText?: string;
  widgetKey?: string;
  widgetLines?: string[];
  widgetPlacement?: "aboveEditor" | "belowEditor";
  text?: string;
  batchQuestions?: BatchAskQuestion[];
  batchReview?: boolean;
}

export interface ExtensionStatus {
  key: string;
  text: string;
}

export interface ExtensionWidget {
  key: string;
  lines: string[];
  placement: "aboveEditor" | "belowEditor";
  instance?: string;
}

interface ModelResponse {
  models: PiModel[];
}

interface ThinkingResponse {
  levels: string[];
}

interface CommandsResponse {
  commands: RpcSlashCommand[];
}

interface ForkResponse {
  text?: string;
  cancelled: boolean;
}

interface ExportResponse {
  path: string;
}

interface BashResponse {
  output: string;
  exitCode?: number;
  cancelled: boolean;
  truncated: boolean;
  fullOutputPath?: string;
}

interface CatalogSession {
  id: string;
  path: string;
  cwd: string;
  anchorWorkspaceId?: string;
  name?: string;
  title: string;
  firstMessage: string;
  createdAt: string;
  modifiedAt: string;
  messageCount: number;
  parentSessionPath?: string;
}

function modelKey(model: PiModel): string {
  return `${model.provider}\u0000${model.id}`;
}

function mergeModels(...sources: PiModel[][]): PiModel[] {
  const models = new Map<string, PiModel>();
  for (const source of sources) {
    for (const model of source) {
      if (!model.provider || !model.id) continue;
      const key = modelKey(model);
      models.set(key, { ...(models.get(key) ?? {} as PiModel), ...model });
    }
  }
  return [...models.values()];
}

let unsubscribePiEvents: (() => void) | undefined;
let unsubscribeTerminalEvents: (() => void) | undefined;
let localSequence = 0;
let desktopSaveTimer: ReturnType<typeof setTimeout> | undefined;
let piStartQueue: Promise<void> = Promise.resolve();
const piStartPromises = new Map<string, Promise<void>>();
const piStartingGenerationFloor = new Map<string, number>();
const piExitedGenerationByThread = new Map<string, number>();
const remoteTargetProjectionEpochByID = new Map<string, number>();
async function waitForPiStart(threadID: string) {
  await piStartPromises.get(threadID)?.catch(() => undefined);
}
function isCurrentPiRequest(threads: ThreadSummary[], thread: ThreadSummary | undefined, generation: number | undefined, wasStarted: boolean | undefined): thread is ThreadSummary {
  return Boolean(thread && threads.includes(thread) && thread.generation === generation && (!wasStarted || thread.started));
}
const settledReloadPromises = new Map<string, Promise<void>>();
const pendingPromptDispatchThreads = new Set<string>();
const TOOL_OUTPUT_LIMIT = 256 << 10;
const TOOL_OUTPUT_TRUNCATION_MARKER = "\n\n... output truncated by Pi Desk ...\n\n";

function createID(prefix: string): string {
  localSequence += 1;
  const uuid = globalThis.crypto?.randomUUID?.();
  return uuid ? `${prefix}-${uuid}` : `${prefix}-${Date.now()}-${localSequence}`;
}

function nowLabel(): string {
  return formatMessageTime(new Date());
}

function boundedExtensionText(value: unknown, limit: number): string {
  return typeof value === "string" ? value.slice(0, limit) : "";
}

const ANSI_ESCAPE_SEQUENCE = /\u001B(?:\[[0-?]*[ -/]*[@-~]|\][^\u0007]*(?:\u0007|\u001B\\))/g;
const ORPHAN_SGR_PREFIX = /^(?:\d{1,3}(?:;\d{1,3})*)m/;

function extensionStatusText(value: unknown): string {
  const text = boundedExtensionText(value, 4096)
    .replace(ANSI_ESCAPE_SEQUENCE, "")
    // Some transports can drop the leading ESC while preserving the SGR payload.
    .replace(ORPHAN_SGR_PREFIX, "");
  return text.replace(/^[^\p{L}\p{N}]+/u, "").trim();
}

function boundedWidgetLines(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  const lines: string[] = [];
  let remaining = 64 << 10;
  for (const item of value.slice(0, 100)) {
    if (typeof item !== "string" || remaining <= 0) continue;
    const line = item.slice(0, Math.min(4096, remaining));
    remaining -= line.length;
    lines.push(line);
  }
  return lines;
}

function blockingExtensionRequest(value: unknown): ExtensionUIRequest | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  const raw = value as Record<string, unknown>;
  const id = boundedExtensionText(raw.id, 256).trim();
  const method = boundedExtensionText(raw.method, 32) as ExtensionUIRequest["method"];
  if (!id || !["select", "confirm", "input", "editor"].includes(method)) return undefined;

  const placeholder = boundedExtensionText(raw.placeholder, 4096);
  const markerConfirmed = method === "input" && placeholder === BATCH_ASK_PLACEHOLDER;
  const envelope = parseBatchAskEnvelope(raw.title ?? raw.question, markerConfirmed)
    ?? (Array.isArray(raw.questions) ? parseBatchAskEnvelope(raw, markerConfirmed) : undefined);
  if (envelope) {
    return {
      id,
      method: "batch_ask",
      batchQuestions: envelope.questions,
      batchReview: envelope.review,
      timeout: typeof raw.timeout === "number" ? raw.timeout : undefined,
    };
  }
  if (markerConfirmed) return undefined;

  const title = boundedExtensionText(raw.title ?? raw.question, 8192);
  const message = boundedExtensionText(raw.message, 8192);
  const options = Array.isArray(raw.options)
    ? raw.options.slice(0, 100).filter((option): option is string => typeof option === "string").map((option) => option.slice(0, 4096))
    : undefined;
  if (method === "select" && !options?.length) return undefined;
  return {
    id,
    method,
    title,
    message,
    options,
    placeholder,
    prefill: boundedExtensionText(raw.prefill, 1 << 20),
    timeout: typeof raw.timeout === "number" ? raw.timeout : undefined,
  };
}

function nowISO(): string {
  return new Date().toISOString();
}

function appIsInBackground(): boolean {
  if (typeof document === "undefined") return false;
  return document.visibilityState === "hidden" || !document.hasFocus();
}

function taskNotificationSummary(title: string): string {
  const normalized = title.replace(/\s+/g, " ").trim() || tr("notifications.untitledTask");
  const characters = Array.from(normalized);
  return characters.length > 160 ? `${characters.slice(0, 159).join("")}…` : normalized;
}

function workspaceName(path: string): string {
  const normalized = path.replace(/[\\/]+$/, "");
  return normalized.split(/[\\/]/).pop() || normalized;
}

function pathKey(path: string): string {
  const normalized = path.replace(/[\\/]+$/, "").replaceAll("\\", "/");
  return /^[a-z]:\//i.test(normalized) ? normalized.toLocaleLowerCase() : normalized;
}

function repositoryKey(thread: ThreadSummary): string {
  return thread.workspaceId || pathKey(thread.workspacePath);
}

function repositoryReference(thread: ThreadSummary): RepositoryWorkspaceReference {
  return thread.workspaceId ? { workspaceId: thread.workspaceId } : thread.workspacePath;
}

function errorMessage(error: unknown): string {
	const message = error instanceof Error ? error.message : String(error ?? "");
	return message.trim() || "Desktop service is unavailable";
}

function isThreadNotRunningError(error: unknown): boolean {
	return /Pi thread is not running/i.test(errorMessage(error));
}

function isClosedRPCStreamError(error: unknown): boolean {
  return /read pi RPC stream:.*(?:file already closed|closed pipe)/i.test(errorMessage(error));
}

function contentText(content: unknown): string {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  return content
    .map((part) => {
      if (!part || typeof part !== "object") return "";
      const value = part as Record<string, unknown>;
      if (value.type === "text" && typeof value.text === "string") return value.text;
      return "";
    })
    .join("");
}

function isInternalRuntimeNotice(text: string): boolean {
  return /^\s*Ponytail loaded:\s*[^\r\n]+\s*$/i.test(text);
}

function contentThinking(content: unknown): string {
  if (!Array.isArray(content)) return "";
  return content
    .map((part) => {
      if (!part || typeof part !== "object") return "";
      const value = part as Record<string, unknown>;
      if (value.type === "thinking" && typeof value.thinking === "string") return value.thinking;
      return "";
    })
    .join("");
}

function snapshotSearchText(source: Array<Record<string, unknown>>): string {
  return source.map((value) => {
    const role = String(value.role ?? "");
    if (role === "piDeskCompaction") return typeof value.summary === "string" ? value.summary : "";
    if (role === "bashExecution") {
      return [value.command, value.output].filter((item): item is string => typeof item === "string").join("\n");
    }
    return [contentText(value.content), contentThinking(value.content)].filter(Boolean).join("\n");
  }).filter(Boolean).join("\n");
}

function timelineSearchText(messages: TimelineMessage[]): string {
  return messages.map((message) => [
    message.text,
    message.thinking,
    message.compaction?.summary ?? "",
    ...message.tools.map((tool) => tool.output),
    ...(message.executionSteps ?? []).flatMap((step) => [
      step.text ?? "",
      ...(step.tools ?? []).map((tool) => tool.output),
    ]),
  ].filter(Boolean).join("\n")).filter(Boolean).join("\n");
}

function sessionSearchVersion(thread: ThreadSummary): string {
  return `${thread.sessionFile ?? ""}\n${thread.messageCount ?? ""}\n${thread.modifiedAt ?? ""}`;
}

function contentImages(content: unknown): PreparedImage[] {
  if (!Array.isArray(content)) return [];
  return content.flatMap((part, index) => {
    if (!part || typeof part !== "object") return [];
    const value = part as Record<string, unknown>;
    if (value.type !== "image" || typeof value.data !== "string" || typeof value.mimeType !== "string") return [];
    return [{
      id: createID(`history-image-${index}`),
      name: `Image ${index + 1}`,
      data: value.data,
      mimeType: value.mimeType,
      previewUrl: `data:${value.mimeType};base64,${value.data}`,
    }];
  });
}

function resultText(result: unknown): string {
  if (typeof result === "string") return result;
  if (!result || typeof result !== "object") return "";
  const value = result as Record<string, unknown>;
  return contentText(value.content) || (typeof value.output === "string" ? value.output : "");
}

function boundedToolOutput(output: string): { text: string; truncated: boolean } {
  if (output.length <= TOOL_OUTPUT_LIMIT) return { text: output, truncated: false };
  const available = TOOL_OUTPUT_LIMIT - TOOL_OUTPUT_TRUNCATION_MARKER.length;
  const headLength = Math.floor(available * 0.6);
  return {
    text: output.slice(0, headLength) + TOOL_OUTPUT_TRUNCATION_MARKER + output.slice(-(available - headLength)),
    truncated: true,
  };
}

function messageTimestamp(value: unknown): number | undefined {
  if (typeof value !== "number" && typeof value !== "string") return undefined;
  const date = new Date(typeof value === "number" ? value : value);
  return Number.isNaN(date.getTime()) ? undefined : date.getTime();
}

function formatMessageTime(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${pad(date.getMonth() + 1)}/${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function messageTime(value: unknown): string {
  const timestamp = messageTimestamp(value);
  return timestamp === undefined ? "" : formatMessageTime(new Date(timestamp));
}

function transcriptDisplayID(value: Record<string, unknown>): string | undefined {
  if (typeof value.piDeskEntryId === "string") return value.piDeskEntryId;
  return typeof value.piDeskDisplayId === "string" ? value.piDeskDisplayId : undefined;
}

function historicalIdentity(value: Record<string, unknown>, fallback: string, prefix = "history"): { id: string; entryId?: string } {
  const entryId = typeof value.piDeskEntryId === "string" ? value.piDeskEntryId : undefined;
  const displayId = transcriptDisplayID(value);
  return {
    id: displayId ? `${prefix}-${displayId}` : createID(fallback),
    entryId,
  };
}

function tokenCount(value: unknown): number | undefined {
  const parsed = typeof value === "number" ? value : Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? Math.trunc(parsed) : undefined;
}

function compactionEstimateKey(summary: string, tokensBefore: number | undefined): string {
  return `${tokensBefore ?? ""}\n${summary}`;
}

function historicalMessages(source: Array<Record<string, unknown>>, compactionEstimates: Record<string, number> = {}): TimelineMessage[] {
  const result: TimelineMessage[] = [];
  const toolsByID = new Map<string, ToolExecution>();
  for (const value of source) {
    const role = String(value.role ?? "");
    if (role === "piDeskCompaction") {
      const identity = historicalIdentity(value, "history-compaction", "history-compaction");
      const summary = typeof value.summary === "string" ? value.summary : "";
      const tokensBefore = tokenCount(value.tokensBefore);
      const persistedEstimate = tokenCount(value.estimatedTokensAfter);
      result.push({
        id: identity.id,
        role: "system", text: "", thinking: "",
        timestamp: messageTime(value.timestamp), timestampMs: messageTimestamp(value.timestamp), streaming: false, tools: [],
        compaction: {
          summary,
          tokensBefore,
          estimatedTokensAfter: persistedEstimate ?? compactionEstimates[compactionEstimateKey(summary, tokensBefore)],
        },
      });
      continue;
    }
    if (role === "toolResult") {
      const toolCallID = String(value.toolCallId ?? "");
      const tool = toolsByID.get(toolCallID);
      if (tool) {
        const output = boundedToolOutput(contentText(value.content));
        tool.output = output.text;
        tool.truncated = output.truncated || undefined;
        tool.status = value.isError ? "error" : "complete";
        const endedAt = messageTimestamp(value.timestamp);
        if (endedAt !== undefined && tool.startedAt !== undefined) tool.durationMs = Math.max(0, endedAt - tool.startedAt);
        tool.diff = buildToolDiff(tool.name, tool.arguments, value.details) ?? tool.diff;
      }
      continue;
    }
    if (role === "user") {
      const images = contentImages(value.content);
      const identity = historicalIdentity(value, "history-user");
      result.push({
        ...identity,
        role: "user", text: contentText(value.content), thinking: "",
        timestamp: messageTime(value.timestamp), timestampMs: messageTimestamp(value.timestamp), streaming: false, images, tools: [],
      });
      continue;
    }
    if (role === "assistant") {
      const content = Array.isArray(value.content) ? value.content : [];
      const tools: ToolExecution[] = [];
      for (const item of content) {
        if (!item || typeof item !== "object") continue;
        const part = item as Record<string, unknown>;
        if (part.type === "toolCall") {
          const tool: ToolExecution = {
            id: String(part.id ?? createID("history-tool")), name: String(part.name ?? "tool"),
            arguments: part.arguments, output: "", status: "complete", startedAt: messageTimestamp(value.timestamp),
          };
          tool.diff = buildToolDiff(tool.name, tool.arguments);
          tools.push(tool);
          toolsByID.set(tool.id, tool);
        }
      }
      const identity = historicalIdentity(value, "history-assistant");
      result.push({
        ...identity,
        role: "assistant", text: contentText(value.content), thinking: contentThinking(value.content),
        timestamp: messageTime(value.timestamp), timestampMs: messageTimestamp(value.timestamp), streaming: false,
        error: runtimeErrorText(value.errorMessage), tools,
      });
      continue;
    }
    if (role === "bashExecution") {
      const command = typeof value.command === "string" ? value.command : "";
      const output = typeof value.output === "string" ? value.output : "";
      result.push({
        ...historicalIdentity(value, "history-bash"), role: "system", text: [`$ ${command}`, output].filter(Boolean).join("\n"),
        thinking: "", timestamp: messageTime(value.timestamp), timestampMs: messageTimestamp(value.timestamp), streaming: false,
        error: Number(value.exitCode ?? 0) !== 0 ? `Command exited with code ${String(value.exitCode)}` : undefined, tools: [],
      });
    }
  }
  return result;
}

function liveCompactionMessage(payload: Record<string, unknown>): TimelineMessage | undefined {
  if (payload.aborted === true) return undefined;
  const result = payload.result && typeof payload.result === "object" ? payload.result as Record<string, unknown> : undefined;
  if (!result || typeof result.summary !== "string") return undefined;
  const tokensBefore = tokenCount(result.tokensBefore);
  const timestampMs = Date.now();
  return {
    id: createID("live-compaction"),
    role: "system", text: "", thinking: "",
    timestamp: formatMessageTime(new Date(timestampMs)), timestampMs, streaming: false, tools: [],
    compaction: {
      summary: result.summary,
      tokensBefore,
      estimatedTokensAfter: tokenCount(result.estimatedTokensAfter),
    },
  };
}

function copyMessages(messages: TimelineMessage[]): TimelineMessage[] {
  return messages.map((message) => ({
    ...message,
    images: message.images?.map((image) => ({ ...image })),
    tools: message.tools.map((tool) => ({ ...tool, diff: tool.diff ? { ...tool.diff } : undefined })),
    executionSteps: message.executionSteps?.map((step) => ({
      ...step,
      tools: step.tools?.map((tool) => ({ ...tool, diff: tool.diff ? { ...tool.diff } : undefined })),
    })),
    compaction: message.compaction ? { ...message.compaction } : undefined,
    runNotice: message.runNotice ? { ...message.runNotice } : undefined,
  }));
}

export const useAppStore = defineStore("app", {
  state: () => ({
    sidebarCollapsed: false,
    sidebarWidth: DEFAULT_SIDEBAR_WIDTH,
    inspectorOpen: true,
    inspectorWidth: DEFAULT_INSPECTOR_WIDTH,
    inspectorTab: "changes" as InspectorTab,
    searchOpen: false,
    searchQuery: "",
    newTaskOpen: false,
    settingsOpen: false,
    settingsSection: "general" as SettingsSection,
    aboutOpen: false,
    orphanSessionsOpen: false,
    remoteReconnectOpen: false,
    remoteReconnectThreadId: "",
    remoteReconnectIntent: "start" as RemoteReconnectIntent,
    remoteReconnectBusy: false,
    remoteReconnectError: "",
    remoteReconnectProgress: [] as RemoteReconnectProgressStep[],
    remoteReadyByWorkspace: {} as Record<string, boolean>,
    terminalGenerationByThread: {} as Record<string, number | undefined>,
    settingsError: "",
    branchPanelOpen: false,
    exportDialogOpen: false,
    exportResultPath: "",
    exportResultError: "",
    deleteDialogOpen: false,
    deleteThreadId: "",
    deleteSessionTitle: "",
    deleteSessionError: "",
    deletedRecoveryPath: "",
    deleteHasSession: false,
    bootstrapLoading: true,
    bootstrapError: "",
    runtimeCheckLoading: false,
    catalogLoading: true,
    catalogError: "",
    catalogReady: false,
    sessionSyncLoading: false,
    sessionSyncError: "",
    desktopStateReady: false,
    bootstrap: null as BootstrapState | null,
    workspaces: [] as WorkspaceSummary[],
    workspaceApplications: [] as WorkspaceApplication[],
    workspaceApplicationsLoading: true,
    workspaceApplication: "" as string,
    workspaceApplicationError: "",
    threads: [] as ThreadSummary[],
    activeThreadId: "",
    messagesByThread: {} as Record<string, TimelineMessage[]>,
    searchBodyTextByThread: {} as Record<string, string>,
    searchBodyVersionByThread: {} as Record<string, string>,
    searchBodyLoading: false,
    transcriptEntriesByThread: {} as Record<string, Array<Record<string, unknown>>>,
    transcriptReloadGenerationByThread: {} as Record<string, number>,
    draftsByThread: {} as Record<string, string>,
    attachmentsByThread: {} as Record<string, PreparedImage[]>,
    activeAssistantByThread: {} as Record<string, string>,
    waitingForOutputByThread: {} as Record<string, boolean | undefined>,
    sessionStateByThread: {} as Record<string, PiSessionState>,
    sessionStatsByThread: {} as Record<string, SessionStats>,
    sessionStatsRefreshGenerationByThread: {} as Record<string, number>,
    compactionEstimatesByThread: {} as Record<string, Record<string, number>>,
    latestCompactionEstimateByThread: {} as Record<string, number | undefined>,
    modelsByThread: {} as Record<string, PiModel[]>,
    configuredModels: [] as PiModel[],
    knownRuntimeModels: [] as PiModel[],
    pendingModelByThread: {} as Record<string, PiModel | undefined>,
    modelSelectionGenerationByThread: {} as Record<string, number>,
    modelCatalogError: "",
    thinkingLevelsByThread: {} as Record<string, string[]>,
    thinkingLevelsRefreshGenerationByThread: {} as Record<string, number>,
    commandsByThread: {} as Record<string, SlashCommand[]>,
    queueByThread: {} as Record<string, QueuedMessages>,
    pendingPromptsByThread: {} as Record<string, PendingPrompt[]>,
    retryByThread: {} as Record<string, RetryInfo | undefined>,
    bashMessageByThread: {} as Record<string, string | undefined>,
    bashRunningByThread: {} as Record<string, boolean | undefined>,
    autoRetryEnabledByThread: {} as Record<string, boolean | undefined>,
    repositoryByWorkspace: {} as Record<string, RepositorySnapshot | undefined>,
    repositoryLoadingByWorkspace: {} as Record<string, boolean>,
    repositoryErrorByWorkspace: {} as Record<string, string>,
    repositoryStaleByWorkspace: {} as Record<string, boolean>,
    repositoryRefreshGenerationByWorkspace: {} as Record<string, number>,
    repositoryBranchesByWorkspace: {} as Record<string, GitBranchInventory | undefined>,
    repositoryBranchesLoadingByWorkspace: {} as Record<string, boolean>,
    repositoryBranchesGenerationByWorkspace: {} as Record<string, number>,
    repositoryBranchesErrorByWorkspace: {} as Record<string, string>,
    repositoryDiffByWorkspace: {} as Record<string, RepositoryFileDiff | undefined>,
    repositoryDiffPathByWorkspace: {} as Record<string, string>,
    repositoryDiffLoadingByWorkspace: {} as Record<string, boolean>,
    repositoryDiffGenerationByWorkspace: {} as Record<string, number>,
    repositoryDiffErrorByWorkspace: {} as Record<string, string>,
    repositoryFilePreviewByThread: {} as Record<string, RepositoryFilePreview | undefined>,
    repositoryFileTabsByThread: {} as Record<string, string[]>,
    repositoryFilePreviewPathByThread: {} as Record<string, string>,
    repositoryFilePreviewLineByThread: {} as Record<string, number | undefined>,
    repositoryFilePreviewLoadingByThread: {} as Record<string, boolean>,
    repositoryFilePreviewGenerationByThread: {} as Record<string, number>,
    repositoryFilePreviewErrorByThread: {} as Record<string, string>,
    workspaceTrustUpdatingPath: "",
    workspaceTrustError: "",
    streamingBehavior: "steer" as StreamingBehavior,
    appearance: "light" as Appearance,
    language: "zh-CN" as Language,
    interfaceFont: "default" as InterfaceFont,
    interfaceFontSize: 14,
    piProcessOrder: [] as string[],
    extensionRequestByThread: {} as Record<string, ExtensionUIRequest | undefined>,
    extensionStatusesByThread: {} as Record<string, Record<string, string>>,
    extensionWidgetsByThread: {} as Record<string, Record<string, ExtensionWidget>>,
    extensionTitleByThread: {} as Record<string, string | undefined>,
    transcriptStateByThread: {} as Record<string, "idle" | "loading" | "loaded" | "error">,
    sessionBranchesByThread: {} as Record<string, SessionBranches | undefined>,
    sessionBranchesErrorByThread: {} as Record<string, string>,
    sessionOperationByThread: {} as Record<string, string | undefined>,
    proxyEnabled: false,
    proxyURL: "socks5://127.0.0.1:10800",
    offlineMode: true,
    notificationsEnabled: true,
    updateChecksEnabled: true,
    closeToTray: true,
    updateCheckLoading: false,
    updateCheckResult: null as import("../../bindings/pi-desk/internal/domain").UpdateCheckResult | null,
  }),
  getters: {
    activeThread(state): ThreadSummary | undefined {
      return state.threads.find((thread) => thread.id === state.activeThreadId);
    },
    remoteReconnectThread(state): ThreadSummary | undefined {
      return state.threads.find((thread) => thread.id === state.remoteReconnectThreadId);
    },
    activeWorkspaceApplication(state): WorkspaceApplication | undefined {
      return state.workspaceApplications.find((application) => application.id === state.workspaceApplication)
        ?? state.workspaceApplications.find((application) => application.id === "file-manager")
        ?? state.workspaceApplications[0];
    },
    activeMessages(state): TimelineMessage[] {
      return state.messagesByThread[state.activeThreadId] ?? [];
    },
    activeWaitingForOutput(state): boolean {
      return Boolean(state.waitingForOutputByThread[state.activeThreadId]);
    },
    activeDraft(state): string {
      return state.draftsByThread[state.activeThreadId] ?? "";
    },
    activeAttachments(state): PreparedImage[] {
      return state.attachmentsByThread[state.activeThreadId] ?? [];
    },
    activeSessionState(state): PiSessionState | undefined {
      return state.sessionStateByThread[state.activeThreadId];
    },
    activeSessionStats(state): SessionStats | undefined {
      return state.sessionStatsByThread[state.activeThreadId];
    },
    activeModels(state): PiModel[] {
      const thread = state.threads.find((candidate) => candidate.id === state.activeThreadId);
      if (!thread) return [];
      const current = state.sessionStateByThread[state.activeThreadId]?.model;
      return mergeModels(
        thread.started ? state.modelsByThread[state.activeThreadId] ?? [] : state.knownRuntimeModels,
        state.configuredModels,
        current ? [current] : [],
      );
    },
    activeModelPending(state): boolean {
      return Boolean(state.pendingModelByThread[state.activeThreadId]);
    },
    activeThinkingLevels(state): string[] {
      return state.thinkingLevelsByThread[state.activeThreadId] ?? [];
    },
    activeCommands(state): SlashCommand[] {
      return state.commandsByThread[state.activeThreadId] ?? [];
    },
    activeQueue(state): QueuedMessages {
      return state.queueByThread[state.activeThreadId] ?? { steering: [], followUp: [] };
    },
    activePendingPrompts(state): PendingPrompt[] {
      return state.pendingPromptsByThread[state.activeThreadId] ?? [];
    },
    activeRetry(state): RetryInfo | undefined {
      return state.retryByThread[state.activeThreadId];
    },
    activeAutoRetryEnabled(state): boolean {
      return state.autoRetryEnabledByThread[state.activeThreadId] ?? true;
    },
    activeBashRunning(state): boolean {
      return Boolean(state.bashRunningByThread[state.activeThreadId]);
    },
    activeExtensionStatuses(state): ExtensionStatus[] {
      return Object.entries(state.extensionStatusesByThread[state.activeThreadId] ?? {})
        .filter(([key, text]) => key.toLocaleLowerCase() !== "mcp" && !/^MCP\s*:/i.test(text))
        .map(([key, text]) => ({ key, text }));
    },
    activeExtensionWidgets(state): ExtensionWidget[] {
      return Object.values(state.extensionWidgetsByThread[state.activeThreadId] ?? {});
    },
    activeExtensionTitle(state): string {
      return state.extensionTitleByThread[state.activeThreadId] ?? "";
    },
    activeRepository(state): RepositorySnapshot | undefined {
      const thread = state.threads.find((item) => item.id === state.activeThreadId);
      return thread ? state.repositoryByWorkspace[repositoryKey(thread)] : undefined;
    },
    activeRepositoryLoading(state): boolean {
      const thread = state.threads.find((item) => item.id === state.activeThreadId);
      return thread ? Boolean(state.repositoryLoadingByWorkspace[repositoryKey(thread)]) : false;
    },
    activeRepositoryError(state): string {
      const thread = state.threads.find((item) => item.id === state.activeThreadId);
      return thread ? state.repositoryErrorByWorkspace[repositoryKey(thread)] ?? "" : "";
    },
    activeRepositoryStale(state): boolean {
      const thread = state.threads.find((item) => item.id === state.activeThreadId);
      return thread ? Boolean(state.repositoryStaleByWorkspace[repositoryKey(thread)]) : false;
    },
    activeRepositoryBranches(state): GitBranchInventory | undefined {
      const thread = state.threads.find((item) => item.id === state.activeThreadId);
      return thread ? state.repositoryBranchesByWorkspace[repositoryKey(thread)] : undefined;
    },
    activeRepositoryBranchesLoading(state): boolean {
      const thread = state.threads.find((item) => item.id === state.activeThreadId);
      return thread ? Boolean(state.repositoryBranchesLoadingByWorkspace[repositoryKey(thread)]) : false;
    },
    activeRepositoryBranchesError(state): string {
      const thread = state.threads.find((item) => item.id === state.activeThreadId);
      return thread ? state.repositoryBranchesErrorByWorkspace[repositoryKey(thread)] ?? "" : "";
    },
    activeRepositoryDiff(state): RepositoryFileDiff | undefined {
      const thread = state.threads.find((item) => item.id === state.activeThreadId);
      return thread ? state.repositoryDiffByWorkspace[repositoryKey(thread)] : undefined;
    },
    activeRepositoryDiffPath(state): string {
      const thread = state.threads.find((item) => item.id === state.activeThreadId);
      return thread ? state.repositoryDiffPathByWorkspace[repositoryKey(thread)] ?? "" : "";
    },
    activeRepositoryDiffLoading(state): boolean {
      const thread = state.threads.find((item) => item.id === state.activeThreadId);
      return thread ? Boolean(state.repositoryDiffLoadingByWorkspace[repositoryKey(thread)]) : false;
    },
    activeRepositoryDiffError(state): string {
      const thread = state.threads.find((item) => item.id === state.activeThreadId);
      return thread ? state.repositoryDiffErrorByWorkspace[repositoryKey(thread)] ?? "" : "";
    },
    activeRepositoryFilePreview(state): RepositoryFilePreview | undefined {
      return state.repositoryFilePreviewByThread[state.activeThreadId];
    },
    activeRepositoryFileTabs(state): string[] {
      return state.repositoryFileTabsByThread[state.activeThreadId] ?? [];
    },
    activeRepositoryFilePreviewPath(state): string {
      return state.repositoryFilePreviewPathByThread[state.activeThreadId] ?? "";
    },
    activeRepositoryFilePreviewLine(state): number | undefined {
      return state.repositoryFilePreviewLineByThread[state.activeThreadId];
    },
    activeRepositoryFilePreviewLoading(state): boolean {
      return Boolean(state.repositoryFilePreviewLoadingByThread[state.activeThreadId]);
    },
    activeRepositoryFilePreviewError(state): string {
      return state.repositoryFilePreviewErrorByThread[state.activeThreadId] ?? "";
    },
    activeSessionBranches(state): SessionBranches | undefined {
      return state.sessionBranchesByThread[state.activeThreadId];
    },
    activeSessionBranchesError(state): string {
      return state.sessionBranchesErrorByThread[state.activeThreadId] ?? "";
    },
    activeSessionOperation(state): string {
      return state.sessionOperationByThread[state.activeThreadId] ?? "";
    },
    activeWorkspaceTrustUpdating(state): boolean {
      const thread = state.threads.find((item) => item.id === state.activeThreadId);
      return Boolean(thread && state.workspaceTrustUpdatingPath === pathKey(thread.workspacePath));
    },
    activeWorkspaceTrustBusy(state): boolean {
      const thread = state.threads.find((item) => item.id === state.activeThreadId);
      if (!thread) return false;
      const workspaceKey = pathKey(thread.workspacePath);
      return state.threads.some((item) => pathKey(item.workspacePath) === workspaceKey
        && (item.status === "running" || item.status === "starting" || Boolean(state.bashRunningByThread[item.id])));
    },
    piMaintenanceBusy(state): boolean {
      return state.threads.some((thread) => thread.started
        && (thread.status === "running" || thread.status === "starting" || Boolean(state.bashRunningByThread[thread.id])));
    },
    filteredThreads(state): ThreadSummary[] {
      const query = state.searchQuery.trim().toLocaleLowerCase();
      const threads = query ? state.threads.filter((thread) => [
        thread.title,
        thread.firstMessage ?? "",
        thread.workspace,
        thread.workspacePath,
        state.searchBodyTextByThread[thread.id] ?? "",
        timelineSearchText(state.messagesByThread[thread.id] ?? []),
      ].join("\n").toLocaleLowerCase().includes(query)) : state.threads;
      return [...threads].sort((left, right) => {
        const leftTime = Date.parse(left.modifiedAt || left.createdAt || "");
        const rightTime = Date.parse(right.modifiedAt || right.createdAt || "");
        const normalizedLeft = Number.isFinite(leftTime) ? leftTime : 0;
        const normalizedRight = Number.isFinite(rightTime) ? rightTime : 0;
        return normalizedRight - normalizedLeft;
      });
    },
  },
  actions: {
    async initialize() {
      if (!unsubscribePiEvents) {
        unsubscribePiEvents = onPiEvent((event) => this.handlePiEvent(event));
      }
      if (!unsubscribeTerminalEvents) {
        unsubscribeTerminalEvents = onTerminalEvent((event) => this.handleTerminalEvent(event));
      }
      const bootstrap = this.loadBootstrapState();
      const catalog = this.loadCatalog();
      const models = this.refreshConfiguredModels();
      await bootstrap;
      if (this.bootstrap && !this.bootstrapError) void this.checkRuntime();
      await Promise.all([catalog, models]);
      if (this.updateChecksEnabled) void this.checkForUpdates();
    },
    async checkRuntime() {
      if (!this.bootstrap || this.runtimeCheckLoading) return;
      this.runtimeCheckLoading = true;
      if (!this.threads.some((thread) => thread.started)) {
        this.bootstrap.runtime = {
          ...this.bootstrap.runtime,
          state: RuntimeState.RuntimeChecking,
          message: "Checking Pi runtime",
        };
      }
      try {
        const status = await checkRuntimeStatus();
        this.bootstrap.runtime = status.state !== RuntimeState.RuntimeReady && this.threads.some((thread) => thread.started)
          ? { ...status, state: RuntimeState.RuntimeReady, message: "Pi RPC session is running" }
          : status;
      } catch (error) {
        this.bootstrap.runtime = this.threads.some((thread) => thread.started)
          ? { ...this.bootstrap.runtime, state: RuntimeState.RuntimeReady, message: "Pi RPC session is running" }
          : { state: RuntimeState.RuntimeError, message: errorMessage(error) || "Pi runtime check failed" };
      } finally {
        this.runtimeCheckLoading = false;
      }
    },
    async checkForUpdates() {
      if (this.updateCheckLoading) return;
      this.updateCheckLoading = true;
      try {
        this.updateCheckResult = await checkForUpdates();
      } catch (error) {
        this.updateCheckResult = {
          status: "error", currentVersion: this.bootstrap?.appVersion || "unknown", checkedAt: new Date().toISOString(), message: errorMessage(error),
        };
      } finally {
        this.updateCheckLoading = false;
      }
    },
    toggleSidebar() {
      this.sidebarCollapsed = !this.sidebarCollapsed;
      this.scheduleDesktopStateSave();
    },
    setSidebarWidth(width: number, persist = false) {
      this.sidebarWidth = Math.min(MAX_SIDEBAR_WIDTH, Math.max(MIN_SIDEBAR_WIDTH, Math.round(width)));
      if (persist) this.scheduleDesktopStateSave();
    },
    appearanceChanged() {
      this.scheduleDesktopStateSave();
    },
    languageChanged() {
      setAppLanguage(this.language);
      this.scheduleDesktopStateSave();
    },
    toggleInspector(tab?: InspectorTab) {
      if (tab) {
        this.inspectorTab = tab;
        this.inspectorOpen = true;
        this.scheduleDesktopStateSave();
        return;
      }
      this.inspectorOpen = !this.inspectorOpen;
      if (this.inspectorOpen && (this.inspectorTab === "changes" || this.inspectorTab === "context")) {
        void this.refreshActiveRepository();
      }
      this.scheduleDesktopStateSave();
    },
    setInspectorWidth(width: number, persist = false) {
      this.inspectorWidth = Math.min(MAX_INSPECTOR_WIDTH, Math.max(MIN_INSPECTOR_WIDTH, Math.round(width)));
      if (persist) this.scheduleDesktopStateSave();
    },
    setInspectorTab(tab: InspectorTab) {
      this.inspectorTab = tab;
      if (tab === "changes" || tab === "context") void this.refreshActiveRepository();
      this.scheduleDesktopStateSave();
    },
    toggleSearch() {
      this.searchOpen = !this.searchOpen;
      if (!this.searchOpen) this.searchQuery = "";
    },
    async loadSessionSearchBodies() {
      if (this.searchBodyLoading || !this.searchQuery.trim()) return;
      this.searchBodyLoading = true;
      // ponytail: Move this cache to the host only if very large catalogs make first body search measurably slow.
      try {
        for (const thread of this.threads) {
          if (!thread.sessionFile || this.searchBodyVersionByThread[thread.id] === sessionSearchVersion(thread)) continue;
          try {
            const snapshot = await catalogService.getSessionSnapshot(thread.sessionFile);
            const messages = (snapshot.messages as Array<Record<string, unknown>> | null) ?? [];
            this.searchBodyTextByThread[thread.id] = snapshotSearchText(messages);
            if (Number.isInteger(snapshot.messageCount) && snapshot.messageCount >= 0) thread.messageCount = snapshot.messageCount;
            this.searchBodyVersionByThread[thread.id] = sessionSearchVersion(thread);
          } catch {
            // One unreadable session must not prevent the remaining sessions from being searched.
          }
        }
      } finally {
        this.searchBodyLoading = false;
      }
    },
    selectThread(threadId: string) {
      if (this.threads.some((thread) => thread.id === threadId)) {
        this.branchPanelOpen = false;
        this.activeThreadId = threadId;
        if (this.activeThread) this.activeThread.unread = false;
        if (this.activeThread?.sessionFile) void this.loadThreadTranscript(threadId);
        const workspace = this.workspaces.find((item) => item.id === this.activeThread?.workspaceId);
        if (this.catalogReady && this.activeThread && workspace?.kind !== "ssh") void this.startThreadInBackground(threadId);
        this.scheduleDesktopStateSave();
      }
    },
    openNewTask() {
      this.newTaskOpen = true;
    },
    async pickWorkspace(initialPath = ""): Promise<string> {
      return catalogService.pickWorkspace(initialPath || this.bootstrap?.workingDirectory);
    },
    async openWorkspace(id: string) {
      await catalogService.openWorkspace(id);
    },
    async renameWorkspace(id: string, name: string) {
      const index = this.workspaces.findIndex((workspace) => workspace.id === id);
      if (index < 0) {
        throw new Error("Workspace not found");
      }
      const renamed = await catalogService.renameWorkspace(id, name);
      this.workspaces[index] = { ...renamed, trust: renamed.trust as "approve" | "deny" };
      this.scheduleDesktopStateSave();
    },
    async openActiveWorkspaceWith(applicationId = ""): Promise<boolean> {
      this.workspaceApplicationError = "";
      const active = this.activeThread;
      const application = this.workspaceApplications.find((item) => item.id === applicationId)
        ?? (!applicationId ? this.activeWorkspaceApplication : undefined);
      if (!active || !application) {
        this.workspaceApplicationError = tr("topbar.noApplication");
        return false;
      }
      if (active.trust !== "approve") {
        this.workspaceApplicationError = tr("topbar.trustToOpen");
        return false;
      }
      const workspace = this.workspaces.find((item) => active.workspaceId ? item.id === active.workspaceId : pathKey(item.path) === pathKey(active.workspacePath));
      if (!workspace || workspace.discovered || workspace.kind === "ssh") {
        this.workspaceApplicationError = tr("topbar.workspaceUnavailable");
        return false;
      }
      this.workspaceApplication = application.id;
      this.scheduleDesktopStateSave();
      try {
        await catalogService.openWorkspaceWith(workspace.id, application.id);
        return true;
      } catch (error) {
        this.workspaceApplicationError = errorMessage(error);
        return false;
      }
    },
    remoteTargetEpoch(targetID: string): number {
      return remoteTargetProjectionEpochByID.get(targetID) ?? 0;
    },
    remoteWorkspaceHasConnection(workspaceID: string): boolean {
      return this.remoteReadyByWorkspace[workspaceID] === true
        || this.threads.some((thread) => thread.workspaceId === workspaceID && thread.started);
    },
    assertRemoteTargetEpoch(targetID: string, epoch: number) {
      if (this.remoteTargetEpoch(targetID) !== epoch) throw new Error("REMOTE_DISCONNECTED: remote target operation was revoked");
    },
    forgetRemoteTargetEpoch(targetID: string) {
      remoteTargetProjectionEpochByID.delete(targetID);
    },
    async stopRemoteTargetThreads(targetID: string): Promise<string> {
      this.markRemoteTargetStale(targetID);
      const reconnectThread = this.remoteReconnectThread;
      if (reconnectThread && this.remoteWorkspaceForThread(reconnectThread)?.targetId === targetID) {
        this.remoteReconnectOpen = false;
        this.remoteReconnectThreadId = "";
        this.remoteReconnectError = "";
      }
      const targetWorkspaces = this.workspaces.filter((item) => item.kind === "ssh" && item.targetId === targetID);
      const workspaceIDs = new Set(targetWorkspaces.map((item) => item.id));
      const threads = this.threads.filter((thread) => Boolean(thread.workspaceId && workspaceIDs.has(thread.workspaceId)));
      let stopFailure = "";
      for (const thread of threads) {
        await waitForPiStart(thread.id);
        if (thread.started && !await this.stopThread(thread.id)) stopFailure ||= `Unable to close ${thread.title}`;
      }
      return stopFailure;
    },
    async disconnectRemoteTarget(targetID: string) {
      const stopFailure = await this.stopRemoteTargetThreads(targetID);
      await remoteWorkspaceService.disconnect(targetID);
      if (stopFailure) throw new Error(stopFailure);
    },
    async disconnectRemoteWorkspace(id: string) {
      const workspace = this.workspaces.find((item) => item.id === id);
      if (!workspace?.targetId || workspace.kind !== "ssh") throw new Error("Remote workspace not found");
      // A workspace may own multiple remote Pi sessions. Stop every one before
      // revoking the shared target connection and its helper runtime.
      await this.disconnectRemoteTarget(workspace.targetId);
    },
    async removeWorkspace(id: string) {
      const workspace = this.workspaces.find((item) => item.id === id);
      if (!workspace) throw new Error("Workspace not found");
      const removedThreads = this.threads.filter((thread) => workspace.kind === "ssh"
        ? thread.workspaceId === workspace.id
        : pathKey(thread.workspacePath) === pathKey(workspace.path));
      let stopFailure = "";
      if (workspace.targetId) {
        stopFailure = await this.stopRemoteTargetThreads(workspace.targetId);
      } else {
        for (const thread of removedThreads) {
          await waitForPiStart(thread.id);
          if (thread.started && !await this.stopThread(thread.id)) stopFailure ||= `Unable to close ${thread.title}`;
        }
      }
      if (stopFailure && workspace.targetId) {
        await remoteWorkspaceService.disconnect(workspace.targetId);
        throw new Error(stopFailure);
      }
      if (stopFailure) throw new Error(stopFailure);
      await catalogService.removeWorkspace(id);
      for (const thread of removedThreads) this.removeThreadState(thread.id);
      this.workspaces = this.workspaces.filter((item) => item.id !== id);
      delete this.remoteReadyByWorkspace[id];
      this.scheduleDesktopStateSave();
    },
    async setActiveWorkspaceTrust(trust: "approve" | "deny"): Promise<boolean> {
      const active = this.activeThread;
      if (!active || active.trust === trust || this.workspaceTrustUpdatingPath) return false;
      this.workspaceTrustError = "";
      const workspace = this.workspaces.find((item) => item.id === active.workspaceId);
      if (workspace?.kind === "ssh") {
        this.workspaceTrustError = "Disconnect or remove the SSH workspace to revoke remote trust.";
        return false;
      }
      const workspaceKey = pathKey(active.workspacePath);
      const affected = this.threads.filter((thread) => pathKey(thread.workspacePath) === workspaceKey);
      if (affected.some((thread) => thread.status === "running" || thread.status === "starting" || this.bashRunningByThread[thread.id])) {
        return false;
      }
      this.workspaceTrustUpdatingPath = workspaceKey;
      this.workspaceTrustError = "";
      const runningIDs = affected.filter((thread) => thread.started).map((thread) => thread.id);
      const stoppedIDs: string[] = [];
      try {
        for (const threadID of runningIDs) {
          if (!await this.stopThread(threadID)) throw new Error("Unable to restart every Pi process in this workspace.");
          stoppedIDs.push(threadID);
        }
        const persisted = await catalogService.addWorkspace(active.workspacePath, trust);
        const normalized: WorkspaceSummary = { ...persisted, trust: persisted.trust as "approve" | "deny", discovered: false };
        const workspaceIndex = this.workspaces.findIndex((workspace) => pathKey(workspace.path) === workspaceKey);
        if (workspaceIndex >= 0) this.workspaces[workspaceIndex] = normalized;
        else this.workspaces.unshift(normalized);
        for (const thread of affected) thread.trust = trust;
        this.scheduleDesktopStateSave();
        if (active.id === this.activeThreadId) void this.refreshActiveRepository();
        for (const threadID of runningIDs) this.startThreadInBackground(threadID);
        return true;
      } catch (error) {
        this.workspaceTrustError = errorMessage(error);
        for (const threadID of stoppedIDs) this.startThreadInBackground(threadID);
        return false;
      } finally {
        this.workspaceTrustUpdatingPath = "";
      }
    },
    recordRemoteWorkspace(workspace: HostWorkspaceSummary, ready = false): WorkspaceSummary {
      if (!workspace.id || workspace.kind !== "ssh" || !workspace.targetId || !workspace.remoteRoot || !["approve", "deny"].includes(workspace.trust)) {
        throw new Error("Remote workspace identity is invalid");
      }
      const normalized: WorkspaceSummary = {
        id: workspace.id, name: workspace.name, path: "", kind: "ssh", targetId: workspace.targetId,
        remoteRoot: workspace.remoteRoot, trust: workspace.trust as "approve" | "deny", addedAt: workspace.addedAt, lastOpenedAt: workspace.lastOpenedAt,
        discovered: false,
      };
      this.remoteReadyByWorkspace[normalized.id] = ready && normalized.trust === "approve";
      const existingIndex = this.workspaces.findIndex((item) => item.id === normalized.id);
      if (existingIndex >= 0) this.workspaces[existingIndex] = normalized;
      else this.workspaces.unshift(normalized);
      return normalized;
    },
    async createRemoteThread(workspace: HostWorkspaceSummary) {
      if (workspace.trust !== "approve") throw new Error("Remote workspace is not ready");
      const normalized = this.recordRemoteWorkspace(workspace, true);
      const threadID = createID("thread");
      const thread: ThreadSummary = {
        id: threadID, title: "New task", workspace: normalized.name,
        workspaceId: normalized.id, workspacePath: "", trust: "approve",
        status: "idle", started: false, generation: 0,
        createdAt: nowISO(), modifiedAt: nowISO(), unread: false,
      };
      this.threads.unshift(thread);
      this.messagesByThread[thread.id] = [];
      this.draftsByThread[thread.id] = "";
      this.activeThreadId = thread.id;
      this.newTaskOpen = false;
      this.scheduleDesktopStateSave();
    },
    async createRemoteTaskInWorkspace(workspaceID: string) {
      const current = this.workspaces.find((workspace) => workspace.id === workspaceID);
      if (!current || current.kind !== "ssh" || !current.targetId || !current.remoteRoot || current.trust !== "approve") {
        throw new Error("Remote workspace is not ready");
      }
      let workspace: HostWorkspaceSummary = {
        id: current.id, name: current.name, path: "", kind: "ssh", targetId: current.targetId,
        remoteRoot: current.remoteRoot, trust: "approve", addedAt: current.addedAt ?? new Date().toISOString(), lastOpenedAt: current.lastOpenedAt ?? new Date().toISOString(),
      };
      if (!this.remoteReadyByWorkspace[current.id]) {
        workspace = await remoteWorkspaceService.resume(current.id);
        this.recordRemoteWorkspace(workspace, true);
      }
      await this.createRemoteThread(workspace);
    },
    async createThread(path: string, trust: "approve" | "deny") {
      const workspacePath = path.trim();
      if (!workspacePath) throw new Error("Choose a workspace folder");
      await this.refreshConfiguredModels();
      const registered = this.workspaces.find((workspace) => pathKey(workspace.path) === pathKey(workspacePath));
      if (registered && registered.trust !== trust) {
        throw new Error("Change access from the control below an existing conversation before creating another task in this workspace.");
      }
      const persisted = await catalogService.addWorkspace(workspacePath, trust);
      const normalized: WorkspaceSummary = { ...persisted, trust: persisted.trust as "approve" | "deny", discovered: false };
      const existingIndex = this.workspaces.findIndex((item) => item.id === normalized.id || pathKey(item.path) === pathKey(normalized.path));
      if (existingIndex >= 0) this.workspaces[existingIndex] = normalized;
      else this.workspaces.unshift(normalized);
      for (const existing of this.threads) {
        if (pathKey(existing.workspacePath) === pathKey(normalized.path)) existing.trust = normalized.trust;
      }
      const threadID = createID("thread");
      const thread: ThreadSummary = {
        id: threadID,
        title: "New task",
        workspace: normalized.name,
        workspaceId: normalized.id,
        workspacePath: normalized.path,
        trust: normalized.trust,
        status: "idle",
        started: false,
        generation: 0,
        createdAt: nowISO(),
        modifiedAt: nowISO(),
        unread: false,
      };
      this.threads.unshift(thread);
      this.messagesByThread[thread.id] = [];
      this.draftsByThread[thread.id] = "";
      this.activeThreadId = thread.id;
      this.newTaskOpen = false;
      this.scheduleDesktopStateSave();
    },
    applySessionSnapshot(thread: ThreadSummary, snapshot: SessionSnapshot, prepend = false) {
      const page = (snapshot.messages as Array<Record<string, unknown>> | null) ?? [];
      const existingEntries = this.transcriptEntriesByThread[thread.id] ?? [];
      const entries = prepend ? [...page, ...existingEntries] : page;
      this.transcriptEntriesByThread[thread.id] = entries;
      const liveMessages = prepend
        ? (this.messagesByThread[thread.id] ?? []).filter((message) => !message.id.startsWith("history-"))
        : [];
      const historical = historicalMessages(entries, this.compactionEstimatesByThread[thread.id]);
      for (const message of historical) {
        if (message.compaction?.estimatedTokensAfter !== undefined) {
          this.applyCompactionEstimate(thread.id, message.compaction, false);
        }
      }
      this.messagesByThread[thread.id] = [...historical, ...liveMessages];
      if (Number.isInteger(snapshot.messageCount) && snapshot.messageCount >= 0) thread.messageCount = snapshot.messageCount;
      this.searchBodyTextByThread[thread.id] = snapshotSearchText(entries);
      this.searchBodyVersionByThread[thread.id] = sessionSearchVersion(thread);
      if (snapshot.model && !this.pendingModelByThread[thread.id] && !thread.started) {
        this.sessionStateByThread[thread.id] = {
          ...(this.sessionStateByThread[thread.id] ?? {}),
          model: { provider: snapshot.model.provider, id: snapshot.model.id },
        };
      }
    },
    async loadThreadTranscript(threadId: string) {
      const thread = this.threads.find((item) => item.id === threadId);
      const state = this.transcriptStateByThread[threadId];
      if (!thread?.sessionFile || state === "loading" || state === "loaded") return;
      this.transcriptStateByThread[threadId] = "loading";
      try {
        const snapshot = await catalogService.getSessionSnapshot(thread.sessionFile);
        this.applySessionSnapshot(thread, snapshot);
        this.transcriptStateByThread[threadId] = "loaded";
      } catch (error) {
        thread.status = "attention";
        thread.error = errorMessage(error);
        this.messagesByThread[thread.id] = [];
        this.transcriptEntriesByThread[thread.id] = [];
        this.appendSystem(thread.id, `Unable to open session: ${thread.error}`, thread.error);
        this.transcriptStateByThread[threadId] = "error";
      }
    },
    updateDraft(value: string) {
      if (this.activeThreadId) {
        this.draftsByThread[this.activeThreadId] = value;
        this.scheduleDesktopStateSave();
      }
    },
    insertFileMention(path: string, directory = false) {
      if (!this.activeThreadId) return;
      const current = this.activeDraft;
      const separator = current && !/\s$/.test(current) ? " " : "";
      this.updateDraft(`${current}${separator}${formatFileMention(path, directory)} `);
    },
    async refreshActiveRepository(threadID?: string) {
      const thread = this.threads.find((item) => item.id === (threadID || this.activeThreadId));
      if (!thread) return;
      const key = repositoryKey(thread);
      if (thread.trust !== "approve") {
        this.repositoryRefreshGenerationByWorkspace[key] = (this.repositoryRefreshGenerationByWorkspace[key] ?? 0) + 1;
        this.repositoryLoadingByWorkspace[key] = false;
        this.repositoryByWorkspace[key] = undefined;
        this.repositoryErrorByWorkspace[key] = "Workspace access is disabled";
        this.repositoryStaleByWorkspace[key] = true;
        return;
      }
      if (this.repositoryLoadingByWorkspace[key]) return;
      const generation = (this.repositoryRefreshGenerationByWorkspace[key] ?? 0) + 1;
      this.repositoryRefreshGenerationByWorkspace[key] = generation;
      this.repositoryLoadingByWorkspace[key] = true;
      this.repositoryErrorByWorkspace[key] = "";
      try {
        const snapshot = await repositoryService.snapshot(repositoryReference(thread));
        if (this.repositoryRefreshGenerationByWorkspace[key] !== generation) return;
        this.repositoryByWorkspace[key] = snapshot;
        this.repositoryStaleByWorkspace[key] = false;
      } catch (error) {
        if (this.repositoryRefreshGenerationByWorkspace[key] !== generation) return;
        this.repositoryErrorByWorkspace[key] = this.remoteFailureMessage(thread.id, error);
        this.repositoryStaleByWorkspace[key] = true;
      } finally {
        if (this.repositoryRefreshGenerationByWorkspace[key] === generation) {
          this.repositoryLoadingByWorkspace[key] = false;
        }
      }
    },
    async openRepositoryDiff(path: string) {
      const thread = this.activeThread;
      if (!thread || thread.trust !== "approve") return;
      const workingPath = repositoryReference(thread);
      const key = repositoryKey(thread);
      const generation = (this.repositoryDiffGenerationByWorkspace[key] ?? 0) + 1;
      this.repositoryDiffGenerationByWorkspace[key] = generation;
      this.repositoryDiffPathByWorkspace[key] = path;
      this.repositoryDiffByWorkspace[key] = undefined;
      this.repositoryDiffLoadingByWorkspace[key] = true;
      this.repositoryDiffErrorByWorkspace[key] = "";
      try {
        const diff = await repositoryService.diff(workingPath, path);
        if (this.repositoryDiffGenerationByWorkspace[key] === generation) this.repositoryDiffByWorkspace[key] = diff;
      } catch (error) {
        if (this.repositoryDiffGenerationByWorkspace[key] === generation) {
          this.repositoryDiffErrorByWorkspace[key] = this.remoteFailureMessage(thread.id, error);
        }
      } finally {
        if (this.repositoryDiffGenerationByWorkspace[key] === generation) this.repositoryDiffLoadingByWorkspace[key] = false;
      }
    },
    async openRepositoryFilePreview(path: string, line?: number) {
      const thread = this.activeThread;
      if (!thread || thread.trust !== "approve") return;
      const threadId = thread.id;
      const workingPath = repositoryReference(thread);
      const generation = (this.repositoryFilePreviewGenerationByThread[threadId] ?? 0) + 1;
      this.repositoryFilePreviewGenerationByThread[threadId] = generation;
      this.inspectorOpen = true;
      this.scheduleDesktopStateSave();
      this.repositoryFilePreviewPathByThread[threadId] = path;
      const tabs = this.repositoryFileTabsByThread[threadId] ?? (this.repositoryFileTabsByThread[threadId] = []);
      if (!tabs.includes(path)) {
        tabs.push(path);
        if (tabs.length > 20) tabs.shift();
      }
      this.repositoryFilePreviewLineByThread[threadId] = line;
      this.repositoryFilePreviewByThread[threadId] = undefined;
      this.repositoryFilePreviewLoadingByThread[threadId] = true;
      this.repositoryFilePreviewErrorByThread[threadId] = "";
      try {
        const preview = await repositoryService.previewFile(workingPath, path);
        if (this.repositoryFilePreviewGenerationByThread[threadId] === generation) this.repositoryFilePreviewByThread[threadId] = preview;
      } catch (error) {
        if (this.repositoryFilePreviewGenerationByThread[threadId] === generation) {
          this.repositoryFilePreviewErrorByThread[threadId] = this.remoteFailureMessage(thread.id, error);
        }
      } finally {
        if (this.repositoryFilePreviewGenerationByThread[threadId] === generation) this.repositoryFilePreviewLoadingByThread[threadId] = false;
      }
    },
    closeRepositoryFilePreview(path?: string) {
      const threadId = this.activeThreadId;
      if (!threadId) return;
      const activePath = this.repositoryFilePreviewPathByThread[threadId] ?? "";
      const closingPath = path ?? activePath;
      const tabs = this.repositoryFileTabsByThread[threadId] ?? [];
      const index = tabs.indexOf(closingPath);
      if (index >= 0) tabs.splice(index, 1);
      if (closingPath !== activePath) return;
      this.repositoryFilePreviewGenerationByThread[threadId] = (this.repositoryFilePreviewGenerationByThread[threadId] ?? 0) + 1;
      this.repositoryFilePreviewPathByThread[threadId] = "";
      this.repositoryFilePreviewLineByThread[threadId] = undefined;
      this.repositoryFilePreviewByThread[threadId] = undefined;
      this.repositoryFilePreviewLoadingByThread[threadId] = false;
      this.repositoryFilePreviewErrorByThread[threadId] = "";
      const next = tabs[Math.min(index, tabs.length - 1)];
      if (next) void this.openRepositoryFilePreview(next);
    },
    async openPreviewedRepositoryFile(reveal = false) {
      const thread = this.activeThread;
      const path = this.activeRepositoryFilePreviewPath;
      if (!thread || !path || thread.trust !== "approve" || this.remoteWorkspaceForThread(thread)) return;
      try {
        if (reveal) await repositoryService.revealFile(thread.workspacePath, path);
        else await repositoryService.openFile(thread.workspacePath, path);
      } catch (error) {
        this.repositoryFilePreviewErrorByThread[thread.id] = errorMessage(error);
      }
    },
    async refreshActiveRepositoryBranches() {
      const thread = this.activeThread;
      if (!thread || thread.trust !== "approve") return;
      const workingPath = repositoryReference(thread);
      const key = repositoryKey(thread);
      if (this.repositoryBranchesLoadingByWorkspace[key]) return;
      const generation = (this.repositoryBranchesGenerationByWorkspace[key] ?? 0) + 1;
      this.repositoryBranchesGenerationByWorkspace[key] = generation;
      this.repositoryBranchesLoadingByWorkspace[key] = true;
      this.repositoryBranchesErrorByWorkspace[key] = "";
      try {
        const branches = await repositoryService.branches(workingPath);
        if (this.repositoryBranchesGenerationByWorkspace[key] === generation) this.repositoryBranchesByWorkspace[key] = branches;
      } catch (error) {
        if (this.repositoryBranchesGenerationByWorkspace[key] === generation) {
          this.repositoryBranchesErrorByWorkspace[key] = this.remoteFailureMessage(thread.id, error);
        }
      } finally {
        if (this.repositoryBranchesGenerationByWorkspace[key] === generation) this.repositoryBranchesLoadingByWorkspace[key] = false;
      }
    },
    closeRepositoryDiff() {
      const thread = this.activeThread;
      if (!thread) return;
      const key = repositoryKey(thread);
      this.repositoryDiffGenerationByWorkspace[key] = (this.repositoryDiffGenerationByWorkspace[key] ?? 0) + 1;
      this.repositoryDiffPathByWorkspace[key] = "";
      this.repositoryDiffByWorkspace[key] = undefined;
      this.repositoryDiffLoadingByWorkspace[key] = false;
      this.repositoryDiffErrorByWorkspace[key] = "";
    },
    async openActiveRepositoryFile(reveal = false) {
      const thread = this.activeThread;
      const path = this.activeRepositoryDiffPath;
      if (!thread || !path || thread.trust !== "approve" || this.remoteWorkspaceForThread(thread)) return;
      const workingPath = thread.workspacePath;
      try {
        if (reveal) await repositoryService.revealFile(workingPath, path);
        else await repositoryService.openFile(workingPath, path);
      } catch (error) {
        this.repositoryDiffErrorByWorkspace[repositoryKey(thread)] = errorMessage(error);
      }
    },
    addActiveAttachments(images: PreparedImage[]) {
      if (!this.activeThreadId || images.length === 0) return;
      const current = this.attachmentsByThread[this.activeThreadId] ?? [];
      this.attachmentsByThread[this.activeThreadId] = [...current, ...images].slice(0, 10);
    },
    removeActiveAttachment(imageId: string) {
      if (!this.activeThreadId) return;
      this.attachmentsByThread[this.activeThreadId] = this.activeAttachments.filter((image) => image.id !== imageId);
    },
    clearActiveAttachments() {
      if (this.activeThreadId) this.attachmentsByThread[this.activeThreadId] = [];
    },
    movePendingPromptToDraft(promptId: string, text: string, images: PreparedImage[]) {
      const threadId = this.activeThreadId;
      const promptIndex = this.activePendingPrompts.findIndex((prompt) => prompt.id === promptId);
      if (!threadId || promptIndex < 0 || (!text.trim() && images.length === 0)) return;

      const currentText = this.activeDraft;
      const currentImages = this.activeAttachments.map((image) => ({ ...image }));
      const queue = [...this.activePendingPrompts];
      if (currentText.trim() || currentImages.length > 0) {
        queue[promptIndex] = {
          ...queue[promptIndex],
          text: currentText.trim(),
          images: currentImages,
          createdAt: nowISO(),
        };
      } else {
        queue.splice(promptIndex, 1);
      }
      this.pendingPromptsByThread[threadId] = queue;
      this.draftsByThread[threadId] = text;
      this.attachmentsByThread[threadId] = images.map((image) => ({ ...image }));
    },
    removePendingPrompt(promptId: string) {
      const threadId = this.activeThreadId;
      if (!threadId) return;
      this.pendingPromptsByThread[threadId] = this.activePendingPrompts.filter((prompt) => prompt.id !== promptId);
    },
    updatePendingPrompt(promptId: string, text: string, images?: PreparedImage[]) {
      const prompt = this.activePendingPrompts.find((candidate) => candidate.id === promptId);
      const nextImages = images?.map((image) => ({ ...image })) ?? prompt?.images ?? [];
      if (!prompt || (!text.trim() && nextImages.length === 0)) return;
      prompt.text = text.trim();
      prompt.images = nextImages;
    },
    async steerPendingPrompt(promptId: string) {
      const thread = this.activeThread;
      if (!thread || thread.status !== "running") return;
      const prompt = this.activePendingPrompts.find((candidate) => candidate.id === promptId);
      if (!prompt) return;
      this.pendingPromptsByThread[thread.id] = this.activePendingPrompts.filter((candidate) => candidate.id !== promptId);
      const delivered = await this.deliverPrompt(thread, prompt.text, prompt.images, "steer", false);
      if (!delivered) this.pendingPromptsByThread[thread.id].unshift(prompt);
    },
    setStreamingBehavior(behavior: StreamingBehavior) {
      this.streamingBehavior = behavior;
      this.scheduleDesktopStateSave();
    },
    preferencesChanged() {
      this.settingsError = "";
      this.scheduleDesktopStateSave();
    },
    openSettings(section: SettingsSection = "general") {
      this.settingsSection = section;
      this.settingsOpen = true;
    },
    closeSettings() {
      this.settingsOpen = false;
      this.scheduleDesktopStateSave();
    },
    openAbout() {
      this.aboutOpen = true;
    },
    closeAbout() {
      this.aboutOpen = false;
    },
    openOrphanSessions() {
      this.orphanSessionsOpen = true;
    },
    closeOrphanSessions() {
      this.orphanSessionsOpen = false;
    },
    async syncAndRestoreSessions() {
      await this.syncLocalSessions();
      this.openOrphanSessions();
    },
    remoteWorkspaceForThread(thread: ThreadSummary): WorkspaceSummary | undefined {
      if (!thread.workspaceId) return undefined;
      const workspace = this.workspaces.find((item) => item.id === thread.workspaceId);
      return workspace?.kind === "ssh" ? workspace : undefined;
    },
    requestRemoteReconnect(thread: ThreadSummary, intent: RemoteReconnectIntent): boolean {
      const workspace = this.remoteWorkspaceForThread(thread);
      if (!workspace || this.remoteReadyByWorkspace[workspace.id]) return false;
      if (this.remoteReconnectOpen || this.remoteReconnectBusy) return true;
      this.remoteReconnectThreadId = thread.id;
      this.remoteReconnectIntent = intent;
      this.remoteReconnectError = "";
      this.remoteReconnectProgress = [];
      this.remoteReconnectOpen = true;
      return true;
    },
    cancelRemoteReconnect() {
      if (this.remoteReconnectBusy) return;
      this.remoteReconnectOpen = false;
      this.remoteReconnectThreadId = "";
      this.remoteReconnectError = "";
      this.remoteReconnectProgress = [];
    },
    async confirmRemoteReconnect() {
      const thread = this.remoteReconnectThread;
      const workspace = thread ? this.remoteWorkspaceForThread(thread) : undefined;
      const targetID = workspace?.targetId;
      if (!thread || !workspace || !targetID || this.remoteReconnectBusy) return;
      const intent = this.remoteReconnectIntent;
      const reconnectThreadID = thread.id;
      this.remoteReconnectBusy = true;
      this.remoteReconnectError = "";
      this.remoteReconnectProgress = remoteReconnectProgressDefinitions.map((step) => ({ ...step }));
      const setProgress = (id: string, status: RemoteReconnectProgressStatus) => {
        this.remoteReconnectProgress = this.remoteReconnectProgress.map((step) => step.id === id ? { ...step, status } : step);
      };
      try {
        if (thread.started && !await this.stopThread(thread.id)) {
          throw new Error("Unable to close the stale remote Pi session");
        }
        setProgress("stop", "complete");
        setProgress("connect", "active");
        await remoteWorkspaceService.resume(workspace.id);
        setProgress("connect", "complete");
        setProgress("restore", "complete");
        if (!this.remoteReconnectOpen || this.remoteReconnectThreadId !== reconnectThreadID) {
          await remoteWorkspaceService.disconnect(targetID).catch(() => undefined);
          this.markRemoteTargetStale(targetID);
          return;
        }
        this.remoteReadyByWorkspace[workspace.id] = true;
        this.remoteReconnectOpen = false;
        this.remoteReconnectThreadId = "";
        this.activeThreadId = thread.id;
      } catch (error) {
        this.remoteReconnectError = errorMessage(error);
        const activeStep = this.remoteReconnectProgress.find((step) => step.status === "active");
        if (activeStep) setProgress(activeStep.id, "error");
        return;
      } finally {
        this.remoteReconnectBusy = false;
      }
      if (intent === "prompt") await this.sendActivePrompt();
      else if (intent === "bash") await this.sendActiveBash();
      else this.startThreadInBackground(thread.id, true);
    },
    async waitForSettledReload(threadId: string) {
      const pending = settledReloadPromises.get(threadId);
      if (pending) await pending;
    },
    async sendActivePrompt(behavior?: StreamingBehavior) {
      const thread = this.activeThread;
      const message = this.activeDraft.trim();
      const attachments = this.activeAttachments.map((image) => ({ ...image }));
      if (!thread || (!message && attachments.length === 0)) return;
      if (this.requestRemoteReconnect(thread, "prompt")) return;

      if (thread.sessionFile && this.transcriptStateByThread[thread.id] !== "loaded") {
        await this.loadThreadTranscript(thread.id);
        if (this.transcriptStateByThread[thread.id] !== "loaded") return;
      }

      if (thread.status === "running" && behavior !== "steer") {
        const queue = this.pendingPromptsByThread[thread.id] ?? (this.pendingPromptsByThread[thread.id] = []);
        queue.push({ id: createID("pending"), text: message, images: attachments, createdAt: nowISO() });
        this.draftsByThread[thread.id] = "";
        this.attachmentsByThread[thread.id] = [];
        return;
      }

      this.draftsByThread[thread.id] = "";
      this.attachmentsByThread[thread.id] = [];
      await this.deliverPrompt(thread, message, attachments, thread.status === "running" && behavior === "steer" ? "steer" : undefined);
    },
    async deliverPrompt(
      thread: ThreadSummary,
      message: string,
      attachments: PreparedImage[],
      streamingBehavior?: StreamingBehavior,
      retainFailedMessage = true,
    ): Promise<boolean> {
      const wasRunning = thread.status === "running";
      if (!wasRunning) await this.waitForSettledReload(thread.id);
      const messages = this.messagesByThread[thread.id] ?? (this.messagesByThread[thread.id] = []);
      const timelineMessage: TimelineMessage = {
        id: createID("user"), role: "user", text: message, thinking: "", timestamp: nowLabel(), timestampMs: Date.now(), streaming: false,
        delivery: streamingBehavior, images: attachments, tools: [],
      };
      messages.push(timelineMessage);
      if (thread.title === "New task") {
        const titleText = skillInvocationTitleText(message);
        thread.firstMessage = titleText;
        thread.title = titleText ? titleText.length > 64 ? `${titleText.slice(0, 61)}...` : titleText : "Image task";
      }
      thread.modifiedAt = nowISO();
      this.scheduleDesktopStateSave();

      try {
        await this.ensureSession(thread);
        if (!wasRunning) thread.status = "running";
        if (!wasRunning) this.waitingForOutputByThread[thread.id] = true;
        await agentService.sendPrompt({
          threadId: thread.id,
          message,
          streamingBehavior,
          ...(attachments.length ? {
            images: attachments.map((image) => ({ type: "image", data: image.data, mimeType: image.mimeType })),
          } : {}),
        });
        if (!wasRunning && !message.trimStart().startsWith("/")) {
          const widgets = { ...(this.extensionWidgetsByThread[thread.id] ?? {}) };
          for (const key of TODO_WIDGET_KEYS) delete widgets[key];
          this.extensionWidgetsByThread[thread.id] = widgets;
        }
        if (!thread.sessionFile) {
          thread.sessionId = this.sessionStateByThread[thread.id]?.sessionId || thread.sessionId;
          thread.sessionFile = this.sessionStateByThread[thread.id]?.sessionFile;
          this.scheduleDesktopStateSave();
        }
        return true;
      } catch (error) {
        if (!wasRunning) this.waitingForOutputByThread[thread.id] = false;
        if (!retainFailedMessage) {
          const index = messages.indexOf(timelineMessage);
          if (index >= 0) messages.splice(index, 1);
        }
        thread.status = wasRunning ? "running" : "attention";
        thread.error = this.remoteFailureMessage(thread.id, error);
        const remoteWorkspace = this.remoteWorkspaceForThread(thread);
        const reconnectRequired = Boolean(remoteWorkspace && requiresRemoteReconnect(thread.error));
        if (reconnectRequired && retainFailedMessage) {
          const index = messages.indexOf(timelineMessage);
          if (index >= 0) messages.splice(index, 1);
          this.draftsByThread[thread.id] = message;
          this.attachmentsByThread[thread.id] = attachments;
          this.requestRemoteReconnect(thread, "prompt");
        } else {
          this.appendSystem(thread.id, `Unable to send prompt: ${thread.error}`, thread.error);
        }
        this.scheduleDesktopStateSave();
        return false;
      }
    },
    async dispatchNextPendingPrompt(threadId: string) {
      if (pendingPromptDispatchThreads.has(threadId)) return;
      const thread = this.threads.find((candidate) => candidate.id === threadId);
      const queue = this.pendingPromptsByThread[threadId];
      if (!thread || thread.status !== "idle" || !queue?.length) return;
      pendingPromptDispatchThreads.add(threadId);
      try {
        await this.waitForSettledReload(threadId);
        if (thread.status !== "idle") return;
        const prompt = this.pendingPromptsByThread[threadId]?.shift();
        if (!prompt) return;
        const delivered = await this.deliverPrompt(thread, prompt.text, prompt.images, undefined, false);
        if (!delivered) this.pendingPromptsByThread[threadId].unshift(prompt);
      } finally {
        pendingPromptDispatchThreads.delete(threadId);
        if (thread.status === "idle" && this.pendingPromptsByThread[threadId]?.length) {
          void this.dispatchNextPendingPrompt(threadId);
        }
      }
    },
    async abortActiveThread() {
      const thread = this.activeThread;
      if (!thread?.started) return;
      try {
        await agentService.abort(thread.id);
      } catch (error) {
        this.appendSystem(thread.id, `Unable to stop Pi: ${errorMessage(error)}`, errorMessage(error));
      }
    },
    async sendActiveBash() {
      const thread = this.activeThread;
      const raw = this.activeDraft.trim();
      if (!thread || !raw.startsWith("!") || this.activeAttachments.length || this.bashRunningByThread[thread.id]) return;
      const excludeFromContext = raw.startsWith("!!");
      const command = raw.slice(excludeFromContext ? 2 : 1).trim();
      if (!command || thread.status === "running") return;
      if (this.requestRemoteReconnect(thread, "bash")) return;

      if (thread.sessionFile && this.transcriptStateByThread[thread.id] !== "loaded") {
        await this.loadThreadTranscript(thread.id);
        if (this.transcriptStateByThread[thread.id] !== "loaded") return;
      }

      try {
        await this.ensureSession(thread);
        const messageID = createID("bash");
        this.messagesByThread[thread.id].push({
          id: messageID, role: "system", text: `$ ${command}\n`, thinking: "", timestamp: nowLabel(), streaming: true, tools: [],
        });
        this.bashMessageByThread[thread.id] = messageID;
        this.bashRunningByThread[thread.id] = true;
        this.draftsByThread[thread.id] = "";
        if (thread.title === "New task") {
          thread.firstMessage = command;
          thread.title = `! ${command}`.slice(0, 64);
        }
        this.scheduleDesktopStateSave();

        this.markRemoteRepositoryStale(thread.id);
        const result = await agentService.bash<BashResponse>({ threadId: thread.id, command, excludeFromContext });
        if (!excludeFromContext && !thread.sessionFile) {
          thread.sessionId = this.sessionStateByThread[thread.id]?.sessionId || thread.sessionId;
          thread.sessionFile = this.sessionStateByThread[thread.id]?.sessionFile;
          this.scheduleDesktopStateSave();
        }
        const message = this.messagesByThread[thread.id].find((item) => item.id === messageID);
        if (message) {
          const output = boundedToolOutput(result.output || "");
          message.text = [`$ ${command}`, output.text].filter(Boolean).join("\n");
          message.streaming = false;
          if (result.cancelled) message.error = "Command cancelled";
          else if (result.exitCode != null && result.exitCode !== 0) message.error = `Command exited with code ${result.exitCode}`;
        }
        void this.refreshStats(thread.id);
      } catch (error) {
        const messageID = this.bashMessageByThread[thread.id];
        const message = this.messagesByThread[thread.id].find((item) => item.id === messageID);
        const failure = this.remoteFailureMessage(thread.id, error);
        const remoteWorkspace = this.remoteWorkspaceForThread(thread);
        const reconnectRequired = Boolean(remoteWorkspace && requiresRemoteReconnect(failure));
        if (reconnectRequired) {
          const index = message ? this.messagesByThread[thread.id].indexOf(message) : -1;
          if (index >= 0) this.messagesByThread[thread.id].splice(index, 1);
          this.draftsByThread[thread.id] = raw;
          this.requestRemoteReconnect(thread, "bash");
          this.scheduleDesktopStateSave();
        } else if (message) {
          message.streaming = false;
          message.error = failure;
        } else {
          this.appendSystem(thread.id, `Unable to run command: ${failure}`, failure);
        }
      } finally {
        this.bashRunningByThread[thread.id] = false;
        this.bashMessageByThread[thread.id] = undefined;
      }
    },
    async abortActiveBash() {
      const thread = this.activeThread;
      if (!thread?.started || !this.bashRunningByThread[thread.id]) return;
      try {
        await agentService.abortBash(thread.id);
      } catch (error) {
        this.appendSystem(thread.id, `Unable to stop command: ${errorMessage(error)}`, errorMessage(error));
      }
    },
    async abortActiveRetry() {
      const thread = this.activeThread;
      if (!thread?.started || !this.retryByThread[thread.id]) return;
      try {
        await agentService.abortRetry(thread.id);
      } catch (error) {
        this.appendSystem(thread.id, `Unable to stop retry: ${errorMessage(error)}`, errorMessage(error));
      }
    },
    async stopThread(threadId: string): Promise<boolean> {
      const thread = this.threads.find((candidate) => candidate.id === threadId);
      if (!thread?.started) return false;
      try {
        await agentService.stopSession(thread.id);
        thread.started = false;
        thread.status = "idle";
        this.piProcessOrder = this.piProcessOrder.filter((id) => id !== thread.id);
        this.queueByThread[thread.id] = { steering: [], followUp: [] };
        this.retryByThread[thread.id] = undefined;
        this.scheduleDesktopStateSave();
        return true;
      } catch (error) {
        const failure = this.remoteFailureMessage(thread.id, error);
        this.appendSystem(thread.id, `Unable to close session: ${failure}`, failure);
        return false;
      }
    },
    async stopActiveSession() {
      if (this.activeThread) await this.stopThread(this.activeThread.id);
    },
    async stopAllSessions(): Promise<boolean> {
      const threadIDs = this.threads.filter((thread) => thread.started).map((thread) => thread.id);
      let stopped = true;
      for (const threadID of threadIDs) {
        if (!await this.stopThread(threadID)) stopped = false;
      }
      return stopped;
    },
    async compactActiveSession(instructions = "") {
      const thread = this.activeThread;
      if (!thread?.started || this.sessionOperationByThread[thread.id]) return;
      const operation = tr("topbar.compacting");
      this.sessionOperationByThread[thread.id] = operation;
      try {
        await agentService.compact({ threadId: thread.id, customInstructions: instructions || undefined });
      } catch (error) {
        this.appendSystem(thread.id, `Compaction failed: ${errorMessage(error)}`, errorMessage(error));
      } finally {
        if (this.sessionOperationByThread[thread.id] === operation) this.sessionOperationByThread[thread.id] = undefined;
      }
    },
    async renameActiveSession(name: string) {
      const thread = this.activeThread;
      const normalized = name.trim();
      if (!thread || !normalized) return;
      try {
        if (thread.sessionFile && !thread.started) await this.ensureSession(thread);
        if (thread.started) await agentService.setSessionName({ threadId: thread.id, name: normalized });
        thread.title = normalized;
        thread.modifiedAt = nowISO();
        this.scheduleDesktopStateSave();
      } catch (error) {
        this.appendSystem(thread.id, `Unable to rename task: ${errorMessage(error)}`, errorMessage(error));
      }
    },
    requestDeleteThread(threadId: string) {
      const thread = this.threads.find((candidate) => candidate.id === threadId);
      if (!thread || this.sessionOperationByThread[thread.id]) return;
      this.deleteThreadId = thread.id;
      this.deleteSessionTitle = thread.title;
      this.deleteSessionError = "";
      this.deletedRecoveryPath = "";
      this.deleteHasSession = Boolean(thread.sessionFile);
      this.deleteDialogOpen = true;
    },
    requestDeleteActiveSession() {
      if (this.activeThread) this.requestDeleteThread(this.activeThread.id);
    },
    closeDeleteDialog() {
      this.deleteDialogOpen = false;
      this.deleteThreadId = "";
      this.deleteSessionTitle = "";
      this.deleteSessionError = "";
      this.deletedRecoveryPath = "";
      this.deleteHasSession = false;
    },
    async confirmDeleteSession() {
      const thread = this.threads.find((candidate) => candidate.id === this.deleteThreadId);
      if (!thread || this.sessionOperationByThread[thread.id]) return;
      this.deleteSessionError = "";
      this.sessionOperationByThread[thread.id] = "Deleting";
      try {
        await waitForPiStart(thread.id);
        if (thread.started) {
          await agentService.stopSession(thread.id);
          thread.started = false;
          thread.status = "idle";
          this.piProcessOrder = this.piProcessOrder.filter((id) => id !== thread.id);
        }
        if (!thread.sessionFile) {
          this.removeThreadState(thread.id);
          this.closeDeleteDialog();
          this.scheduleDesktopStateSave();
          return;
        }
        const deleted = await catalogService.deleteSession(thread.sessionFile);
        this.removeThreadState(thread.id);
        this.deleteThreadId = "";
        this.deletedRecoveryPath = deleted.recoveryPath;
        this.scheduleDesktopStateSave();
      } catch (error) {
        this.deleteSessionError = this.remoteFailureMessage(thread.id, error);
      } finally {
        delete this.sessionOperationByThread[thread.id];
      }
    },
    removeThreadState(threadId: string) {
      const index = this.threads.findIndex((candidate) => candidate.id === threadId);
      if (index >= 0) this.threads.splice(index, 1);
      for (const collection of [
        this.messagesByThread, this.searchBodyTextByThread, this.searchBodyVersionByThread,
        this.transcriptEntriesByThread, this.transcriptReloadGenerationByThread,
        this.draftsByThread, this.attachmentsByThread, this.activeAssistantByThread,
        this.waitingForOutputByThread, this.sessionStateByThread,
        this.sessionStatsByThread, this.sessionStatsRefreshGenerationByThread,
        this.compactionEstimatesByThread, this.latestCompactionEstimateByThread,
        this.modelsByThread, this.thinkingLevelsByThread, this.thinkingLevelsRefreshGenerationByThread, this.commandsByThread,
        this.queueByThread, this.pendingPromptsByThread, this.retryByThread, this.extensionRequestByThread, this.extensionStatusesByThread,
        this.extensionWidgetsByThread, this.extensionTitleByThread, this.transcriptStateByThread,
        this.sessionBranchesByThread, this.sessionBranchesErrorByThread, this.sessionOperationByThread, this.pendingModelByThread,
        this.modelSelectionGenerationByThread,
        this.repositoryFilePreviewByThread, this.repositoryFilePreviewPathByThread, this.repositoryFilePreviewLineByThread,
        this.repositoryFilePreviewLoadingByThread, this.repositoryFilePreviewGenerationByThread, this.repositoryFilePreviewErrorByThread,
        this.terminalGenerationByThread,
      ]) delete collection[threadId];
      piExitedGenerationByThread.delete(threadId);
      if (this.activeThreadId === threadId) {
        this.activeThreadId = this.threads[0]?.id ?? "";
      }
      this.piProcessOrder = this.piProcessOrder.filter((id) => id !== threadId);
    },
    async cloneActiveSession() {
      const thread = this.activeThread;
      if (!thread?.sessionFile || thread.status === "running" || this.sessionOperationByThread[thread.id]) return;
      this.sessionOperationByThread[thread.id] = "Cloning";
      try {
        const previous = { ...thread };
        const result = await this.callWithSession(thread, () => agentService.cloneSession<ForkResponse>(thread.id));
        if (result?.cancelled) return;
        await this.completeSessionReplacement(thread, previous, "copy");
      } catch (error) {
        thread.status = "attention";
        thread.error = errorMessage(error);
        this.appendSystem(thread.id, `Unable to clone task: ${thread.error}`, thread.error);
      } finally {
        this.sessionOperationByThread[thread.id] = undefined;
      }
    },
    async forkFromMessage(messageId: string): Promise<boolean> {
      const thread = this.activeThread;
      const message = this.activeMessages.find((item) => item.id === messageId && (item.role === "user" || item.role === "assistant"));
      if (!thread?.sessionFile || !message?.entryId || thread.status === "running" || thread.status === "starting" || this.sessionOperationByThread[thread.id]) return false;
      this.sessionOperationByThread[thread.id] = "Forking";
      try {
        const previous = { ...thread };
        const result = await this.callWithSession(thread, () => agentService.forkSessionAt<ForkResponse>({
          threadId: thread.id,
          path: thread.sessionFile!,
          entryId: message.entryId!,
          before: message.role === "user",
        }));
        if (result?.cancelled) return false;
        await this.completeSessionReplacement(thread, previous, "fork", message.role === "user" ? result?.text || message.text : "");
        return true;
      } catch (error) {
        thread.status = "attention";
        thread.error = errorMessage(error);
        this.appendSystem(thread.id, `Unable to fork task: ${errorMessage(error)}`, errorMessage(error));
        return false;
      } finally {
        this.sessionOperationByThread[thread.id] = undefined;
      }
    },
    async resendEditedMessage(messageId: string, text: string): Promise<boolean> {
      const threadId = this.activeThreadId;
      const message = this.activeMessages.find((item) => item.id === messageId && item.role === "user");
      const latestUserMessage = this.activeMessages.findLast((item) => item.role === "user");
      if (!message || latestUserMessage?.id !== messageId || !text.trim()) return false;
      const images = message.images?.map((image) => ({ ...image })) ?? [];
      if (!await this.forkFromMessage(messageId)) return false;
      if (this.activeThreadId !== threadId) {
        this.draftsByThread[threadId] = text;
        this.attachmentsByThread[threadId] = images;
        return false;
      }
      this.updateDraft(text);
      this.attachmentsByThread[this.activeThreadId] = images;
      await this.sendActivePrompt();
      return true;
    },
    async editMessage(messageId: string, text: string): Promise<boolean> {
      const thread = this.activeThread;
      const message = this.activeMessages.find((item) => item.id === messageId && (item.role === "user" || item.role === "assistant"));
      if (!thread?.sessionFile || !message?.entryId || !text.trim() || thread.status === "running" || thread.status === "starting" || this.sessionOperationByThread[thread.id]) return false;
      this.sessionOperationByThread[thread.id] = "Editing message";
      try {
        await this.callWithSession(thread, () => agentService.editSessionMessage({
          threadId: thread.id, path: thread.sessionFile!, entryId: message.entryId!, text,
        }));
        await this.reloadSessionTranscript(thread, false);
        return true;
      } catch (error) {
        thread.status = "attention";
        thread.error = errorMessage(error);
        return false;
      } finally {
        delete this.sessionOperationByThread[thread.id];
      }
    },
    async deleteMessage(messageId: string): Promise<boolean> {
      const thread = this.activeThread;
      const message = this.activeMessages.find((item) => item.id === messageId && (item.role === "user" || item.role === "assistant"));
      if (!thread?.sessionFile || !message?.entryId || thread.status === "running" || thread.status === "starting" || this.sessionOperationByThread[thread.id]) return false;
      this.sessionOperationByThread[thread.id] = "Deleting message";
      try {
        await this.callWithSession(thread, () => agentService.deleteSessionMessage({
          threadId: thread.id, path: thread.sessionFile!, entryId: message.entryId!,
        }));
        await this.reloadSessionTranscript(thread, false);
        return true;
      } catch (error) {
        thread.status = "attention";
        thread.error = errorMessage(error);
        return false;
      } finally {
        delete this.sessionOperationByThread[thread.id];
      }
    },
    async reloadSessionTranscript(thread: ThreadSummary, preserveLoadedHistory = true) {
      const sessionFile = thread.sessionFile;
      if (!sessionFile) return;
      const generation = (this.transcriptReloadGenerationByThread[thread.id] ?? 0) + 1;
      this.transcriptReloadGenerationByThread[thread.id] = generation;
      const existingEntries = preserveLoadedHistory ? this.transcriptEntriesByThread[thread.id] ?? [] : [];
      const snapshot = await catalogService.getSessionSnapshot(sessionFile);
      if (thread.sessionFile !== sessionFile || this.transcriptReloadGenerationByThread[thread.id] !== generation) return;
      if (thread.status === "running" || thread.status === "starting") return;

      const page = (snapshot.messages as Array<Record<string, unknown>> | null) ?? [];
      if (existingEntries.length && page.length) {
        const existingPositions = new Map<string, number>();
        existingEntries.forEach((entry, position) => {
          const id = transcriptDisplayID(entry);
          if (id) existingPositions.set(id, position);
        });
        const overlap = page.find((entry) => {
          const id = transcriptDisplayID(entry);
          return Boolean(id && existingPositions.has(id));
        });
        const overlapID = overlap ? transcriptDisplayID(overlap) : undefined;
        if (overlapID) {
          snapshot.messages = [...existingEntries.slice(0, existingPositions.get(overlapID)), ...page];
        } else if (existingPositions.size > 0 && page.some((entry) => transcriptDisplayID(entry))) {
          snapshot.messages = [...existingEntries, ...page];
        }
      }

      this.applySessionSnapshot(thread, snapshot);
      this.transcriptStateByThread[thread.id] = "loaded";
      thread.modifiedAt = nowISO();
      this.sessionBranchesByThread[thread.id] = undefined;
      this.sessionBranchesErrorByThread[thread.id] = "";
      this.scheduleDesktopStateSave();
    },
    async forkActiveSession(entryId: string, fallbackText = "") {
      const thread = this.activeThread;
      if (!thread?.sessionFile || thread.status === "running" || this.sessionOperationByThread[thread.id]) return;
      this.sessionOperationByThread[thread.id] = "Forking";
      try {
        const previous = { ...thread };
        const result = await this.callWithSession(thread, () => agentService.forkSessionAt<ForkResponse>({
          threadId: thread.id, path: thread.sessionFile!, entryId, before: true,
        }));
        if (result?.cancelled) return;
        await this.completeSessionReplacement(thread, previous, "fork", result?.text || fallbackText);
      } catch (error) {
        thread.status = "attention";
        thread.error = errorMessage(error);
        this.appendSystem(thread.id, `Unable to fork task: ${thread.error}`, thread.error);
      } finally {
        this.sessionOperationByThread[thread.id] = undefined;
      }
    },
    async completeSessionReplacement(thread: ThreadSummary, previous: ThreadSummary, kind: "copy" | "fork", draft = "") {
      const state = await agentService.getState<PiSessionState>(thread.id);
      if (!state.sessionFile || !previous.sessionFile || pathKey(state.sessionFile) === pathKey(previous.sessionFile)) {
        throw new Error("Pi did not create a new session file");
      }

      const sourceID = createID("thread");
      const source: ThreadSummary = {
        ...previous,
        id: sourceID,
        status: "idle",
        started: false,
        generation: 0,
        unread: false,
        error: undefined,
      };
      const activeIndex = this.threads.findIndex((candidate) => candidate.id === thread.id);
      this.threads.splice(activeIndex < 0 ? this.threads.length : activeIndex + 1, 0, source);
      this.messagesByThread[sourceID] = copyMessages(this.messagesByThread[thread.id] ?? []);
      this.transcriptEntriesByThread[sourceID] = [...(this.transcriptEntriesByThread[thread.id] ?? [])];
      this.transcriptReloadGenerationByThread[sourceID] = this.transcriptReloadGenerationByThread[thread.id] ?? 0;
      this.draftsByThread[sourceID] = "";
      this.transcriptStateByThread[sourceID] = this.transcriptStateByThread[thread.id] ?? "idle";
      this.sessionStateByThread[sourceID] = {
        ...(this.sessionStateByThread[thread.id] ?? {}),
        sessionId: previous.sessionId,
        sessionFile: previous.sessionFile,
        isStreaming: false,
      };

      const titleSuffix = kind === "fork" ? "fork" : "copy";
      const newTitle = `${previous.title} (${titleSuffix})`;
      thread.title = newTitle;
      thread.sessionId = state.sessionId;
      thread.sessionFile = state.sessionFile;
      thread.parentSessionFile = previous.sessionFile;
      thread.modifiedAt = nowISO();
      thread.status = "idle";
      thread.error = undefined;
      this.sessionStateByThread[thread.id] = state;
      await this.reloadSessionTranscript(thread, false);
      this.draftsByThread[thread.id] = skillInvocationCommandText(draft);
      this.sessionBranchesByThread[thread.id] = undefined;
      this.sessionBranchesErrorByThread[thread.id] = "";
      this.branchPanelOpen = false;
      try {
        await agentService.setSessionName({ threadId: thread.id, name: newTitle });
      } catch (error) {
        this.appendSystem(thread.id, `New session created, but naming failed: ${errorMessage(error)}`, errorMessage(error));
      }
      await this.refreshStats(thread.id).catch(() => undefined);
      this.scheduleDesktopStateSave();
    },
    async openBranchPanel() {
      const thread = this.activeThread;
      if (!thread?.sessionFile) return;
      this.sessionBranchesByThread[thread.id] = undefined;
      this.sessionBranchesErrorByThread[thread.id] = "";
      this.branchPanelOpen = true;
      try {
        this.sessionBranchesByThread[thread.id] = await this.callWithSession(thread, () => agentService.getSessionBranches(thread.id));
      } catch (error) {
        this.sessionBranchesErrorByThread[thread.id] = errorMessage(error);
      }
    },
    closeBranchPanel() {
      this.branchPanelOpen = false;
    },
    async exportActiveSession() {
      const thread = this.activeThread;
      if (!thread?.sessionFile || this.sessionOperationByThread[thread.id]) return;
      this.exportDialogOpen = false;
      this.exportResultPath = "";
      this.exportResultError = "";
      this.sessionOperationByThread[thread.id] = "Exporting";
      try {
        const result = await this.callWithSession(thread, () => agentService.exportSession<ExportResponse>(thread.id, thread.title, thread.workspacePath));
        if (result?.path) {
          this.exportResultPath = result.path;
          this.exportDialogOpen = true;
        }
      } catch (error) {
        this.exportResultError = errorMessage(error);
        this.exportDialogOpen = true;
      } finally {
        this.sessionOperationByThread[thread.id] = undefined;
      }
    },
    closeExportDialog() {
      this.exportDialogOpen = false;
      this.exportResultPath = "";
      this.exportResultError = "";
    },
    async chooseModel(model: PiModel) {
      const thread = this.activeThread;
      if (!thread) return;
      const selectionGeneration = (this.modelSelectionGenerationByThread[thread.id] ?? 0) + 1;
      this.modelSelectionGenerationByThread[thread.id] = selectionGeneration;
      this.thinkingLevelsByThread[thread.id] = [];
      this.thinkingLevelsRefreshGenerationByThread[thread.id] = (this.thinkingLevelsRefreshGenerationByThread[thread.id] ?? 0) + 1;
      if (!thread.started) {
        this.pendingModelByThread[thread.id] = { ...model };
        this.sessionStateByThread[thread.id] = { ...(this.sessionStateByThread[thread.id] ?? {}), model: { ...model } };
        return;
      }
      delete this.pendingModelByThread[thread.id];
      await agentService.setModel({ threadId: thread.id, provider: model.provider, modelId: model.id });
      if (this.modelSelectionGenerationByThread[thread.id] !== selectionGeneration) return;
      await this.refreshState(thread.id);
      if (this.modelSelectionGenerationByThread[thread.id] !== selectionGeneration) return;
      await this.refreshThinkingLevels(thread.id);
    },
    async chooseThinkingLevel(level: string) {
      const thread = this.activeThread;
      if (!thread?.started) return;
      await agentService.setThinkingLevel({ threadId: thread.id, level });
      await this.refreshState(thread.id);
    },
    async setSteeringMode(mode: QueueMode) {
      const thread = this.activeThread;
      if (!thread?.started) return;
      await agentService.setSteeringMode({ threadId: thread.id, mode });
      await this.refreshState(thread.id);
    },
    async setFollowUpMode(mode: QueueMode) {
      const thread = this.activeThread;
      if (!thread?.started) return;
      await agentService.setFollowUpMode({ threadId: thread.id, mode });
      await this.refreshState(thread.id);
    },
    async setAutoCompaction(enabled: boolean) {
      const thread = this.activeThread;
      if (!thread?.started) return;
      await agentService.setAutoCompaction({ threadId: thread.id, enabled });
      await this.refreshState(thread.id);
    },
    async setAutoRetry(enabled: boolean) {
      const thread = this.activeThread;
      if (!thread?.started) return;
      await agentService.setAutoRetry({ threadId: thread.id, enabled });
      this.autoRetryEnabledByThread[thread.id] = enabled;
    },
    async respondToExtension(value: string | boolean | undefined, cancelled = false) {
      const thread = this.activeThread;
      const request = thread ? this.extensionRequestByThread[thread.id] : undefined;
      if (!thread || !request) return;
      await agentService.respondExtensionUI({
        threadId: thread.id,
        requestId: request.id,
        value: typeof value === "string" ? value : undefined,
        confirmed: typeof value === "boolean" ? value : undefined,
        cancelled: cancelled || undefined,
      });
      this.extensionRequestByThread[thread.id] = undefined;
    },
    dismissExtensionRequest(requestID: string) {
      const request = this.extensionRequestByThread[this.activeThreadId];
      if (request?.id === requestID) this.extensionRequestByThread[this.activeThreadId] = undefined;
    },
    startThreadInBackground(threadId: string, explicit = false) {
      const thread = this.threads.find((candidate) => candidate.id === threadId);
      if (!thread || thread.started || piStartPromises.has(thread.id)) return;
      if (!explicit && this.requestRemoteReconnect(thread, "start")) return;
      void this.ensureSession(thread).catch((error) => {
        thread.started = false;
        thread.status = "attention";
        thread.error = this.remoteFailureMessage(thread.id, error);
        this.appendSystem(thread.id, `Unable to start Pi: ${thread.error}`, thread.error);
        this.scheduleDesktopStateSave();
      });
    },
    async ensureSession(thread: ThreadSummary) {
      const remoteWorkspace = this.remoteWorkspaceForThread(thread);
      if (remoteWorkspace && !this.remoteReadyByWorkspace[remoteWorkspace.id]) {
        throw new Error("SSH workspace must be reconnected before starting Pi");
      }
      if (thread.started) return;
      const existing = piStartPromises.get(thread.id);
      if (existing) return existing;
      thread.status = "starting";
      thread.error = undefined;
      const generationFloor = thread.generation;
      piStartingGenerationFloor.set(thread.id, generationFloor);
      const operation = piStartQueue.then(() => this.startSessionNow(thread));
      piStartPromises.set(thread.id, operation);
      piStartQueue = operation.catch(() => undefined);
      try {
        await operation;
      } finally {
        piStartPromises.delete(thread.id);
        if (piStartingGenerationFloor.get(thread.id) === generationFloor) piStartingGenerationFloor.delete(thread.id);
      }
    },
    async callWithSession<T>(thread: ThreadSummary, operation: () => Promise<T>): Promise<T> {
      await this.ensureSession(thread);
      try {
        return await operation();
      } catch (error) {
        if (!isThreadNotRunningError(error)) throw error;
        thread.started = false;
        thread.status = "idle";
        thread.error = undefined;
        this.piProcessOrder = this.piProcessOrder.filter((id) => id !== thread.id);
        await this.ensureSession(thread);
        return operation();
      }
    },
    async startSessionNow(thread: ThreadSummary) {
      if (thread.started) return;
      const queuedRemoteWorkspace = this.remoteWorkspaceForThread(thread);
      if (queuedRemoteWorkspace && !this.remoteReadyByWorkspace[queuedRemoteWorkspace.id]) {
        throw new Error("SSH workspace must be reconnected before starting Pi");
      }
      const resumesPersistedSession = Boolean(thread.sessionFile);
      this.piProcessOrder = this.piProcessOrder.filter((id) => this.threads.some((candidate) => candidate.id === id && candidate.started));
      while (this.piProcessOrder.length >= MAX_PI_PROCESSES) {
        const oldestIdle = this.piProcessOrder.find((id) => {
          const candidate = this.threads.find((item) => item.id === id);
          return candidate?.started && candidate.status !== "running" && candidate.status !== "starting";
        });
        if (!oldestIdle) throw new Error(`All ${MAX_PI_PROCESSES} Pi processes are busy. Close a task or wait for one to finish.`);
        if (!await this.stopThread(oldestIdle)) throw new Error("Unable to free a Pi process slot");
      }
      thread.status = "starting";
      thread.error = undefined;
      const remoteWorkspace = this.remoteWorkspaceForThread(thread);
      const session = await agentService.startSession({
        threadId: thread.id,
        workspace: remoteWorkspace ? "" : thread.workspacePath,
        workspaceId: remoteWorkspace?.id,
        sessionPath: thread.sessionFile,
        trust: thread.trust,
        offline: this.offlineMode,
        disableThemes: true,
        proxyUrl: this.proxyEnabled ? this.proxyURL.trim() : undefined,
      });
      if (piExitedGenerationByThread.get(thread.id) === session.generation) {
        throw new Error(thread.error || "Pi process exited during startup");
      }
      let state: PiSessionState;
      try {
        state = JSON.parse(session.stateJson) as PiSessionState;
      } catch (error) {
        try {
          await agentService.stopSession(thread.id);
        } catch (stopError) {
          throw new Error(`${errorMessage(error)}; unable to stop invalid Pi session: ${errorMessage(stopError)}`);
        }
        throw error;
      }
      thread.started = true;
      this.piProcessOrder = [...this.piProcessOrder.filter((id) => id !== thread.id), thread.id];
      thread.generation = session.generation;
      thread.status = "idle";
      this.sessionStateByThread[thread.id] = state;
      if (this.bootstrap) {
        this.bootstrap.runtime = {
          ...this.bootstrap.runtime,
          state: RuntimeState.RuntimeReady,
          message: "Pi RPC session started successfully",
        };
      }
      this.autoRetryEnabledByThread[thread.id] ??= true;
      thread.sessionId = state.sessionId || thread.sessionId;
      if (resumesPersistedSession) {
        thread.sessionFile = state.sessionFile || thread.sessionFile;
      } else {
        // Pi reserves a JSONL path for a fresh session before the first prompt,
        // but does not create the file until that prompt is persisted.
        this.transcriptStateByThread[thread.id] = "loaded";
      }
      await this.applyPendingModel(thread);
      if (!thread.started || piExitedGenerationByThread.get(thread.id) === session.generation) {
        throw new Error(thread.error || "Pi process exited during startup");
      }
      this.scheduleDesktopStateSave();
      await Promise.allSettled([
        this.refreshModels(thread.id), this.refreshThinkingLevels(thread.id), this.refreshCommands(thread.id), this.refreshStats(thread.id),
      ]);
    },
    async refreshState(threadId: string) {
      const thread = this.threads.find((item) => item.id === threadId);
      const generation = thread?.generation;
      const wasStarted = thread?.started;
      const state = await agentService.getState<PiSessionState>(threadId);
      if (!isCurrentPiRequest(this.threads, thread, generation, wasStarted)) return;
      this.sessionStateByThread[threadId] = state;
    },
    async refreshModels(threadId: string) {
      const thread = this.threads.find((item) => item.id === threadId);
      const generation = thread?.generation;
      const wasStarted = thread?.started;
      const response = await agentService.getAvailableModels<ModelResponse>(threadId);
      if (!isCurrentPiRequest(this.threads, thread, generation, wasStarted)) return;
      this.modelsByThread[threadId] = response.models ?? [];
      this.knownRuntimeModels = mergeModels(this.knownRuntimeModels, this.modelsByThread[threadId]);
    },
    async refreshConfiguredModels(throwOnError = false) {
      try {
        this.configuredModels = (await modelConfigService.selectable()).map((model) => ({ ...model }));
        this.modelCatalogError = "";
      } catch (error) {
        this.modelCatalogError = errorMessage(error);
        if (throwOnError) throw error;
      }
    },
    async applyPendingModel(thread: ThreadSummary) {
      const pending = this.pendingModelByThread[thread.id];
      if (!pending || !thread.started) return;
      const generation = thread.generation;
      const current = this.sessionStateByThread[thread.id]?.model;
      let clearPending = true;
      try {
        if (!current || modelKey(current) !== modelKey(pending)) {
          await agentService.setModel({ threadId: thread.id, provider: pending.provider, modelId: pending.id });
          if (!thread.started || thread.generation !== generation) {
            clearPending = false;
            return;
          }
          await this.refreshState(thread.id);
        }
        if (!thread.started || thread.generation !== generation) clearPending = false;
      } catch (error) {
        if (!thread.started || thread.generation !== generation || isThreadNotRunningError(error)) {
          clearPending = false;
          return;
        }
        const message = errorMessage(error);
        thread.status = "attention";
        thread.error = message;
        this.appendSystem(thread.id, `Unable to apply selected model: ${message}`, message);
      } finally {
        if (clearPending && this.pendingModelByThread[thread.id] === pending) delete this.pendingModelByThread[thread.id];
      }
    },
    async refreshThinkingLevels(threadId: string) {
      const thread = this.threads.find((item) => item.id === threadId);
      const piGeneration = thread?.generation;
      const wasStarted = thread?.started;
      const generation = (this.thinkingLevelsRefreshGenerationByThread[threadId] ?? 0) + 1;
      this.thinkingLevelsRefreshGenerationByThread[threadId] = generation;
      const requestedModel = this.sessionStateByThread[threadId]?.model;
      const requestedModelKey = requestedModel ? modelKey(requestedModel) : "";
      const response = await agentService.getAvailableThinkingLevels<ThinkingResponse>(threadId);
      const currentModel = this.sessionStateByThread[threadId]?.model;
      const currentModelKey = currentModel ? modelKey(currentModel) : "";
      if (!isCurrentPiRequest(this.threads, thread, piGeneration, wasStarted)) return;
      if (this.thinkingLevelsRefreshGenerationByThread[threadId] !== generation || currentModelKey !== requestedModelKey) return;
      this.thinkingLevelsByThread[threadId] = response.levels ?? [];
    },
    async refreshCommands(threadId: string) {
      const thread = this.threads.find((item) => item.id === threadId);
      const generation = thread?.generation;
      const wasStarted = thread?.started;
      const response = await agentService.getCommands<CommandsResponse>(threadId);
      if (!isCurrentPiRequest(this.threads, thread, generation, wasStarted)) return;
      this.commandsByThread[threadId] = (response.commands ?? []).map((command) => ({
        name: command.name,
        description: command.description,
        source: command.source,
        location: command.location ?? (command.sourceInfo?.scope === "temporary" ? "path" : command.sourceInfo?.scope),
        path: command.path ?? command.sourceInfo?.path,
      }));
    },
    applyCompactionEstimate(threadId: string, compaction: TimelineCompaction, invalidateStats = true) {
      const estimate = compaction.estimatedTokensAfter;
      if (estimate === undefined) return;
      const estimates = this.compactionEstimatesByThread[threadId] ?? (this.compactionEstimatesByThread[threadId] = {});
      estimates[compactionEstimateKey(compaction.summary, compaction.tokensBefore)] = estimate;
      this.latestCompactionEstimateByThread[threadId] = estimate;

      const current = this.sessionStatsByThread[threadId] ?? {};
      if (!invalidateStats && current.contextUsage?.tokens != null && current.contextUsage.estimated !== true) return;
      if (invalidateStats) {
        this.sessionStatsRefreshGenerationByThread[threadId] = (this.sessionStatsRefreshGenerationByThread[threadId] ?? 0) + 1;
      }
      const contextWindow = current.contextUsage?.contextWindow ?? this.sessionStateByThread[threadId]?.model?.contextWindow;
      if (typeof contextWindow !== "number" || contextWindow <= 0) return;
      this.sessionStatsByThread[threadId] = {
        ...current,
        contextUsage: {
          tokens: estimate,
          contextWindow,
          percent: (estimate / contextWindow) * 100,
          estimated: true,
        },
      };
    },
    async refreshStats(threadId: string) {
      const thread = this.threads.find((item) => item.id === threadId);
      const piGeneration = thread?.generation;
      const wasStarted = thread?.started;
      const generation = (this.sessionStatsRefreshGenerationByThread[threadId] ?? 0) + 1;
      this.sessionStatsRefreshGenerationByThread[threadId] = generation;
      let stats = await agentService.getSessionStats<SessionStats>(threadId);
      if (!isCurrentPiRequest(this.threads, thread, piGeneration, wasStarted)) return;
      if (this.sessionStatsRefreshGenerationByThread[threadId] !== generation) return;
      const estimate = this.latestCompactionEstimateByThread[threadId];
      const contextWindow = stats.contextUsage?.contextWindow ?? this.sessionStateByThread[threadId]?.model?.contextWindow;
      if (stats.contextUsage?.tokens == null && estimate !== undefined && typeof contextWindow === "number" && contextWindow > 0) {
        stats = {
          ...stats,
          contextUsage: {
            tokens: estimate,
            contextWindow,
            percent: (estimate / contextWindow) * 100,
            estimated: true,
          },
        };
      }
      this.sessionStatsByThread[threadId] = stats;
    },
    remoteFailureMessage(threadID: string, error: unknown): string {
      const message = errorMessage(error);
      const thread = this.threads.find((item) => item.id === threadID);
      const workspace = thread ? this.remoteWorkspaceForThread(thread) : undefined;
      if (workspace && hasRemoteCode(message, "REMOTE_CONTEXT_CHANGED_WAIT_FOR_IDLE")) {
        this.remoteReadyByWorkspace[workspace.id] = false;
        this.clearTerminalGenerations(workspace.id);
        this.markRemoteRepositoryStale(threadID);
      } else if (workspace && requiresRemoteReconnect(message)) {
        this.markRemoteTargetStale(workspace.targetId);
      }
      return message;
    },
    markRemoteWorkspaceStale(workspaceID: string) {
      this.repositoryRefreshGenerationByWorkspace[workspaceID] = (this.repositoryRefreshGenerationByWorkspace[workspaceID] ?? 0) + 1;
      this.repositoryLoadingByWorkspace[workspaceID] = false;
      this.repositoryBranchesGenerationByWorkspace[workspaceID] = (this.repositoryBranchesGenerationByWorkspace[workspaceID] ?? 0) + 1;
      this.repositoryBranchesLoadingByWorkspace[workspaceID] = false;
      this.repositoryDiffGenerationByWorkspace[workspaceID] = (this.repositoryDiffGenerationByWorkspace[workspaceID] ?? 0) + 1;
      this.repositoryDiffLoadingByWorkspace[workspaceID] = false;
      this.repositoryStaleByWorkspace[workspaceID] = true;
      for (const thread of this.threads) {
        if (thread.workspaceId !== workspaceID) continue;
        this.repositoryFilePreviewGenerationByThread[thread.id] = (this.repositoryFilePreviewGenerationByThread[thread.id] ?? 0) + 1;
        this.repositoryFilePreviewLoadingByThread[thread.id] = false;
      }
    },
    markRemoteTargetStale(targetID?: string) {
      if (!targetID) return;
      remoteTargetProjectionEpochByID.set(targetID, (remoteTargetProjectionEpochByID.get(targetID) ?? 0) + 1);
      for (const workspace of this.workspaces) {
        if (workspace.kind !== "ssh" || workspace.targetId !== targetID) continue;
        const key = workspace.id;
        this.remoteReadyByWorkspace[key] = false;
        this.clearTerminalGenerations(key);
        this.markRemoteWorkspaceStale(key);
      }
    },
    clearTerminalGenerations(workspaceID: string) {
      for (const thread of this.threads) {
        if (thread.workspaceId === workspaceID) delete this.terminalGenerationByThread[thread.id];
      }
    },
    markRemoteRepositoryStale(threadID: string) {
      const thread = this.threads.find((item) => item.id === threadID);
      const workspace = thread ? this.remoteWorkspaceForThread(thread) : undefined;
      if (workspace) this.markRemoteWorkspaceStale(workspace.id);
    },
    setTerminalGeneration(threadID: string, generation?: number) {
      if (generation) this.terminalGenerationByThread[threadID] = generation;
      else delete this.terminalGenerationByThread[threadID];
    },
    handleTerminalEvent(event: TerminalEvent) {
      if (event.type !== "exit" && event.type !== "error") return;
      if (event.generation && this.terminalGenerationByThread[event.threadId] !== event.generation) return;
      this.markRemoteRepositoryStale(event.threadId);
      if (event.error) this.remoteFailureMessage(event.threadId, event.error);
    },
    appendSystem(threadId: string, text: string, error?: string) {
      const messages = this.messagesByThread[threadId] ?? (this.messagesByThread[threadId] = []);
      messages.push({ id: createID("system"), role: "system", text, thinking: "", timestamp: nowLabel(), streaming: false, error, tools: [] });
    },
    handlePiEvent(sessionEvent: PiSessionEvent) {
      const thread = this.threads.find((item) => item.id === sessionEvent.threadId);
      if (!thread) return;
      const generationFloor = piStartingGenerationFloor.get(thread.id);
      const exitedGeneration = piExitedGenerationByThread.get(thread.id);
      if (exitedGeneration !== undefined && sessionEvent.event.generation <= exitedGeneration) return;
      if (generationFloor !== undefined) {
        if (sessionEvent.event.generation <= generationFloor) return;
        if (sessionEvent.event.generation > thread.generation) thread.generation = sessionEvent.event.generation;
      } else if (thread.generation && thread.generation !== sessionEvent.event.generation) {
        return;
      }
      const payload = sessionEvent.event.payload ?? {};
      const messages = this.messagesByThread[thread.id] ?? (this.messagesByThread[thread.id] = []);

      switch (sessionEvent.event.type) {
        case "agent_start":
          thread.status = "running";
          this.waitingForOutputByThread[thread.id] = true;
          this.scheduleDesktopStateSave();
          break;
        case "agent_end": {
          // Pi's agent_end only closes one low-level run. RPC may still emit
          // an automatic retry/compaction or drain queued messages before
          // agent_settled, which is the authoritative idle boundary.
          const willRetry = payload.willRetry === true;
          if (willRetry) {
            thread.status = "running";
            this.waitingForOutputByThread[thread.id] = true;
          } else {
            this.waitingForOutputByThread[thread.id] = false;
            this.finishAssistant(thread.id);
          }
          void this.refreshState(thread.id);
          break;
        }
        case "agent_settled": {
          thread.status = "idle";
          this.waitingForOutputByThread[thread.id] = false;
          const unfinishedRetry = this.retryByThread[thread.id];
          if (unfinishedRetry) {
            const retryIndex = messages.findLastIndex((message) => message.runNotice?.status === "retrying");
            const retryMessage = retryIndex >= 0 ? messages[retryIndex] : undefined;
            if (retryMessage?.runNotice) {
              const recovered = messages.slice(retryIndex + 1)
                .some((message) => message.role === "assistant" && !message.error);
              retryMessage.runNotice = {
                status: recovered ? "recovered" : "failed",
                error: retryMessage.runNotice.error ?? unfinishedRetry.errorMessage,
                attempt: unfinishedRetry.attempt,
                maxAttempts: unfinishedRetry.maxAttempts,
              };
            }
            this.retryByThread[thread.id] = undefined;
          }
          this.finishAssistant(thread.id);
          if (thread.id !== this.activeThreadId || appIsInBackground()) thread.unread = true;
          {
            const reload = (async () => {
              if (thread.sessionFile) await this.reloadSessionTranscript(thread).catch(() => undefined);
              void this.refreshActiveRepository(thread.id);
            })();
            settledReloadPromises.set(thread.id, reload);
            void reload.finally(() => {
              if (settledReloadPromises.get(thread.id) === reload) settledReloadPromises.delete(thread.id);
              void this.dispatchNextPendingPrompt(thread.id);
            });
          }
          if (this.notificationsEnabled && !(this.pendingPromptsByThread[thread.id]?.length) && (thread.id !== this.activeThreadId || appIsInBackground())) {
            void notifyDesktop(tr("notifications.taskCompleted"), taskNotificationSummary(thread.title));
          }
          void this.refreshState(thread.id);
          void this.refreshStats(thread.id).catch(() => undefined);
          thread.modifiedAt = nowISO();
          this.scheduleDesktopStateSave();
          break;
        }
        case "queue_update": {
          const steering = Array.isArray(payload.steering) ? payload.steering.filter((item): item is string => typeof item === "string") : [];
          const followUp = Array.isArray(payload.followUp) ? payload.followUp.filter((item): item is string => typeof item === "string") : [];
          this.queueByThread[thread.id] = { steering, followUp };
          const state = this.sessionStateByThread[thread.id];
          if (state) state.pendingMessageCount = steering.length + followUp.length;
          break;
        }
        case "bash_execution_update": {
          const messageID = this.bashMessageByThread[thread.id];
          const message = messages.find((item) => item.id === messageID);
          if (!message || typeof payload.delta !== "string") break;
          const output = boundedToolOutput(message.text + payload.delta);
          message.text = output.text;
          break;
        }
        case "message_start": {
          const message = payload.message as Record<string, unknown> | undefined;
          if (message?.role !== "assistant") break;
          const id = typeof message.id === "string" ? message.id : createID("assistant");
          const text = contentText(message.content);
          const thinking = contentThinking(message.content);
          this.activeAssistantByThread[thread.id] = id;
          messages.push({
            id, role: "assistant", text, thinking, timestamp: nowLabel(), timestampMs: Date.now(), streaming: true, tools: [],
            activeExecution: thinking ? "thinking" : text ? "text" : undefined,
          });
          if (text || thinking) this.waitingForOutputByThread[thread.id] = false;
          break;
        }
        case "message_update": {
          const update = payload.assistantMessageEvent as Record<string, unknown> | undefined;
          const assistant = this.currentAssistant(thread.id, true);
          if (!update || !assistant) break;
          if (update.type === "text_delta" && typeof update.delta === "string") {
            assistant.text += update.delta;
            assistant.activeExecution = "text";
            if (update.delta) this.waitingForOutputByThread[thread.id] = false;
          }
          if (update.type === "thinking_delta" && typeof update.delta === "string") {
            assistant.thinking += update.delta;
            assistant.activeExecution = "thinking";
            if (update.delta) this.waitingForOutputByThread[thread.id] = false;
          }
          break;
        }
        case "message_end": {
          const message = payload.message as Record<string, unknown> | undefined;
          const assistant = this.currentAssistant(thread.id, false);
          if (assistant && message?.role === "assistant") {
            const finalText = contentText(message.content);
            const finalThinking = contentThinking(message.content);
            if (finalText) assistant.text = finalText;
            if (finalThinking) assistant.thinking = finalThinking;
            assistant.activeExecution = undefined;
            const finalError = runtimeErrorText(message.errorMessage);
            if (finalText || finalThinking || finalError) this.waitingForOutputByThread[thread.id] = false;
            if (finalError) {
              assistant.error = finalError;
              assistant.runNotice = { status: "failed", error: finalError };
            }
            const endedAt = messageTimestamp(message.timestamp) ?? Date.now();
            assistant.timestampMs = endedAt;
            assistant.timestamp = formatMessageTime(new Date(endedAt));
          }
          void this.refreshStats(thread.id).catch(() => undefined);
          break;
        }
        case "tool_execution_start": {
          this.waitingForOutputByThread[thread.id] = false;
          const assistant = this.currentAssistant(thread.id, true);
          if (!assistant) break;
          assistant.tools.push({
            id: String(payload.toolCallId ?? createID("tool")),
            name: String(payload.toolName ?? "tool"),
            arguments: payload.args,
            output: "",
            status: "running",
            startedAt: Date.now(),
            diff: buildToolDiff(String(payload.toolName ?? "tool"), payload.args),
          });
          assistant.activeExecution = "tool";
          if (REMOTE_MUTATING_TOOLS.has(String(payload.toolName ?? ""))) this.markRemoteRepositoryStale(thread.id);
          break;
        }
        case "tool_execution_update":
        case "tool_execution_end": {
          const assistant = this.currentAssistant(thread.id, true);
          if (!assistant) break;
          const toolID = String(payload.toolCallId ?? "");
          let tool = assistant.tools.find((item) => item.id === toolID);
          if (!tool) {
            tool = { id: toolID || createID("tool"), name: String(payload.toolName ?? "tool"), output: "", status: "running" };
            assistant.tools.push(tool);
          }
          assistant.activeExecution = sessionEvent.event.type === "tool_execution_end" ? undefined : "tool";
          const output = resultText(payload.partialResult ?? payload.result);
          if (output) {
            const bounded = boundedToolOutput(output);
            tool.output = bounded.text;
            tool.truncated = bounded.truncated || undefined;
          }
          if (sessionEvent.event.type === "tool_execution_end") {
            tool.status = payload.isError ? "error" : "complete";
            this.waitingForOutputByThread[thread.id] = true;
            if (tool.startedAt !== undefined) tool.durationMs = Math.max(0, Date.now() - tool.startedAt);
            const result = payload.result && typeof payload.result === "object" ? payload.result as Record<string, unknown> : undefined;
            tool.diff = buildToolDiff(tool.name, tool.arguments, result?.details) ?? tool.diff;
            if (REMOTE_MUTATING_TOOLS.has(tool.name)) this.markRemoteRepositoryStale(thread.id);
          }
          break;
        }
        case "compaction_start":
          // Pi writes the durable compaction entry only when compaction succeeds.
          // Keep start progress out of the conversation timeline, but keep
          // automatic compaction visibly busy until Pi emits agent_settled.
          if (payload.reason === "threshold" || payload.reason === "overflow") {
            thread.status = "running";
            this.waitingForOutputByThread[thread.id] = true;
            this.sessionOperationByThread[thread.id] = tr("topbar.compacting");
          }
          break;
        case "compaction_end":
          if (payload.reason === "threshold" || payload.reason === "overflow") {
            thread.status = "running";
            this.waitingForOutputByThread[thread.id] = true;
          }
          if (this.sessionOperationByThread[thread.id] === tr("topbar.compacting")) {
            this.sessionOperationByThread[thread.id] = undefined;
          }
          if (payload.errorMessage) {
            const message = String(payload.errorMessage);
            this.appendSystem(thread.id, message, message);
          } else if (payload.aborted === true) {
            break;
          } else {
            const marker = liveCompactionMessage(payload);
            if (!marker) break;
            this.applyCompactionEstimate(thread.id, marker.compaction!);
            if (thread.status === "idle" && thread.sessionFile) {
              void this.reloadSessionTranscript(thread).catch(() => undefined);
            } else {
              messages.push(marker);
            }
          }
          break;
        case "auto_retry_start": {
          thread.status = "running";
          const retryError = runtimeErrorText(payload.errorMessage);
          const retry = {
            attempt: Number(payload.attempt ?? 0),
            maxAttempts: Number(payload.maxAttempts ?? 0),
            delayMs: Number(payload.delayMs ?? 0),
            errorMessage: retryError,
          };
          this.retryByThread[thread.id] = retry;
          const assistant = this.currentAssistant(thread.id, false)
            ?? messages.findLast((message) => message.role === "assistant");
          const previousRetry = messages.findLast((message) => message.runNotice?.status === "retrying");
          if (previousRetry && previousRetry !== assistant && previousRetry.runNotice) {
            previousRetry.runNotice = { ...previousRetry.runNotice, status: "retried", delayMs: undefined };
          }
          if (assistant) {
            assistant.runNotice = {
              status: "retrying",
              error: retryError ?? assistant.error,
              attempt: retry.attempt,
              maxAttempts: retry.maxAttempts,
              delayMs: retry.delayMs,
            };
          }
          break;
        }
        case "auto_retry_end": {
          const retry = this.retryByThread[thread.id];
          const retrySucceeded = payload.success === true;
          const finalError = runtimeErrorText(payload.finalError) ?? retry?.errorMessage;
          const assistant = messages.findLast((message) => message.runNotice?.status === "retrying")
            ?? this.currentAssistant(thread.id, false)
            ?? messages.findLast((message) => message.role === "assistant");
          this.retryByThread[thread.id] = undefined;
          if (assistant) {
            assistant.runNotice = {
              status: retrySucceeded ? "recovered" : "failed",
              error: finalError ?? assistant.error,
              attempt: Number(payload.attempt ?? retry?.attempt ?? 0),
              maxAttempts: retry?.maxAttempts,
            };
          } else if (!retrySucceeded && finalError) {
            this.appendSystem(thread.id, `Retry failed: ${finalError}`, finalError);
          }
          break;
        }
        case "extension_error":
        case "protocol_error": {
          const message = sessionEvent.event.error || String(payload.error ?? "Pi extension error");
          if (!isClosedRPCStreamError(message)) this.appendSystem(thread.id, message, sessionEvent.event.error);
          break;
        }
        case "extension_ui_request": {
          const request = payload as unknown as ExtensionUIRequest;
          if (["select", "confirm", "input", "editor"].includes(request.method)) {
            const projected = blockingExtensionRequest(payload);
            if (projected) {
              this.extensionRequestByThread[thread.id] = projected;
            } else if (request.placeholder === BATCH_ASK_PLACEHOLDER) {
              const requestID = boundedExtensionText(request.id, 256).trim();
              delete this.extensionRequestByThread[thread.id];
              if (requestID) {
                void agentService.respondExtensionUI({ threadId: thread.id, requestId: requestID, cancelled: true });
              }
              this.appendSystem(thread.id, tr("extension.invalidBatchRequest"), tr("extension.invalidBatchRequest"));
            }
          } else if (request.method === "notify" && request.message) {
            if (isInternalRuntimeNotice(request.message)) break;
            this.appendSystem(thread.id, request.message, request.notifyType === "error" ? request.message : undefined);
          } else if (request.method === "setStatus") {
            const key = boundedExtensionText(request.statusKey, 256);
            if (!key) break;
            const statuses = { ...(this.extensionStatusesByThread[thread.id] ?? {}) };
            const text = extensionStatusText(request.statusText);
            if (text) statuses[key] = text;
            else delete statuses[key];
            this.extensionStatusesByThread[thread.id] = statuses;
          } else if (request.method === "setWidget") {
            const key = boundedExtensionText(request.widgetKey, 256);
            if (!key) break;
            const widgets = { ...(this.extensionWidgetsByThread[thread.id] ?? {}) };
            const lines = boundedWidgetLines(request.widgetLines);
            if (lines.length) {
              widgets[key] = {
                key,
                lines,
                placement: request.widgetPlacement === "belowEditor" ? "belowEditor" : "aboveEditor",
                instance: (widgets[key]?.instance ?? boundedExtensionText(request.id, 256)) || createID("widget"),
              };
            } else {
              delete widgets[key];
            }
            this.extensionWidgetsByThread[thread.id] = widgets;
          } else if (request.method === "setTitle") {
            this.extensionTitleByThread[thread.id] = boundedExtensionText(request.title, 200) || undefined;
          } else if (request.method === "set_editor_text") {
            this.draftsByThread[thread.id] = boundedExtensionText(request.text, 1 << 20);
          }
          break;
        }
        case "runtime_exit":
          piExitedGenerationByThread.set(thread.id, sessionEvent.event.generation);
          thread.started = false;
          this.waitingForOutputByThread[thread.id] = false;
          this.piProcessOrder = this.piProcessOrder.filter((id) => id !== thread.id);
          thread.status = "attention";
          thread.error = sessionEvent.event.error || "Pi process exited";
          this.finishAssistant(thread.id);
          if (thread.id !== this.activeThreadId || appIsInBackground()) thread.unread = true;
          this.queueByThread[thread.id] = { steering: [], followUp: [] };
          this.retryByThread[thread.id] = undefined;
          this.extensionRequestByThread[thread.id] = undefined;
          this.extensionStatusesByThread[thread.id] = {};
          this.extensionWidgetsByThread[thread.id] = {};
          this.extensionTitleByThread[thread.id] = undefined;
          if (!isClosedRPCStreamError(thread.error)) this.appendSystem(thread.id, thread.error, thread.error);
          this.scheduleDesktopStateSave();
          break;
      }
    },
    currentAssistant(threadId: string, create: boolean): TimelineMessage | undefined {
      const messages = this.messagesByThread[threadId] ?? (this.messagesByThread[threadId] = []);
      const activeID = this.activeAssistantByThread[threadId];
      let assistant = messages.find((message) => message.id === activeID);
      if (!assistant && create) {
        assistant = { id: createID("assistant"), role: "assistant", text: "", thinking: "", timestamp: nowLabel(), timestampMs: Date.now(), streaming: true, tools: [] };
        messages.push(assistant);
        this.activeAssistantByThread[threadId] = assistant.id;
      }
      return assistant;
    },
    finishAssistant(threadId: string) {
      const messages = this.messagesByThread[threadId] ?? [];
      const activeID = this.activeAssistantByThread[threadId];
      const activeIndex = messages.findIndex((message) => message.id === activeID);
      for (let index = activeIndex; index >= 0; index -= 1) {
        const message = messages[index];
        if (message.role !== "assistant") break;
        message.streaming = false;
        message.activeExecution = undefined;
      }
      delete this.activeAssistantByThread[threadId];
    },
    async loadBootstrapState() {
      this.bootstrapLoading = true;
      this.bootstrapError = "";
      try {
        this.bootstrap = await getBootstrapState();
      } catch (error) {
        this.bootstrapError = errorMessage(error) || "Desktop service is unavailable";
      } finally {
        this.bootstrapLoading = false;
      }
    },
    restoreDesktopPreferences(desktop: DesktopState) {
      if (!desktop.preferences) return;
      this.appearance = (desktop.preferences.appearance || "light") as Appearance;
      this.language = (desktop.preferences.language || "zh-CN") as Language;
      this.interfaceFont = (["default", "system", "serif", "mono"] as const).includes(desktop.preferences.fontFamily as InterfaceFont)
        ? desktop.preferences.fontFamily as InterfaceFont
        : "default";
      this.interfaceFontSize = Number.isInteger(desktop.preferences.fontSize) && desktop.preferences.fontSize >= 12 && desktop.preferences.fontSize <= 18
        ? desktop.preferences.fontSize
        : 14;
      setAppLanguage(this.language);
      this.offlineMode = desktop.preferences.offlineMode;
      this.proxyEnabled = desktop.preferences.proxyEnabled;
      this.proxyURL = desktop.preferences.proxyUrl || "socks5://127.0.0.1:10800";
      this.streamingBehavior = desktop.preferences.streamingBehavior as StreamingBehavior;
      this.sidebarCollapsed = desktop.preferences.sidebarCollapsed;
      this.setSidebarWidth(desktop.preferences.sidebarWidth || DEFAULT_SIDEBAR_WIDTH);
      this.inspectorOpen = desktop.preferences.inspectorOpen;
      this.setInspectorWidth(desktop.preferences.inspectorWidth || DEFAULT_INSPECTOR_WIDTH);
      this.inspectorTab = desktop.preferences.inspectorTab as InspectorTab;
      this.notificationsEnabled = desktop.preferences.notificationsEnabled ?? true;
      this.updateChecksEnabled = desktop.preferences.updateChecksEnabled ?? true;
      this.closeToTray = true;
      this.workspaceApplication = desktop.preferences.workspaceApplication || "";
    },
    async loadWorkspaceApplications() {
      this.workspaceApplicationsLoading = true;
      this.workspaceApplicationError = "";
      try {
        this.workspaceApplications = await catalogService.listWorkspaceApplications();
      } catch (error) {
        this.workspaceApplications = [];
        this.workspaceApplicationError = errorMessage(error);
      } finally {
        this.workspaceApplicationsLoading = false;
      }
    },
    async loadCatalog() {
      this.catalogLoading = true;
      this.catalogError = "";
      this.catalogReady = false;
      this.desktopStateReady = false;
      void this.loadWorkspaceApplications();
      try {
        const catalogPromise = Promise.all([
          catalogService.listWorkspaces(),
          catalogService.listSessions(),
        ]);
        const desktopPromise = catalogService.getDesktopState()
          .then((desktop) => {
            this.restoreDesktopPreferences(desktop);
            return desktop;
          })
          .finally(() => {
            // The shell may now render with either the restored preferences or
            // safe defaults. Session discovery is intentionally not on this path.
            this.desktopStateReady = true;
          });
        const [[catalogWorkspaces, sessions], desktop] = await Promise.all([catalogPromise, desktopPromise]);
        this.workspaces = catalogWorkspaces.map((workspace) => ({
          ...workspace,
          trust: workspace.trust as "approve" | "deny",
          discovered: false,
        }));
        const historicalThreads: ThreadSummary[] = [];
        for (const session of sessions as CatalogSession[]) {
          if (!session.cwd) continue;
          const basePath = session.cwd;
          let workspace = session.anchorWorkspaceId
            ? this.workspaces.find((item) => item.id === session.anchorWorkspaceId)
            : this.workspaces.find((item) => pathKey(item.path) === pathKey(basePath));
          if (session.anchorWorkspaceId && !workspace) continue;
          if (!workspace) {
            workspace = {
              id: `discovered-${session.id}`,
              name: workspaceName(basePath),
              path: basePath,
              trust: "deny",
              discovered: true,
              lastOpenedAt: session.modifiedAt,
            };
            this.workspaces.push(workspace);
          }
          const id = `session-${session.id}`;
          historicalThreads.push({
            id,
            title: session.title,
            workspace: workspace.name,
            workspaceId: workspace.discovered ? undefined : workspace.id,
            workspacePath: workspace.path,
            trust: workspace.trust,
            status: "idle",
            started: false,
            generation: 0,
            sessionId: session.id,
            sessionFile: session.path,
            createdAt: session.createdAt,
            modifiedAt: session.modifiedAt,
            messageCount: session.messageCount,
            firstMessage: session.firstMessage,
            parentSessionFile: session.parentSessionPath,
            unread: false,
          });
        }

        const drafts = new Map<string, string>();
        for (const saved of desktop.threads ?? []) {
          const existing = saved.sessionPath
            ? historicalThreads.find((thread) => thread.sessionFile && pathKey(thread.sessionFile) === pathKey(saved.sessionPath!)
              && (saved.workspaceId ? thread.workspaceId === saved.workspaceId : pathKey(thread.workspacePath) === pathKey(saved.workspacePath)))
            : undefined;
          const workspace = saved.workspaceId
            ? this.workspaces.find((item) => item.id === saved.workspaceId)
            : this.workspaces.find((item) => pathKey(item.path) === pathKey(saved.workspacePath));
          const interrupted = saved.status === "running" || saved.status === "starting";
          if (existing) {
            existing.id = saved.id;
            existing.trust = (workspace?.trust ?? saved.trust) as "approve" | "deny";
            if (workspace) {
              existing.workspace = workspace.name;
              existing.workspaceId = workspace.id;
              existing.workspacePath = workspace.path;
            }
            existing.unread = saved.unread;
            existing.status = interrupted ? "attention" : saved.status as ThreadStatus;
            if (interrupted) existing.error = "Previous Pi run was interrupted";
            existing.createdAt ||= saved.createdAt;
            existing.modifiedAt ||= saved.updatedAt;
            drafts.set(existing.id, saved.draft ?? "");
          } else if (!saved.sessionPath && workspace) {
            historicalThreads.push({
              id: saved.id,
              title: saved.title,
              workspace: workspace.name,
              workspaceId: workspace.id,
              workspacePath: workspace.path,
              trust: workspace.trust,
              status: interrupted ? "attention" : saved.status as ThreadStatus,
              started: false,
              generation: 0,
              createdAt: saved.createdAt,
              modifiedAt: saved.updatedAt,
              unread: saved.unread,
              error: interrupted ? "Previous Pi run was interrupted" : undefined,
            });
            drafts.set(saved.id, saved.draft ?? "");
          }
        }
        this.threads = historicalThreads;
        for (const thread of this.threads) {
          this.messagesByThread[thread.id] ??= [];
          this.transcriptEntriesByThread[thread.id] ??= [];
          this.draftsByThread[thread.id] = drafts.get(thread.id) ?? "";
          this.transcriptStateByThread[thread.id] ??= "idle";
        }
        this.activeThreadId = this.threads.some((thread) => thread.id === desktop.activeThreadId)
          ? desktop.activeThreadId ?? ""
          : this.threads[0]?.id ?? "";
        this.catalogReady = true;
      } catch (error) {
        this.catalogError = errorMessage(error);
      } finally {
        this.catalogLoading = false;
      }
    },
    async syncLocalSessions() {
      if (this.sessionSyncLoading) return;
      this.sessionSyncLoading = true;
      this.sessionSyncError = "";
      try {
        const sessions = await catalogService.listSessions() as CatalogSession[];
        for (const session of sessions) {
          if (!session.cwd) continue;
          let workspace = session.anchorWorkspaceId
            ? this.workspaces.find((item) => item.id === session.anchorWorkspaceId)
            : this.workspaces.find((item) => pathKey(item.path) === pathKey(session.cwd));
          if (session.anchorWorkspaceId && !workspace) continue;
          if (!workspace) {
            workspace = {
              id: `discovered-${session.id}`,
              name: workspaceName(session.cwd),
              path: session.cwd,
              trust: "deny",
              discovered: true,
              lastOpenedAt: session.modifiedAt,
            };
            this.workspaces.push(workspace);
          }
          const sessionFileKey = pathKey(session.path);
          const existing = this.threads.find((thread) => thread.sessionFile && pathKey(thread.sessionFile) === sessionFileKey);
          if (existing) {
            existing.title = session.title;
            existing.modifiedAt = session.modifiedAt;
            existing.messageCount = session.messageCount;
            existing.firstMessage = session.firstMessage;
            continue;
          }
          const thread: ThreadSummary = {
            id: `session-${session.id}`,
            title: session.title,
            workspace: workspace.name,
            workspaceId: workspace.discovered ? undefined : workspace.id,
            workspacePath: workspace.path,
            trust: workspace.trust,
            status: "idle",
            started: false,
            generation: 0,
            sessionId: session.id,
            sessionFile: session.path,
            createdAt: session.createdAt,
            modifiedAt: session.modifiedAt,
            messageCount: session.messageCount,
            firstMessage: session.firstMessage,
            parentSessionFile: session.parentSessionPath,
            unread: false,
          };
          this.threads.push(thread);
          this.messagesByThread[thread.id] = [];
          this.transcriptEntriesByThread[thread.id] = [];
          this.draftsByThread[thread.id] = "";
          this.transcriptStateByThread[thread.id] = "idle";
        }
        if (!this.activeThreadId) this.activeThreadId = this.threads[0]?.id ?? "";
        this.scheduleDesktopStateSave();
      } catch (error) {
        this.sessionSyncError = errorMessage(error);
      } finally {
        this.sessionSyncLoading = false;
      }
    },
    scheduleDesktopStateSave() {
      if (!this.catalogReady) return;
      if (desktopSaveTimer) clearTimeout(desktopSaveTimer);
      desktopSaveTimer = setTimeout(() => {
        desktopSaveTimer = undefined;
        void this.persistDesktopState();
      }, 250);
    },
    async persistDesktopState() {
      if (!this.catalogReady) return;
      const state: DesktopState = {
        activeThreadId: this.activeThreadId || undefined,
        preferences: {
          appearance: this.appearance,
          language: this.language,
          fontFamily: this.interfaceFont,
          fontSize: this.interfaceFontSize,
          offlineMode: this.offlineMode,
          proxyEnabled: this.proxyEnabled,
          proxyUrl: this.proxyURL.trim(),
          streamingBehavior: this.streamingBehavior,
          sidebarCollapsed: this.sidebarCollapsed,
          sidebarWidth: this.sidebarWidth,
          inspectorOpen: this.inspectorOpen,
          inspectorWidth: this.inspectorWidth,
          inspectorTab: this.inspectorTab,
          notificationsEnabled: this.notificationsEnabled,
          updateChecksEnabled: this.updateChecksEnabled,
          closeToTray: true,
          workspaceApplication: this.workspaceApplication || undefined,
        },
        threads: this.threads.map((thread) => ({
          id: thread.id,
          title: thread.title,
          workspaceId: thread.workspaceId,
          workspacePath: thread.workspacePath,
          trust: thread.trust,
          status: thread.status,
          sessionPath: thread.sessionFile,
          draft: this.draftsByThread[thread.id] || undefined,
          createdAt: thread.createdAt,
          updatedAt: thread.modifiedAt,
          unread: thread.unread || undefined,
        })),
      };
      try {
        await catalogService.saveDesktopState(state);
        this.settingsError = "";
      } catch (error) {
        this.settingsError = errorMessage(error);
      }
    },
  },
});
