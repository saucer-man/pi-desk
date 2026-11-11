export type Mode = "standard" | "minimal";
export interface AgentForm {name:string;description:string;model:string;instructions:string}
export interface PluginSummary { id:string;name:string;type:string;path:string;description:string;source:string;enabled:boolean }
export interface McpSummary { name:string;scope:"global"|"project";source:string;description:string;enabled:boolean;overridden:boolean }
export interface McpCatalog { paths:{global:string;project:string};servers:McpSummary[];errors:string[] }
export interface McpDefinition { name:string;scope:"global"|"project";source:string;definition:Record<string,unknown>;revision:string }
export interface McpDebugResult { state:string;error?:string;durationMs:number;serverInfo?:{name:string;version:string};instructions?:string;tools:{name:string;description?:string;inputSchema:unknown}[] }
export type Status = "idle" | "running" | "paused" | "error";
export interface Workspace { id: string; name: string; path: string; trusted: boolean; hidden?:boolean; pinned?:boolean; remote?: {target:string;port?:number;identity?:string;configFingerprint?:string;hostFingerprint?:string;workspaceId?:string;connected?:boolean;error?:string;execution?:"local-sdk";historyImported?:boolean} }
export interface DraftImage { type: "image"; id: string; name: string; mimeType: string; cacheKey?: string; data?: string; missing?: boolean }
export interface PendingPrompt { id: string; text: string; images: DraftImage[]; behavior: "steer" | "followUp"; paused: boolean; queuedText?: string }
export interface SessionRecord {
  id: string; title: string; workspaceId: string; cwd: string; mode: Mode;
  status: Status; sessionPath?: string; parentId?: string; depth: number;
  createdAt: string; updatedAt: string; draft: string; archived?: boolean; pinned?: boolean;
  draftImages?: DraftImage[]; pending?: PendingPrompt[]; unread?: boolean; scheduledTaskId?: string;
  error?: string; readOnly?: boolean; model?: string; thinking?: string; instructions?: string;
  remoteHistory?: {sourcePath:string;sha256:string;importedAt:string};
  modelRequests?: number; modelRequestsEstimated?: boolean;
  userTurns?: number; lastStats?: Stats;
  parentToolCallId?: string;
  worktree?: { root: string; branch: string; baseHead: string; baseBranch: string; commit?: string; integrated?: boolean };
}
export interface Content { type: string; text?: string; thinking?: string; name?: string; id?: string; arguments?: unknown; data?: string; mimeType?: string }
export interface ChatMessage { model?: string; provider?: string; entryId?: string; completedAt?: number; contextExcluded?: boolean; details?: { diff?: string }; role: string; content: string | Content[]; summary?: string; tokensBefore?: number; estimatedTokensAfter?: number; timestamp?: number; durationMs?: number; toolCallId?: string; toolName?: string; isError?: boolean; errorMessage?: string; usage?: { input?: number; output?: number; cacheRead?: number; cacheWrite?: number; totalTokens?: number } }
export interface TraceEntry { id: string; sessionId: string; runId: string; type: string; label: string; at: string; durationMs?: number; data?: unknown }
export interface RunMetrics { startedAt?: string; endedAt?: string; durationMs?: number; firstTokenMs?: number; tokensPerSecond?: number }
export interface Stats {
  userMessages: number; assistantMessages: number; modelRequests: number; modelRequestsEstimated?: boolean; toolCalls: number;
  tokens: { input: number; output: number; cacheRead: number; cacheWrite: number; total: number };
  performance?: { modelMs?: number; toolMs?: number; firstTokenMs?: number; tokensPerSecond?: number; timedResponses: number };
  contextUsage?: { tokens: number | null; contextWindow: number; percent: number | null; breakdown?: { system: number; tools: number; messages: number } };
}
export interface ExecutionState { streaming?: { timestamp: number; thinkingIndex?: number }; runningTools: string[]; retry?: TraceEntry }
export interface SessionSnapshot {
  record: SessionRecord; messages: ChatMessage[]; stats: Stats; metrics: RunMetrics; execution?: ExecutionState;
  tools: string[]; resources: { skills: { name: string; description: string }[]; extensions: string[]; prompts: { name: string; description: string }[] };
  model?: string; thinking: string; queue: { steering: readonly string[]; followUp: readonly string[] };
  widgets: Record<string, string[]>;
  aggregate: Stats & { sessions: number };
  thinkingLevels: string[]; systemPrompt: string;
}
export interface ModelInfo { id: string; provider: string; name: string; contextWindow: number; reasoning: boolean }
export interface ModelProbeLog { at:string; kind:"info"|"request"|"response"|"output"|"result"|"error"; data:unknown }
export interface OAuthState { provider: string; url?: string; message?: string; userCode?: string; status: "waiting" | "complete" | "error" | "cancelled" }
export interface UiRequest { id: string; sessionId?: string; type: string; title: string; message?: string; options?: string[]; url?: string; oauth?: { provider: string; kind: string; placeholder?: string; labels?: string[] } }
export interface ScheduledTask { id:string;name:string;prompt:string;workspaceId:string;model:string;thinking?:string;mode:Mode;frequency:"once"|"daily"|"weekly";time?:string;weekday?:number;runAt?:string;enabled:boolean;nextRunAt?:string;lastRunAt?:string;lastSessionId?:string;lastStatus?:"running"|"idle"|"paused"|"error"|"skipped";lastError?:string;lastSlot?:string;createdAt:string;updatedAt:string }
export interface ProxyConfig { global?: string; providers?:Record<string,string|null>; models?: Record<string, string | null> }
export interface State { sshLegacyImported?:boolean; schedules?: ScheduledTask[]; version: 1; workspaces: Workspace[]; sessions: SessionRecord[]; preferences: { theme: "light" | "dark" | "system"; maxRetries?: number; disabledSkills?: string[]; pluginOverrides?:Record<string,boolean>; proxy?: ProxyConfig; modelSelection?:Record<string,string[]> }; migratedFrom?: string }
export interface ServerEvent { type: string; sessionId?: string; data?: unknown }

export interface ManagedModel { id: string; name?: string; contextWindow?: number; maxTokens?: number; reasoning?: boolean; input?: string[]; compaction?: Record<string, number>; [key: string]: any }
export interface ManagedProvider { id: string; baseUrl?: string; api?: string; hasApiKey?: boolean; models: ManagedModel[]; [key: string]: any }
export interface ModelConfig { path: string; providers: ManagedProvider[] }
