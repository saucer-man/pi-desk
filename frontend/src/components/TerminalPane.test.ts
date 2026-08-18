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
    expect(terminalMocks.snapshot).toHaveBeenCalledWith("thread-1");

    await wrapper.get('button[title="Start terminal"]').trigger("click");
    await flushPromises();
    expect(terminalMocks.start).toHaveBeenCalledWith("thread-1", "D:\\repo", 80, 24);
    expect(terminalHarness.write).toHaveBeenCalledWith(expect.any(Uint8Array));
    expect(wrapper.text()).toContain("pwsh.exe");

    terminalHarness.dataHandler?.("dir\r");
    await flushPromises();
    expect(terminalMocks.write).toHaveBeenCalledWith("thread-1", "dir\r");

    terminalHarness.eventHandler?.({ threadId: "thread-1", type: "output", sequence: 2, dataB64: btoa("next") });
    terminalHarness.eventHandler?.({ threadId: "thread-1", type: "output", sequence: 2, dataB64: btoa("duplicate") });
    expect(terminalHarness.write).toHaveBeenCalledTimes(2);

    await wrapper.get('button[title="Stop terminal"]').trigger("click");
    await flushPromises();
    expect(terminalMocks.stop).toHaveBeenCalledWith("thread-1");
  });

  it("loads the selected workspace terminal", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-2", title: "Workspace", workspace: "repo", workspacePath: "D:\\repo",
        trust: "approve", status: "idle", started: false, generation: 0,
      }],
      activeThreadId: "thread-2",
    });
    terminalMocks.snapshot.mockResolvedValueOnce({ threadId: "thread-2", running: false, sequence: 0 });
    terminalMocks.start.mockResolvedValueOnce({ threadId: "thread-2", running: true, sequence: 0 });
    const wrapper = mount(TerminalPane, { global: { plugins: [pinia] } });
    await flushPromises();
    await wrapper.get('button[title="Start terminal"]').trigger("click");
    await flushPromises();
    expect(terminalMocks.start).toHaveBeenCalledWith("thread-2", "D:\\repo", 80, 24);
  });
});
