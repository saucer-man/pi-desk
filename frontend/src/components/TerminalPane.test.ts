import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "../stores/app";
import TerminalPane from "./TerminalPane.vue";

const terminalHarness = vi.hoisted(() => ({
  dataHandler: undefined as ((data: string) => void) | undefined,
  resizeHandler: undefined as ((size: { cols: number; rows: number }) => void) | undefined,
  eventHandler: undefined as ((event: { threadId: string; type: "output" | "error" | "exit"; sequence: number; dataB64?: string }) => void) | undefined,
  write: vi.fn(),
  reset: vi.fn(),
  clear: vi.fn(),
  focus: vi.fn(),
  dispose: vi.fn(),
  fit: vi.fn(),
}));

vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80;
    rows = 24;
    loadAddon() {}
    open() {}
    write = terminalHarness.write;
    reset = terminalHarness.reset;
    clear = terminalHarness.clear;
    focus = terminalHarness.focus;
    dispose = terminalHarness.dispose;
    hasSelection() { return false; }
    getSelection() { return ""; }
    paste() {}
    attachCustomKeyEventHandler() {}
    onData(handler: (data: string) => void) {
      terminalHarness.dataHandler = handler;
      return { dispose: vi.fn() };
    }
    onResize(handler: (size: { cols: number; rows: number }) => void) {
      terminalHarness.resizeHandler = handler;
      return { dispose: vi.fn() };
    }
  },
}));

vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class { fit = terminalHarness.fit; },
}));

const terminalMocks = vi.hoisted(() => ({
  snapshot: vi.fn(),
  start: vi.fn(),
  write: vi.fn(),
  resize: vi.fn(),
  stop: vi.fn(),
}));

vi.mock("../services/terminal", () => ({
  terminalService: terminalMocks,
  onTerminalEvent: (handler: typeof terminalHarness.eventHandler) => {
    terminalHarness.eventHandler = handler;
    return vi.fn();
  },
}));
vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

class TestResizeObserver {
  observe() {}
  disconnect() {}
  unobserve() {}
}

describe("TerminalPane", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    terminalHarness.dataHandler = undefined;
    terminalHarness.resizeHandler = undefined;
    terminalHarness.eventHandler = undefined;
    vi.stubGlobal("ResizeObserver", TestResizeObserver);
    terminalMocks.snapshot.mockResolvedValue({ threadId: "thread-1", running: false, sequence: 0 });
    terminalMocks.start.mockResolvedValue({
      threadId: "thread-1", cwd: "D:\\repo", shell: "C:\\Program Files\\PowerShell\\7\\pwsh.exe",
      running: true, sequence: 1, outputB64: btoa("ready\r\n"),
    });
    terminalMocks.write.mockResolvedValue(undefined);
    terminalMocks.resize.mockResolvedValue(undefined);
    terminalMocks.stop.mockResolvedValue(undefined);
  });

  it("starts a trusted task terminal and forwards input and sequenced output", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-1", title: "Terminal", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: false, generation: 0,
      }],
      activeThreadId: "thread-1",
    });
    const wrapper = mount(TerminalPane, { global: { plugins: [pinia] } });
    await flushPromises();
    expect(terminalMocks.snapshot).toHaveBeenCalledWith("thread-1", undefined);

    await wrapper.get('button[title="Start terminal"]').trigger("click");
    await flushPromises();
    expect(terminalMocks.start).toHaveBeenCalledWith("thread-1", "D:\\repo", 80, 24);
    expect(terminalHarness.write).toHaveBeenCalledWith(expect.any(Uint8Array));
    expect(wrapper.text()).toContain("pwsh.exe");

    terminalHarness.dataHandler?.("dir\r");
    await flushPromises();
    expect(terminalMocks.write).toHaveBeenCalledWith("thread-1", "dir\r");
    expect(store.activeRepositoryStale).toBe(false);

    terminalHarness.eventHandler?.({ threadId: "thread-1", type: "output", sequence: 2, dataB64: btoa("next") });
    terminalHarness.eventHandler?.({ threadId: "thread-1", type: "output", sequence: 2, dataB64: btoa("duplicate") });
    expect(terminalHarness.write).toHaveBeenCalledTimes(2);

    terminalMocks.snapshot.mockResolvedValueOnce({ threadId: "thread-1", running: true, sequence: 4, outputB64: btoa("replayed") });
    terminalHarness.eventHandler?.({ threadId: "thread-1", type: "output", sequence: 4, dataB64: btoa("gap") });
    await flushPromises();
    expect(terminalMocks.snapshot).toHaveBeenCalledTimes(2);
    expect(terminalHarness.reset).toHaveBeenCalled();

    await wrapper.get('button[title="Stop terminal"]').trigger("click");
    await flushPromises();
    expect(terminalMocks.stop).toHaveBeenCalledWith("thread-1");
  });

  it("clears remote readiness when Terminal input loses its generation", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      workspaces: [{
        id: "workspace-0123456789abcdef0123456789abcdef", name: "repo", path: "", kind: "ssh",
        targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve",
      }],
      threads: [{
        id: "thread-2", title: "Workspace", workspace: "repo", workspaceId: "workspace-0123456789abcdef0123456789abcdef", workspacePath: "",
        trust: "approve", status: "idle", started: true, generation: 1,
      }],
      activeThreadId: "thread-2",
    });
    store.remoteReadyByWorkspace["workspace-0123456789abcdef0123456789abcdef"] = true;
    terminalMocks.snapshot.mockResolvedValueOnce({ threadId: "thread-2", running: false, sequence: 0 });
    terminalMocks.start.mockResolvedValueOnce({ threadId: "thread-2", running: true, sequence: 0 });
    const wrapper = mount(TerminalPane, { global: { plugins: [pinia] } });
    await flushPromises();
    expect(terminalMocks.snapshot).toHaveBeenCalledWith("thread-2", "workspace-0123456789abcdef0123456789abcdef");
    await wrapper.get('button[title="Start terminal"]').trigger("click");
    await flushPromises();
    expect(terminalMocks.start).toHaveBeenCalledWith("thread-2", { workspaceId: "workspace-0123456789abcdef0123456789abcdef" }, 80, 24);
    terminalMocks.write.mockRejectedValueOnce(new Error("REMOTE_OUTCOME_UNKNOWN: terminal input delivery is unknown"));
    terminalHarness.dataHandler?.("touch changed.txt\r");
    await flushPromises();
    expect(store.activeRepositoryStale).toBe(true);
    expect(store.remoteReadyByWorkspace["workspace-0123456789abcdef0123456789abcdef"]).toBe(false);
  });

  it("reconnects and starts Pi before starting a cold remote Terminal", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      workspaces: [{ id: "workspace-remote", name: "repo", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }],
      threads: [
        { id: "thread-cold", title: "Cold", workspace: "repo", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0 },
        { id: "thread-local", title: "Local", workspace: "local", workspacePath: "D:\\local", trust: "approve", status: "idle", started: false, generation: 0 },
      ],
      activeThreadId: "thread-cold",
      remoteReadyByWorkspace: { "workspace-remote": false },
    });
    terminalMocks.snapshot.mockResolvedValueOnce({ threadId: "thread-cold", running: false, sequence: 0 });
    terminalMocks.start.mockResolvedValueOnce({ threadId: "thread-cold", running: true, sequence: 0 });
    const wrapper = mount(TerminalPane, { global: { plugins: [pinia] } });
    await flushPromises();
    expect(wrapper.text()).toContain("/srv/repo");

    await wrapper.get('button[title="Start terminal"]').trigger("click");
    expect(store.remoteReconnectOpen).toBe(true);
    expect(store.remoteReconnectIntent).toBe("terminal");
    expect(terminalMocks.start).not.toHaveBeenCalled();

    store.remoteReadyByWorkspace["workspace-remote"] = true;
    store.threads[0].started = true;
    await flushPromises();
    expect(terminalMocks.start).toHaveBeenCalledWith("thread-cold", { workspaceId: "workspace-remote" }, 80, 24);
  });

  it("cancels a cold remote Terminal intent when the user switches tasks", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      workspaces: [{ id: "workspace-remote", name: "repo", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }],
      threads: [
        { id: "thread-cold", title: "Cold", workspace: "repo", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0 },
        { id: "thread-local", title: "Local", workspace: "local", workspacePath: "D:\\local", trust: "approve", status: "idle", started: false, generation: 0 },
      ],
      activeThreadId: "thread-cold",
      remoteReadyByWorkspace: { "workspace-remote": true },
    });
    const wrapper = mount(TerminalPane, { global: { plugins: [pinia] } });
    await flushPromises();

    await wrapper.get('button[title="Start terminal"]').trigger("click");
    expect(terminalMocks.start).not.toHaveBeenCalled();
    store.activeThreadId = "thread-local";
    await flushPromises();
    store.threads[0].started = true;
    store.activeThreadId = "thread-cold";
    await flushPromises();

    expect(terminalMocks.start).not.toHaveBeenCalled();
  });

  it("drops a late local snapshot after switching to an offline remote task", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    let finish!: (state: { threadId: string; running: boolean; sequence: number; shell: string }) => void;
    terminalMocks.snapshot.mockReturnValueOnce(new Promise((resolve) => { finish = resolve; }));
    store.$patch({
      workspaces: [
        { id: "workspace-local", name: "local", path: "D:\\repo", trust: "approve" },
        { id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" },
      ],
      threads: [
        { id: "thread-local", title: "Local", workspace: "local", workspaceId: "workspace-local", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: false, generation: 0 },
        { id: "thread-remote", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0 },
      ],
      activeThreadId: "thread-local",
      remoteReadyByWorkspace: { "workspace-remote": false },
    });
    const wrapper = mount(TerminalPane, { global: { plugins: [pinia] } });
    await Promise.resolve();
    store.activeThreadId = "thread-remote";
    await flushPromises();
    finish({ threadId: "thread-local", running: true, sequence: 1, shell: "pwsh.exe" });
    await flushPromises();

    expect(wrapper.text()).not.toContain("pwsh.exe");
    expect(wrapper.text()).toContain("/srv/repo");
  });

  it("requests reconnect instead of using a stale started remote Terminal", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      workspaces: [{ id: "workspace-remote", name: "repo", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }],
      threads: [{ id: "thread-stale", title: "Stale", workspace: "repo", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: true, generation: 1 }],
      activeThreadId: "thread-stale",
      remoteReadyByWorkspace: { "workspace-remote": false },
    });
    const wrapper = mount(TerminalPane, { global: { plugins: [pinia] } });
    await flushPromises();

    expect(terminalMocks.snapshot).not.toHaveBeenCalled();
    await wrapper.get('button[title="Start terminal"]').trigger("click");
    expect(store.remoteReconnectOpen).toBe(true);
    expect(store.remoteReconnectIntent).toBe("terminal");
    expect(terminalMocks.start).not.toHaveBeenCalled();
  });

  it("allows retry when starting Pi for a remote Terminal fails", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      workspaces: [{ id: "workspace-remote", name: "repo", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }],
      threads: [{ id: "thread-failed", title: "Failed", workspace: "repo", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0 }],
      activeThreadId: "thread-failed",
      remoteReadyByWorkspace: { "workspace-remote": true },
    });
    const start = vi.spyOn(store, "startThreadInBackground");
    const wrapper = mount(TerminalPane, { global: { plugins: [pinia] } });
    await flushPromises();

    await wrapper.get('button[title="Start terminal"]').trigger("click");
    await flushPromises();
    expect(store.threads[0].status).toBe("attention");
    await wrapper.get('button[title="Start terminal"]').trigger("click");
    expect(start).toHaveBeenCalledTimes(2);
  });
});
