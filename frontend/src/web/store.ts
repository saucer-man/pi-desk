import { useRef, useSyncExternalStore } from "react";
import type { State, SessionSnapshot, ServerEvent, ChatMessage, TraceEntry, UiRequest, ModelInfo, ExecutionState } from "../../../src/types";

export async function api<T = unknown>(path: string, data?: unknown, method = data === undefined ? "GET" : "POST", signal?:AbortSignal): Promise<T> {
  const res = await fetch("/api" + path, { method, signal, headers: data === undefined ? undefined : { "Content-Type": "application/json" }, body: data === undefined ? undefined : JSON.stringify(data) });
  const body = await res.json(); if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`); return body;
}
export interface Catalog { defaultProvider?: string; defaultModel?: string; models: ModelInfo[]; allModels?: ModelInfo[]; selection?:Record<string,string[]>; providers: { id: string; name: string; configured: boolean; oauth: boolean; apiKey: boolean; authType?:"api_key"|"oauth" }[]; warning?: string; agentDir: string }
export const CODE_THEMES = [["github-light","GitHub Light"],["github-dark","GitHub Dark"],["vitesse-light","Vitesse Light"],["vitesse-dark","Vitesse Dark"],["minimal-light","Minimal Light"],["minimal-dark","Minimal Dark"],["github-hc-light","GitHub HC Light"],["github-hc-dark","GitHub HC Dark"],["catppuccin-latte","Catppuccin Latte"],["catppuccin-mocha","Catppuccin Mocha"]] as const;
interface CodePreferences { lightTheme: string; darkTheme: string; lineNumbers: boolean; wrap: boolean; fontSize: number }
function readCodePreferences(): CodePreferences {
  let saved: Partial<CodePreferences> = {}; try { saved = JSON.parse(localStorage.getItem("pi-desk.code") || "{}") || {}; } catch {}
  const theme = (value: unknown, fallback: string) => CODE_THEMES.some(([id])=>id===value) ? String(value) : fallback;
  return { lightTheme:theme(saved.lightTheme,"github-light"), darkTheme:theme(saved.darkTheme,"github-dark"), lineNumbers:saved.lineNumbers===true, wrap:saved.wrap===true, fontSize:typeof saved.fontSize==="number"&&Number.isInteger(saved.fontSize)&&saved.fontSize>=10&&saved.fontSize<=18?saved.fontSize:12 };
}
interface AppState {
  code: CodePreferences; setCode: (update: Partial<CodePreferences>) => void;
  state?: State; activeId: string; workspaceId: string; page: "workbench" | "trajectory"; connected: boolean;
  snapshot?: SessionSnapshot; loading: boolean; error: string; catalog?: Catalog; traces: Record<string, TraceEntry[]>;
  subagentId:string; subagentSnapshot?:SessionSnapshot;
  requests: UiRequest[]; auth?: { url: string; message: string };
  select: (id: string) => Promise<void>; refresh: () => Promise<void>; loadModels: (refresh?: boolean) => Promise<void>;
  setPage: (page: "workbench" | "trajectory") => void; fail: (error: unknown) => void;
  receive: (event: ServerEvent) => void;
}
type Update = Partial<AppState> | ((state: AppState) => Partial<AppState>);
const listeners = new Set<() => void>();
let pending: ReturnType<typeof setTimeout> | undefined;
const notify = () => { clearTimeout(pending); pending = undefined; for (const listener of listeners) listener(); };
const set = (update: Update, streaming=false) => { state = { ...state, ...(typeof update === "function" ? update(state) : update) }; if(streaming)pending??=setTimeout(notify,32);else notify(); };
const subscribe = (listener: () => void) => { listeners.add(listener); return () => listeners.delete(listener); };
const get = () => state;
const notified=new Set<string>();
let state: AppState = ({
  code: readCodePreferences(),
  setCode: update => { const code = { ...get().code, ...update }; localStorage.setItem("pi-desk.code", JSON.stringify(code)); set({ code }); },
  activeId: localStorage.getItem("pi-desk.active") || "", subagentId:"", workspaceId: "", page: localStorage.getItem("pi-desk.page")==="trajectory"?"trajectory":"workbench", connected: false, loading: false, error: "", traces: {}, requests: [],
  fail: error => set({ error: error instanceof Error ? error.message : String(error) }),
  setPage: page => { localStorage.setItem("pi-desk.page",page);set({ page }); },
  select: async id => {
    let record = get().state?.sessions.find(r => r.id === id);const subagentId=record?.parentId?id:"";
    while(record?.parentId){const parent=get().state?.sessions.find(r=>r.id===record!.parentId);if(!parent)break;record=parent;}
    id=record?.id||id;localStorage.setItem("pi-desk.active", id);
    set({ activeId: id, subagentId, subagentSnapshot:undefined, workspaceId: record?.workspaceId || get().workspaceId, snapshot: undefined, loading: true, error: "" });
    try { const snapshot = await api<SessionSnapshot>(`/sessions/${id}`); if (get().activeId === id) { set({ snapshot }); void get().loadModels(); } }
    catch (error) { if (get().activeId === id) get().fail(error); }
    finally { if (get().activeId === id) set({ loading: false }); }
  },
  refresh: async () => { try { set({ state: await api<State>("/state") }); const id = get().activeId; if (id && !get().state?.sessions.some(r => r.id === id)) { localStorage.removeItem("pi-desk.active"); set({ activeId: "", snapshot: undefined }); return; } if (id) { const snapshot = await api<SessionSnapshot>(`/sessions/${id}`); if (get().activeId === id) set({ snapshot }); } } catch (error) { get().fail(error); } },
  loadModels: async (refresh=false) => { try { const id = get().state?.sessions.some(r=>r.id===get().activeId) ? get().activeId : ""; const workspaceId=get().workspaceId; const context=id||workspaceId; const catalog = await api<Catalog>("/models" + (context ? `?${id?"sessionId":"workspaceId"}=${encodeURIComponent(context)}` : "") + (refresh ? (context ? "&" : "?") + "refresh=1" : "")); if (id === get().activeId && workspaceId===get().workspaceId) set({ catalog }); } catch (error) { get().fail(error); } },
  receive: event => {
    if (event.type === "state") {
      const state = event.data as State; const current = get();
      const activeId = state.sessions.some(r => r.id === current.activeId) ? current.activeId : current.state ? "" : state.sessions.find(r => !r.parentId && !r.archived)?.id || "";
      const record = state.sessions.find(r => r.id === activeId);
      set({ state, activeId, workspaceId: record?.workspaceId || state.workspaces.find(w=>w.id===current.workspaceId&&!w.hidden)?.id || state.workspaces.find(w=>!w.hidden)?.id || "", snapshot: current.snapshot && record ? { ...current.snapshot, record } : undefined });
      if (!current.snapshot && activeId && !current.loading) void get().select(activeId);
    }
    const snapshotKey=event.sessionId===get().activeId?"snapshot":event.sessionId===get().subagentId?"subagentSnapshot":undefined;
    if (event.type === "snapshot" && snapshotKey) set({ [snapshotKey]: event.data as SessionSnapshot });
    if (event.type === "event" && snapshotKey) {
      const data = event.data as { type: string; message?: ChatMessage; steering?: string[]; followUp?: string[]; toolCallId?: string; assistantMessageEvent?: { type: string; contentIndex?: number } };
      const snapshot = get()[snapshotKey]; if (!snapshot) return;
      if (data.type === "queue_update") { set({ [snapshotKey]: { ...snapshot, queue: { steering: data.steering || [], followUp: data.followUp || [] } } }); return; }
      const execution: ExecutionState = { runningTools: [], ...snapshot.execution };
      if (data.message?.role === "assistant") {
        const delta = data.assistantMessageEvent;
        if (data.type === "message_start" || data.type === "message_update") execution.streaming = { timestamp: data.message.timestamp!, thinkingIndex: delta?.type === "thinking_start" || delta?.type === "thinking_delta" ? delta.contentIndex : undefined };
        if (data.type === "message_end") execution.streaming = undefined;
      }
      if (data.type === "tool_execution_start" && data.toolCallId) execution.runningTools = [...new Set([...execution.runningTools, data.toolCallId])];
      if (data.type === "tool_execution_end") execution.runningTools = execution.runningTools.filter(id => id !== data.toolCallId);
      if (data.type === "agent_settled") { execution.streaming = undefined; execution.runningTools = []; }
      let messages = snapshot.messages;
      if (data.message && ["message_start", "message_update", "message_end"].includes(data.type)) {
        messages = [...snapshot.messages]; const last = messages.at(-1); const message = data.message;
        const same = last && last.role === message.role && (message.toolCallId ? last.toolCallId === message.toolCallId : last.timestamp === message.timestamp);
        if (same) messages[messages.length - 1] = message; else messages.push(message);
      }
      set({ [snapshotKey]: { ...snapshot, messages, execution } }, data.type === "message_update");
    }
    if (event.type === "trace" && event.sessionId) {
      const trace = event.data as TraceEntry; const id = event.sessionId;
      if(trace.type==="settled"&&!notified.has(trace.id)){
        notified.add(trace.id);if(notified.size>500)notified.delete(notified.values().next().value!);
        const record=get().state?.sessions.find(r=>r.id===id);
        if(record&&(document.visibilityState!=="visible"||id!==get().activeId)&&localStorage.getItem("pi-desk.notifications")==="true"&&"Notification" in window&&Notification.permission==="granted"){
          try{const notification=new Notification(record.title,{body:trace.label,tag:"pi-desk-"+id});notification.onclick=()=>{window.focus();get().setPage("workbench");void get().select(id);notification.close();};}catch{}
        }
      }
      set(s => ({ traces: { ...s.traces, [id]: [...(s.traces[id] || []), trace] } }));
      if (trace.type === "run_start" && id === get().activeId && get().snapshot) set(s => ({ snapshot: { ...s.snapshot!, metrics: { startedAt: trace.at } } }));
    }
    if (event.type === "notice") { const notice = event.data as { message: string }; set({ error: notice.message }); }
    if (event.type === "ui") { const request = event.data as UiRequest; set(s => ({ requests: [...s.requests.filter(r => r.id !== request.id), request] })); }
    if (event.type === "ui_done") set(s => ({ requests: s.requests.filter(r => r.id !== (event.data as { id: string }).id) }));
    if (event.type === "models") set({ catalog: event.data as Catalog, auth: undefined });
    if (event.type === "widget" && event.sessionId === get().activeId && get().snapshot) { const widget = event.data as { key: string; lines: string[] }; set(s => ({ snapshot: { ...s.snapshot!, widgets: { ...s.snapshot!.widgets, [widget.key]: widget.lines } } })); }
    if (event.type === "auth") set({ auth: event.data as { url: string; message: string } });
  },
});
export const useApp = Object.assign(function useApp<T=AppState>(select:(state:AppState)=>T = state=>state as unknown as T) {
  const previous=useRef<T>();
  return useSyncExternalStore(subscribe,()=>{
    const value=select(get()),before=previous.current;
    if(Object.is(value,before))return value;
    if(value&&before&&typeof value==="object"&&typeof before==="object"&&Object.keys(value).length===Object.keys(before).length&&Object.entries(value).every(([key,item])=>Object.is(item,(before as Record<string,unknown>)[key])))return before;
    previous.current=value;return value;
  });
}, { getState: get, setState: set, subscribe });
export function connect() {
  let events: EventSource | undefined, subscription = "";
  const open = () => {
    const { activeId, subagentId } = get();
    const query = new URLSearchParams([["sessionId", activeId], ["sessionId", subagentId]]).toString();
    if (events && subscription === query) return;
    subscription = query; events?.close();
    const source = events = new EventSource("/api/events?" + query);
    source.onopen = () => { if (events !== source) return; set({ connected: true }); const { state, activeId, loading } = get(); if (state?.sessions.some(r => r.id === activeId) && !loading) void get().refresh(); };
    source.onmessage = message => { if (events !== source) return; try { get().receive(JSON.parse(message.data)); } catch (error) { get().fail(error); } };
    source.onerror = () => { if (events === source) set({ connected: false }); };
  };
  listeners.add(open); open();
  void get().loadModels(); return () => { listeners.delete(open); events?.close(); events = undefined; };
}
