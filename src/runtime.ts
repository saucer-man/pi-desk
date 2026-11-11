import { randomUUID } from "node:crypto";
import { AsyncLocalStorage } from "node:async_hooks";
import { Agent, ProxyAgent, fetch as proxyFetch } from "undici";
import { existsSync, readFileSync, realpathSync, unlinkSync } from "node:fs";
import { join, resolve, relative } from "node:path";
import {
  createAgentSession, DefaultResourceLoader, SessionManager, SettingsManager, ModelRuntime, Theme, getPackageDir,
  estimateTokens, convertToLlm, sessionEntryToContextMessages, buildSessionProjection,
  type AgentSession, type AgentSessionEvent, type ExtensionUIContext,
} from "@earendil-works/pi-coding-agent";
import { Store, agentDir, deskDir, sessionRoot, safeError, inside } from "./storage.ts";
import type { Mode, SessionRecord, SessionSnapshot, ServerEvent, TraceEntry, RunMetrics, UiRequest, ChatMessage, PendingPrompt, DraftImage, ExecutionState } from "./types.ts";
import { subagentTool } from "./subagents.ts";
import { skillCatalog, pluginCatalog, embeddedPlugins } from "./resources.ts";
import { Scheduler } from "./schedules.ts";
import { RemoteManager } from "./remote.ts";
import { remoteTools } from "./remote-tools.ts";
import { BrowserHost, browserTool } from "./browser.ts";

const themeTokens = JSON.parse(readFileSync(join(getPackageDir(), "dist/modes/interactive/theme/dark.json"), "utf8")).colors;
const plainColors = Object.fromEntries(Object.keys(themeTokens).map(key => [key, ""]));
const webTheme = new Theme(plainColors as ConstructorParameters<typeof Theme>[0], plainColors as ConstructorParameters<typeof Theme>[1], "truecolor", { name: "web", appearance: "light" });

// Google adapters use global fetch; async context keeps concurrent model routes isolated.
const proxyContext = new AsyncLocalStorage<Agent | ProxyAgent>();
const originalFetch = globalThis.fetch;
globalThis.fetch = (input, init) => {
  const dispatcher = proxyContext.getStore();
  return dispatcher ? (proxyFetch as unknown as typeof originalFetch)(input, { ...init, dispatcher } as RequestInit) : originalFetch(input, init);
};

interface Live {
  session: AgentSession; loader: DefaultResourceLoader; metrics: RunMetrics;
  runId: string; started: number; turnStarted: number; firstDelta?: number;
  lastDelta?: number; generationMs: number; output: number;
  tools: Map<string, { started: number; trace: TraceEntry }>;
  widgets: Record<string, string[]>;
  progressTimer?: ReturnType<typeof setTimeout>;
  modelNeedsSelection?: boolean;
  resourcesStale?: boolean;
  streaming?: ExecutionState["streaming"];
}
export class Runtime {
  readonly store = new Store();
  readonly remote = new RemoteManager(this);
  readonly browser = new BrowserHost();
  readonly scheduler = new Scheduler(this);
  readonly live = new Map<string, Live>();
  readonly creating = new Map<string, Promise<Live>>();
  readonly listeners = new Set<((event: ServerEvent) => void) & {sessions?: Set<string>}>();
  readonly requests = new Map<string, { request: UiRequest; resolve: (value: unknown) => void }>();
  readonly authControllers = new Set<AbortController>();
  private catalog?: Promise<ModelRuntime>;
  private sessionOwners = new Map<string, string>();
  private stopping = new Set<string>();
  private closing = false;
  private proxyAgents = new Map<string, Agent | ProxyAgent>();
  private withProxy<T>(model: {provider:string;id:string}, request:(env?:Record<string,string>)=>T): T {
    const config=this.store.state.preferences.proxy, key=`${model.provider}/${model.id}`;
    const proxy=config?.models && Object.hasOwn(config.models,key) ? config.models[key] : config?.global;
    if(proxy===undefined || proxy==="")return proxyContext.exit(()=>request());
    const route=proxy??"";
    let agent=this.proxyAgents.get(route);
    if(!agent){agent=proxy===null?new Agent():new ProxyAgent({uri:proxy,proxyTunnel:true});this.proxyAgents.set(route,agent);}
    // Bedrock uses Node HTTP agents instead of fetch. Override both env casings and NO_PROXY.
    const env={http_proxy:route,https_proxy:route,all_proxy:route,HTTP_PROXY:route,HTTPS_PROXY:route,ALL_PROXY:route,no_proxy:proxy===null?"*":" ",NO_PROXY:proxy===null?"*":" "};
    return proxyContext.run(agent,()=>request(env));
  }
  emit(event: ServerEvent) { for (const listener of this.listeners) listener(event); }
  changed(record?: SessionRecord) { this.store.save(); this.emit({ type: "state", data: this.store.state }); if (record) this.emit({ type: "record", sessionId: record.id, data: record }); }
  modelRuntime() { return this.catalog ??= ModelRuntime.create({ authPath: join(agentDir, "auth.json"), modelsPath: join(agentDir, "models.json"), allowModelNetwork: false }); }
  async models(id?: string, refresh=false) {
    const models = id ? (await this.get(id)).session.modelRuntime : await this.modelRuntime();
    const result = await models.refresh({ allowNetwork: refresh, force: refresh, signal: refresh ? AbortSignal.timeout(15000) : undefined });
    const settings = SettingsManager.create(agentDir, agentDir).getGlobalSettings();
    return { defaultProvider: settings.defaultProvider, defaultModel: settings.defaultModel, models: models.getAvailableSnapshot().map(m => ({ id: m.id, provider: m.provider, name: m.name, contextWindow: m.contextWindow, reasoning: Boolean(m.reasoning) })), providers: models.getProviders().map(p => ({ id: p.id, name: p.name, configured: models.hasConfiguredAuth(p.id), oauth: Boolean(p.auth.oauth?.login), apiKey: Boolean(p.auth.apiKey?.login) })), warning: [models.getError(),...Array.from(result.errors,([provider,error])=>`${provider}: ${safeError(error)}`),result.aborted?"模型刷新超时，请重试":undefined].filter(Boolean).join("；") || undefined, agentDir };
  }
  async testModel(provider: string, id: string, sessionId?: string) {
    const models=sessionId?(await this.get(sessionId)).session.modelRuntime:await this.modelRuntime();await models.refresh({allowNetwork:false});
    const model=models.getModel(provider,id);if(!model)throw new Error("模型不存在，请刷新后重试");
    const started=performance.now();
    const result=await this.withProxy(model,env=>models.completeSimple(model,{messages:[{role:"user",content:[{type:"text",text:"Reply with OK."}],timestamp:Date.now()}]},{maxTokens:16,maxRetries:0,reasoning:"minimal",signal:AbortSignal.timeout(15000),...(env?{env,transport:"sse" as const}:{})}));
    if(result.stopReason==="error"||result.stopReason==="aborted")throw new Error(result.errorMessage||"模型连接测试失败或超时");
    return {durationMs:Math.round(performance.now()-started)};
  }
  async discover() {
    const known = new Set(this.store.state.sessions.map(r => r.sessionPath));
    for (const info of await SessionManager.listAll(sessionRoot)) {
      if (known.has(info.path) || !info.cwd || relative(sessionRoot,info.path).split(/[/\\]/)[0].startsWith("--ssh-")) continue;
      let workspace = this.store.state.workspaces.find(w => w.path.toLowerCase() === info.cwd.toLowerCase());
      if (!workspace) { workspace = { id: randomUUID(), name: info.cwd.split(/[/\\]/).filter(Boolean).at(-1) || info.cwd, path: info.cwd, trusted: true }; this.store.state.workspaces.push(workspace); }
      this.store.state.sessions.push({ id: info.id, title: info.name || info.firstMessage.slice(0, 80) || "历史会话", workspaceId: workspace.id, cwd: info.cwd, mode: "standard", depth: 0, status: "idle", sessionPath: info.path, draft: "", createdAt: info.created.toISOString(), updatedAt: info.modified.toISOString() });
    }
    this.changed();
  }
  create(workspaceId: string, mode: Mode, extra: Partial<SessionRecord> = {}) {
    const w = this.store.state.workspaces.find(w => w.id === workspaceId);
    if (!w) throw new Error("工作区不存在");
    const now = new Date().toISOString(); const defaults = SettingsManager.create(w.remote ? join(deskDir,"ssh-workspaces",w.id) : w.path, agentDir).getSettings();
    const record: SessionRecord = { id: randomUUID(), title: "新会话", workspaceId, cwd: w.path, mode, model: defaults.defaultProvider && defaults.defaultModel ? `${defaults.defaultProvider}/${defaults.defaultModel}` : undefined, depth: 0, draft: "", createdAt: now, updatedAt: now, status: "idle", ...extra };
    this.store.state.sessions.unshift(record); this.changed(record); return record;
  }
  async get(id: string): Promise<Live> {
    const pending = this.creating.get(id); if (pending) return pending;
    const existing = this.live.get(id); if (existing&&(!existing.resourcesStale||!existing.session.isIdle||this.store.record(id).status==="running")) return existing;
    const record = this.store.record(id);
    const source = record.sessionPath && existsSync(record.sessionPath) ? realpathSync(record.sessionPath) : undefined;
    const key = source && (process.platform === "win32" ? source.toLowerCase() : source);
    if (key) { const owner = this.sessionOwners.get(key); if (owner && owner !== id) throw new Error("此原生会话已在另一个入口打开，请切换使用或创建独立分支"); this.sessionOwners.set(key, id); }
    const promise = Promise.resolve().then(async()=>{if(existing){await this.release(existing,"reload");this.live.delete(id);}return this.load(id);}); this.creating.set(id, promise);
    try { const live = await promise; const file = live.session.sessionManager.getSessionFile(); if (file) this.sessionOwners.set(process.platform === "win32" ? file.toLowerCase() : file, id); return live; }
    catch (error) { if (key && this.sessionOwners.get(key) === id) this.sessionOwners.delete(key); throw error; }
    finally { this.creating.delete(id); }
  }
  private async load(id: string): Promise<Live> {
    let record = this.store.record(id);
    const workspace = this.store.state.workspaces.find(w => w.id === record.workspaceId);
    if (!workspace) throw new Error("工作区不存在");
    if (record.sessionPath && [...this.live.values()].some(l => l.session.sessionManager.getSessionFile() === record.sessionPath)) throw new Error("此原生会话已经在另一个入口打开");
    const contextCwd=workspace.remote?await this.remote.anchor(workspace):record.cwd;
    if(workspace.remote&&record.sessionPath&&!existsSync(record.sessionPath))throw new Error("SSH 历史尚未完整导入，拒绝创建空会话");
    const diskSettings = SettingsManager.create(contextCwd, agentDir);
    const minimal = record.mode === "minimal";
    const overrides={...this.store.state.preferences.pluginOverrides};
    if(record.worktree&&!workspace.remote)for(const [path,enabled] of Object.entries(overrides))if(!path.startsWith("builtin:")&&inside(workspace.path,path))overrides[resolve(record.cwd,relative(workspace.path,path))]=enabled;
    const plugins=minimal?[]:await pluginCatalog(contextCwd,overrides);
    const settings = minimal ? SettingsManager.inMemory({ ...diskSettings.getSettings(), packages: [], extensions: [], skills: [] }) : diskSettings;
    let loader:DefaultResourceLoader;
    const remote=workspace.remote?remoteTools(this,workspace,record,contextCwd,()=>loader.getSkills().skills):undefined;
    loader = new DefaultResourceLoader({ cwd: contextCwd, agentDir, settingsManager: settings,
      noExtensions: true, noSkills: minimal, noThemes: true,
      disabledBuiltinExtensions:embeddedPlugins.filter(p=>p.factory&&overrides[p.id]===false).map(p=>p.name),
      // Discover with the native package manager, then pass only enabled entry points to the SDK.
      additionalExtensionPaths: plugins.filter(p=>p.enabled&&p.id!=="builtin:subagent").map(p=>p.id), additionalSkillPaths: [],
      extensionFactories: [...(minimal ? [] : embeddedPlugins.flatMap(p=>p.factory?[p.factory]:[])),...(remote?[{name:"pi-desk-remote",hidden:true,factory:remote.extension}]:[])],
      appendSystemPrompt: record.instructions ? [record.instructions] : [],
    });
    const nativeSkills=loader.getSkills.bind(loader);
    loader.getSkills=()=>{const result=nativeSkills();return {...result,skills:result.skills.filter(s=>!this.store.state.preferences.disabledSkills?.includes(s.name)&&existsSync(s.filePath))};};
    await loader.reload({ resolveProjectTrust: async () => true });
    const manager = record.sessionPath && existsSync(record.sessionPath)
      ? SessionManager.open(record.sessionPath, undefined, contextCwd)
      : SessionManager.create(contextCwd, workspace.remote?join(sessionRoot,"--ssh-"+workspace.id+"--"):join(sessionRoot, `--${record.cwd.replace(/^[/\\]/, "").replace(/[/\\:]/g, "-")}--`));
    const modelRuntime = await ModelRuntime.create({ authPath: join(agentDir, "auth.json"), modelsPath: join(agentDir, "models.json"), allowModelNetwork: false });
    record.modelRequests = 0; record.modelRequestsEstimated = false;
    let requested = false;
    for (const entry of manager.getEntries()) {
      if (entry.type === "custom" && entry.customType === "pi-desk-event") {
        const type = (entry.data as { type?: string } | undefined)?.type;
        if (type === "model_request") { record.modelRequests++; requested = true; }
        else if (type === "run_start" || type === "settled") requested = false;
      } else if (entry.type === "message" && entry.message.role === "user") requested = false;
      else if (entry.type === "message" && entry.message.role === "assistant" || entry.type === "compaction" || entry.type === "branch_summary") {
        if (!requested) { record.modelRequests++; record.modelRequestsEstimated = true; }
        requested = false;
      }
    }
    const countRequest = (model: { provider: string; id: string }) => {
      if (!modelRuntime.getPhysicalModel(model.provider, model.id)) return;
      record.modelRequests = (record.modelRequests || 0) + 1;
      this.trace(id, "model_request", "模型调用", { provider: model.provider, model: model.id }); this.store.save();
      this.progress(id);
    };
    const stream = modelRuntime.stream.bind(modelRuntime); const streamSimple = modelRuntime.streamSimple.bind(modelRuntime);
    modelRuntime.stream = (model, context, options) => { countRequest(model); return this.withProxy(model,env=>stream(model, context, env?{...options,env:{...options?.env,...env},transport:"sse"} as typeof options:options)); };
    modelRuntime.streamSimple = (model, context, options) => { countRequest(model); return this.withProxy(model,env=>streamSimple(model, context, env?{...options,env:{...options?.env,...env},transport:"sse"}:options)); };
    const model = record.model ? modelRuntime.getAvailableSnapshot().find(m => `${m.provider}/${m.id}` === record.model) : undefined;
    const subagents=record.depth<2&&overrides["builtin:subagent"]!==false;
    const customTools = [...(remote?.tools||[]),...(minimal?[]:[...(subagents?[subagentTool(this,record)]:[]),...(!record.readOnly?[browserTool(this,record)]:[])])];
    const result = await createAgentSession({ cwd: contextCwd, agentDir, sessionManager: manager, settingsManager: settings, resourceLoader: loader, modelRuntime, model,
      tools: record.readOnly ? (minimal ? ["read"] : ["read", "grep", "find", "ls", ...(subagents ? ["subagent"] : [])]) : minimal ? ["read", "bash", "edit", "write"] : undefined,
      customTools, excludeTools: workspace.remote?["powershell"]:undefined,
    });
    if (result.extensionsResult.errors.length) { result.session.dispose(); throw new Error("扩展加载失败：" + result.extensionsResult.errors.map(e => safeError(e.error)).join("；")); }
    const selected = record.model && modelRuntime.getAvailableSnapshot().find(m => `${m.provider}/${m.id}` === record.model);
    if (selected && selected !== result.session.model) await result.session.setModel(selected);
    const live: Live = { session: result.session, loader, metrics: {}, runId: "", started: 0, turnStarted: 0, generationMs: 0, output: 0, tools: new Map(), widgets: {}, modelNeedsSelection: Boolean(record.model && !selected || result.modelFallbackMessage) };
    this.live.set(id, live);
    live.metrics = (this.traces(id).filter(e => e.type === "settled").at(-1)?.data as RunMetrics) || {};
    result.session.subscribe(event => this.onEvent(record, live, event));
    try { await result.session.bindExtensions({ mode: "rpc", uiContext: this.ui(id), onError: error => { record.error = safeError(error); this.emit({ type: "notice", sessionId: id, data: { type: "error", message: record.error } }); this.changed(record); } }); }
    catch (error) { await this.release(live, "quit").catch(() => {}); this.live.delete(id); throw error; }
    if (record.thinking) result.session.setThinkingLevel(record.thinking as Parameters<AgentSession["setThinkingLevel"]>[0]);
    record.sessionPath = manager.getSessionFile(); this.changed(record);
    return live;
  }
  private onEvent(record: SessionRecord, live: Live, event: AgentSessionEvent) {
    const now = performance.now();
    if (event.type === "message_start" || event.type === "message_end") { record.updatedAt = new Date().toISOString(); this.changed(record); }
    if (event.type === "queue_update") {
      const remaining = { steer: [...event.steering], followUp: [...event.followUp] };
      record.pending = (record.pending || []).filter(p => { if (p.paused || !p.queuedText) return true; const queue = remaining[p.behavior]; const index = queue.indexOf(p.queuedText); if (index < 0) return false; queue.splice(index, 1); return true; });
      this.changed(record);
    }
    if (event.type === "agent_start" && record.status !== "running") { record.status = "running"; live.started = now; live.runId = randomUUID(); live.metrics = { startedAt: new Date().toISOString() }; this.changed(record); }
    if (event.type === "turn_start") { live.turnStarted = now; live.firstDelta = undefined; live.lastDelta = undefined; this.trace(record.id, "model", "模型轮次开始"); }
    if ((event.type === "message_start" || event.type === "message_update") && event.message.role === "assistant") {
      const delta = event.type === "message_update" ? event.assistantMessageEvent : undefined;
      live.streaming = { timestamp: event.message.timestamp, thinkingIndex: delta && (delta.type === "thinking_start" || delta.type === "thinking_delta") ? delta.contentIndex : undefined };
    }
    if (event.type === "message_update" && ["text_delta", "thinking_delta", "toolcall_delta"].includes(event.assistantMessageEvent.type)) {
      live.firstDelta ??= now; live.lastDelta = now;
      if (live.metrics.firstTokenMs === undefined && live.started) { live.metrics.firstTokenMs = now - live.started; this.progress(record.id); }
    }
    if (event.type === "message_end") {
      const message = event.message;
      if (message.role === "assistant") {
        if (message.stopReason === "error") {
          record.error = safeError(message.errorMessage || "模型调用失败");
          for (const pending of record.pending || []) pending.paused = true;
          live.session.clearQueue();
          void live.session.abort().catch(error => this.emit({ type: "notice", sessionId: record.id, data: { message: safeError(error) } }));
          this.trace(record.id, "error", record.error);
        }
        live.streaming = undefined;
        if (live.firstDelta !== undefined && live.lastDelta !== undefined) live.generationMs += Math.max(0, live.lastDelta - live.firstDelta);
        live.output += message.usage?.output || 0;
        if (live.generationMs > 0) live.metrics.tokensPerSecond = live.output / (live.generationMs / 1000);
        this.trace(record.id, "assistant", "模型响应完成", { messageTimestamp: message.timestamp, firstTokenMs: live.firstDelta === undefined ? undefined : live.firstDelta - live.turnStarted, decodeMs: live.firstDelta === undefined || live.lastDelta === undefined ? undefined : live.lastDelta - live.firstDelta }, live.turnStarted ? now - live.turnStarted : undefined);
        this.progress(record.id);
      }
    }
    if (event.type === "tool_execution_start") { const trace = this.trace(record.id, "tool_start", event.toolName, { toolCallId: event.toolCallId, parentToolCallId: event.parentToolCallId }); live.tools.set(event.toolCallId, { started: now, trace }); }
    if (event.type === "tool_execution_end") { const tool = live.tools.get(event.toolCallId); this.trace(record.id, event.isError ? "tool_error" : "tool_end", event.toolName, { toolCallId: event.toolCallId, parentToolCallId: event.parentToolCallId }, tool ? now - tool.started : undefined); live.tools.delete(event.toolCallId); }
    if (["compaction_start", "compaction_end", "auto_retry_start", "auto_retry_end"].includes(event.type)) this.trace(record.id, event.type, event.type.startsWith("compaction") ? "上下文压缩" : "自动重试", Object.fromEntries(Object.entries(event).filter(([key]) => key !== "type" && key !== "result")));
    if (event.type === "agent_settled") {
      live.streaming = undefined; live.tools.clear();
      record.status = record.error ? "error" : event.aborted ? "paused" : "idle";
      live.metrics.endedAt = new Date().toISOString(); live.metrics.durationMs = live.started ? now - live.started : undefined;
      record.unread = true; record.updatedAt = live.metrics.endedAt; this.trace(record.id, "settled", event.aborted ? "运行已停止" : "运行结束", live.metrics); this.changed(record); this.progress(record.id);
    }
    record.sessionPath = live.session.sessionManager.getSessionFile();
    this.emit({ type: "event", sessionId: record.id, data: event });
  }
  trace(id: string, type: string, label: string, data?: unknown, durationMs?: number) {
    const live = this.live.get(id); if (!live) throw new Error("会话尚未初始化，无法记录运行事件");
    const event = JSON.parse(JSON.stringify({ runId: live.runId, type, label, durationMs, data })) as Omit<TraceEntry, "id" | "sessionId" | "at">;
    const entryId = live.session.sessionManager.appendCustomEntry("pi-desk-event", event);
    const entry: TraceEntry = { ...event, id: entryId, sessionId: id, at: live.session.sessionManager.getEntry(entryId)!.timestamp };
    this.emit({ type: "trace", sessionId: id, data: entry }); return entry;
  }
  traces(id: string): TraceEntry[] {
    const record = this.store.record(id);
    const manager = this.live.get(id)?.session.sessionManager || (record.sessionPath && existsSync(record.sessionPath) ? SessionManager.open(record.sessionPath) : undefined);
    return (manager?.getEntries() || []).flatMap(entry => {
      if (entry.type !== "custom" || entry.customType !== "pi-desk-event" || !entry.data || typeof entry.data !== "object") return [];
      const data = entry.data as Omit<TraceEntry, "id" | "sessionId" | "at">;
      return typeof data.type === "string" && typeof data.label === "string" ? [{ ...data, id: entry.id, sessionId: id, at: entry.timestamp }] : [];
    });
  }
  private progress(id: string) {
    const live = this.live.get(id); if (!live || !this.listeners.size || this.closing) return;
    const parent = this.store.record(id).parentId; if (parent) this.progress(parent);
    const watched=()=>[...this.listeners].some(listener=>!listener.sessions||listener.sessions.has(id));
    if(live.progressTimer||!watched())return;
    live.progressTimer = setTimeout(() => {
      live.progressTimer = undefined; if (this.live.get(id) !== live || this.closing || !watched()) return;
      void this.snapshot(id).then(data => this.emit({ type: "snapshot", sessionId: id, data })).catch(error => this.emit({ type: "notice", sessionId: id, data: { message: safeError(error) } }));
    }, 100);
    live.progressTimer.unref();
  }
  async snapshot(id: string): Promise<SessionSnapshot> {
    const record = this.store.record(id);
    const { session, loader, metrics, streaming, tools } = await this.get(id);
    const stats = session.getSessionStats();
    const traces = this.traces(id);
    const timing = new Map(traces.filter(t=>t.type === "assistant").map(t=>[(t.data as {messageTimestamp?:number}|undefined)?.messageTimestamp,t]));
    const toolTiming = new Map(traces.filter(t=>t.type === "tool_end" || t.type === "tool_error").map(t=>[(t.data as {toolCallId?:string}|undefined)?.toolCallId,t.durationMs]));
    let modelMs=0, toolMs=0, timedResponses=0, timedTools=0, ttftMs=0, ttftCount=0, decodeMs=0, decodeTokens=0;
    for(const entry of session.sessionManager.getEntries()) {
      if(entry.type !== "message") continue;
      const m=entry.message;
      if(m.role === "assistant") {
        const trace=timing.get(m.timestamp), data=trace?.data as {firstTokenMs?:number;decodeMs?:number}|undefined;
        const duration=m.durationMs ?? trace?.durationMs;
        if(duration !== undefined && Number.isFinite(duration) && duration >= 0) {modelMs+=duration;timedResponses++;}
        if(data?.firstTokenMs !== undefined && Number.isFinite(data.firstTokenMs) && data.firstTokenMs >= 0) {ttftMs+=data.firstTokenMs;ttftCount++;}
        if(data?.decodeMs && data.decodeMs > 0 && Number.isFinite(data.decodeMs) && m.usage.output > 0) {decodeMs+=data.decodeMs;decodeTokens+=m.usage.output;}
      } else if(m.role === "toolResult") {
        const duration=m.durationMs ?? toolTiming.get(m.toolCallId);
        if(duration !== undefined && Number.isFinite(duration) && duration >= 0) {toolMs+=duration;timedTools++;}
      }
    }
    const performance={modelMs:timedResponses?modelMs:undefined,toolMs:timedTools?toolMs:undefined,firstTokenMs:ttftCount?ttftMs/ttftCount:undefined,tokensPerSecond:decodeMs>0?decodeTokens/(decodeMs/1000):undefined,timedResponses};
    let contextUsage: SessionSnapshot["stats"]["contextUsage"] = stats.contextUsage;
    if(contextUsage?.tokens != null) {
      // SDK occupancy is provider-anchored; component shares are character-based estimates, not billing counts.
      const active=new Set(session.getActiveToolNames());
      const weights={system:Math.ceil(session.systemPrompt.length/4),tools:Math.ceil(JSON.stringify(session.getAllTools().filter(t=>active.has(t.name)).map(({name,description,parameters})=>({name,description,parameters}))).length/4),messages:convertToLlm(session.messages).filter(m=>m.role!=="system").reduce((sum,m)=>sum+estimateTokens(m),0)};
      const total=weights.system+weights.tools+weights.messages;
      const system=total?Math.round(contextUsage.tokens*weights.system/total):0, tools=total?Math.min(contextUsage.tokens-system,Math.round(contextUsage.tokens*weights.tools/total)):0;
      contextUsage={...contextUsage,breakdown:{system,tools,messages:contextUsage.tokens-system-tools}};
    }
    const own = { userMessages: stats.userMessages, assistantMessages: stats.assistantMessages, modelRequests: record.modelRequests || 0, modelRequestsEstimated: record.modelRequestsEstimated, toolCalls: stats.toolCalls, tokens: stats.tokens, contextUsage, performance };
    record.lastStats = own;
    const related = new Set([id]); let changed = true;
    while (changed) { changed = false; for (const r of this.store.state.sessions) if (r.parentId && related.has(r.parentId) && !related.has(r.id)) { related.add(r.id); changed = true; } }
    const aggregate = { ...own, tokens: { ...own.tokens }, sessions: 1 }; const seen = new Set([record.sessionPath || id]);
    for (const child of this.store.state.sessions.filter(r => r.id !== id && related.has(r.id))) {
      const identity = child.sessionPath || child.id; if (seen.has(identity)) continue; seen.add(identity);
      const liveStats = this.live.get(child.id)?.session.getSessionStats();
      const totals = liveStats || child.lastStats; if (!totals) continue;
      aggregate.sessions++; aggregate.userMessages += totals.userMessages; aggregate.assistantMessages += totals.assistantMessages; aggregate.modelRequests += child.modelRequests || 0; aggregate.toolCalls += totals.toolCalls;
      aggregate.modelRequestsEstimated ||= child.modelRequestsEstimated;
      for (const key of ["input", "output", "cacheRead", "cacheWrite", "total"] as const) aggregate.tokens[key] += totals.tokens[key];
    }
    // Display the full branch, including compacted/excluded history; the SDK controls model context.
    const branch = session.sessionManager.getBranch();
    const excluded = new Set<string>(); const hidden = new Set<string>(); const replacements = new Map<string,ChatMessage["content"]>();
    for(const entry of branch) {
      if(entry.type === "context_edit") { if(entry.replacement === null) excluded.add(entry.targetId); else { excluded.delete(entry.targetId); replacements.set(entry.targetId,entry.replacement.content as ChatMessage["content"]); } }
      if(entry.type === "custom" && entry.customType === "pi-desk-message-delete") { const target=(entry.data as {entryId?:string}|undefined)?.entryId; if(target)hidden.add(target); }
    }
    const persisted = branch.flatMap(sourceEntry => {
      const estimatedTokensAfter=sourceEntry.type==="compaction"?buildSessionProjection(branch,sourceEntry.id).messages.reduce((sum,m)=>sum+estimateTokens(m),0):undefined;
      return sessionEntryToContextMessages(sourceEntry).map(message=>({...message, ...(message.role==="compactionSummary"?{estimatedTokensAfter}:{}), ...(replacements.has(sourceEntry.id)?{content:replacements.get(sourceEntry.id)}:{}), entryId:sourceEntry.id, completedAt:Date.parse(sourceEntry.timestamp), contextExcluded:excluded.has(sourceEntry.id)} as ChatMessage));
    });
    const key = (m:ChatMessage) => `${m.role}/${m.timestamp}/${m.toolCallId||""}`;
    const known = new Set(persisted.map(key));
    const messages = persisted.filter(m=>!hidden.has(m.entryId!));
    const pending = [...session.messages as ChatMessage[], ...(session.state.streamingMessage ? [session.state.streamingMessage as ChatMessage] : [])];
    for (const message of pending) if (!known.has(key(message))) { messages.push(message); known.add(key(message)); }
    return { record, messages, stats: own, aggregate, metrics, execution: { streaming, runningTools: [...tools.keys()] }, widgets: this.live.get(id)!.widgets, thinkingLevels: session.getAvailableThinkingLevels(), systemPrompt: session.systemPrompt,
      tools: session.getActiveToolNames(), model: session.model && `${session.model.provider}/${session.model.id}`, thinking: session.thinkingLevel,
      resources: { skills: loader.getSkills().skills.map(s => ({ name: s.name, description: s.description })), extensions: loader.getExtensions().extensions.filter(e=>!e.hidden).map(e => e.path), prompts: loader.getPrompts().prompts.map(p => ({ name: p.name, description: p.description })) },
      queue: { steering: session.getSteeringMessages(), followUp: session.getFollowUpMessages() },
    };
  }
  async preparePrompt(id: string) {
    const record = this.store.record(id), live = await this.get(id);
    if (live.session.isIdle && record.status !== "running") {
      live.session.setActiveToolsByName(live.session.getActiveToolNames());
      await live.session.modelRuntime.refresh({ allowNetwork: false });
      const current = live.session.model;
      const model = current && live.session.modelRuntime.getAvailableSnapshot().find(m => m.provider === current.provider && m.id === current.id);
      if (current && !model) live.modelNeedsSelection = true;
      if (model && JSON.stringify(model) !== JSON.stringify(current)) await live.session.setModel(model);
      // Minimal settings are in memory; reload model-specific compaction without enabling resources.
      live.session.settingsManager.applyOverrides({ compaction: SettingsManager.create(record.cwd, agentDir).getSettings().compaction });
    }
    if (!live.session.model) throw new Error("尚未配置可用模型，请到设置添加认证或选择模型");
    if (live.modelNeedsSelection) throw new Error("原模型不可用，请明确选择一个可用模型后继续");
    const workspace=this.remote.workspaceFor(id);if(workspace)await this.remote.connect(workspace);
    return live;
  }
  async prompt(id: string, text: string, images?: { type: "image"; data: string; mimeType: string }[], behavior: "steer" | "followUp" = "followUp", signal?: AbortSignal, preserveDraft=false) {
    const record = this.store.record(id);
    const live = await this.preparePrompt(id);
    signal?.throwIfAborted();
    if (record.archived) throw new Error("请先恢复归档会话");
    if (!text.trim() && !images?.length) throw new Error("请输入消息或添加图片");
    if (live.session.isStreaming || record.status === "running") {
      if (behavior === "followUp") {
        (record.pending ??= []).push({ id: randomUUID(), text, images: this.store.cacheImages((images || []).map(i => ({ ...i, id: randomUUID(), name: "队列图片" }))), behavior, paused: false });
        record.updatedAt = new Date().toISOString(); this.changed(record); return;
      }
      await live.session.prompt(text, { images, streamingBehavior: behavior });
      const queuedText = (behavior === "steer" ? live.session.getSteeringMessages() : live.session.getFollowUpMessages()).at(-1);
      if (queuedText) (record.pending ??= []).push({ id: randomUUID(), text, images: this.store.cacheImages((images || []).map(i => ({ ...i, id: randomUUID(), name: "队列图片" }))), behavior, queuedText, paused: false });
      record.userTurns = (record.userTurns ?? live.session.getSessionStats().userMessages) + 1; record.updatedAt = new Date().toISOString(); this.changed(record); return;
    }
    record.userTurns = (record.userTurns ?? live.session.getSessionStats().userMessages) + 1;
    record.status = "running"; record.error = undefined; if(!preserveDraft){record.draft = ""; record.draftImages = [];} record.updatedAt = new Date().toISOString();
    if (record.title === "新会话") { record.title = text.trim().slice(0, 64) || "图片任务"; live.session.setSessionName(record.title); }
    live.runId = randomUUID(); live.started = performance.now(); live.generationMs = 0; live.output = 0;
    live.metrics = { startedAt: new Date().toISOString() };
    this.trace(id, "run_start", "任务开始"); this.changed(record);
    try { await live.session.prompt(text, { images }); await live.session.waitForIdle(); }
    catch (error) { record.status = this.stopping.has(id) ? "paused" : "error"; record.error = safeError(error); this.trace(id, "error", record.error); throw error; }
    finally {
      if (record.status === "running") record.status = "idle";
      record.updatedAt = new Date().toISOString(); const snapshot = await this.snapshot(id); this.changed(record); this.emit({ type: "snapshot", sessionId: id, data: snapshot });
      const next = record.pending?.find(p => !p.paused && !p.queuedText);
      if (next && record.status === "idle" && !this.closing && !this.stopping.has(id)) void this.sendQueued(id, next.id, false).catch(error => this.emit({ type: "notice", sessionId: id, data: { message: safeError(error) } }));
    }
  }
  async stop(id: string) {
    this.stopping.add(id);
    try { for (const child of this.store.state.sessions.filter(r => r.parentId === id && r.status === "running")) await this.stop(child.id); await this.live.get(id)?.session.abort(); const record = this.store.record(id); record.status = "paused"; for (const pending of record.pending || []) pending.paused = true; this.changed(record); }
    finally { this.stopping.delete(id); }
  }
  async change(id: string, options: { model?: string; thinking?: string; mode?: Mode }) {
    const record = this.store.record(id); const live = await this.get(id);
    if (!live.session.isIdle || record.status === "running") throw new Error("请先停止运行，再修改模型或模式");
    if (options.model) { const runtime = live.session.modelRuntime; await runtime.refresh({ allowNetwork: false }); const model = runtime.getAvailableSnapshot().find(m => `${m.provider}/${m.id}` === options.model); if (!model) throw new Error("模型不可用"); await live.session.setModel(model); record.model = options.model; live.modelNeedsSelection = false; }
    if (options.thinking) { if (!live.session.getAvailableThinkingLevels().includes(options.thinking as Parameters<AgentSession["setThinkingLevel"]>[0])) throw new Error("当前模型不支持此思考强度"); live.session.setThinkingLevel(options.thinking as Parameters<AgentSession["setThinkingLevel"]>[0]); record.thinking = live.session.thinkingLevel; }
    if (options.mode && options.mode !== record.mode) { try { await this.release(live, "reload"); } finally { this.live.delete(id); } record.mode = options.mode; await this.get(id); }
    this.changed(record); return this.snapshot(id);
  }
  async mutateMessage(id:string, entryId:string, operation:"edit"|"resend"|"delete"|"exclude", text?:string) {
    const record=this.store.record(id); const {session}=await this.get(id);
    if(!session.isIdle || record.status==="running") throw new Error("请先停止运行，再修改消息");
    const entry=session.sessionManager.getBranch().find(e=>e.id===entryId);
    if(!entry || entry.type!=="message" || entry.message.role!=="assistant"&&entry.message.role!=="user" || entry.message.role==="assistant"&&entry.message.content.some(p=>p.type==="toolCall")) throw new Error("只能修改当前分支的用户消息或助手正文消息");
    let images: {type:"image";data:string;mimeType:string}[] | undefined;
    if(operation==="edit"||operation==="resend") {
      const before=await this.snapshot(id),current=before.messages.find(m=>m.entryId===entryId);
      if(!current || current.contextExcluded) throw new Error("已排除的消息不能编辑");
      if(!text?.trim() || text.length>1024*1024) throw new Error("消息内容不能为空或超过 1 MiB");
      const content=Array.isArray(current.content)?current.content.filter(p=>p.type!=="text"):[];
      if(operation==="resend") {
        if(entry.message.role!=="user"||before.messages.findLast(m=>m.role==="user")?.entryId!==entryId)throw new Error("只能编辑并重发最后一条用户消息");
        if(record.archived)throw new Error("请先恢复归档会话");
        const manager=session.sessionManager,leaf=manager.getLeafId();
        await this.preparePrompt(id);
        if(!session.isIdle||this.store.record(id).status==="running"||manager.getLeafId()!==leaf)throw new Error("会话已更新，请刷新后重试");
        const edits=manager.getBranch().filter(e=>e.type==="context_edit"||e.type==="custom"&&e.customType==="pi-desk-message-delete");
        if(entry.parentId)manager.branch(entry.parentId);else manager.resetLeaf();
        const retained=new Set(manager.getBranch().map(e=>e.id));
        for(const edit of edits)if(!retained.has(edit.id)) {
          if(edit.type==="context_edit"&&retained.has(edit.targetId))manager.appendContextEdit(edit.targetId,edit.replacement);
          else if(edit.type==="custom"&&retained.has((edit.data as {entryId:string}).entryId))manager.appendCustomEntry(edit.customType,edit.data);
        }
        images=content.filter(p=>p.type==="image") as typeof images;
      } else session.sessionManager.appendContextEdit(entryId,{content:[...content,{type:"text",text}] as typeof entry.message.content});
    } else {
      session.sessionManager.appendContextEdit(entryId,null);
      if(operation==="delete") session.sessionManager.appendCustomEntry("pi-desk-message-delete",{entryId});
    }
    session.refreshContext();
    const snapshot=await this.snapshot(id); this.changed(record); this.emit({type:"snapshot",sessionId:id,data:snapshot});
    if(operation==="resend")void this.prompt(id,text!,images,"followUp",undefined,true).catch(error=>this.emit({type:"notice",sessionId:id,data:{type:"error",message:safeError(error)}}));
    return snapshot;
  }
  async changeSkill(cwd: string, data: {operation?: unknown;path?: unknown;enabled?: unknown}) {
    const skills=await skillCatalog(cwd);const skill=Array.isArray(skills)&&skills.find(s=>s.path===data.path);
    if(!skill)throw new Error("Skill 不存在，请重新打开列表");
    if(data.operation==="toggle") {
      if(typeof data.enabled!=="boolean")throw new Error("Skill 启用状态必须是布尔值");
      const previous=this.store.state.preferences.disabledSkills,disabled=new Set(previous||[]);
      if(data.enabled)disabled.delete(skill.name);else disabled.add(skill.name);
      this.store.state.preferences.disabledSkills=[...disabled];
      try{this.changed();}catch(error){this.store.state.preferences.disabledSkills=previous;throw error;}
    } else if(data.operation==="delete") {unlinkSync(skill.path);this.emit({type:"state",data:this.store.state});}
    else throw new Error("未知 Skill 操作");
    for(const [id,live] of this.live){live.session.setActiveToolsByName(live.session.getActiveToolNames());this.progress(id);}
  }
  async changePlugin(cwd:string,data:{id?:unknown;enabled?:unknown}) {
    if(typeof data.enabled!=="boolean")throw Error("插件启用状态必须是布尔值");
    const plugin=(await pluginCatalog(cwd,this.store.state.preferences.pluginOverrides)).find(p=>p.id===data.id);
    if(!plugin)throw Error("插件不存在，请重新打开列表");
    const previous=this.store.state.preferences.pluginOverrides;
    this.store.state.preferences.pluginOverrides={...previous,[plugin.id]:data.enabled};
    try{this.changed();}catch(error){this.store.state.preferences.pluginOverrides=previous;throw error;}
    await Promise.allSettled([...this.creating.values()]);
    const idle:string[]=[];for(const [id,live] of this.live){if(this.store.record(id).mode==="minimal")continue;live.resourcesStale=true;if(live.session.isIdle&&this.store.record(id).status!=="running")idle.push(id);}
    const results=await Promise.allSettled(idle.map(id=>this.get(id)));
    return results.flatMap((result,i)=>result.status==="rejected"?[`${this.store.record(idle[i]).title}：${safeError(result.reason)}`]:[]);
  }
  async reload(id: string) {
    const live=await this.get(id);live.resourcesStale=true;return this.snapshot(id);
  }
  async fork(id: string, entryId?: string) {
    const parent = this.store.record(id); const { session } = await this.get(id);
    if (!session.isIdle || parent.status === "running") throw new Error("运行期间不能修改历史或创建分支");
    const leaf = entryId || session.sessionManager.getLeafId();
    if (!leaf) throw new Error("当前会话没有可分支的历史");
    const source = session.sessionManager.getSessionFile();
    if (!source || !existsSync(source)) throw new Error("原始会话尚未保存");
    // createBranchedSession mutates its manager: fork on a separate reader, never the live parent.
    const path = SessionManager.open(source, undefined, parent.cwd).createBranchedSession(leaf);
    if (!path) throw new Error("无法创建会话分支");
    return this.create(parent.workspaceId, parent.mode, { cwd: parent.cwd, title: parent.title + " · 分支", sessionPath: path, model: parent.model });
  }
  async remove(id: string | string[], descendants: boolean) {
    const ids = new Set(Array.isArray(id) ? id : [id]); for (const key of ids) this.store.record(key);
    for (const record of this.store.state.sessions) if (record.parentId && ids.has(record.parentId) && !ids.has(record.id) && !descendants) throw new Error("此会话包含子任务，请同时删除子任务");
    let more = true; while (descendants && more) { more = false; for (const r of this.store.state.sessions) if (r.parentId && ids.has(r.parentId) && !ids.has(r.id)) { ids.add(r.id); more = true; } }
    const records = this.store.state.sessions.filter(r => ids.has(r.id));
    if (records.some(r => r.status === "running" || this.creating.has(r.id))) throw new Error("请先停止任务，等待资源加载结束再删除");
    const files = [...new Set(records.flatMap(r => r.sessionPath ? [r.sessionPath] : []))].filter(file => !this.store.state.sessions.some(r => !ids.has(r.id) && r.sessionPath === file));
    for (const file of files) if (existsSync(file) && (!inside(realpathSync(sessionRoot), realpathSync(file)) || !file.endsWith(".jsonl"))) throw new Error("原生会话位于 Pi 会话目录之外，请手动检查文件后删除");
    for (const record of records) { const live = this.live.get(record.id); if (live) await this.release(live, "quit"); this.live.delete(record.id); }
    for (const file of files) { if (existsSync(file)) unlinkSync(file); this.sessionOwners.delete(process.platform === "win32" ? file.toLowerCase() : file); }
    this.store.state.sessions = this.store.state.sessions.filter(r => !ids.has(r.id)); this.changed(); return { deleted: [...ids] };
  }
  async pauseQueue(id: string) {
    const record = this.store.record(id); const { session } = await this.get(id);
    const pending = (record.pending || []).map(p => ({ ...p, paused: true }));
    const seen = pending.filter(p => p.queuedText).map(p => p.queuedText!);
    for (const [behavior, items] of [["steer", session.getSteeringMessages()], ["followUp", session.getFollowUpMessages()]] as const) for (const value of items) {
      const index = seen.indexOf(value); if (index >= 0) seen.splice(index, 1); else pending.push({ id: randomUUID(), text: value, images: [], behavior, paused: true, queuedText: value });
    }
    record.pending = pending; session.clearQueue(); await this.stop(id); this.changed(record); return pending.map(p => ({ ...p, images: this.store.images(p.images) }));
  }
  editQueued(id: string, promptId: string, draft: string, images: DraftImage[]) {
    const record = this.store.record(id), pending = record.pending?.find(p => p.id === promptId);
    if (!pending || !pending.paused && pending.queuedText) throw new Error("待发送内容已开始处理，请刷新后重试");
    const value = [pending.text, draft].filter(Boolean).join("\n\n");
    if (value.length > 1024 * 1024) throw new Error("合并后的草稿不能超过 1 MiB");
    const attachments = this.store.cacheImages([...pending.images, ...images]);
    record.draft = value; record.draftImages = attachments;
    record.pending = record.pending!.filter(p => p.id !== promptId); this.changed(record);
    return { text: record.draft, images: this.store.images(attachments) };
  }
  async sendQueued(id: string, promptId: string, immediate = true) {
    const live = await this.preparePrompt(id), record = this.store.record(id);
    if (!immediate && (this.closing || this.stopping.has(id) || record.status !== "idle" || !live.session.isIdle)) return;
    const pending = record.pending?.find(p => p.id === promptId);
    if (!immediate && (!pending || pending.paused)) return;
    if (!pending || !pending.paused && pending.queuedText) throw new Error("待发送内容已开始处理，请刷新后重试");
    if (record.archived) throw new Error("请先恢复归档会话");
    const images = this.store.images(pending.images);
    if (images.some(i => i.missing)) throw new Error("队列图片已丢失，请重新编辑并移除缺失附件");
    const attachments = images as { type: "image"; data: string; mimeType: string }[];
    record.pending = record.pending!.filter(p => p.id !== promptId); this.changed(record);
    const restore = (error: unknown) => { (record.pending ??= []).unshift({ ...pending, paused: true }); this.changed(record); this.emit({ type: "notice", sessionId: id, data: { message: safeError(error) } }); };
    if (live.session.isStreaming || record.status === "running") {
      try { await live.session.steer(pending.text, attachments); record.userTurns = (record.userTurns ?? live.session.getSessionStats().userMessages) + 1; this.changed(record); }
      catch (error) { restore(error); throw error; }
    } else void this.prompt(id, pending.text, attachments, "followUp", undefined, true).catch(restore);
  }
  updateQueue(id: string, pending: PendingPrompt[]) {
    const record = this.store.record(id);
    if (record.pending?.some(p => !p.paused)) throw new Error("请先暂停队列再编辑");
    if (!Array.isArray(pending) || pending.length > 500 || pending.some(p => !p || typeof p.id !== "string" || typeof p.text !== "string" || p.text.length > 1024 * 1024 || !["steer", "followUp"].includes(p.behavior))) throw new Error("待发送内容无效");
    record.pending = pending.map(p => ({ id: p.id, text: p.text, images: this.store.cacheImages(p.images || []), behavior: p.behavior, paused: true })); this.changed(record);
  }
  async ask(request: Omit<UiRequest, "id">, signal?: AbortSignal): Promise<unknown> {
    if (this.closing) return undefined;
    signal?.throwIfAborted(); const id = randomUUID();
    return new Promise(resolve => {
      const complete = (value: unknown) => { signal?.removeEventListener("abort", abort); this.requests.delete(id); this.emit({ type: "ui_done", data: { id } }); resolve(value); };
      const abort = () => complete(undefined);
      const full = { ...request, id }; this.requests.set(id, { request: full, resolve: complete }); signal?.addEventListener("abort", abort, { once: true }); this.emit({ type: "ui", data: full });
    });
  }
  private ui(sessionId: string): ExtensionUIContext {
    const ask = (type: string, title: string, extra = {}, opts?: { signal?: AbortSignal }) => this.ask({ sessionId, type, title, ...extra }, opts?.signal);
    const unsupported = () => { throw new Error("此扩展需要原生终端 UI，Web 暂不支持。请选择支持文本问答的扩展。"); };
    return { select: async (title, options, opts) => await ask("select", title, { options }, opts) as string | undefined,
      confirm: async (title, message, opts) => Boolean(await ask("confirm", title, { message }, opts)),
      input: async (title, placeholder, opts) => await ask("input", title, { message: placeholder }, opts) as string | undefined,
      notify: (message, type) => this.emit({ type: "notice", sessionId, data: { message, type } }),
      setStatus: (key, text) => this.emit({ type: "status", sessionId, data: { key, text } }),
      setWidget: (key, value) => { const live = this.live.get(sessionId); if (!live) return; if (Array.isArray(value)) live.widgets[key] = value; else if (!value) delete live.widgets[key]; this.emit({ type: "widget", sessionId, data: { key, lines: live.widgets[key] || [] } }); },
      setWorkingMessage: message => this.emit({ type: "status", sessionId, data: { key: "working", text: message } }),
      editor: async (title, prefill) => await ask("input", title, { message: prefill }) as string | undefined,
      onTerminalInput: () => () => {}, setWorkingVisible() {}, setWorkingIndicator() {}, setHiddenThinkingLabel() {}, setFooter() {}, setHeader() {}, setTitle() {}, custom: unsupported,
      setEditorText: text => { this.store.record(sessionId).draft = text; this.changed(); }, getEditorText: () => this.store.record(sessionId).draft,
      pasteToEditor: text => { this.store.record(sessionId).draft += text; this.changed(); },
      addAutocompleteProvider() {}, setEditorComponent: factory => { if (factory) unsupported(); }, getEditorComponent: () => undefined,
      theme: webTheme, getAllThemes: () => [{ name: "web", path: undefined }], getTheme: () => webTheme,
      setTheme: () => ({ success: false, error: "Web 外观请在 Pi Desk 设置中切换" }), getToolsExpanded: () => false, setToolsExpanded() {},
    };
  }
  private async release(live: Live, reason: "quit" | "reload") {
    clearTimeout(live.progressTimer);
    await live.session.abort();
    try { await live.session.extensionRunner.emit({ type: "session_shutdown", reason }); } finally { live.session.dispose(); }
  }
  async shutdown() { this.closing = true; this.scheduler.close(); await this.remote.close(); await this.browser.close(); for (const controller of this.authControllers) controller.abort(); for (const pending of this.requests.values()) pending.resolve(undefined); await Promise.allSettled([...this.live.values()].map(l => this.release(l, "quit"))); for (const record of this.store.state.sessions) if (record.status === "running") record.status = "paused"; await this.scheduler.drain(); await Promise.allSettled([...this.proxyAgents.values()].map(agent=>agent.destroy())); this.store.save(); }
}
