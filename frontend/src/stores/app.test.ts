import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { RuntimeState } from "../../bindings/pi-desk/internal/domain";

const mocks = vi.hoisted(() => ({
  getBootstrapState: vi.fn(),
  checkRuntime: vi.fn(),
  checkForUpdates: vi.fn(),
  notifyDesktop: vi.fn(),
  listWorkspaces: vi.fn(),
  listWorkspaceApplications: vi.fn(),
  openWorkspaceWith: vi.fn(),
  listSessions: vi.fn(),
  getSessionSnapshot: vi.fn(),
  getConfiguredModels: vi.fn(),
  addWorkspace: vi.fn(),
  removeWorkspace: vi.fn(),
  deleteWorkspaceSessions: vi.fn(),
  pickWorkspace: vi.fn(),
  deleteSession: vi.fn(),
  getDesktopState: vi.fn(),
  saveDesktopState: vi.fn(),
  startSession: vi.fn(),
  stopSession: vi.fn(),
  sendPrompt: vi.fn(),
  abort: vi.fn(),
  abortRetry: vi.fn(),
  bash: vi.fn(),
  abortBash: vi.fn(),
  getState: vi.fn(),
  getMessages: vi.fn(),
  getSessionStats: vi.fn(),
  getAvailableModels: vi.fn(),
  getAvailableThinkingLevels: vi.fn(),
  getCommands: vi.fn(),
  getForkMessages: vi.fn(),
  getSessionBranches: vi.fn(),
  cloneSession: vi.fn(),
  forkSession: vi.fn(),
  forkSessionAt: vi.fn(),
  editSessionMessage: vi.fn(),
  replaySessionMessage: vi.fn(),
  deleteSessionMessage: vi.fn(),
  exportSession: vi.fn(),
  setModel: vi.fn(),
  setThinkingLevel: vi.fn(),
  setSteeringMode: vi.fn(),
  setFollowUpMode: vi.fn(),
  setAutoCompaction: vi.fn(),
  setAutoRetry: vi.fn(),
  compact: vi.fn(),
  setSessionName: vi.fn(),
  respondExtensionUI: vi.fn(),
  getDiagnostics: vi.fn(),
  snapshotRepository: vi.fn(),
  sessionFileChanges: vi.fn(),
  rollbackSessionFile: vi.fn(),
  diffRepository: vi.fn(),
  openRepositoryFile: vi.fn(),
  openRepositoryFileWith: vi.fn(),
  previewRepositoryFile: vi.fn(),
  revealRepositoryFile: vi.fn(),
  resumeRemoteWorkspace: vi.fn(),
  disconnectRemoteTarget: vi.fn(),
  eventHandler: undefined as ((event: unknown) => void) | undefined,
  terminalEventHandler: undefined as ((event: unknown) => void) | undefined,
}));

vi.mock("../services/desktop", () => ({
  getBootstrapState: mocks.getBootstrapState,
  checkRuntime: mocks.checkRuntime,
  checkForUpdates: mocks.checkForUpdates,
  notifyDesktop: mocks.notifyDesktop,
}));
vi.mock("../services/catalog", () => ({
  catalogService: {
    listWorkspaces: mocks.listWorkspaces,
    listWorkspaceApplications: mocks.listWorkspaceApplications,
    openWorkspaceWith: mocks.openWorkspaceWith,
    listSessions: mocks.listSessions,
    getSessionSnapshot: mocks.getSessionSnapshot,
    addWorkspace: mocks.addWorkspace,
    removeWorkspace: mocks.removeWorkspace,
    deleteWorkspaceSessions: mocks.deleteWorkspaceSessions,
    pickWorkspace: mocks.pickWorkspace,
    deleteSession: mocks.deleteSession,
    getDesktopState: mocks.getDesktopState,
    saveDesktopState: mocks.saveDesktopState,
  },
}));
vi.mock("../services/modelconfig", () => ({
  modelConfigService: {
    selectable: mocks.getConfiguredModels,
  },
}));
vi.mock("../services/agent", () => ({
  agentService: {
    startSession: mocks.startSession,
    stopSession: mocks.stopSession,
    sendPrompt: mocks.sendPrompt,
    abort: mocks.abort,
    abortRetry: mocks.abortRetry,
    bash: mocks.bash,
    abortBash: mocks.abortBash,
    getState: mocks.getState,
    getMessages: mocks.getMessages,
    getSessionStats: mocks.getSessionStats,
    getAvailableModels: mocks.getAvailableModels,
    getAvailableThinkingLevels: mocks.getAvailableThinkingLevels,
    getCommands: mocks.getCommands,
    getForkMessages: mocks.getForkMessages,
    getSessionBranches: mocks.getSessionBranches,
    cloneSession: mocks.cloneSession,
    forkSession: mocks.forkSession,
    forkSessionAt: mocks.forkSessionAt,
    editSessionMessage: mocks.editSessionMessage,
    replaySessionMessage: mocks.replaySessionMessage,
    deleteSessionMessage: mocks.deleteSessionMessage,
    exportSession: mocks.exportSession,
    setModel: mocks.setModel,
    setThinkingLevel: mocks.setThinkingLevel,
    setSteeringMode: mocks.setSteeringMode,
    setFollowUpMode: mocks.setFollowUpMode,
    setAutoCompaction: mocks.setAutoCompaction,
    setAutoRetry: mocks.setAutoRetry,
    compact: mocks.compact,
    setSessionName: mocks.setSessionName,
    respondExtensionUI: mocks.respondExtensionUI,
    getDiagnostics: mocks.getDiagnostics,
  },
  onPiEvent: (handler: (event: unknown) => void) => {
    mocks.eventHandler = handler;
    return vi.fn();
  },
}));
vi.mock("../services/repository", () => ({
  repositoryService: {
    snapshot: mocks.snapshotRepository,
    diff: mocks.diffRepository,
    openFile: mocks.openRepositoryFile,
    openFileWith: mocks.openRepositoryFileWith,
    previewFile: mocks.previewRepositoryFile,
    revealFile: mocks.revealRepositoryFile,
    sessionFileChanges: mocks.sessionFileChanges,
    rollbackSessionFile: mocks.rollbackSessionFile,
  },
}));
vi.mock("../services/remoteWorkspaces", () => ({
  remoteWorkspaceService: { resume: mocks.resumeRemoteWorkspace, disconnect: mocks.disconnectRemoteTarget },
}));
vi.mock("../services/terminal", () => ({
  onTerminalEvent: (handler: (event: unknown) => void) => {
    mocks.terminalEventHandler = handler;
    return vi.fn();
  },
}));
import { useAppStore } from "./app";

describe("app store", () => {
  it("keeps background browser activity in its owning conversation without stealing a file tab", async () => {
    const store = useAppStore();
    store.scheduleDesktopStateSave = vi.fn();
    store.threads = ["a", "b"].map(id => ({ id, title: id, workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: false, generation: 0 }));
    store.activateThread("a");
    store.openPanelTab({ id: "file", kind: "file", title: "file.ts", pinned: true });
    const status = { tabId: "browser-a", threadId: "a", attached: true, human: false, temporary: true, loading: false, canGoBack: false, canGoForward: false, url: "https://example.com", title: "Example" };
    store.handleBrowserEvent({ type: "opened", threadId: "a", tabId: "browser-a", status });
    expect(store.activePanelTab?.id).toBe("file");
    expect(store.activePanel?.tabs.at(-1)?.title).toBe("Example");
    store.handleBrowserEvent({ type: "opened", threadId: "b", tabId: "browser-b", status: { ...status, tabId: "browser-b", threadId: "b" } });
    expect(store.activePanelTab?.id).toBe("file");
    expect(store.inspectorByThread.b.tabs).toHaveLength(1);
    store.catalogReady = true;
    await store.persistDesktopState();
    const saved = JSON.parse(mocks.saveDesktopState.mock.calls.at(-1)![0].preferences.panelState);
    expect(saved.a.tabs.map((tab: { id: string }) => tab.id)).toEqual(["file"]);
    store.handleBrowserEvent({ type: "state", threadId: "a", tabId: "browser-a", status: { ...status, temporary: false } });
    await store.persistDesktopState();
    expect(JSON.parse(mocks.saveDesktopState.mock.calls.at(-1)![0].preferences.panelState).a.tabs).toHaveLength(2);
    store.handleBrowserEvent({ type: "closed", threadId: "b", tabId: "browser-b" });
    expect(store.inspectorByThread.b.tabs).toHaveLength(0);
    expect(store.activePanelTab?.id).toBe("file");
  });
  it("isolates panel tabs, preview slots, historical diffs and view state per conversation", async () => {
    const store = useAppStore();
    store.scheduleDesktopStateSave = vi.fn();
    store.threads = ["a", "b"].map(id => ({ id, title: id, workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: false, generation: 0 }));
    store.activateThread("a");
    await store.openRepositoryFilePreview("one.ts", undefined, false);
    await store.openRepositoryFilePreview("two.ts", undefined, false);
    expect(store.activePanel?.tabs.map(tab => tab.path)).toEqual(["two.ts"]);
    store.pinPanelTab(store.activePanelTab!.id);
    store.activePanelTab!.treeOpen = true;
    store.activePanelTab!.scroll = { ".file-preview-content": [10, 240] };
    await store.openRepositoryFilePreview("three.ts", undefined, false);
    expect(store.activePanel?.tabs).toHaveLength(2);
    await store.openRepositoryDiff("same.ts", "+first", "message-one");
    const firstDiff = store.activePanelTab!.id;
    await store.openRepositoryDiff("same.ts", "+second", "message-two");
    expect(store.activePanelTab!.id).not.toBe(firstDiff);
    expect(store.activeRepositoryDiff?.working).toBe("+second");
    await store.openRepositoryDiff("same.ts", "+first", "message-one");
    expect(store.activePanel?.tabs).toHaveLength(4);
    expect(store.activePanelTab!.id).toBe(firstDiff);
    store.setInspectorWidth(580);
    store.contextOpen = true;
    store.activateThread("b");
    expect(store.inspectorOpen).toBe(false);
    expect(store.contextOpen).toBe(false);
    await store.openRepositoryDiff("same.ts", "+other conversation", "message-one");
    expect(store.activePanel?.tabs).toHaveLength(1);
    store.activateThread("a");
    expect(store.activePanel?.tabs).toHaveLength(4);
    expect(store.inspectorWidth).toBe(580);
    expect(store.activeRepositoryDiff?.working).toBe("+first");
    store.selectPanelTab(store.activePanel!.tabs[0].id);
    expect(store.activePanelTab?.scroll).toEqual({ ".file-preview-content": [10, 240] });
    expect(store.activePanelTab?.treeOpen).toBe(true);
  });

  it("persists panel metadata without cached file contents and collapses the last tab", async () => {
    const store = useAppStore();
    store.scheduleDesktopStateSave = vi.fn();
    store.threads = [{ id: "a", title: "a", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: false, generation: 0 }];
    store.activateThread("a");
    store.catalogReady = true;
    await store.openRepositoryFilePreview("main.go");
    const fileId = store.activePanelTab!.id;
    store.activePanelTab!.scroll = { ".file-preview-content": [0, 320] };
    const browser = store.openBrowserTab("https://example.com");
    const browser2 = store.openBrowserTab("https://example.org");
    expect(browser.id).not.toBe(browser2.id);
    store.reorderPanelTab(browser2.id, fileId);
    await store.persistDesktopState();
    const saved = mocks.saveDesktopState.mock.calls.at(-1)![0];
    const panel = JSON.parse(saved.preferences.panelState).a;
    expect(panel.tabs[0].id).toBe(browser2.id);
    expect(panel.tabs.find((tab: { id: string }) => tab.id === fileId).preview).toBeUndefined();
    expect(panel.tabs.find((tab: { id: string }) => tab.id === fileId).scroll).toEqual({ ".file-preview-content": [0, 320] });
    const restored = useAppStore(createPinia());
    restored.restoreDesktopPreferences(saved);
    expect(restored.inspectorByThread.a.tabs).toEqual(panel.tabs);
    store.inspectorByThread.a.tabs = [store.inspectorByThread.a.tabs.find(tab => tab.id === fileId)!];
    store.selectPanelTab(fileId);
    await store.closePanelTab(fileId);
    expect(store.activePanel?.tabs).toHaveLength(0);
    expect(store.inspectorOpen).toBe(false);
  });

  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
    mocks.eventHandler = undefined;
    mocks.terminalEventHandler = undefined;
    mocks.getBootstrapState.mockResolvedValue({
      productName: "Pi Desk",
      appVersion: "0.1.0",
      wailsVersion: "v3.0.0-beta.16",
      workingDirectory: "D:\\work\\repo",
      runtime: { state: "checking", message: "Pi runtime check is pending" },
    });
    mocks.checkRuntime.mockResolvedValue({ state: "ready", version: "0.83.0", command: "pi.cmd" });
    mocks.checkForUpdates.mockResolvedValue({ status: "current", currentVersion: "0.1.0", latestVersion: "0.1.0", message: "Pi Desk is up to date" });
    mocks.notifyDesktop.mockResolvedValue(true);
    mocks.startSession.mockResolvedValue({
      threadId: "thread-1",
      generation: 4,
      stateJson: JSON.stringify({ sessionId: "session-1", thinkingLevel: "medium", isStreaming: false }),
    });
    mocks.sendPrompt.mockResolvedValue({ command: "prompt" });
    mocks.bash.mockResolvedValue({ output: "clean\n", exitCode: 0, cancelled: false, truncated: false });
    mocks.abortBash.mockResolvedValue(undefined);
    mocks.getAvailableModels.mockResolvedValue({ models: [] });
    mocks.getAvailableThinkingLevels.mockResolvedValue({ levels: ["off", "medium"] });
    mocks.getCommands.mockResolvedValue({ commands: [] });
    mocks.getForkMessages.mockResolvedValue({ messages: [] });
    mocks.getSessionBranches.mockResolvedValue({ entries: [], leafId: "" });
    mocks.cloneSession.mockResolvedValue({ cancelled: false });
    mocks.forkSession.mockResolvedValue({ cancelled: false, text: "" });
    mocks.exportSession.mockResolvedValue({ path: "D:\\work\\repo\\session.html" });
    mocks.getSessionStats.mockResolvedValue({ totalMessages: 0 });
    mocks.getState.mockResolvedValue({ sessionId: "session-1", isStreaming: false });
    mocks.respondExtensionUI.mockResolvedValue(undefined);
    mocks.stopSession.mockResolvedValue(undefined);
    mocks.setSteeringMode.mockResolvedValue(undefined);
    mocks.setFollowUpMode.mockResolvedValue(undefined);
    mocks.setAutoCompaction.mockResolvedValue(undefined);
    mocks.setAutoRetry.mockResolvedValue(undefined);
    mocks.listWorkspaces.mockResolvedValue([]);
    mocks.listWorkspaceApplications.mockResolvedValue([
      { id: "vscode", name: "Visual Studio Code", iconDataUrl: "data:image/png;base64,dnNjb2Rl" },
      { id: "file-manager", name: "File Explorer", iconDataUrl: "data:image/png;base64,ZmlsZXM=" },
    ]);
    mocks.openWorkspaceWith.mockResolvedValue(undefined);
    mocks.listSessions.mockResolvedValue([]);
    mocks.getSessionSnapshot.mockResolvedValue({ messages: [] });
    mocks.getConfiguredModels.mockResolvedValue([]);
    mocks.setModel.mockResolvedValue(undefined);
    mocks.addWorkspace.mockImplementation(async (path: string, trust: "approve" | "deny") => ({
      id: "workspace-1", name: "repo", path, trust,
      addedAt: "2026-08-10T08:00:00Z", lastOpenedAt: "2026-08-10T08:00:00Z",
    }));
    mocks.pickWorkspace.mockResolvedValue("D:\\work\\repo");
    mocks.deleteSession.mockResolvedValue({ recoveryPath: "C:\\sessions\\one.jsonl.deleted-test" });
    mocks.getDesktopState.mockResolvedValue({ threads: [] });
    mocks.saveDesktopState.mockResolvedValue(undefined);
    mocks.getMessages.mockResolvedValue({ messages: [] });
    mocks.resumeRemoteWorkspace.mockResolvedValue({
      id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote",
      remoteRoot: "/srv/repo", trust: "approve",
    });
    mocks.disconnectRemoteTarget.mockResolvedValue(undefined);
    mocks.snapshotRepository.mockResolvedValue({
      files: [{ path: "main.go", name: "main.go" }],
      git: { isRepository: true, branch: "main", files: [] },
    });
    mocks.diffRepository.mockResolvedValue({ path: "main.go", staged: "", working: "+change", content: "", binary: false, truncated: false });
    mocks.openRepositoryFile.mockResolvedValue(undefined);
    mocks.openRepositoryFileWith.mockResolvedValue(undefined);
    mocks.previewRepositoryFile.mockResolvedValue({
      path: "main.go", absolutePath: "D:\\work\\repo\\main.go", content: "package main", size: 12, binary: false, truncated: false,
    });
    mocks.revealRepositoryFile.mockResolvedValue(undefined);
  });

  it("loads desktop state and toggles independent rails", async () => {
    const store = useAppStore();
    await store.initialize();

    expect(store.bootstrap?.workingDirectory).toBe("D:\\work\\repo");
    expect(mocks.eventHandler).toBeTypeOf("function");
    store.toggleSidebar();
    store.toggleInspector();
    expect(store.sidebarCollapsed).toBe(true);
    expect(store.inspectorOpen).toBe(false);
  });

  it("searches unopened session bodies and caches their text", async () => {
    const store = useAppStore();
    store.$patch({
      searchQuery: "hidden semaphore",
      threads: [{
        id: "thread-1", title: "Runtime audit", workspace: "pi-desk", workspacePath: "D:\\repo", trust: "deny",
        status: "idle", started: false, generation: 0, sessionFile: "C:\\sessions\\one.jsonl",
        modifiedAt: "2026-08-10T09:00:00Z",
      }],
    });
    mocks.getSessionSnapshot.mockResolvedValueOnce({
      messageCount: 2,
      messages: [
        { role: "user", content: "Inspect the worker" },
        { role: "assistant", content: [{ type: "text", text: "The hidden semaphore is released here." }] },
      ],
    });

    expect(store.filteredThreads).toEqual([]);
    await store.loadSessionSearchBodies();

    expect(mocks.getSessionSnapshot).toHaveBeenCalledWith("C:\\sessions\\one.jsonl");
    expect(store.filteredThreads.map((thread) => thread.id)).toEqual(["thread-1"]);

    await store.loadSessionSearchBodies();
    expect(mocks.getSessionSnapshot).toHaveBeenCalledTimes(1);
  });

  it("does not mark loaded sessions unavailable when desktop state persistence fails", async () => {
    mocks.listWorkspaces.mockResolvedValueOnce([{
      id: "workspace-1", name: "repo", path: "D:\\work\\repo", trust: "deny",
    }]);
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-1", path: "C:\\sessions\\one.jsonl", cwd: "D:\\work\\repo", title: "Runtime audit",
      firstMessage: "Inspect runtime", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z", messageCount: 1,
    }]);
    const store = useAppStore();
    await store.initialize();
    mocks.saveDesktopState.mockRejectedValueOnce(new Error("state file is locked"));

    await store.persistDesktopState();

    expect(store.catalogReady).toBe(true);
    expect(store.catalogError).toBe("");
    expect(store.settingsError).toBe("state file is locked");
    expect(store.threads).toHaveLength(1);
  });

  it("checks the Pi runtime without delaying initialization", async () => {
    let finishRuntimeCheck!: (status: { state: string; version: string; command: string }) => void;
    mocks.checkRuntime.mockReturnValueOnce(new Promise((resolve) => {
      finishRuntimeCheck = resolve;
    }));
    const store = useAppStore();

    await store.initialize();

    expect(mocks.checkRuntime).toHaveBeenCalledOnce();
    expect(store.runtimeCheckLoading).toBe(true);
    expect(store.bootstrap?.runtime.state).toBe("checking");

    finishRuntimeCheck({ state: "ready", version: "0.83.0", command: "pi.cmd" });
    await vi.waitFor(() => expect(store.runtimeCheckLoading).toBe(false));
    expect(store.bootstrap?.runtime).toMatchObject({ state: "ready", version: "0.83.0" });
  });

  it("does not replace a live runtime with a stale failed probe", async () => {
    const store = useAppStore();
    store.bootstrap = {
      productName: "Pi Desk", appVersion: "0.1.0", wailsVersion: "v3.0.0-beta.16",
      workingDirectory: "D:\\work\\repo", runtime: { state: RuntimeState.RuntimeReady, version: "0.83.0", command: "pi.cmd" },
      window: { x: 0, y: 0, width: 0, height: 0, maximized: false, valid: false },
    };
    await store.createThread("D:\\work\\repo", "deny");
    store.activeThread!.started = true;
    mocks.checkRuntime.mockRejectedValueOnce(new Error("probe timed out"));

    await store.checkRuntime();

    expect(store.bootstrap?.runtime).toMatchObject({ state: "ready", message: "Pi RPC session is running" });
  });

  it("restores and persists desktop behavior preferences", async () => {
    mocks.getDesktopState.mockResolvedValueOnce({
      threads: [],
      preferences: {
        appearance: "dark",
        language: "en",
        fontFamily: "serif",
        fontSize: 16,
        lightCodeTheme: "vitesse-light",
        darkCodeTheme: "catppuccin-mocha",
        showCodeLineNumbers: false,
        wrapCodeLines: true,
        codeFontSize: 14,
        offlineMode: false,
        proxyEnabled: true,
        proxyUrl: "http://127.0.0.1:7890",
        streamingBehavior: "followUp",
        sidebarCollapsed: true,
        sidebarWidth: 344,
        inspectorOpen: false,
        inspectorWidth: 468,
        inspectorTab: "context",
        workspaceApplication: "vscode",
      },
    });
    const store = useAppStore();

    await store.initialize();

    expect(store).toMatchObject({
      appearance: "dark", language: "en", interfaceFont: "serif", interfaceFontSize: 16,
      lightCodeTheme: "vitesse-light", darkCodeTheme: "catppuccin-mocha", showCodeLineNumbers: false, wrapCodeLines: true, codeFontSize: 14,
      offlineMode: false, proxyEnabled: true, proxyURL: "http://127.0.0.1:7890",
      streamingBehavior: "followUp", sidebarCollapsed: true, sidebarWidth: 344,
      inspectorOpen: false, inspectorWidth: 468, inspectorTab: "context", workspaceApplication: "vscode",
    });
    await store.persistDesktopState();
    expect(mocks.saveDesktopState).toHaveBeenCalledWith(expect.objectContaining({
      preferences: expect.objectContaining({
        proxyUrl: "http://127.0.0.1:7890", streamingBehavior: "followUp", fontFamily: "serif", fontSize: 16,
        lightCodeTheme: "vitesse-light", darkCodeTheme: "catppuccin-mocha", showCodeLineNumbers: false, wrapCodeLines: true, codeFontSize: 14,
        sidebarWidth: 344, inspectorOpen: false, inspectorWidth: 468, workspaceApplication: "vscode",
      }),
    }));
  });

  it("opens the active trusted workspace with an installed application and persists the selection", async () => {
    mocks.listWorkspaces.mockResolvedValueOnce([{
      id: "workspace-1", name: "repo", path: "D:\\work\\repo", trust: "approve",
    }]);
    mocks.getDesktopState.mockResolvedValueOnce({
      activeThreadId: "thread-1",
      threads: [{
        id: "thread-1", title: "Audit", workspacePath: "D:\\work\\repo", trust: "approve", status: "idle",
      }],
      preferences: {
        appearance: "light", language: "en", offlineMode: false, proxyEnabled: false, proxyUrl: "",
        streamingBehavior: "steer", sidebarCollapsed: false, inspectorOpen: true, inspectorTab: "changes",
        workspaceApplication: "file-manager",
      },
    });
    const store = useAppStore();
    await store.initialize();

    expect(store.activeWorkspaceApplication?.id).toBe("file-manager");
    expect(await store.openActiveWorkspaceWith("vscode")).toBe(true);
    expect(mocks.openWorkspaceWith).toHaveBeenCalledWith("workspace-1", "vscode");
    expect(store.workspaceApplication).toBe("vscode");

    await store.persistDesktopState();
    expect(mocks.saveDesktopState).toHaveBeenCalledWith(expect.objectContaining({
      preferences: expect.objectContaining({ workspaceApplication: "vscode" }),
    }));
  });

  it("does not open an untrusted workspace in an external application", async () => {
    mocks.listWorkspaces.mockResolvedValueOnce([{
      id: "workspace-1", name: "repo", path: "D:\\work\\repo", trust: "deny",
    }]);
    mocks.getDesktopState.mockResolvedValueOnce({
      activeThreadId: "thread-1",
      threads: [{
        id: "thread-1", title: "Audit", workspacePath: "D:\\work\\repo", trust: "deny", status: "idle",
      }],
    });
    const store = useAppStore();
    await store.initialize();

    expect(await store.openActiveWorkspaceWith("vscode")).toBe(false);
    expect(mocks.openWorkspaceWith).not.toHaveBeenCalled();
    expect(store.workspaceApplicationError).toBeTruthy();
  });

  it("clamps manually resized side panes", () => {
    const store = useAppStore();

    store.setSidebarWidth(100);
    store.setInspectorWidth(2_000);

    expect(store.sidebarWidth).toBe(180);
    expect(store.inspectorWidth).toBe(840);
  });

  it("restores layout preferences before slow session discovery finishes", async () => {
    let finishSessionDiscovery!: (sessions: never[]) => void;
    mocks.listSessions.mockReturnValueOnce(new Promise((resolve) => {
      finishSessionDiscovery = resolve;
    }));
    mocks.getDesktopState.mockResolvedValueOnce({
      threads: [],
      preferences: {
        appearance: "dark",
        language: "zh-CN",
        offlineMode: false,
        proxyEnabled: false,
        proxyUrl: "",
        streamingBehavior: "steer",
        sidebarCollapsed: true,
        inspectorOpen: false,
        inspectorTab: "changes",
      },
    });
    const store = useAppStore();

    const initialization = store.initialize();
    await vi.waitFor(() => expect(store.desktopStateReady).toBe(true));

    expect(store).toMatchObject({
      appearance: "dark",
      sidebarCollapsed: true,
      inspectorOpen: false,
      catalogReady: false,
      catalogLoading: true,
    });

    finishSessionDiscovery([]);
    await initialization;
    expect(store.catalogReady).toBe(true);
  });

  it("does not block catalog initialization while native application icons load", async () => {
    let finishApplications!: (applications: Array<{ id: string; name: string; iconDataUrl: string }>) => void;
    mocks.listWorkspaceApplications.mockReturnValueOnce(new Promise((resolve) => {
      finishApplications = resolve;
    }));
    const store = useAppStore();

    await store.initialize();

    expect(store.catalogReady).toBe(true);
    expect(store.workspaceApplicationsLoading).toBe(true);
    expect(store.workspaceApplications).toEqual([]);

    finishApplications([{ id: "file-manager", name: "File Explorer", iconDataUrl: "data:image/png;base64,ZmlsZXM=" }]);
    await vi.waitFor(() => expect(store.workspaceApplicationsLoading).toBe(false));
    expect(store.activeWorkspaceApplication?.id).toBe("file-manager");
  });

  it("uses Chinese and the light theme as new-install defaults", () => {
    const store = useAppStore();
    expect(store.appearance).toBe("light");
    expect(store.language).toBe("zh-CN");
    expect(store.interfaceFont).toBe("default");
    expect(store.interfaceFontSize).toBe(14);
    expect(store.lightCodeTheme).toBe("github-light");
    expect(store.darkCodeTheme).toBe("github-dark");
    expect(store.showCodeLineNumbers).toBe(true);
    expect(store.wrapCodeLines).toBe(false);
    expect(store.codeFontSize).toBe(12);
  });

  it("starts Pi asynchronously when a catalog task is selected and closes the oldest idle process above ten", async () => {
    const store = useAppStore();
    store.catalogReady = true;
    store.threads = Array.from({ length: 11 }, (_, index) => ({
      id: `thread-${index + 1}`,
      title: `Task ${index + 1}`,
      workspace: "repo",
      workspacePath: "D:\\work\\repo",
      trust: "deny" as const,
      status: "idle" as const,
      started: false,
      generation: 0,
    }));
    for (const thread of store.threads) {
      store.messagesByThread[thread.id] = [];
      store.transcriptStateByThread[thread.id] = "idle";
    }
    mocks.startSession.mockImplementation(async (request: { threadId: string }) => ({
      threadId: request.threadId,
      generation: 1,
      stateJson: JSON.stringify({ sessionId: `session-${request.threadId}` }),
    }));
    mocks.stopSession.mockResolvedValue(undefined);

    for (const thread of store.threads) {
      store.selectThread(thread.id);
      await vi.waitFor(() => expect(thread.started).toBe(true));
    }

    expect(mocks.startSession).toHaveBeenCalledTimes(11);
    expect(mocks.stopSession).toHaveBeenCalledWith("thread-1");
    expect(store.threads[0].started).toBe(false);
    expect(store.piProcessOrder).toEqual(store.threads.slice(1).map((thread) => thread.id));
  });

  it("creates a remote thread with WorkspaceID and no local path", async () => {
    const store = useAppStore();
    await store.createRemoteThread({
      id: "workspace-remote", name: "remote repo", path: "", kind: "ssh",
      targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve",
      addedAt: "2026-08-10T08:00:00Z", lastOpenedAt: "2026-08-10T08:00:00Z",
    });

    expect(store.activeThread).toMatchObject({
      workspaceId: "workspace-remote", workspacePath: "", workspace: "remote repo", trust: "approve",
    });
    expect(store.workspaces[0]).toMatchObject({ id: "workspace-remote", kind: "ssh", remoteRoot: "/srv/repo" });
  });

  it("selecting a persisted remote thread stays offline until explicit reconnect", async () => {
    const store = useAppStore();
    store.catalogReady = true;
    store.workspaces = [{
      id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote",
      remoteRoot: "/srv/repo", trust: "approve",
    }];
    store.threads = [{
      id: "thread-remote", title: "Remote task", workspace: "remote", workspaceId: "workspace-remote",
      workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0,
      sessionFile: "C:\\sessions\\remote.jsonl",
    }];
    store.transcriptStateByThread["thread-remote"] = "loaded";

    store.selectThread("thread-remote");
    await Promise.resolve();
    expect(mocks.resumeRemoteWorkspace).not.toHaveBeenCalled();
    expect(mocks.startSession).not.toHaveBeenCalled();

    store.startThreadInBackground("thread-remote");
    expect(store.remoteReconnectOpen).toBe(true);
    expect(mocks.startSession).not.toHaveBeenCalled();
    await store.confirmRemoteReconnect();

    expect(mocks.resumeRemoteWorkspace).toHaveBeenCalledWith("workspace-remote");
    await vi.waitFor(() => expect(mocks.startSession).toHaveBeenCalledWith(expect.objectContaining({
      threadId: "thread-remote", workspace: "", workspaceId: "workspace-remote",
      sessionPath: "C:\\sessions\\remote.jsonl",
    })));
  });

  it("resumes the requested remote thread even if active selection changes", async () => {
    const store = useAppStore();
    store.workspaces = [{
      id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote",
      remoteRoot: "/srv/repo", trust: "approve",
    }];
    store.threads = [
      { id: "thread-remote", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: true, generation: 1 },
      { id: "thread-local", title: "Local", workspace: "local", workspacePath: "D:\\work\\local", trust: "approve", status: "idle", started: false, generation: 0 },
    ];
    store.messagesByThread["thread-remote"] = [];
    store.messagesByThread["thread-local"] = [];
    store.draftsByThread["thread-remote"] = "Resume the original task";
    store.activeThreadId = "thread-remote";
    expect(store.requestRemoteReconnect(store.threads[0], "prompt")).toBe(true);
    store.activeThreadId = "thread-local";

    await store.confirmRemoteReconnect();

    expect(store.activeThreadId).toBe("thread-remote");
    expect(mocks.sendPrompt).toHaveBeenCalledWith(expect.objectContaining({
      threadId: "thread-remote", message: "Resume the original task",
    }));
  });

  it("rejects a reconnect completion after the target is concurrently disconnected", async () => {
    const store = useAppStore();
    store.workspaces = [{ id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }];
    store.threads = [{ id: "thread-remote", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0 }];
    store.activeThreadId = "thread-remote";
    store.messagesByThread["thread-remote"] = [];
    store.draftsByThread["thread-remote"] = "Do not resume";
    expect(store.requestRemoteReconnect(store.threads[0], "prompt")).toBe(true);
    let finishResume!: () => void;
    mocks.resumeRemoteWorkspace.mockReturnValueOnce(new Promise<void>((resolve) => { finishResume = resolve; }));

    const reconnect = store.confirmRemoteReconnect();
    await vi.waitFor(() => expect(mocks.resumeRemoteWorkspace).toHaveBeenCalled());
    const disconnect = store.disconnectRemoteTarget("target-remote");
    await disconnect;
    finishResume();
    await reconnect;

    expect(store.remoteReadyByWorkspace["workspace-remote"]).toBe(false);
    expect(store.remoteReconnectOpen).toBe(false);
    expect(mocks.disconnectRemoteTarget).toHaveBeenCalledTimes(2);
    expect(mocks.sendPrompt).not.toHaveBeenCalled();
    expect(mocks.startSession).not.toHaveBeenCalled();
    expect(store.draftsByThread["thread-remote"]).toBe("Do not resume");
  });

  it("keeps a remote prompt draft until reconnect succeeds and then resumes it", async () => {
    const store = useAppStore();
    store.workspaces = [{
      id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote",
      remoteRoot: "/srv/repo", trust: "approve",
    }];
    store.threads = [{
      id: "thread-remote", title: "Remote task", workspace: "remote", workspaceId: "workspace-remote",
      workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0,
    }];
    store.activeThreadId = "thread-remote";
    store.messagesByThread["thread-remote"] = [];
    store.transcriptStateByThread["thread-remote"] = "loaded";
    store.draftsByThread["thread-remote"] = "Continue remote work";

    await store.sendActivePrompt();
    expect(store.remoteReconnectIntent).toBe("prompt");
    expect(store.activeDraft).toBe("Continue remote work");
    expect(mocks.sendPrompt).not.toHaveBeenCalled();

    await store.confirmRemoteReconnect();
    await vi.waitFor(() => expect(mocks.sendPrompt).toHaveBeenCalledWith(expect.objectContaining({ message: "Continue remote work" })));
    expect(store.activeDraft).toBe("");
  });

  it("stops a stale started Pi session before reconnecting", async () => {
    const store = useAppStore();
    store.workspaces = [{ id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }];
    store.threads = [{ id: "thread-remote", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: true, generation: 1 }];
    store.activeThreadId = "thread-remote";
    store.messagesByThread["thread-remote"] = [];
    expect(store.requestRemoteReconnect(store.threads[0], "start")).toBe(true);

    await store.confirmRemoteReconnect();

    expect(mocks.stopSession).toHaveBeenCalledWith("thread-remote");
    expect(mocks.stopSession.mock.invocationCallOrder[0]).toBeLessThan(mocks.resumeRemoteWorkspace.mock.invocationCallOrder[0]);
    await vi.waitFor(() => expect(mocks.startSession).toHaveBeenCalled());
  });

  it("does not dispatch to a started remote Pi after readiness is revoked", async () => {
    const store = useAppStore();
    store.workspaces = [{ id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }];
    store.threads = [{ id: "thread-remote", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: true, generation: 1 }];
    store.activeThreadId = "thread-remote";
    store.messagesByThread["thread-remote"] = [];
    store.draftsByThread["thread-remote"] = "Do not dispatch yet";
    store.remoteReadyByWorkspace["workspace-remote"] = false;

    await store.sendActivePrompt();

    expect(mocks.sendPrompt).not.toHaveBeenCalled();
    expect(store.remoteReconnectOpen).toBe(true);
    expect(store.activeDraft).toBe("Do not dispatch yet");
  });

  it("reopens reconnect when resumed prompt startup immediately loses its generation", async () => {
    const store = useAppStore();
    store.workspaces = [{ id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }];
    store.threads = [{ id: "thread-remote", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0 }];
    store.activeThreadId = "thread-remote";
    store.messagesByThread["thread-remote"] = [];
    store.transcriptStateByThread["thread-remote"] = "loaded";
    store.draftsByThread["thread-remote"] = "Retry after reconnect";
    expect(store.requestRemoteReconnect(store.threads[0], "prompt")).toBe(true);
    mocks.startSession.mockRejectedValueOnce(new Error("REMOTE_DISCONNECTED: generation expired"));

    await store.confirmRemoteReconnect();

    expect(store.remoteReconnectBusy).toBe(false);
    expect(store.remoteReconnectOpen).toBe(true);
    expect(store.remoteReconnectThreadId).toBe("thread-remote");
    expect(store.activeDraft).toBe("Retry after reconnect");
  });

  it("restores a remote prompt when startup loses its generation", async () => {
    const store = useAppStore();
    store.workspaces = [{
      id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote",
      remoteRoot: "/srv/repo", trust: "approve",
    }];
    store.threads = [{
      id: "thread-remote", title: "Remote task", workspace: "remote", workspaceId: "workspace-remote",
      workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0,
    }];
    store.activeThreadId = "thread-remote";
    store.messagesByThread["thread-remote"] = [];
    store.transcriptStateByThread["thread-remote"] = "loaded";
    store.remoteReadyByWorkspace["workspace-remote"] = true;
    store.draftsByThread["thread-remote"] = "Keep this prompt";
    mocks.startSession.mockRejectedValueOnce(new Error("REMOTE_DISCONNECTED: generation expired: remote adapter coverage handshake"));

    await store.sendActivePrompt();

    expect(store.activeDraft).toBe("Keep this prompt");
    expect(store.messagesByThread["thread-remote"]).toHaveLength(0);
    expect(store.remoteReconnectOpen).toBe(true);
    expect(store.remoteReconnectIntent).toBe("prompt");
  });

  it("disconnects every task on a shared remote target and keeps workspace history", async () => {
    const store = useAppStore();
    store.workspaces = [
      { id: "workspace-a", name: "remote A", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/a", trust: "approve" },
      { id: "workspace-b", name: "remote B", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/b", trust: "approve" },
    ];
    store.threads = [
      { id: "thread-a", title: "Task A", workspace: "remote A", workspaceId: "workspace-a", workspacePath: "", trust: "approve", status: "idle", started: true, generation: 1 },
      { id: "thread-b", title: "Task B", workspace: "remote B", workspaceId: "workspace-b", workspacePath: "", trust: "approve", status: "idle", started: true, generation: 1 },
    ];
    store.piProcessOrder = ["thread-a", "thread-b"];
    store.remoteReadyByWorkspace = { "workspace-a": true, "workspace-b": true };
    mocks.stopSession.mockImplementation(async () => {
      expect(store.remoteReadyByWorkspace).toEqual({ "workspace-a": false, "workspace-b": false });
    });

    await store.disconnectRemoteWorkspace("workspace-a");

    expect(mocks.stopSession).toHaveBeenCalledWith("thread-a");
    expect(mocks.stopSession).toHaveBeenCalledWith("thread-b");
    expect(mocks.disconnectRemoteTarget).toHaveBeenCalledWith("target-remote");
    expect(store.workspaces).toHaveLength(2);
    expect(store.remoteReadyByWorkspace).toEqual({ "workspace-a": false, "workspace-b": false });
  });

  it("waits for an in-flight Pi start before disconnecting its remote target", async () => {
    const store = useAppStore();
    store.workspaces = [{ id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }];
    store.threads = [{ id: "thread-remote", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0 }];
    store.remoteReadyByWorkspace["workspace-remote"] = true;
    let finish!: (value: { threadId: string; generation: number; stateJson: string }) => void;
    mocks.startSession.mockReturnValueOnce(new Promise((resolve) => { finish = resolve; }));
    const start = store.ensureSession(store.threads[0]);
    await vi.waitFor(() => expect(mocks.startSession).toHaveBeenCalled());

    const disconnect = store.disconnectRemoteWorkspace("workspace-remote");
    await Promise.resolve();
    expect(mocks.disconnectRemoteTarget).not.toHaveBeenCalled();
    finish({ threadId: "thread-remote", generation: 1, stateJson: JSON.stringify({ sessionId: "session-1", isStreaming: false }) });
    await start;
    await disconnect;

    expect(mocks.stopSession).toHaveBeenCalledWith("thread-remote");
    expect(mocks.disconnectRemoteTarget).toHaveBeenCalledWith("target-remote");
    expect(store.threads[0].started).toBe(false);
  });

  it("marks a remote target stale when Pi stop reports revoked capability", async () => {
    const store = useAppStore();
    store.workspaces = [{ id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }];
    store.threads = [{ id: "thread-remote", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: true, generation: 1 }];
    store.messagesByThread["thread-remote"] = [];
    store.remoteReadyByWorkspace["workspace-remote"] = true;
    mocks.stopSession.mockRejectedValueOnce(new Error("REMOTE_DISCONNECTED: remote task was revoked because Pi did not stop cleanly"));

    expect(await store.stopThread("thread-remote")).toBe(false);
    expect(store.remoteReadyByWorkspace["workspace-remote"]).toBe(false);
    expect(store.repositoryStaleByWorkspace["workspace-remote"]).toBe(true);
  });

  it("revokes a remote target even when its local Pi stop reports failure", async () => {
    const store = useAppStore();
    store.workspaces = [{ id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }];
    store.threads = [{ id: "thread-remote", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: true, generation: 1 }];
    store.messagesByThread["thread-remote"] = [];
    store.remoteReadyByWorkspace["workspace-remote"] = true;
    mocks.stopSession.mockRejectedValueOnce(new Error("REMOTE_DISCONNECTED: task revoked"));

    await expect(store.disconnectRemoteWorkspace("workspace-remote")).rejects.toThrow("Unable to close Remote");
    expect(mocks.disconnectRemoteTarget).toHaveBeenCalledWith("target-remote");
    expect(store.remoteReadyByWorkspace["workspace-remote"]).toBe(false);
  });

  it("does not remove a remote workspace while its local Pi may still be running", async () => {
    const store = useAppStore();
    store.workspaces = [{ id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }];
    store.threads = [{ id: "thread-remote", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: true, generation: 1 }];
    store.messagesByThread["thread-remote"] = [];
    store.remoteReadyByWorkspace["workspace-remote"] = true;
    mocks.stopSession.mockRejectedValueOnce(new Error("REMOTE_DISCONNECTED: task revoked"));

    await expect(store.removeWorkspace("workspace-remote")).rejects.toThrow("Unable to close Remote");
    expect(mocks.disconnectRemoteTarget).toHaveBeenCalledWith("target-remote");
    expect(mocks.removeWorkspace).not.toHaveBeenCalled();
    expect(store.workspaces).toHaveLength(1);
  });

  it("drops an in-flight remote Repository refresh when the target disconnects", async () => {
    const store = useAppStore();
    store.workspaces = [{ id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }];
    store.threads = [{ id: "thread-remote", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0 }];
    store.activeThreadId = "thread-remote";
    let finish!: (value: { files: never[]; git: { isRepository: boolean; files: never[] } }) => void;
    mocks.snapshotRepository.mockReturnValueOnce(new Promise((resolve) => { finish = resolve; }));

    const refresh = store.refreshActiveRepository();
    await vi.waitFor(() => expect(mocks.snapshotRepository).toHaveBeenCalled());
    await store.disconnectRemoteWorkspace("workspace-remote");
    finish({ files: [], git: { isRepository: true, files: [] } });
    await refresh;

    expect(store.activeRepository).toBeUndefined();
    expect(store.activeRepositoryLoading).toBe(false);
    expect(store.activeRepositoryStale).toBe(true);
  });

  it("revokes sibling tasks when removing one workspace from a shared remote target", async () => {
    const store = useAppStore();
    store.workspaces = [
      { id: "workspace-a", name: "remote A", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/a", trust: "approve" },
      { id: "workspace-b", name: "remote B", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/b", trust: "approve" },
    ];
    store.threads = [
      { id: "thread-a", title: "Task A", workspace: "remote A", workspaceId: "workspace-a", workspacePath: "", trust: "approve", status: "idle", started: true, generation: 1 },
      { id: "thread-b", title: "Task B", workspace: "remote B", workspaceId: "workspace-b", workspacePath: "", trust: "approve", status: "idle", started: true, generation: 1 },
    ];
    store.piProcessOrder = ["thread-a", "thread-b"];
    store.remoteReadyByWorkspace = { "workspace-a": true, "workspace-b": true };
    mocks.stopSession.mockResolvedValue(undefined);

    await store.removeWorkspace("workspace-a");

    expect(mocks.stopSession).toHaveBeenCalledWith("thread-a");
    expect(mocks.stopSession).toHaveBeenCalledWith("thread-b");
    expect(mocks.removeWorkspace).toHaveBeenCalledWith("workspace-a");
    expect(store.workspaces.map((item) => item.id)).toEqual(["workspace-b"]);
    expect(store.threads.map((item) => item.id)).toEqual(["thread-b"]);
    expect(store.threads[0].started).toBe(false);
    expect(store.remoteReadyByWorkspace["workspace-b"]).toBe(false);
    expect(store.repositoryStaleByWorkspace["workspace-b"]).toBe(true);
  });

  it("keeps a shared remote target disconnected when workspace removal fails", async () => {
    const store = useAppStore();
    store.workspaces = [
      { id: "workspace-a", name: "remote A", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/a", trust: "approve" },
      { id: "workspace-b", name: "remote B", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/b", trust: "approve" },
    ];
    store.threads = [{ id: "thread-b", title: "Task B", workspace: "remote B", workspaceId: "workspace-b", workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0 }];
    store.remoteReadyByWorkspace = { "workspace-a": true, "workspace-b": true };
    mocks.removeWorkspace.mockRejectedValueOnce(new Error("state write failed after revoke"));

    await expect(store.removeWorkspace("workspace-a")).rejects.toThrow("state write failed after revoke");

    expect(store.workspaces).toHaveLength(2);
    expect(store.remoteReadyByWorkspace).toEqual({ "workspace-a": false, "workspace-b": false });
    expect(store.repositoryStaleByWorkspace["workspace-b"]).toBe(true);
  });

  it("only clears remote readiness for errors that require reconnect", async () => {
    const store = useAppStore();
    store.workspaces = [{ id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }];
    store.threads = [{
      id: "thread-remote", title: "Remote", workspace: "remote", workspaceId: "workspace-remote",
      workspacePath: "", trust: "approve", status: "idle", started: true, generation: 1,
    }];
    store.activeThreadId = "thread-remote";
    store.messagesByThread["thread-remote"] = [];
    store.remoteReadyByWorkspace["workspace-remote"] = true;
    mocks.bash.mockRejectedValueOnce(new Error("REMOTE_OUTPUT_LIMIT: output is too large"));
    store.draftsByThread["thread-remote"] = "! first";

    await store.sendActiveBash();
    expect(store.remoteReadyByWorkspace["workspace-remote"]).toBe(true);
    expect(store.activeRepositoryStale).toBe(true);

    mocks.bash.mockRejectedValueOnce(new Error("REMOTE_DISCONNECTED: transport closed"));
    store.draftsByThread["thread-remote"] = "! second";
    await store.sendActiveBash();
    expect(store.remoteReadyByWorkspace["workspace-remote"]).toBe(false);
    expect(store.draftsByThread["thread-remote"]).toBe("! second");
    expect(store.remoteReconnectOpen).toBe(true);
    expect(store.remoteReconnectIntent).toBe("bash");
  });

  it("does not reconnect for business errors that merely mention a remote code", async () => {
    const store = useAppStore();
    store.workspaces = [{ id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }];
    store.threads = [{ id: "thread-remote", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: true, generation: 1 }];
    store.activeThreadId = "thread-remote";
    store.messagesByThread["thread-remote"] = [];
    store.remoteReadyByWorkspace["workspace-remote"] = true;
    store.draftsByThread["thread-remote"] = "! echo REMOTE_DISCONNECTED";
    mocks.bash.mockRejectedValueOnce(new Error("Command failed while printing REMOTE_DISCONNECTED"));

    await store.sendActiveBash();

    expect(store.remoteReadyByWorkspace["workspace-remote"]).toBe(true);
    expect(store.remoteReconnectOpen).toBe(false);
  });

  it("keeps the first reconnect request while its dialog is open", () => {
    const store = useAppStore();
    store.workspaces = [
      { id: "workspace-a", name: "A", path: "", kind: "ssh", targetId: "target-a", remoteRoot: "/a", trust: "approve" },
      { id: "workspace-b", name: "B", path: "", kind: "ssh", targetId: "target-b", remoteRoot: "/b", trust: "approve" },
    ];
    const first = { id: "thread-a", title: "A", workspace: "A", workspaceId: "workspace-a", workspacePath: "", trust: "approve" as const, status: "idle" as const, started: false, generation: 0 };
    const second = { id: "thread-b", title: "B", workspace: "B", workspaceId: "workspace-b", workspacePath: "", trust: "approve" as const, status: "idle" as const, started: false, generation: 0 };
    store.threads = [first, second];

    expect(store.requestRemoteReconnect(first, "prompt")).toBe(true);
    expect(store.requestRemoteReconnect(second, "bash")).toBe(true);

    expect(store.remoteReconnectThreadId).toBe("thread-a");
    expect(store.remoteReconnectIntent).toBe("prompt");
  });

  it("creates a trusted thread and starts Pi lazily on first prompt", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread;
    if (!thread) throw new Error("thread was not created");
    mocks.startSession.mockResolvedValueOnce({
      threadId: thread.id,
      generation: 4,
      stateJson: JSON.stringify({ sessionId: "session-1", thinkingLevel: "medium" }),
    });
    store.updateDraft("Inspect the repository");

    await store.sendActivePrompt();

    expect(mocks.startSession).toHaveBeenCalledWith(expect.objectContaining({
      threadId: thread.id,
      workspace: "D:\\work\\repo",
      trust: "approve",
      offline: true,
    }));
    expect(mocks.sendPrompt).toHaveBeenCalledWith({
      threadId: thread.id,
      message: "Inspect the repository",
      streamingBehavior: undefined,
    });
    expect(thread.started).toBe(true);
    expect(thread.generation).toBe(4);
    expect(store.activeMessages[0]).toMatchObject({ role: "user", text: "Inspect the repository" });
    expect(thread.title).toBe("Inspect the repository");
  });

  it("uses the user request rather than skill transport text for a new task title", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    mocks.startSession.mockResolvedValueOnce({
      threadId: thread.id,
      generation: 4,
      stateJson: JSON.stringify({ sessionId: "session-skill", thinkingLevel: "medium" }),
    });
    store.updateDraft("/skill:grill-me Review the image generation plan.");

    await store.sendActivePrompt();

    expect(mocks.sendPrompt).toHaveBeenCalledWith({
      threadId: thread.id,
      message: "/skill:grill-me Review the image generation plan.",
      streamingBehavior: undefined,
    });
    expect(thread.firstMessage).toBe("Review the image generation plan.");
    expect(thread.title).toBe("Review the image generation plan.");
  });

  it("clears Todo widgets when a new idle turn is accepted", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    mocks.startSession.mockResolvedValueOnce({
      threadId: thread.id,
      generation: 4,
      stateJson: JSON.stringify({ sessionId: "session-1" }),
    });
    store.extensionWidgetsByThread[thread.id] = {
      "pi-deck-todo": { key: "pi-deck-todo", lines: ["old"], placement: "aboveEditor" },
      "pi-desk-todo": { key: "pi-desk-todo", lines: ["new"], placement: "aboveEditor" },
      retained: { key: "retained", lines: ["keep"], placement: "belowEditor" },
    };
    store.updateDraft("Start the next turn");

    await store.sendActivePrompt();

    expect(store.activeExtensionWidgets).toEqual([{ key: "retained", lines: ["keep"], placement: "belowEditor" }]);
  });

  it("keeps Todo widgets when invoking an extension command without a model turn", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    thread.started = true;
    thread.generation = 2;
    store.extensionWidgetsByThread[thread.id] = {
      "pi-desk-todo": { key: "pi-desk-todo", lines: ["current"], placement: "aboveEditor" },
    };
    store.updateDraft("/todo");

    await store.sendActivePrompt();

    expect(store.activeExtensionWidgets).toEqual([{ key: "pi-desk-todo", lines: ["current"], placement: "aboveEditor" }]);
  });

  it("does not load Pi's reserved JSONL path before a fresh session receives its first prompt", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    mocks.startSession.mockResolvedValueOnce({
      threadId: thread.id,
      generation: 5,
      stateJson: JSON.stringify({
        sessionId: "session-reserved",
        sessionFile: "C:\\sessions\\reserved-but-not-created.jsonl",
      }),
    });

    await store.ensureSession(thread);
    expect(store.transcriptStateByThread[thread.id]).toBe("loaded");
    expect(thread.sessionFile).toBeUndefined();
    store.updateDraft("First prompt");
    await store.sendActivePrompt();

    expect(mocks.getSessionSnapshot).not.toHaveBeenCalled();
    expect(thread.sessionFile).toBe("C:\\sessions\\reserved-but-not-created.jsonl");
    expect(mocks.sendPrompt).toHaveBeenCalledWith({
      threadId: thread.id,
      message: "First prompt",
      streamingBehavior: undefined,
    });
  });

  it("updates workspace access for every related task and restarts idle Pi processes", async () => {
    const store = useAppStore();
    store.$patch({
      workspaces: [{ id: "workspace-1", name: "repo", path: "D:\\work\\repo", trust: "deny" }],
      threads: [
        { id: "thread-1", title: "One", workspace: "repo", workspacePath: "D:\\work\\repo", trust: "deny", status: "idle", started: true, generation: 2 },
        { id: "thread-2", title: "Two", workspace: "repo", workspacePath: "D:\\work\\repo", trust: "deny", status: "idle", started: false, generation: 0 },
      ],
      activeThreadId: "thread-1",
      piProcessOrder: ["thread-1"],
    });
    mocks.stopSession.mockResolvedValue(undefined);
    store.startThreadInBackground = vi.fn();

    const changed = await store.setActiveWorkspaceTrust("approve");

    expect(changed).toBe(true);
    expect(mocks.stopSession).toHaveBeenCalledWith("thread-1");
    expect(mocks.addWorkspace).toHaveBeenCalledWith("D:\\work\\repo", "approve");
    expect(store.workspaces[0].trust).toBe("approve");
    expect(store.threads.map((thread) => thread.trust)).toEqual(["approve", "approve"]);
    expect(store.startThreadInBackground).toHaveBeenCalledWith("thread-1");
  });

  it("does not change workspace access while a related task is running", async () => {
    const store = useAppStore();
    store.$patch({
      workspaces: [{ id: "workspace-1", name: "repo", path: "D:\\work\\repo", trust: "deny" }],
      threads: [
        { id: "thread-1", title: "One", workspace: "repo", workspacePath: "D:\\work\\repo", trust: "deny", status: "idle", started: false, generation: 0 },
        { id: "thread-2", title: "Two", workspace: "repo", workspacePath: "D:\\work\\repo", trust: "deny", status: "running", started: true, generation: 2 },
      ],
      activeThreadId: "thread-1",
    });

    expect(store.activeWorkspaceTrustBusy).toBe(true);
    expect(await store.setActiveWorkspaceTrust("approve")).toBe(false);
    expect(mocks.addWorkspace).not.toHaveBeenCalled();
  });

  it("shows export completion in dialog state instead of the conversation", async () => {
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-1", path: "C:\\sessions\\one.jsonl", cwd: "D:\\work\\repo", title: "Runtime audit",
      firstMessage: "Inspect runtime", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z", messageCount: 1,
    }]);
    const store = useAppStore();
    await store.initialize();

    await store.exportActiveSession();

    expect(store.exportDialogOpen).toBe(true);
    expect(store.exportResultPath).toBe("D:\\work\\repo\\session.html");
    expect(store.activeMessages).toEqual([]);
  });

  it("removes an idle workspace registration without deleting its directory", async () => {
    const store = useAppStore();
    store.$patch({
      workspaces: [{ id: "workspace-1", name: "repo", path: "D:\\work\\repo", trust: "deny" }],
      threads: [{
        id: "thread-1", title: "Workspace task", workspace: "repo", workspacePath: "D:\\work\\repo", trust: "deny",
        status: "idle", started: false, generation: 0,
      }],
      activeThreadId: "thread-1",
    });

    await store.removeWorkspace("workspace-1");

    expect(mocks.removeWorkspace).toHaveBeenCalledWith("workspace-1");
    expect(store.workspaces).toEqual([]);
    expect(store.threads).toEqual([]);
    expect(store.activeThreadId).toBe("");
  });

  it("permanently deletes workspace sessions before removing its registration", async () => {
    const store = useAppStore();
    store.workspaces = [{ id: "workspace-1", name: "repo", path: "D:\\work\\repo", trust: "deny" }];

    await store.removeWorkspace("workspace-1", true);

    expect(mocks.deleteWorkspaceSessions).toHaveBeenCalledWith("workspace-1");
    expect(mocks.removeWorkspace).toHaveBeenCalledWith("workspace-1");
    expect(mocks.deleteWorkspaceSessions.mock.invocationCallOrder[0]).toBeLessThan(mocks.removeWorkspace.mock.invocationCallOrder[0]);
  });

  it("permanently deletes a discovered workspace by its local path", async () => {
    const store = useAppStore();
    store.workspaces = [{ id: "discovered-session-1", name: "removed", path: "D:\\work\\removed", trust: "deny", discovered: true }];
    store.threads = [{
      id: "thread-1", title: "Old task", workspace: "removed", workspacePath: "D:\\work\\removed", trust: "deny",
      status: "idle", started: false, generation: 0, sessionFile: "C:\\sessions\\old.jsonl",
    }];

    await store.removeWorkspace("discovered-session-1", true);

    expect(mocks.deleteWorkspaceSessions).toHaveBeenCalledWith("discovered-session-1", "D:\\work\\removed");
    expect(mocks.removeWorkspace).not.toHaveBeenCalled();
    expect(store.workspaces).toEqual([]);
    expect(store.threads).toEqual([]);
  });

  it("removes missing workspaces and sessions during local synchronization", async () => {
    mocks.listWorkspaces.mockResolvedValueOnce([{ id: "workspace-live", name: "live", path: "D:\\work\\live", trust: "deny" }]);
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-live", path: "C:\\sessions\\live.jsonl", cwd: "D:\\work\\live", title: "Live task",
      firstMessage: "Keep", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z", messageCount: 1,
    }]);
    const store = useAppStore();
    store.workspaces = [
      { id: "workspace-live", name: "old live", path: "D:\\work\\live", trust: "deny" },
      { id: "workspace-missing", name: "missing", path: "D:\\work\\missing", trust: "deny" },
      { id: "discovered-old", name: "old", path: "D:\\work\\old", trust: "deny", discovered: true },
    ];
    store.threads = [
      { id: "thread-live", title: "Old title", workspace: "old live", workspaceId: "workspace-live", workspacePath: "D:\\work\\live", trust: "deny", status: "idle", started: false, generation: 0, sessionFile: "C:\\sessions\\live.jsonl" },
      { id: "thread-missing-session", title: "Missing session", workspace: "old", workspacePath: "D:\\work\\old", trust: "deny", status: "idle", started: false, generation: 0, sessionFile: "C:\\sessions\\missing.jsonl" },
      { id: "thread-missing-workspace", title: "Missing workspace", workspace: "missing", workspaceId: "workspace-missing", workspacePath: "D:\\work\\missing", trust: "deny", status: "idle", started: false, generation: 0 },
    ];
    store.activeThreadId = "thread-missing-session";

    await store.syncLocalSessions();

    expect(store.workspaces.map((workspace) => workspace.id)).toEqual(["workspace-live"]);
    expect(store.threads.map((thread) => thread.id)).toEqual(["thread-live"]);
    expect(store.threads[0]).toMatchObject({ title: "Live task", workspace: "live", workspaceId: "workspace-live" });
    expect(store.activeThreadId).toBe("thread-live");
  });

  it("selects a model before Pi starts and applies it after startup", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    const selected = { provider: "custom", id: "gpt-5.6-sol", name: "GPT 5.6 Sol" };

    await store.chooseModel(selected);

    expect(mocks.setModel).not.toHaveBeenCalled();
    expect(store.activeModelPending).toBe(true);
    expect(store.activeSessionState?.model).toEqual(selected);

    mocks.startSession.mockResolvedValueOnce({
      threadId: thread.id,
      generation: 5,
      stateJson: JSON.stringify({ sessionId: "session-model", model: { provider: "openai", id: "gpt-4.1" } }),
    });
    mocks.getState.mockResolvedValueOnce({ sessionId: "session-model", model: selected });

    await store.ensureSession(thread);

    expect(mocks.setModel).toHaveBeenCalledWith({ threadId: thread.id, provider: "custom", modelId: "gpt-5.6-sol" });
    expect(store.activeModelPending).toBe(false);
    expect(store.activeSessionState?.model).toEqual(selected);
  });

  it("keeps the selected model when restarting before the first persisted prompt", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    const selected = { provider: "质谱官方", id: "glm-5.3-flash", name: "GLM" };
    await store.chooseModel(selected);
    mocks.startSession.mockResolvedValue({ threadId: thread.id, generation: 5,
      stateJson: JSON.stringify({ sessionId: "fresh", model: { provider: "default", id: "default" } }) });
    mocks.getState.mockResolvedValue({ sessionId: "fresh", model: selected });
    await store.ensureSession(thread);
    expect(store.pendingModelByThread[thread.id]).toBeUndefined();
    expect(thread.sessionFile).toBeFalsy();
    await store.stopThread(thread.id);
    await store.ensureSession(thread);
    expect(mocks.setModel).toHaveBeenCalledTimes(2);
    expect(store.activeSessionState?.model).toEqual(selected);
  });

  it("preserves a newer pending model when Pi exits during startup model application", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    const oldModel = { provider: "custom", id: "old", name: "Old" };
    const newModel = { provider: "custom", id: "new", name: "New" };
    await store.chooseModel(oldModel);
    mocks.startSession.mockResolvedValueOnce({
      threadId: thread.id, generation: 5,
      stateJson: JSON.stringify({ sessionId: "session-model", model: { provider: "openai", id: "default" } }),
    });
    let finishSetModel!: () => void;
    mocks.setModel.mockReturnValueOnce(new Promise<void>((resolve) => { finishSetModel = resolve; }));

    const starting = store.ensureSession(thread);
    await vi.waitFor(() => expect(mocks.setModel).toHaveBeenCalledWith({ threadId: thread.id, provider: "custom", modelId: "old" }));
    store.handlePiEvent({ threadId: thread.id, event: { generation: 5, type: "runtime_exit", error: "Pi exited while setting model" } });
    await store.chooseModel(newModel);
    finishSetModel();

    await expect(starting).rejects.toThrow("Pi exited while setting model");
    expect(store.pendingModelByThread[thread.id]).toEqual(newModel);
    expect(store.sessionStateByThread[thread.id]?.model).toEqual(newModel);
    expect(thread.started).toBe(false);
  });

  it("refreshes available thinking levels after switching the active model", async () => {
    const store = useAppStore();
    const grok = { provider: "grok", id: "grok-4.6", name: "Grok 4.6", reasoning: true };
    store.$patch({
      threads: [{
        id: "thread-model", title: "Models", workspace: "repo", workspacePath: "D:\\work\\repo", trust: "approve",
        status: "idle", started: true, generation: 1,
      }],
      activeThreadId: "thread-model",
      sessionStateByThread: { "thread-model": {
        model: { provider: "openai", id: "gpt-5.6-sol", reasoning: true }, thinkingLevel: "xhigh",
      } },
      thinkingLevelsByThread: { "thread-model": ["off", "minimal", "low", "medium", "high", "xhigh"] },
    });
    mocks.getState.mockResolvedValueOnce({ model: grok, thinkingLevel: "high" });
    mocks.getAvailableThinkingLevels.mockResolvedValueOnce({ levels: ["low", "medium", "high"] });

    const selection = store.chooseModel(grok);
    expect(store.activeThinkingLevels).toEqual([]);
    await selection;

    expect(mocks.setModel).toHaveBeenCalledWith({ threadId: "thread-model", provider: "grok", modelId: "grok-4.6" });
    expect(mocks.getAvailableThinkingLevels).toHaveBeenCalledWith("thread-model");
    expect(mocks.setModel.mock.invocationCallOrder[0]).toBeLessThan(mocks.getAvailableThinkingLevels.mock.invocationCallOrder[0]);
    expect(store.activeSessionState?.model).toEqual(grok);
    expect(store.activeThinkingLevels).toEqual(["low", "medium", "high"]);
  });

  it("waits for model selection before sending and keeps the latest selection", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    thread.started = true;
    thread.generation = 1;
    const glm = { provider: "custom", id: "glm-5.3" };
    const newer = { provider: "custom", id: "newer" };
    store.sessionStateByThread[thread.id] = { model: { provider: "custom", id: "gpt" } };
    let finish!: () => void;
    mocks.setModel.mockReturnValueOnce(new Promise<void>((resolve) => { finish = resolve; }));
    mocks.getState.mockResolvedValueOnce({ model: glm }).mockResolvedValueOnce({ model: newer });
    const selection = store.chooseModel(glm);
    const latest = store.chooseModel(newer);
    const sending = store.deliverPrompt(thread, "hello", []);
    await vi.waitFor(() => expect(mocks.setModel).toHaveBeenCalledTimes(1));
    expect(mocks.sendPrompt).not.toHaveBeenCalled();
    finish();
    await Promise.all([selection, latest, sending]);
    expect(mocks.setModel).toHaveBeenLastCalledWith({ threadId: thread.id, provider: newer.provider, modelId: newer.id });
    expect(store.activeSessionState?.model).toEqual(newer);
    expect(mocks.sendPrompt).toHaveBeenCalledTimes(1);
    expect(store.pendingModelByThread[thread.id]).toBeUndefined();
  });

  it("blocks an immediate send when an in-flight model switch fails, then allows an explicit retry", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    thread.started = true;
    const glm = { provider: "custom", id: "glm-5.3" };
    let fail!: (error: Error) => void;
    mocks.setModel.mockReturnValueOnce(new Promise((_, reject) => { fail = reject; }));
    const selection = store.chooseModel(glm);
    const sending = store.deliverPrompt(thread, "hello", []);
    await vi.waitFor(() => expect(store.messagesByThread[thread.id]).toHaveLength(1));
    fail(new Error("selection failed"));
    await selection;
    expect(await sending).toBe(false);
    expect(mocks.sendPrompt).not.toHaveBeenCalled();
    expect(store.pendingModelByThread[thread.id]).toEqual(glm);
    mocks.getState.mockResolvedValueOnce({ model: glm });
    expect(await store.deliverPrompt(thread, "retry", [])).toBe(true);
    expect(store.pendingModelByThread[thread.id]).toBeUndefined();
    expect(mocks.sendPrompt).toHaveBeenCalledTimes(1);
  });

  it.each(["rejected", "unconfirmed"])("blocks sending when startup model selection is %s and retains the choice", async (failure) => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    const glm = { provider: "custom", id: "glm-5.3" };
    const gpt = { provider: "custom", id: "gpt-5.6-sol" };
    await store.chooseModel(glm);
    mocks.startSession.mockResolvedValueOnce({ threadId: thread.id, generation: 1, stateJson: JSON.stringify({ model: gpt }) });
    if (failure === "rejected") mocks.setModel.mockRejectedValueOnce(new Error("selection failed"));
    else mocks.getState.mockResolvedValueOnce({ model: gpt });
    expect(await store.deliverPrompt(thread, "hello", [])).toBe(false);
    expect(mocks.sendPrompt).not.toHaveBeenCalled();
    expect(store.pendingModelByThread[thread.id]).toEqual(glm);
    expect(thread.status).toBe("attention");
  });

  it("waits for background startup model application even after the process is marked started", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    const glm = { provider: "custom", id: "glm-5.3" };
    await store.chooseModel(glm);
    mocks.startSession.mockResolvedValueOnce({ threadId: thread.id, generation: 1, stateJson: JSON.stringify({ model: { provider: "custom", id: "gpt" } }) });
    let finish!: () => void;
    mocks.setModel.mockReturnValueOnce(new Promise<void>((resolve) => { finish = resolve; }));
    mocks.getState.mockResolvedValueOnce({ model: glm });
    const startup = store.ensureSession(thread);
    await vi.waitFor(() => expect(mocks.setModel).toHaveBeenCalledTimes(1));
    expect(thread.started).toBe(true);
    const sending = store.deliverPrompt(thread, "hello", []);
    await Promise.resolve();
    expect(mocks.sendPrompt).not.toHaveBeenCalled();
    finish();
    await Promise.all([startup, sending]);
    expect(mocks.setModel).toHaveBeenCalledTimes(1);
    expect(mocks.sendPrompt).toHaveBeenCalledTimes(1);
  });

  it("ignores older state responses after selecting a new model", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    thread.started = true;
    const glm = { provider: "custom", id: "glm-5.3" };
    let finish!: (value: unknown) => void;
    mocks.getState.mockReturnValueOnce(new Promise((resolve) => { finish = resolve; })).mockResolvedValueOnce({ model: glm });
    const oldRefresh = store.refreshState(thread.id);
    await store.chooseModel(glm);
    finish({ model: { provider: "custom", id: "gpt" } });
    await oldRefresh;
    expect(store.activeSessionState?.model).toEqual(glm);
  });

  it("ignores a thinking-level response requested for a previously selected model", async () => {
    const store = useAppStore();
    const gpt = { provider: "openai", id: "gpt-5.6-sol", reasoning: true };
    const grok = { provider: "grok", id: "grok-4.6", reasoning: true };
    store.$patch({
      threads: [{
        id: "thread-model-race", title: "Models", workspace: "repo", workspacePath: "D:\\work\\repo", trust: "approve",
        status: "idle", started: true, generation: 1,
      }],
      activeThreadId: "thread-model-race",
      sessionStateByThread: { "thread-model-race": { model: gpt } },
      thinkingLevelsByThread: { "thread-model-race": ["off", "low", "medium", "high", "xhigh"] },
    });
    let resolveOld!: (value: { levels: string[] }) => void;
    mocks.getAvailableThinkingLevels
      .mockReturnValueOnce(new Promise((resolve) => { resolveOld = resolve; }))
      .mockResolvedValueOnce({ levels: ["low", "medium", "high"] });

    const oldRefresh = store.refreshThinkingLevels("thread-model-race");
    store.sessionStateByThread["thread-model-race"] = { model: grok };
    await store.refreshThinkingLevels("thread-model-race");
    resolveOld({ levels: ["off", "low", "medium", "high", "xhigh"] });
    await oldRefresh;

    expect(store.activeThinkingLevels).toEqual(["low", "medium", "high"]);
  });

  it("does not leak models from an older runtime into a started task", async () => {
    const store = useAppStore();
    const oldOpenAIModel = { provider: "openai", id: "gpt-5.4", name: "GPT 5.4" };
    const configured = { provider: "openai-direct", id: "gpt-5.6-sol", name: "GPT 5.6 Sol" };
    store.$patch({
      threads: [{
        id: "thread-runtime-models", title: "Models", workspace: "repo", workspacePath: "D:\\work\\repo", trust: "approve",
        status: "running", started: true, generation: 1,
      }],
      activeThreadId: "thread-runtime-models",
      modelsByThread: { "thread-runtime-models": [configured] },
      configuredModels: [configured],
      knownRuntimeModels: [oldOpenAIModel],
    });

    expect(store.activeModels).toEqual([configured]);
  });

  it("loads configured models without exposing model-management credentials", async () => {
    mocks.getConfiguredModels.mockResolvedValueOnce([
      { provider: "custom", id: "deepseek-v3", name: "DeepSeek V3", contextWindow: 128000, reasoning: true },
    ]);
    const store = useAppStore();

    await store.initialize();

    expect(store.activeModels).toEqual([]);
    expect(store.configuredModels).toEqual([
      { provider: "custom", id: "deepseek-v3", name: "DeepSeek V3", contextWindow: 128000, reasoning: true },
    ]);
  });

  it("fails a required configured-model refresh without clearing the previous catalog", async () => {
    const store = useAppStore();
    store.configuredModels = [{ provider: "custom", id: "stable", name: "Stable" }];
    mocks.getConfiguredModels.mockRejectedValueOnce(new Error("model catalog unavailable"));

    await expect(store.refreshConfiguredModels(true)).rejects.toThrow("model catalog unavailable");
    expect(store.configuredModels).toEqual([{ provider: "custom", id: "stable", name: "Stable" }]);
    expect(store.modelCatalogError).toBe("model catalog unavailable");
  });

  it("refreshes configured models before creating a new thread", async () => {
    mocks.getConfiguredModels.mockResolvedValueOnce([
      { provider: "custom", id: "edited-model", name: "Edited model", contextWindow: 256000, reasoning: true },
    ]);
    const store = useAppStore();

    await store.createThread("D:\\work\\repo", "deny");

    expect(mocks.getConfiguredModels).toHaveBeenCalledOnce();
    expect(store.configuredModels).toEqual([
      { provider: "custom", id: "edited-model", name: "Edited model", contextWindow: 256000, reasoning: true },
    ]);
  });

  it("assembles streamed text and tool execution while dropping stale generations", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.generation = 8;
    thread.started = true;

    store.handlePiEvent({ threadId: thread.id, event: { generation: 7, type: "message_start", payload: { message: { role: "assistant" } } } });
    expect(store.activeMessages).toHaveLength(0);

    store.handlePiEvent({ threadId: thread.id, event: { generation: 8, type: "agent_start", payload: {} } });
    expect(store.activeWaitingForOutput).toBe(true);
    store.handlePiEvent({ threadId: thread.id, event: { generation: 8, type: "message_start", payload: { message: { id: "assistant-1", role: "assistant", content: [] } } } });
    expect(store.activeWaitingForOutput).toBe(true);
    store.handlePiEvent({ threadId: thread.id, event: { generation: 8, type: "message_update", payload: { assistantMessageEvent: { type: "text_delta", delta: "Hello" } } } });
    expect(store.activeWaitingForOutput).toBe(false);
    store.handlePiEvent({ threadId: thread.id, event: { generation: 8, type: "tool_execution_start", payload: { toolCallId: "tool-1", toolName: "read", args: { path: "README.md" } } } });
    expect(store.activeWaitingForOutput).toBe(false);
    store.handlePiEvent({ threadId: thread.id, event: { generation: 8, type: "tool_execution_end", payload: { toolCallId: "tool-1", toolName: "read", result: { content: [{ type: "text", text: "done" }] } } } });
    expect(store.activeWaitingForOutput).toBe(true);
    store.handlePiEvent({ threadId: thread.id, event: { generation: 8, type: "agent_settled", payload: {} } });
    expect(store.activeWaitingForOutput).toBe(false);

    expect(thread.status).toBe("idle");
    expect(store.activeMessages[0]).toMatchObject({ role: "assistant", text: "Hello", streaming: false });
    expect(store.activeMessages[0].tools[0]).toMatchObject({ id: "tool-1", name: "read", output: "done", status: "complete" });
    expect(store.activeMessages[0].tools[0].resultReceived).toBe(true);
  });

  it("refreshes the settled background workspace instead of the active Repository", async () => {
    const store = useAppStore();
    store.workspaces = [
      { id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" },
      { id: "workspace-local", name: "local", path: "D:\\local", trust: "approve" },
    ];
    store.threads = [
      { id: "thread-remote", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "running", started: true, generation: 4 },
      { id: "thread-local", title: "Local", workspace: "local", workspaceId: "workspace-local", workspacePath: "D:\\local", trust: "approve", status: "idle", started: false, generation: 0 },
    ];
    store.messagesByThread["thread-remote"] = [];
    store.activeThreadId = "thread-local";

    store.handlePiEvent({ threadId: "thread-remote", event: { generation: 4, type: "agent_settled", payload: {} } });
    await vi.waitFor(() => expect(mocks.snapshotRepository).toHaveBeenCalledWith({ workspaceId: "workspace-remote" }));
    expect(mocks.snapshotRepository).not.toHaveBeenCalledWith({ workspaceId: "workspace-local" });
  });

  it("marks only settled background output unread and clears it when selected", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const background = store.activeThread!;
    background.generation = 8;
    background.started = true;
    await store.createThread("D:\\work\\repo", "deny");

    store.handlePiEvent({ threadId: background.id, event: { generation: 8, type: "agent_start", payload: {} } });
    expect(background.status).toBe("running");
    expect(background.unread).toBe(false);

    store.handlePiEvent({
      threadId: background.id,
      event: { generation: 8, type: "message_update", payload: { assistantMessageEvent: { type: "text_delta", delta: "Working" } } },
    });
    store.handlePiEvent({ threadId: background.id, event: { generation: 8, type: "queue_update", payload: { steering: [], followUp: [] } } });
    store.handlePiEvent({ threadId: background.id, event: { generation: 8, type: "agent_end", payload: {} } });
    expect(background.unread).toBe(false);

    store.handlePiEvent({ threadId: background.id, event: { generation: 8, type: "agent_settled", payload: {} } });
    expect(background.status).toBe("idle");
    expect(background.unread).toBe(true);

    store.selectThread(background.id);
    expect(background.unread).toBe(false);

    vi.spyOn(document, "hasFocus").mockReturnValue(true);
    store.handlePiEvent({ threadId: background.id, event: { generation: 8, type: "agent_start", payload: {} } });
    store.handlePiEvent({ threadId: background.id, event: { generation: 8, type: "agent_settled", payload: {} } });
    expect(background.unread).toBe(false);

    vi.spyOn(document, "hasFocus").mockReturnValue(false);
    store.handlePiEvent({ threadId: background.id, event: { generation: 8, type: "agent_start", payload: {} } });
    store.handlePiEvent({ threadId: background.id, event: { generation: 8, type: "agent_settled", payload: {} } });
    expect(background.unread).toBe(true);
    store.selectThread(background.id);
    expect(background.unread).toBe(false);
  });

  it("refreshes session stats after every message and calibrates again when settled", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.generation = 8;
    thread.started = true;
    mocks.getSessionStats.mockClear();

    for (const role of ["user", "assistant", "toolResult"]) {
      store.handlePiEvent({
        threadId: thread.id,
        event: { generation: 8, type: "message_end", payload: { message: { role, content: [] } } },
      });
    }
    await vi.waitFor(() => expect(mocks.getSessionStats).toHaveBeenCalledTimes(3));

    store.handlePiEvent({ threadId: thread.id, event: { generation: 8, type: "agent_end", payload: {} } });
    await Promise.resolve();
    expect(mocks.getSessionStats).toHaveBeenCalledTimes(3);

    store.handlePiEvent({ threadId: thread.id, event: { generation: 8, type: "agent_settled", payload: {} } });
    await vi.waitFor(() => expect(mocks.getSessionStats).toHaveBeenCalledTimes(4));
  });

  it("does not let an older stats request overwrite a newer result", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    let resolveOlder!: (value: { totalMessages: number }) => void;
    let resolveNewer!: (value: { totalMessages: number }) => void;
    mocks.getSessionStats
      .mockReturnValueOnce(new Promise((resolve) => { resolveOlder = resolve; }))
      .mockReturnValueOnce(new Promise((resolve) => { resolveNewer = resolve; }));

    const older = store.refreshStats(thread.id);
    const newer = store.refreshStats(thread.id);
    resolveNewer({ totalMessages: 2 });
    await newer;
    resolveOlder({ totalMessages: 1 });
    await older;

    expect(store.sessionStatsByThread[thread.id]?.totalMessages).toBe(2);
  });

  it("captures Pi edit result diffs and tool duration from RPC events", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.generation = 11;
    thread.started = true;

    store.handlePiEvent({ threadId: thread.id, event: { generation: 11, type: "message_start", payload: { message: { id: "assistant-edit", role: "assistant", content: [] } } } });
    store.handlePiEvent({ threadId: thread.id, event: { generation: 11, type: "tool_execution_start", payload: { toolCallId: "edit-1", toolName: "edit", args: { path: "main.go", oldText: "old", newText: "new" } } } });
    store.handlePiEvent({ threadId: thread.id, event: { generation: 11, type: "tool_execution_end", payload: { toolCallId: "edit-1", toolName: "edit", result: { content: [{ type: "text", text: "done" }], details: { diff: "- 1 old\n+ 1 new" } } } } });

    expect(store.activeMessages[0].tools[0]).toMatchObject({
      name: "edit",
      resultReceived: true,
      status: "complete",
      diff: { path: "main.go", text: "- 1 old\n+ 1 new" },
    });
    expect(store.activeMessages[0].tools[0].durationMs).toBeGreaterThanOrEqual(0);
  });

  it("recovers complete reasoning from the final assistant message", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.generation = 10;
    thread.started = true;

    store.handlePiEvent({ threadId: thread.id, event: { generation: 10, type: "message_start", payload: { message: { id: "assistant-thinking", role: "assistant", content: [] } } } });
    store.handlePiEvent({ threadId: thread.id, event: { generation: 10, type: "message_update", payload: { assistantMessageEvent: { type: "thinking_delta", delta: "Partial" } } } });
    expect(store.activeMessages[0].activeExecution).toBe("thinking");
    store.handlePiEvent({
      threadId: thread.id,
      event: {
        generation: 10,
        type: "message_end",
        payload: { message: { role: "assistant", content: [
          { type: "thinking", thinking: "Complete reasoning" },
          { type: "text", text: "Final answer" },
        ] } },
      },
    });

    expect(store.activeMessages[0]).toMatchObject({ text: "Final answer", thinking: "Complete reasoning", streaming: true });
    expect(store.activeMessages[0].activeExecution).toBeUndefined();

    store.handlePiEvent({ threadId: thread.id, event: { generation: 10, type: "tool_execution_start", payload: { toolCallId: "verify-1", toolName: "bash", args: { command: "npm test" } } } });
    expect(store.activeMessages[0]).toMatchObject({ streaming: true, activeExecution: "tool" });
    store.handlePiEvent({ threadId: thread.id, event: { generation: 10, type: "tool_execution_end", payload: { toolCallId: "verify-1", toolName: "bash", result: { output: "ok" } } } });
    expect(store.activeMessages[0].activeExecution).toBeUndefined();

    store.handlePiEvent({ threadId: thread.id, event: { generation: 10, type: "message_start", payload: { message: { id: "assistant-final", role: "assistant", content: [] } } } });
    store.handlePiEvent({ threadId: thread.id, event: { generation: 10, type: "message_update", payload: { assistantMessageEvent: { type: "text_delta", delta: "All checks passed" } } } });
    expect(store.activeMessages.map((message) => message.streaming)).toEqual([true, true]);

    store.handlePiEvent({ threadId: thread.id, event: { generation: 10, type: "agent_end", payload: {} } });
    expect(store.activeMessages.map((message) => message.streaming)).toEqual([false, false]);
  });

  it("notifies when the active task finishes while the app is unfocused", async () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(false);
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.generation = 8;
    thread.started = true;

    store.handlePiEvent({ threadId: thread.id, event: { generation: 8, type: "agent_settled", payload: {} } });

    expect(mocks.notifyDesktop).toHaveBeenCalledWith("任务已完成", "New task");
  });

  it("bounds projected tool output while retaining its beginning and end", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.generation = 9;
    thread.started = true;
    const output = `BEGIN-${"x".repeat(300_000)}-END`;

    store.handlePiEvent({ threadId: thread.id, event: { generation: 9, type: "message_start", payload: { message: { id: "assistant-large", role: "assistant", content: [] } } } });
    store.handlePiEvent({ threadId: thread.id, event: { generation: 9, type: "tool_execution_end", payload: { toolCallId: "tool-large", toolName: "bash", result: { output } } } });

    const tool = store.activeMessages[0].tools[0];
    expect(tool.truncated).toBe(true);
    expect(tool.output.length).toBe(256 << 10);
    expect(tool.output).toMatch(/^BEGIN-/);
    expect(tool.output).toMatch(/-END$/);
    expect(tool.output).toContain("output truncated by Pi Desk");
  });

  it("normalizes current Pi command source metadata", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const threadID = store.activeThreadId;
    mocks.getCommands.mockResolvedValueOnce({ commands: [
      {
        name: "skill:context-mode",
        description: "Process large output",
        source: "skill",
        sourceInfo: {
          path: "C:\\packages\\context-mode\\SKILL.md",
          source: "npm:context-mode",
          scope: "user",
          origin: "package",
        },
      },
      {
        name: "temporary-command",
        source: "extension",
        sourceInfo: {
          path: "C:\\temp\\temporary-command.ts",
          source: "--extension",
          scope: "temporary",
          origin: "top-level",
        },
      },
    ] });

    await store.refreshCommands(threadID);

    expect(store.commandsByThread[threadID]).toEqual([
      expect.objectContaining({ name: "skill:context-mode", location: "user", path: "C:\\packages\\context-mode\\SKILL.md" }),
      expect.objectContaining({ name: "temporary-command", location: "path", path: "C:\\temp\\temporary-command.ts" }),
    ]);
  });

  it("keeps running prompts in an editable local queue and steers on demand", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.started = true;
    thread.generation = 5;
    thread.status = "running";
    store.updateDraft("Check the failing test next");

    await store.sendActivePrompt();

    expect(mocks.sendPrompt).not.toHaveBeenCalled();
    expect(store.activePendingPrompts).toEqual([expect.objectContaining({ text: "Check the failing test next", images: [] })]);
    expect(store.activeDraft).toBe("");

    const queued = store.activePendingPrompts[0];
    const replacementImage = {
      id: "replacement", name: "replacement.png", data: "aW1hZ2U=", mimeType: "image/png", previewUrl: "data:image/png;base64,aW1hZ2U=",
    };
    store.updatePendingPrompt(queued.id, "Check the build logs instead", [replacementImage]);
    expect(store.activePendingPrompts[0]).toMatchObject({ text: "Check the build logs instead", images: [replacementImage] });
    expect(store.activePendingPrompts[0].images[0]).not.toBe(replacementImage);

    await store.steerPendingPrompt(queued.id);
    expect(mocks.sendPrompt).toHaveBeenCalledWith({
      threadId: thread.id,
      message: "Check the build logs instead",
      streamingBehavior: "steer",
      images: [{ type: "image", data: "aW1hZ2U=", mimeType: "image/png" }],
    });
    expect(store.activePendingPrompts).toEqual([]);
    expect(store.activeMessages[0]).toMatchObject({ delivery: "steer" });
  });

  it("restores a queued prompt when immediate steering fails", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.started = true;
    thread.generation = 5;
    thread.status = "running";
    store.updateDraft("Try another approach");
    await store.sendActivePrompt();
    const queued = store.activePendingPrompts[0];
    mocks.sendPrompt.mockRejectedValueOnce(new Error("steer rejected"));

    await store.steerPendingPrompt(queued.id);

    expect(store.activePendingPrompts).toEqual([queued]);
    expect(store.activeMessages.some((message) => message.role === "user")).toBe(false);
    expect(thread.status).toBe("running");
  });

  it("dispatches one local queued prompt after the settled transcript reload", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.started = true;
    thread.generation = 5;
    thread.status = "idle";
    thread.sessionFile = "C:\\sessions\\one.jsonl";
    store.transcriptStateByThread[thread.id] = "loaded";
    store.updateDraft("Inspect the failure");
    await store.sendActivePrompt();
    mocks.sendPrompt.mockClear();
    store.updateDraft("Run the focused tests");
    await store.sendActivePrompt();
    let resolveSnapshot!: (value: { messages: unknown[]; messageCount: number }) => void;
    mocks.getSessionSnapshot.mockReturnValueOnce(new Promise((resolve) => {
      resolveSnapshot = resolve;
    }));

    store.handlePiEvent({ threadId: thread.id, event: { generation: 5, type: "agent_settled", payload: {} } });
    await vi.waitFor(() => expect(mocks.getSessionSnapshot).toHaveBeenCalledWith("C:\\sessions\\one.jsonl"));
    expect(mocks.sendPrompt).not.toHaveBeenCalled();
    resolveSnapshot({ messages: [], messageCount: 0 });
    await vi.waitFor(() => expect(mocks.sendPrompt).toHaveBeenCalledWith({
      threadId: thread.id,
      message: "Run the focused tests",
      streamingBehavior: undefined,
    }));

    expect(store.activePendingPrompts).toEqual([]);
    expect(thread.status).toBe("running");
  });

  it("still mirrors Pi-owned queue events for runtime diagnostics", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.started = true;
    thread.generation = 5;
    thread.status = "running";

    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 5, type: "queue_update", payload: { steering: ["Inspect logs"], followUp: ["Run tests"] } },
    });
    expect(store.activeQueue).toEqual({ steering: ["Inspect logs"], followUp: ["Run tests"] });
  });

  it("places successful compaction at the live Pi event position without temporary timeline notices", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.started = true;
    thread.generation = 7;
    thread.status = "running";
    store.sessionStateByThread[thread.id] = { model: { id: "gpt-5", provider: "openai", contextWindow: 200_000 } };
    store.sessionStatsByThread[thread.id] = {
      contextUsage: { tokens: 123_158, contextWindow: 200_000, percent: 61.579 },
    };
    store.messagesByThread[thread.id] = [{
      id: "assistant-before", role: "assistant", text: "Before compaction", thinking: "",
      timestamp: "08/14 07:00", streaming: false, tools: [],
    }];

    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 7, type: "compaction_start", payload: { reason: "threshold" } },
    });
    expect(store.activeMessages).toHaveLength(1);

    store.handlePiEvent({
      threadId: thread.id,
      event: {
        generation: 7,
        type: "compaction_end",
        payload: {
          reason: "threshold", aborted: false,
          result: { summary: "## Saved context", tokensBefore: 241_443, estimatedTokensAfter: 32_000 },
        },
      },
    });
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 7, type: "message_start", payload: { message: { id: "assistant-after", role: "assistant", content: [] } } },
    });

    expect(store.activeMessages.map((message) => message.id)).toEqual([
      "assistant-before", expect.stringMatching(/^live-compaction-/), "assistant-after",
    ]);
    expect(store.activeMessages[1]).toMatchObject({
      role: "system", text: "", compaction: {
        summary: "## Saved context", tokensBefore: 241_443, estimatedTokensAfter: 32_000,
      },
    });
    expect(store.activeSessionStats?.contextUsage).toEqual({
      tokens: 32_000, contextWindow: 200_000, percent: 16, estimated: true,
    });
    expect(store.activeMessages.some((message) => /Compacting conversation|Conversation context compacted/.test(message.text))).toBe(false);

    delete store.compactionEstimatesByThread[thread.id];
    delete store.latestCompactionEstimateByThread[thread.id];
    store.applySessionSnapshot(thread, {
      messages: [{
        role: "piDeskCompaction", summary: "## Saved context", tokensBefore: 241_443, estimatedTokensAfter: 32_000,
        timestamp: "2026-08-14T07:01:00Z", piDeskEntryId: "compact-1",
      }],
      messageCount: 1,
    });
    expect(store.activeMessages[0].compaction?.estimatedTokensAfter).toBe(32_000);

    mocks.getSessionStats.mockResolvedValueOnce({
      contextUsage: { tokens: null, contextWindow: 200_000, percent: null },
    });
    await store.refreshStats(thread.id);
    expect(store.activeSessionStats?.contextUsage).toEqual({
      tokens: 32_000, contextWindow: 200_000, percent: 16, estimated: true,
    });

    store.sessionStatsByThread[thread.id] = {
      contextUsage: { tokens: 40_000, contextWindow: 200_000, percent: 20 },
    };
    store.applySessionSnapshot(thread, {
      messages: [{
        role: "piDeskCompaction", summary: "## Saved context", tokensBefore: 241_443, estimatedTokensAfter: 32_000,
        timestamp: "2026-08-14T07:01:00Z", piDeskEntryId: "compact-1",
      }],
      messageCount: 1,
    });
    expect(store.activeSessionStats?.contextUsage).toEqual({
      tokens: 40_000, contextWindow: 200_000, percent: 20,
    });
  });

  it("keeps automatic compaction busy until Pi settles", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.started = true;
    thread.generation = 7;
    thread.status = "running";

    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 7, type: "agent_end", payload: { willRetry: true } },
    });
    expect(thread.status).toBe("running");
    expect(store.activeWaitingForOutput).toBe(true);

    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 7, type: "compaction_start", payload: { reason: "threshold" } },
    });
    expect(store.activeWaitingForOutput).toBe(true);

    store.handlePiEvent({
      threadId: thread.id,
      event: {
        generation: 7,
        type: "compaction_end",
        payload: {
          reason: "threshold", aborted: false, willRetry: false,
          result: { summary: "## Saved context", tokensBefore: 200_000, estimatedTokensAfter: 32_000 },
        },
      },
    });
    expect(thread.status).toBe("running");
    expect(store.activeWaitingForOutput).toBe(true);

    store.handlePiEvent({ threadId: thread.id, event: { generation: 7, type: "agent_settled", payload: {} } });
    expect(thread.status).toBe("idle");
    expect(store.activeWaitingForOutput).toBe(false);
  });

  it("updates Pi-owned queue, compaction, and retry behavior", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.started = true;
    store.sessionStateByThread[thread.id] = { sessionId: "session-1", steeringMode: "one-at-a-time", followUpMode: "one-at-a-time", autoCompactionEnabled: true };
    mocks.getState.mockResolvedValue({ sessionId: "session-1", steeringMode: "all", followUpMode: "all", autoCompactionEnabled: false });

    await store.setSteeringMode("all");
    await store.setFollowUpMode("all");
    await store.setAutoCompaction(false);
    await store.setAutoRetry(false);

    expect(mocks.setSteeringMode).toHaveBeenCalledWith({ threadId: thread.id, mode: "all" });
    expect(mocks.setFollowUpMode).toHaveBeenCalledWith({ threadId: thread.id, mode: "all" });
    expect(mocks.setAutoCompaction).toHaveBeenCalledWith({ threadId: thread.id, enabled: false });
    expect(mocks.setAutoRetry).toHaveBeenCalledWith({ threadId: thread.id, enabled: false });
    expect(store.activeSessionState).toMatchObject({ steeringMode: "all", followUpMode: "all", autoCompactionEnabled: false });
    expect(store.activeAutoRetryEnabled).toBe(false);
  });

  it("sends an image-only prompt using Pi image content", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    mocks.startSession.mockResolvedValueOnce({
      threadId: thread.id,
      generation: 6,
      stateJson: JSON.stringify({ sessionId: "session-image", isStreaming: false }),
    });
    store.addActiveAttachments([{
      id: "image-1",
      name: "screen.png",
      data: "aW1hZ2U=",
      mimeType: "image/png",
      previewUrl: "data:image/png;base64,aW1hZ2U=",
    }]);

    await store.sendActivePrompt();

    expect(mocks.sendPrompt).toHaveBeenCalledWith({
      threadId: thread.id,
      message: "",
      streamingBehavior: undefined,
      images: [{ type: "image", data: "aW1hZ2U=", mimeType: "image/png" }],
    });
    expect(store.activeMessages[0].images?.[0]).toMatchObject({ name: "screen.png" });
    expect(store.activeAttachments).toHaveLength(0);
    expect(thread.title).toBe("Image task");
  });

  it("runs and streams a Pi bash command with double-bang context exclusion", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    mocks.startSession.mockResolvedValueOnce({
      threadId: thread.id,
      generation: 12,
      stateJson: JSON.stringify({ sessionId: "session-bash", isStreaming: false }),
    });
    let finish!: (value: { output: string; exitCode?: number; cancelled: boolean; truncated: boolean }) => void;
    mocks.bash.mockReturnValueOnce(new Promise((resolve) => { finish = resolve; }));
    store.updateDraft("!! git status");

    const running = store.sendActiveBash();
    await vi.waitFor(() => expect(mocks.bash).toHaveBeenCalled());
    store.handlePiEvent({ threadId: thread.id, event: { generation: 12, type: "bash_execution_update", payload: { delta: "working\n" } } });
    expect(store.activeBashRunning).toBe(true);
    expect(store.activeMessages.at(-1)?.text).toContain("working");
    finish({ output: "clean\n", exitCode: 0, cancelled: false, truncated: false });
    await running;

    expect(mocks.bash).toHaveBeenCalledWith({ threadId: thread.id, command: "git status", excludeFromContext: true });
    expect(store.activeMessages.at(-1)).toMatchObject({ role: "system", text: "$ git status\nclean\n", streaming: false });
    expect(store.activeBashRunning).toBe(false);
  });

  it("anchors automatic retry and recovery state to the assistant run", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.started = true;
    thread.generation = 3;

    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 3, type: "message_start", payload: { message: { id: "assistant-failed", role: "assistant", content: [] } } },
    });
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 3, type: "message_end", payload: { message: {
        role: "assistant", content: [], errorMessage: "OpenAI API error (520); apiKey=sk-1234567890abcdefghijkl",
      } } },
    });
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 3, type: "agent_end", payload: { willRetry: true } },
    });
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 3, type: "auto_retry_start", payload: {
        attempt: 1, maxAttempts: 3, delayMs: 1000,
        errorMessage: "OpenAI API error (520); Authorization: Bearer provider-secret",
      } },
    });

    expect(store.activeMessages[0].runNotice).toEqual({
      status: "retrying",
      error: "OpenAI API error (520); Authorization: [redacted]",
      attempt: 1,
      maxAttempts: 3,
      delayMs: 1000,
      retryAt: expect.any(Number),
    });
    expect(store.activeMessages[0].error).not.toContain("sk-1234567890abcdefghijkl");

    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 3, type: "message_start", payload: { message: { id: "assistant-recovered", role: "assistant", content: [] } } },
    });
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 3, type: "message_end", payload: { message: {
        role: "assistant", content: [{ type: "text", text: "Recovered output" }],
      } } },
    });
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 3, type: "auto_retry_end", payload: { success: true, attempt: 1 } },
    });

    expect(store.activeMessages[0]).toMatchObject({
      runNotice: {
        status: "recovered",
        error: "OpenAI API error (520); Authorization: [redacted]",
        attempt: 1,
        maxAttempts: 3,
      },
    });
    expect(store.activeMessages[1]).toMatchObject({ text: "Recovered output" });
    expect(store.activeMessages[1].runNotice).toBeUndefined();
    expect(store.activeRetry).toBeUndefined();
  });

  it("finalizes earlier retry attempts before starting the next one", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.started = true;
    thread.generation = 6;

    const failedAttempt = (id: string, error: string) => {
      store.handlePiEvent({
        threadId: thread.id,
        event: { generation: 6, type: "message_start", payload: { message: { id, role: "assistant", content: [] } } },
      });
      store.handlePiEvent({
        threadId: thread.id,
        event: { generation: 6, type: "message_end", payload: { message: { role: "assistant", content: [], errorMessage: error } } },
      });
      store.handlePiEvent({ threadId: thread.id, event: { generation: 6, type: "agent_end", payload: { willRetry: true } } });
    };

    failedAttempt("assistant-attempt-1", "Request timed out.");
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 6, type: "auto_retry_start", payload: {
        attempt: 1, maxAttempts: 3, delayMs: 1000, errorMessage: "Request timed out.",
      } },
    });
    failedAttempt("assistant-attempt-2", "OpenAI API error (520)");
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 6, type: "auto_retry_start", payload: {
        attempt: 2, maxAttempts: 3, delayMs: 2000, errorMessage: "OpenAI API error (520)",
      } },
    });

    expect(store.activeMessages[0].runNotice).toMatchObject({ status: "retried", attempt: 1 });
    expect(store.activeMessages[1].runNotice).toMatchObject({ status: "retrying", attempt: 2 });

    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 6, type: "message_start", payload: { message: { id: "assistant-after-retries", role: "assistant", content: [] } } },
    });
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 6, type: "message_end", payload: { message: {
        role: "assistant", content: [{ type: "text", text: "Recovered output" }],
      } } },
    });
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 6, type: "auto_retry_end", payload: { success: true, attempt: 2 } },
    });

    expect(store.activeMessages[1].runNotice).toMatchObject({ status: "recovered", attempt: 2 });
    expect(store.activeMessages[2]).toMatchObject({ text: "Recovered output" });
    expect(store.activeMessages[2].runNotice).toBeUndefined();
  });

  it("finalizes a missing retry-end event at the authoritative settled boundary", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.started = true;
    thread.generation = 5;

    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 5, type: "message_start", payload: { message: { id: "assistant-retry-gap", role: "assistant", content: [] } } },
    });
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 5, type: "message_end", payload: { message: {
        role: "assistant", content: [], errorMessage: "Request timed out.",
      } } },
    });
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 5, type: "auto_retry_start", payload: {
        attempt: 1, maxAttempts: 3, delayMs: 1000, errorMessage: "Request timed out.",
      } },
    });
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 5, type: "message_start", payload: { message: { id: "assistant-after-gap", role: "assistant", content: [] } } },
    });
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 5, type: "message_end", payload: { message: {
        role: "assistant", content: [{ type: "text", text: "Recovered without retry end" }],
      } } },
    });
    store.handlePiEvent({ threadId: thread.id, event: { generation: 5, type: "agent_settled", payload: {} } });

    expect(store.activeMessages[0].runNotice).toMatchObject({
      status: "recovered", error: "Request timed out.", attempt: 1, maxAttempts: 3,
    });
    expect(store.activeMessages[1].runNotice).toBeUndefined();
    expect(store.activeRetry).toBeUndefined();
  });

  it("shows exhausted retry errors on the assistant message without a duplicate system message", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.started = true;
    thread.generation = 4;

    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 4, type: "message_start", payload: { message: { id: "assistant-final-error", role: "assistant", content: [] } } },
    });
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 4, type: "message_end", payload: { message: {
        role: "assistant", content: [], errorMessage: "Request timed out.",
      } } },
    });
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 4, type: "auto_retry_start", payload: {
        attempt: 3, maxAttempts: 3, delayMs: 4000, errorMessage: "Request timed out.",
      } },
    });
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 4, type: "agent_end", payload: { willRetry: false } },
    });
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 4, type: "auto_retry_end", payload: {
        success: false, attempt: 3, finalError: "Request timed out.",
      } },
    });

    expect(store.activeMessages).toHaveLength(1);
    expect(store.activeMessages[0].runNotice).toEqual({
      status: "failed",
      error: "Request timed out.",
      attempt: 3,
      maxAttempts: 3,
    });
  });

  it("shows and cancels Pi auto-retry state", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.started = true;
    thread.generation = 3;

    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 3, type: "auto_retry_start", payload: { attempt: 2, maxAttempts: 4, delayMs: 1500, errorMessage: "rate limited" } },
    });
    expect(store.activeRetry).toEqual({ attempt: 2, maxAttempts: 4, delayMs: 1500, errorMessage: "rate limited" });
    expect(thread.status).toBe("running");

    await store.abortActiveRetry();
    expect(mocks.abortRetry).toHaveBeenCalledWith(thread.id);

    store.handlePiEvent({ threadId: thread.id, event: { generation: 3, type: "auto_retry_end", payload: { success: true, attempt: 2 } } });
    expect(store.activeRetry).toBeUndefined();
  });

  it("responds to blocking extension UI requests", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.generation = 2;
    thread.started = true;
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 2, type: "extension_ui_request", payload: { id: "ui-1", method: "confirm", title: "Continue?", message: "Proceed" } },
    });

    expect(store.extensionRequestByThread[thread.id]?.id).toBe("ui-1");
    await store.respondToExtension(false);
    expect(mocks.respondExtensionUI).toHaveBeenCalledWith({
      threadId: thread.id,
      requestId: "ui-1",
      value: undefined,
      confirmed: false,
      cancelled: undefined,
    });
    expect(store.extensionRequestByThread[thread.id]).toBeUndefined();
  });

  it("projects the ask_question batch envelope instead of exposing transport JSON", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.generation = 3;
    thread.started = true;
    const envelope = JSON.stringify({
      __piDeckBatchAsk: 1,
      review: true,
      questions: [
        {
          id: "listen_port", type: "select", question: "服务器对外访问端口使用哪个？",
          options: [{ label: "8000（推荐）", value: "8000", description: "访问地址为 http://example:8000" }],
          allowOther: true, placeholder: "也可以填写其他端口", prefill: "8000",
        },
        { id: "data_mode", type: "confirm", question: "是否迁移现有数据？" },
      ],
    });

    store.handlePiEvent({
      threadId: thread.id,
      event: {
        generation: 3,
        type: "extension_ui_request",
        payload: { id: "batch-1", method: "input", title: envelope, placeholder: "__piDeckBatchAsk__" },
      },
    });

    expect(store.extensionRequestByThread[thread.id]).toMatchObject({
      id: "batch-1",
      method: "batch_ask",
      batchReview: true,
      batchQuestions: [
        { id: "listen_port", type: "select", prefill: "8000", options: [{ label: "8000（推荐）", value: "8000" }] },
        { id: "data_mode", type: "confirm" },
      ],
    });
    expect(store.extensionRequestByThread[thread.id]?.title).toBeUndefined();

    const response = JSON.stringify({ answers: [
      { id: "listen_port", type: "select", value: "8000", label: "8000（推荐）" },
      { id: "data_mode", type: "confirm", value: true, label: "是" },
    ] });
    await store.respondToExtension(response);
    expect(mocks.respondExtensionUI).toHaveBeenCalledWith({
      threadId: thread.id, requestId: "batch-1", value: response, confirmed: undefined, cancelled: undefined,
    });
  });

  it("accepts questions and review directly on a marked extension request", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.generation = 3;
    thread.started = true;

    store.handlePiEvent({
      threadId: thread.id,
      event: {
        generation: 3,
        type: "extension_ui_request",
        payload: {
          id: "direct-batch", method: "input", title: "", placeholder: "__piDeckBatchAsk__", review: true,
          questions: [{ id: "port", type: "select", question: "Port?", options: ["8000", "8080"], allowOther: true }],
          type: "select", question: "", options: [],
        },
      },
    });

    expect(store.extensionRequestByThread[thread.id]).toMatchObject({
      id: "direct-batch", method: "batch_ask", batchReview: true,
      batchQuestions: [{ id: "port", options: [{ label: "8000", value: "8000" }, { label: "8080", value: "8080" }] }],
    });
  });

  it("cancels malformed marked batch envelopes without opening a raw JSON dialog", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.generation = 3;
    thread.started = true;

    store.handlePiEvent({
      threadId: thread.id,
      event: {
        generation: 3,
        type: "extension_ui_request",
        payload: { id: "bad-batch", method: "input", title: "{broken", placeholder: "__piDeckBatchAsk__" },
      },
    });

    expect(store.extensionRequestByThread[thread.id]).toBeUndefined();
    expect(mocks.respondExtensionUI).toHaveBeenCalledWith({ threadId: thread.id, requestId: "bad-batch", cancelled: true });
  });

  it("does not start a queued remote Pi after its target becomes stale", async () => {
    const store = useAppStore();
    const blocker = { id: "thread-blocker-stale", title: "Blocker", workspace: "local", workspacePath: "D:\\local", trust: "deny" as const, status: "idle" as const, started: false, generation: 0 };
    const remote = { id: "thread-remote-stale", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve" as const, status: "idle" as const, started: false, generation: 0 };
    store.workspaces = [{ id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }];
    store.threads = [blocker, remote];
    store.remoteReadyByWorkspace["workspace-remote"] = true;
    let finishBlocker!: (value: { threadId: string; generation: number; stateJson: string }) => void;
    mocks.startSession.mockReturnValueOnce(new Promise((resolve) => { finishBlocker = resolve; }));

    const blocking = store.ensureSession(blocker);
    await vi.waitFor(() => expect(mocks.startSession).toHaveBeenCalledTimes(1));
    const queued = store.ensureSession(remote);
    store.markRemoteTargetStale("target-remote");
    finishBlocker({ threadId: blocker.id, generation: 1, stateJson: JSON.stringify({ sessionId: "session-blocker" }) });
    await blocking;

    await expect(queued).rejects.toThrow("must be reconnected");
    expect(mocks.startSession).toHaveBeenCalledTimes(1);
    expect(remote.started).toBe(false);
  });

  it("isolates generations while a replacement Pi start waits in the queue", async () => {
    const store = useAppStore();
    const blocker = { id: "thread-blocker", title: "Blocker", workspace: "local", workspacePath: "D:\\local", trust: "deny" as const, status: "idle" as const, started: false, generation: 0 };
    const thread = { id: "thread-restart", title: "Restart", workspace: "repo", workspacePath: "D:\\repo", trust: "deny" as const, status: "idle" as const, started: false, generation: 2 };
    store.threads = [blocker, thread];
    let finishBlocker!: (value: { threadId: string; generation: number; stateJson: string }) => void;
    let finishRestart!: (value: { threadId: string; generation: number; stateJson: string }) => void;
    mocks.startSession
      .mockReturnValueOnce(new Promise((resolve) => { finishBlocker = resolve; }))
      .mockReturnValueOnce(new Promise((resolve) => { finishRestart = resolve; }));

    const blocking = store.ensureSession(blocker);
    await vi.waitFor(() => expect(mocks.startSession).toHaveBeenCalledTimes(1));
    const starting = store.ensureSession(thread);
    await Promise.resolve();
    expect(mocks.startSession).toHaveBeenCalledTimes(1);
    store.handlePiEvent({ threadId: thread.id, event: { generation: 2, type: "runtime_exit", error: "old Pi exited" } });
    expect(thread.status).toBe("starting");

    finishBlocker({ threadId: blocker.id, generation: 1, stateJson: JSON.stringify({ sessionId: "session-1", isStreaming: false }) });
    await blocking;
    await vi.waitFor(() => expect(mocks.startSession).toHaveBeenCalledTimes(2));
    store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 3, type: "extension_ui_request", payload: { id: "status", method: "setStatus", statusKey: "remote", statusText: "Ready" } },
    });
    expect(store.extensionStatusesByThread[thread.id]).toEqual({ remote: "Ready" });

    finishRestart({ threadId: thread.id, generation: 3, stateJson: JSON.stringify({ sessionId: "session-3", isStreaming: false }) });
    await starting;
    expect(thread.generation).toBe(3);
  });

  it("drops Pi RPC refreshes that complete after their generation exits", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    Object.assign(thread, { started: true, generation: 8 });
    store.sessionStateByThread[thread.id] = { sessionId: "current", model: { provider: "openai", id: "current" } };
    store.modelsByThread[thread.id] = [{ provider: "openai", id: "current" }];
    store.thinkingLevelsByThread[thread.id] = ["medium"];
    store.commandsByThread[thread.id] = [{ name: "current", source: "extension" }];
    store.sessionStatsByThread[thread.id] = { totalMessages: 1 };
    let resolveState!: (value: Record<string, unknown>) => void;
    let resolveModels!: (value: { models: Array<{ provider: string; id: string }> }) => void;
    let resolveThinking!: (value: { levels: string[] }) => void;
    let resolveCommands!: (value: { commands: Array<{ name: string; source: "extension" }> }) => void;
    let resolveStats!: (value: { totalMessages: number }) => void;
    mocks.getState.mockReturnValueOnce(new Promise((resolve) => { resolveState = resolve; }));
    mocks.getAvailableModels.mockReturnValueOnce(new Promise((resolve) => { resolveModels = resolve; }));
    mocks.getAvailableThinkingLevels.mockReturnValueOnce(new Promise((resolve) => { resolveThinking = resolve; }));
    mocks.getCommands.mockReturnValueOnce(new Promise((resolve) => { resolveCommands = resolve; }));
    mocks.getSessionStats.mockReturnValueOnce(new Promise((resolve) => { resolveStats = resolve; }));

    const refreshes = [store.refreshState(thread.id), store.refreshModels(thread.id), store.refreshThinkingLevels(thread.id), store.refreshCommands(thread.id), store.refreshStats(thread.id)];
    store.handlePiEvent({ threadId: thread.id, event: { generation: 8, type: "runtime_exit", error: "closed" } });
    resolveState({ sessionId: "stale", model: { provider: "openai", id: "stale" } });
    resolveModels({ models: [{ provider: "openai", id: "stale" }] });
    resolveThinking({ levels: ["stale"] });
    resolveCommands({ commands: [{ name: "stale", source: "extension" }] });
    resolveStats({ totalMessages: 99 });
    await Promise.all(refreshes);

    expect(store.sessionStateByThread[thread.id]?.sessionId).toBe("current");
    expect(store.modelsByThread[thread.id]?.[0].id).toBe("current");
    expect(store.thinkingLevelsByThread[thread.id]).toEqual(["medium"]);
    expect(store.commandsByThread[thread.id]?.[0].name).toBe("current");
    expect(store.sessionStatsByThread[thread.id]).toEqual({ totalMessages: 1 });
  });

  it("keeps a thread restartable when Pi returns malformed startup state", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    mocks.startSession.mockResolvedValueOnce({ threadId: thread.id, generation: 5, stateJson: "{" });

    await expect(store.ensureSession(thread)).rejects.toThrow();

    expect(thread.started).toBe(false);
    expect(thread.generation).toBe(0);
    expect(mocks.stopSession).toHaveBeenCalledWith(thread.id);
    mocks.startSession.mockResolvedValueOnce({
      threadId: thread.id, generation: 6, stateJson: JSON.stringify({ sessionId: "session-6", isStreaming: false }),
    });
    await store.ensureSession(thread);
    expect(thread).toMatchObject({ started: true, generation: 6, status: "idle" });
  });

  it("does not revive a Pi generation that exits before its start response", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    let finish!: (value: { threadId: string; generation: number; stateJson: string }) => void;
    mocks.startSession.mockReturnValueOnce(new Promise((resolve) => { finish = resolve; }));

    const starting = store.ensureSession(thread);
    await vi.waitFor(() => expect(mocks.startSession).toHaveBeenCalled());
    store.handlePiEvent({ threadId: thread.id, event: { generation: 4, type: "runtime_exit", error: "Pi exited during startup" } });
    finish({ threadId: thread.id, generation: 4, stateJson: JSON.stringify({ sessionId: "session-4", isStreaming: false }) });

    await expect(starting).rejects.toThrow("Pi exited during startup");
    expect(thread).toMatchObject({ started: false, status: "attention", generation: 4 });
    store.handlePiEvent({ threadId: thread.id, event: { generation: 4, type: "agent_start", payload: {} } });
    expect(thread).toMatchObject({ started: false, status: "attention", generation: 4 });
  });

  it("projects and clears fire-and-forget extension UI state per thread", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.generation = 4;
    thread.started = true;

    const emit = (payload: Record<string, unknown>) => store.handlePiEvent({
      threadId: thread.id,
      event: { generation: 4, type: "extension_ui_request", payload },
    });
    emit({
      id: "mcp",
      method: "setStatus",
      statusKey: "mcp",
      statusText: "\u001b[38;5;109m🔌 MCP: 1 server enabled\u001b[39m",
    });
    expect(store.activeExtensionStatuses).toEqual([]);
    emit({ id: "mcp-clear", method: "setStatus", statusKey: "mcp" });
    emit({
      id: "mcp-orphan-ansi",
      method: "setStatus",
      statusKey: "mcp",
      statusText: "38;5;109m🔌 MCP: 1 server enabled\u001b[39m",
    });
    expect(store.activeExtensionStatuses).toEqual([]);
    emit({ id: "mcp-orphan-ansi-clear", method: "setStatus", statusKey: "mcp" });
    emit({ id: "status", method: "setStatus", statusKey: "plan", statusText: "Planning" });
    emit({ id: "widget", method: "setWidget", widgetKey: "plan", widgetLines: ["Step 1", "Step 2"], widgetPlacement: "belowEditor" });
    emit({ id: "title", method: "setTitle", title: "Plan mode" });
    emit({ id: "editor", method: "set_editor_text", text: "Review @src/main.go" });

    expect(store.activeExtensionStatuses).toEqual([{ key: "plan", text: "Planning" }]);
    expect(store.activeExtensionWidgets).toEqual([{ key: "plan", lines: ["Step 1", "Step 2"], placement: "belowEditor", instance: "widget" }]);
    emit({ id: "widget-update", method: "setWidget", widgetKey: "plan", widgetLines: ["Step 3"], widgetPlacement: "belowEditor" });
    expect(store.activeExtensionWidgets[0]).toMatchObject({ lines: ["Step 3"], instance: "widget" });
    expect(store.activeExtensionTitle).toBe("Plan mode");
    expect(store.activeDraft).toBe("Review @src/main.go");

    emit({ id: "status-clear", method: "setStatus", statusKey: "plan" });
    emit({ id: "widget-clear", method: "setWidget", widgetKey: "plan" });
    expect(store.activeExtensionStatuses).toEqual([]);
    expect(store.activeExtensionWidgets).toEqual([]);
    emit({ id: "widget-next-turn", method: "setWidget", widgetKey: "plan", widgetLines: ["Step 1"] });
    expect(store.activeExtensionWidgets[0]).toMatchObject({ instance: "widget-next-turn" });

    store.handlePiEvent({ threadId: thread.id, event: { generation: 4, type: "runtime_exit", error: "closed" } });
    expect(store.activeExtensionTitle).toBe("");
    expect(store.extensionRequestByThread[thread.id]).toBeUndefined();
  });

  it("does not append closed RPC stream shutdown noise to the conversation", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "deny");
    const thread = store.activeThread!;
    thread.generation = 4;
    thread.started = true;
    const initialCount = store.activeMessages.length;
    const closedError = "read pi RPC stream: read |0: file already closed";

    store.handlePiEvent({ threadId: thread.id, event: { generation: 4, type: "protocol_error", error: closedError } });
    store.handlePiEvent({ threadId: thread.id, event: { generation: 4, type: "runtime_exit", error: closedError } });

    expect(store.activeMessages).toHaveLength(initialCount);
    expect(thread.started).toBe(false);
    expect(thread.status).toBe("attention");
    expect(thread.error).toBe(closedError);
  });

  it("only selects known threads", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\one", "deny");
    const first = store.activeThreadId;
    mocks.addWorkspace.mockResolvedValueOnce({ id: "workspace-2", name: "two", path: "D:\\work\\two", trust: "deny" });
    await store.createThread("D:\\work\\two", "deny");

    store.selectThread(first);
    expect(store.activeThreadId).toBe(first);
    store.selectThread("missing");
    expect(store.activeThreadId).toBe(first);
  });

  it("loads repository context only for a trusted workspace", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");

    await store.refreshActiveRepository();

    expect(mocks.snapshotRepository).toHaveBeenCalledWith({ workspaceId: "workspace-1" });
    expect(store.activeRepository?.git.branch).toBe("main");

    mocks.addWorkspace.mockResolvedValueOnce({ id: "workspace-2", name: "private", path: "D:\\work\\private", trust: "deny" });
    await store.createThread("D:\\work\\private", "deny");
    await store.refreshActiveRepository();
    expect(mocks.snapshotRepository).toHaveBeenCalledTimes(1);
    expect(store.activeRepositoryError).toBe("Workspace access is disabled");
  });

  it("keeps context-change invalidation scoped to one workspace", () => {
    const store = useAppStore();
    store.workspaces = [
      { id: "workspace-a", name: "A", path: "", kind: "ssh", targetId: "target-shared", remoteRoot: "/a", trust: "approve" },
      { id: "workspace-b", name: "B", path: "", kind: "ssh", targetId: "target-shared", remoteRoot: "/b", trust: "approve" },
    ];
    store.threads = [{ id: "thread-a", title: "A", workspace: "A", workspaceId: "workspace-a", workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0 }];
    store.remoteReadyByWorkspace = { "workspace-a": true, "workspace-b": true };

    store.remoteFailureMessage("thread-a", new Error("REMOTE_CONTEXT_CHANGED_WAIT_FOR_IDLE"));

    expect(store.remoteReadyByWorkspace).toEqual({ "workspace-a": false, "workspace-b": true });
    expect(store.repositoryStaleByWorkspace["workspace-a"]).toBe(true);
    expect(store.repositoryStaleByWorkspace["workspace-b"]).toBeUndefined();
  });

  it("revokes every sibling workspace projection after a target failure", () => {
    const store = useAppStore();
    store.workspaces = [
      { id: "workspace-a", name: "A", path: "", kind: "ssh", targetId: "target-shared", remoteRoot: "/a", trust: "approve" },
      { id: "workspace-b", name: "B", path: "", kind: "ssh", targetId: "target-shared", remoteRoot: "/b", trust: "approve" },
    ];
    store.threads = [{ id: "thread-a", title: "A", workspace: "A", workspaceId: "workspace-a", workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0 }];
    store.remoteReadyByWorkspace = { "workspace-a": true, "workspace-b": true };

    expect(store.remoteFailureMessage("thread-a", new Error("REMOTE_DISCONNECTED: helper exited"))).toContain("REMOTE_DISCONNECTED");

    expect(store.remoteReadyByWorkspace).toEqual({ "workspace-a": false, "workspace-b": false });
    expect(store.repositoryStaleByWorkspace).toEqual({ "workspace-a": true, "workspace-b": true });
  });

  it("clears remote readiness when a Repository request reports disconnection", async () => {
    const store = useAppStore();
    store.workspaces = [{ id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }];
    store.threads = [{ id: "thread-remote", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0 }];
    store.activeThreadId = "thread-remote";
    store.remoteReadyByWorkspace["workspace-remote"] = true;
    mocks.snapshotRepository.mockRejectedValueOnce(new Error("REMOTE_DISCONNECTED: read lease revoked"));

    await store.refreshActiveRepository();

    expect(store.remoteReadyByWorkspace["workspace-remote"]).toBe(false);
    expect(store.activeRepositoryStale).toBe(true);
  });

  it("keeps the last Repository snapshot stale when refresh fails", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    await store.refreshActiveRepository();
    const snapshot = store.activeRepository;

    mocks.snapshotRepository.mockRejectedValueOnce(new Error("remote repository is disconnected or stale"));
    await store.refreshActiveRepository();

    expect(store.activeRepository).toBe(snapshot);
    expect(store.activeRepositoryStale).toBe(true);
    expect(store.activeRepositoryError).toContain("disconnected or stale");

    mocks.snapshotRepository.mockResolvedValueOnce({ files: [], git: { isRepository: false, files: [] } });
    await store.refreshActiveRepository();
    expect(store.activeRepositoryStale).toBe(false);
  });

  it("keeps remote Repository data stale across a mutating tool dispatch and failure", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    Object.assign(store.workspaces[0], { kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", path: "" });
    thread.workspacePath = "";
    thread.generation = 12;
    store.handlePiEvent({ threadId: thread.id, event: { generation: 12, type: "tool_execution_start", payload: { toolCallId: "write-1", toolName: "write", args: { path: "README.md" } } } });
    expect(store.activeRepositoryStale).toBe(true);

    store.repositoryStaleByWorkspace[thread.workspaceId!] = false;
    store.handlePiEvent({ threadId: thread.id, event: { generation: 12, type: "tool_execution_end", payload: { toolCallId: "write-1", toolName: "write", isError: true, result: { content: [{ type: "text", text: "REMOTE_OUTCOME_UNKNOWN" }] } } } });
    expect(store.activeRepositoryStale).toBe(true);
  });

  it("marks only remote Repository data stale after terminal exit or error", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const local = store.activeThread!;
    store.handleTerminalEvent({ threadId: local.id, type: "exit", sequence: 1 });
    expect(store.activeRepositoryStale).toBe(false);

    store.workspaces.push({ id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" });
    store.threads.push({ id: "thread-remote", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0 });
    store.activeThreadId = "thread-remote";
    store.remoteReadyByWorkspace["workspace-remote"] = true;
    store.handleTerminalEvent({ threadId: "thread-remote", type: "exit", sequence: 1, error: "context canceled" });
    expect(store.activeRepositoryStale).toBe(true);
    expect(store.remoteReadyByWorkspace["workspace-remote"]).toBe(true);

    store.repositoryStaleByWorkspace["workspace-remote"] = false;
    store.handleTerminalEvent({ threadId: "thread-remote", type: "error", sequence: 2, error: "REMOTE_OUTCOME_UNKNOWN: terminal delivery is unknown" });
    expect(store.activeRepositoryStale).toBe(true);
    expect(store.remoteReadyByWorkspace["workspace-remote"]).toBe(false);
  });

  it("ignores a Terminal failure from an older remote session incarnation", () => {
    const store = useAppStore();
    store.workspaces = [{ id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }];
    store.threads = [{ id: "thread-remote", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: true, generation: 2 }];
    store.remoteReadyByWorkspace["workspace-remote"] = true;
    store.setTerminalGeneration("thread-remote", 2);
    store.markRemoteRepositoryStale("thread-remote");
    expect(store.terminalGenerationByThread["thread-remote"]).toBe(2);
    store.repositoryStaleByWorkspace["workspace-remote"] = false;

    store.handleTerminalEvent({ threadId: "thread-remote", type: "exit", generation: 1, sequence: 5, error: "REMOTE_DISCONNECTED: old terminal" });
    expect(store.remoteReadyByWorkspace["workspace-remote"]).toBe(true);
    expect(store.repositoryStaleByWorkspace["workspace-remote"]).toBe(false);

    store.handleTerminalEvent({ threadId: "thread-remote", type: "exit", generation: 2, sequence: 1, error: "REMOTE_DISCONNECTED: current terminal" });
    expect(store.remoteReadyByWorkspace["workspace-remote"]).toBe(false);
  });

  it("drops an in-flight Repository refresh after trust is revoked", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    let finish!: (value: { files: never[]; git: { isRepository: boolean; files: never[] } }) => void;
    mocks.snapshotRepository.mockReturnValueOnce(new Promise((resolve) => { finish = resolve; }));

    const refresh = store.refreshActiveRepository();
    await vi.waitFor(() => expect(mocks.snapshotRepository).toHaveBeenCalled());
    store.activeThread!.trust = "deny";
    await store.refreshActiveRepository();
    finish({ files: [], git: { isRepository: true, files: [] } });
    await refresh;

    expect(store.activeRepository).toBeUndefined();
    expect(store.activeRepositoryStale).toBe(true);
    expect(store.activeRepositoryError).toBe("Workspace access is disabled");
  });

  it("isolates panel state and late file results between sessions in the same workspace", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const firstId = store.activeThreadId;
    let finish!: (value: { path: string; working: string }) => void;
    mocks.diffRepository.mockReturnValueOnce(new Promise((resolve) => { finish = resolve; }));
    const pending = store.openRepositoryDiff("main.go");

    await store.createThread("D:\\work\\repo", "approve");
    const secondId = store.activeThreadId;
    expect(store.inspectorOpen).toBe(false);
    expect(store.activeRepositoryDiffPath).toBe("");
    await store.openRepositoryDiff("main.go", "+second session");
    finish({ path: "main.go", working: "+first session" });
    await pending;
    expect(store.activeRepositoryDiff?.working).toBe("+second session");

    store.selectThread(firstId);
    expect(store.inspectorOpen).toBe(true);
    expect(store.activeRepositoryDiff?.working).toBe("+first session");
    await store.openRepositoryFilePreview("main.go", 19);
    store.setInspectorTab("context");
    store.selectThread(secondId);
    expect(store.inspectorTab).toBe("changes");
    expect(store.activeRepositoryFilePreviewPath).toBe("");
    expect(store.activeRepositoryFileTabs).toEqual([]);
    expect(store.activeRepositoryDiff?.working).toBe("+second session");
    store.toggleInspector();

    store.selectThread(firstId);
    expect(store.inspectorOpen).toBe(true);
    expect(store.inspectorTab).toBe("changes");
    expect(store.contextOpen).toBe(false);
    expect(store.activeRepositoryFilePreviewPath).toBe("main.go");
    expect(store.activeRepositoryFilePreview?.content).toBe("package main");
    expect(store.activeRepositoryFilePreviewLine).toBe(19);
    expect(store.activeRepositoryFileTabs).toEqual(["main.go"]);
    store.selectThread(secondId);
    expect(store.inspectorOpen).toBe(false);
  });

  it("drops a late Repository diff after the same file is reopened", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    let finish!: (value: { path: string; staged: string; working: string; content: string; binary: boolean; truncated: boolean }) => void;
    mocks.diffRepository.mockReturnValueOnce(new Promise((resolve) => { finish = resolve; }));

    const first = store.openRepositoryDiff("same.go");
    await vi.waitFor(() => expect(mocks.diffRepository).toHaveBeenCalled());
    store.closeRepositoryDiff();
    mocks.diffRepository.mockResolvedValueOnce({ path: "same.go", staged: "", working: "+new", content: "", binary: false, truncated: false });
    await store.openRepositoryDiff("same.go");
    finish({ path: "same.go", staged: "", working: "+old", content: "", binary: false, truncated: false });
    await first;

    expect(store.activeRepositoryDiffPath).toBe("same.go");
    expect(store.activeRepositoryDiff?.working).toBe("+new");
  });

  it("drops a late Repository preview after the same file is reopened", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    let finish!: (value: { path: string; absolutePath: string; content: string; size: number; binary: boolean; truncated: boolean }) => void;
    mocks.previewRepositoryFile.mockReturnValueOnce(new Promise((resolve) => { finish = resolve; }));

    const first = store.openRepositoryFilePreview("same.go");
    await vi.waitFor(() => expect(mocks.previewRepositoryFile).toHaveBeenCalled());
    store.closeRepositoryFilePreview();
    mocks.previewRepositoryFile.mockResolvedValueOnce({ path: "same.go", absolutePath: "D:\\work\\repo\\same.go", content: "new", size: 3, binary: false, truncated: false });
    await store.openRepositoryFilePreview("same.go");
    finish({ path: "same.go", absolutePath: "D:\\work\\repo\\same.go", content: "old", size: 3, binary: false, truncated: false });
    await first;

    expect(store.activeRepositoryFilePreviewPath).toBe("same.go");
    expect(store.activeRepositoryFilePreview?.content).toBe("new");
  });

  it("loads and opens a changed file through the trusted repository service", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    store.inspectorOpen = false;
    store.inspectorTab = "context";
    store.openPanelTab({ id: "readme", kind: "file", title: "README.md", path: "README.md", preview: { path: "README.md", absolutePath: "D:\\work\\repo\\README.md", content: "old", size: 3, binary: false, truncated: false } });

    await store.openRepositoryDiff("main.go");
    expect(mocks.diffRepository).toHaveBeenCalledWith({ workspaceId: "workspace-1" }, "main.go");
    expect(store.activeRepositoryDiff?.working).toBe("+change");
    expect(store.inspectorOpen).toBe(true);
    expect(store.inspectorTab).toBe("changes");
    expect(store.activeRepositoryFilePreviewPath).toBe("");
    expect(store.activeRepositoryFilePreview).toBeUndefined();

    await store.openActiveRepositoryFile();
    await store.openActiveRepositoryFile(true);
    expect(mocks.openRepositoryFile).toHaveBeenCalledWith("D:\\work\\repo", "main.go");
    expect(mocks.revealRepositoryFile).toHaveBeenCalledWith("D:\\work\\repo", "main.go");
  });

  it("opens a conversation change from the recorded session diff", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");

    await store.openRepositoryDiff("main.go", "@@ -1 +1 @@\n-old\n+new");

    expect(mocks.diffRepository).not.toHaveBeenCalled();
    expect(store.activeRepositoryDiff).toMatchObject({
      path: "main.go", working: "@@ -1 +1 @@\n-old\n+new", session: true,
    });
    expect(store.activeRepositoryDiffLoading).toBe(false);
  });

  it("opens a linked file in its own selected inspector tab", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    store.inspectorTab = "context";

    await store.openRepositoryFilePreview("main.go", 19);

    expect(mocks.previewRepositoryFile).toHaveBeenCalledWith({ workspaceId: "workspace-1" }, "main.go");
    expect(store.activeRepositoryFilePreview).toMatchObject({ absolutePath: "D:\\work\\repo\\main.go", content: "package main" });
    expect(store.activeRepositoryFilePreviewLine).toBe(19);
    expect(store.inspectorTab).toBe("changes");
    expect(store.contextOpen).toBe(false);
    expect(store.inspectorOpen).toBe(true);
    await store.openPreviewedRepositoryFile();
    await store.openPreviewedRepositoryFile(true);
    expect(mocks.openRepositoryFile).toHaveBeenCalledWith("D:\\work\\repo", "main.go");
    expect(mocks.revealRepositoryFile).toHaveBeenCalledWith("D:\\work\\repo", "main.go");

    store.closeRepositoryFilePreview();
    expect(store.activeRepositoryFilePreview).toBeUndefined();
  });

  it("loads persisted workspaces and discovers historical sessions", async () => {
    mocks.listWorkspaces.mockResolvedValueOnce([{
      id: "workspace-1", name: "repo", path: "D:\\work\\repo", trust: "approve",
      addedAt: "2026-08-01T08:00:00Z", lastOpenedAt: "2026-08-10T08:00:00Z",
    }]);
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-1", path: "C:\\sessions\\one.jsonl", cwd: "D:\\work\\repo", title: "Runtime audit",
      firstMessage: "Inspect runtime", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z",
      messageCount: 3,
    }]);
    const store = useAppStore();

    await store.initialize();

    expect(store.workspaces).toHaveLength(1);
    expect(store.threads[0]).toMatchObject({
      id: "session-session-1", title: "Runtime audit", trust: "approve", sessionFile: "C:\\sessions\\one.jsonl", messageCount: 3,
    });
    expect(store.activeThreadId).toBe("session-session-1");
  });

  it("keeps discovered workspace identities out of restored desktop state", async () => {
    mocks.listSessions.mockResolvedValueOnce([{
      id: "outside", path: "C:\\sessions\\outside.jsonl", cwd: "D:\\outside", title: "History",
      createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z", messageCount: 1,
    }]);
    mocks.getDesktopState.mockResolvedValueOnce({ threads: [{
      id: "session-outside", title: "History", workspacePath: "D:\\outside",
      sessionPath: "C:\\sessions\\outside.jsonl", trust: "deny", status: "idle",
    }] });
    const store = useAppStore();
    await store.loadCatalog();
    expect(store.threads[0].workspaceId).toBeUndefined();
    await store.persistDesktopState();
    expect(mocks.saveDesktopState.mock.lastCall?.[0].threads[0]).toMatchObject({
      workspaceId: undefined, workspacePath: "D:\\outside", sessionPath: "C:\\sessions\\outside.jsonl",
    });
  });

  it("restores an SSH anchor transcript by immutable WorkspaceID", async () => {
    mocks.listWorkspaces.mockResolvedValueOnce([{
      id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote",
      remoteRoot: "/srv/repo", trust: "approve", addedAt: "2026-08-01T08:00:00Z", lastOpenedAt: "2026-08-10T08:00:00Z",
    }]);
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-remote", path: "C:\\sessions\\remote.jsonl", cwd: "C:\\anchors\\workspace-remote",
      anchorWorkspaceId: "workspace-remote", title: "Remote audit", firstMessage: "Inspect remote",
      createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z", messageCount: 2,
    }]);
    const store = useAppStore();

    await store.initialize();

    expect(store.workspaces).toHaveLength(1);
    expect(store.threads[0]).toMatchObject({
      id: "session-session-remote", workspaceId: "workspace-remote", workspacePath: "", sessionFile: "C:\\sessions\\remote.jsonl",
    });
    expect(mocks.resumeRemoteWorkspace).not.toHaveBeenCalled();
    expect(mocks.startSession).not.toHaveBeenCalled();
  });

  it("does not rebind an unknown persisted WorkspaceID by an empty remote path", async () => {
    mocks.listWorkspaces.mockResolvedValueOnce([
      { id: "workspace-a", name: "A", path: "", kind: "ssh", targetId: "target-a", remoteRoot: "/a", trust: "approve" },
      { id: "workspace-b", name: "B", path: "", kind: "ssh", targetId: "target-b", remoteRoot: "/b", trust: "approve" },
    ]);
    mocks.getDesktopState.mockResolvedValueOnce({ threads: [{
      id: "thread-stale", title: "Stale", workspaceId: "workspace-missing", workspacePath: "",
      trust: "approve", status: "idle",
    }] });
    const store = useAppStore();

    await store.initialize();

    expect(store.threads).toEqual([]);
    expect(store.activeThreadId).toBe("");
  });

  it("does not rename a persisted session while starting its Pi process", async () => {
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-1", path: "C:\\sessions\\one.jsonl", cwd: "D:\\work\\repo", title: "Runtime audit",
      firstMessage: "Inspect runtime", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z",
      messageCount: 3,
    }]);
    const store = useAppStore();
    await store.initialize();

    await store.ensureSession(store.activeThread!);

    expect(mocks.startSession).toHaveBeenCalledWith(expect.objectContaining({
      threadId: "session-session-1", sessionPath: "C:\\sessions\\one.jsonl",
    }));
    expect(mocks.startSession.mock.calls[0][0]).not.toHaveProperty("sessionName");
  });

  it("resumes a historical session and reconstructs tool output", async () => {
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-1", path: "C:\\sessions\\one.jsonl", cwd: "D:\\work\\repo", title: "Runtime audit",
      firstMessage: "Inspect runtime", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z",
      messageCount: 3,
    }]);
    mocks.getSessionSnapshot.mockResolvedValueOnce({
      model: { provider: "openai", id: "gpt-5" },
      messages: [
      { role: "user", content: "Inspect runtime", timestamp: 1786348800000 },
      { role: "piDeskCompaction", summary: "## Context\n\nRepository inspection is complete.", tokensBefore: 241443, timestamp: "2026-08-10T08:00:30Z", piDeskEntryId: "compact-1" },
      { role: "assistant", content: [
        { type: "thinking", thinking: "Check files" },
        { type: "text", text: "I checked it." },
        { type: "toolCall", id: "tool-1", name: "read", arguments: { path: "main.go" } },
        { type: "toolCall", id: "tool-pending", name: "edit", arguments: { path: "pending.go", oldText: "old", newText: "new" } },
      ], timestamp: 1786348860000 },
      { role: "toolResult", toolCallId: "tool-1", content: [{ type: "text", text: "package main" }], isError: false },
      ],
    });
    const store = useAppStore();
    await store.initialize();

    await store.loadThreadTranscript("session-session-1");

    expect(mocks.getSessionSnapshot).toHaveBeenCalledWith("C:\\sessions\\one.jsonl");
    expect(mocks.startSession).not.toHaveBeenCalled();
    expect(store.activeMessages).toHaveLength(3);
    expect(store.activeMessages[1]).toMatchObject({
      id: "history-compaction-compact-1", role: "system", timestamp: expect.any(String),
      compaction: { summary: "## Context\n\nRepository inspection is complete.", tokensBefore: 241443 },
    });
    expect(store.activeMessages[2]).toMatchObject({ role: "assistant", text: "I checked it.", thinking: "Check files" });
    expect(store.activeMessages[2].tools[0]).toMatchObject({ id: "tool-1", output: "package main", resultReceived: true, status: "complete" });
    expect(store.activeMessages[2].tools[1]).toMatchObject({ id: "tool-pending", status: "complete" });
    expect(store.activeMessages[2].tools[1].resultReceived).toBeUndefined();
    expect(store.activeSessionState?.model).toEqual({ provider: "openai", id: "gpt-5" });
    expect(store.transcriptStateByThread["session-session-1"]).toBe("loaded");
  });

  it("loads transcript without pagination", async () => {
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-1", path: "C:\\sessions\\one.jsonl", cwd: "D:\\work\\repo", title: "Runtime audit",
      firstMessage: "Old prompt", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z",
      messageCount: 3,
    }]);
    mocks.getSessionSnapshot.mockResolvedValueOnce({
      messages: [
        { role: "user", content: "Old prompt", piDeskEntryId: "entry-1" },
        { role: "assistant", content: "Old reply", piDeskEntryId: "entry-2" },
        { role: "user", content: "Recent prompt", piDeskEntryId: "entry-3" },
      ],
      messageCount: 4,
    });
    const store = useAppStore();
    await store.initialize();
    await store.loadThreadTranscript("session-session-1");
    expect(store.activeMessages.map((message) => message.text)).toEqual(["Old prompt", "Old reply", "Recent prompt"]);
    expect(store.activeThread?.messageCount).toBe(4);
  });

  it("loads history before sending from a resumed session", async () => {
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-1", path: "C:\\sessions\\one.jsonl", cwd: "D:\\work\\repo", title: "Runtime audit",
      firstMessage: "Old prompt", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z", messageCount: 1,
    }]);
    mocks.getSessionSnapshot.mockResolvedValueOnce({ messages: [{ role: "user", content: "Old prompt", timestamp: 1786348800000 }] });
    const store = useAppStore();
    await store.initialize();
    store.updateDraft("Continue the audit");

    await store.sendActivePrompt();

    expect(store.activeMessages.map((message) => message.text)).toEqual(["Old prompt", "Continue the audit"]);
    expect(mocks.sendPrompt).toHaveBeenCalledWith(expect.objectContaining({ message: "Continue the audit" }));
  });

  it("restores persisted drafts and converts an interrupted run to attention", async () => {
    mocks.listWorkspaces.mockResolvedValueOnce([{
      id: "workspace-1", name: "repo", path: "D:\\work\\repo", trust: "deny",
    }]);
    mocks.getDesktopState.mockResolvedValueOnce({
      activeThreadId: "local-1",
      threads: [{
        id: "local-1", title: "Unsent task", workspacePath: "D:\\work\\repo", trust: "deny", status: "running",
        draft: "recover this draft", createdAt: "2026-08-10T08:00:00Z", updatedAt: "2026-08-10T09:00:00Z",
      }],
    });
    const store = useAppStore();

    await store.initialize();

    expect(store.activeThread).toMatchObject({ id: "local-1", status: "attention", error: "Previous Pi run was interrupted" });
    expect(store.activeDraft).toBe("recover this draft");

    await store.persistDesktopState();
    expect(mocks.saveDesktopState).toHaveBeenCalledWith(expect.objectContaining({
      activeThreadId: "local-1",
      threads: [expect.objectContaining({ id: "local-1", draft: "recover this draft", status: "attention" })],
    }));
  });

  it("keeps the original task when cloning switches Pi to a new session", async () => {
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-1", path: "C:\\sessions\\one.jsonl", cwd: "D:\\work\\repo", title: "Runtime audit",
      firstMessage: "Inspect runtime", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z", messageCount: 1,
    }]);
    mocks.getState.mockResolvedValueOnce({
      sessionId: "session-2", sessionFile: "C:\\sessions\\two.jsonl", isStreaming: false,
    });
    mocks.getMessages.mockResolvedValueOnce({ messages: [{ role: "user", content: "Inspect runtime", timestamp: 1786348800000 }] });
    const store = useAppStore();
    await store.initialize();

    await store.cloneActiveSession();

    expect(mocks.cloneSession).toHaveBeenCalledWith("session-session-1");
    expect(store.activeThread).toMatchObject({ sessionId: "session-2", sessionFile: "C:\\sessions\\two.jsonl", title: "Runtime audit (copy)", started: true });
    expect(store.threads).toHaveLength(2);
    expect(store.threads[1]).toMatchObject({ sessionId: "session-1", sessionFile: "C:\\sessions\\one.jsonl", started: false });
    expect(mocks.setSessionName).toHaveBeenCalledWith({ threadId: "session-session-1", name: "Runtime audit (copy)" });
  });

  it("restarts a missing Pi process before loading session branches", async () => {
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-1", path: "C:\\sessions\\one.jsonl", cwd: "D:\\work\\repo", title: "Runtime audit",
      firstMessage: "Inspect runtime", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z", messageCount: 1,
    }]);
    mocks.getSessionBranches
      .mockRejectedValueOnce(new Error("Pi thread is not running"))
      .mockResolvedValueOnce({ entries: [{ id: "entry-1", type: "message", parentId: "", timestamp: "", role: "user", text: "Inspect runtime", label: "" }], leafId: "entry-1" });
    const store = useAppStore();
    await store.initialize();
    store.activeThread!.started = true;

    await store.openBranchPanel();

    expect(mocks.startSession).toHaveBeenCalledOnce();
    expect(mocks.getSessionBranches).toHaveBeenCalledTimes(2);
    expect(store.branchPanelOpen).toBe(true);
    expect(store.activeSessionBranches?.leafId).toBe("entry-1");
  });

  it("keeps branch loading failures in the warning dialog instead of the transcript", async () => {
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-1", path: "C:\\sessions\\one.jsonl", cwd: "D:\\work\\repo", title: "Runtime audit",
      firstMessage: "Inspect runtime", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z", messageCount: 1,
    }]);
    mocks.getSessionBranches.mockRejectedValueOnce(new Error("Maximum call stack size exceeded"));
    const store = useAppStore();
    await store.initialize();
    const messageCount = store.activeMessages.length;

    await store.openBranchPanel();

    expect(store.branchPanelOpen).toBe(true);
    expect(store.activeSessionBranchesError).toBe("Maximum call stack size exceeded");
    expect(store.activeMessages).toHaveLength(messageCount);
  });

  it("forks from the session branch panel before a user entry", async () => {
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-1", path: "C:\\sessions\\one.jsonl", cwd: "D:\\work\\repo", title: "Runtime audit",
      firstMessage: "Inspect runtime", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z", messageCount: 1,
    }]);
    mocks.forkSessionAt.mockResolvedValueOnce({ cancelled: false, text: "Inspect runtime" });
    mocks.getState.mockResolvedValueOnce({ sessionId: "session-2", sessionFile: "C:\\sessions\\fork.jsonl", isStreaming: false });
    const store = useAppStore();
    await store.initialize();
    const thread = store.activeThread!;
    thread.started = true;
    await store.forkActiveSession("entry-1");

    expect(mocks.forkSessionAt).toHaveBeenCalledWith({
      threadId: "session-session-1", path: "C:\\sessions\\one.jsonl", entryId: "entry-1", before: true,
    });
    expect(store.activeDraft).toBe("Inspect runtime");
  });

  it("restores an expanded skill invocation as an executable command when forking", async () => {
    const expanded = `<skill name="grill-me" location="C:\\Users\\yanq\\.agents\\skills\\grill-me\\SKILL.md">\nReferences are relative to C:\\Users\\yanq\\.agents\\skills\\grill-me.\n\nRun a \`/grilling\` session.\n</skill>\n\nReview the image plan.`;
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-1", path: "C:\\sessions\\one.jsonl", cwd: "D:\\work\\repo", title: "Runtime audit",
      firstMessage: "Review the image plan", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z", messageCount: 1,
    }]);
    mocks.forkSessionAt.mockResolvedValueOnce({ cancelled: false, text: expanded });
    mocks.getState.mockResolvedValueOnce({ sessionId: "session-2", sessionFile: "C:\\sessions\\fork.jsonl", isStreaming: false });
    const store = useAppStore();
    await store.initialize();
    store.activeThread!.started = true;

    await store.forkActiveSession("entry-1");

    expect(store.activeDraft).toBe("/skill:grill-me Review the image plan.");
  });

  it("forks from the Pi entry matching a user message and prefills that prompt", async () => {
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-1", path: "C:\\sessions\\one.jsonl", cwd: "D:\\work\\repo", title: "Runtime audit",
      firstMessage: "Inspect runtime", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z", messageCount: 1,
    }]);
    mocks.getSessionSnapshot.mockResolvedValueOnce({ messages: [
      { role: "user", content: "Inspect runtime", timestamp: 1786348800000, piDeskEntryId: "entry-1" },
      { role: "user", content: "Inspect runtime", timestamp: 1786348801000, piDeskEntryId: "entry-2" },
    ] });
    mocks.getSessionSnapshot.mockResolvedValueOnce({ messages: [] });
    mocks.forkSessionAt.mockResolvedValueOnce({ cancelled: false, text: "Inspect runtime" });
    mocks.getState.mockResolvedValueOnce({ sessionId: "session-2", sessionFile: "C:\\sessions\\fork.jsonl", isStreaming: false });
    const store = useAppStore();
    await store.initialize();
    await store.loadThreadTranscript(store.activeThreadId);
    const userMessage = store.activeMessages[0];

    await store.forkFromMessage(userMessage.id);

    expect(mocks.forkSessionAt).toHaveBeenCalledWith({
      threadId: "session-session-1", path: "C:\\sessions\\one.jsonl", entryId: "entry-1", before: true,
    });
    expect(store.activeThread?.sessionFile).toBe("C:\\sessions\\fork.jsonl");
    expect(store.activeDraft).toBe("Inspect runtime");
  });

  it("blocks prompts during history operations without consuming the draft", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    store.updateDraft("Keep this message");
    store.sessionOperationByThread[thread.id] = "Deleting message";
    await store.sendActivePrompt();
    expect(await store.deliverPrompt(thread, "No race", [])).toBe(false);
    expect(mocks.sendPrompt).not.toHaveBeenCalled();
    expect(store.activeDraft).toBe("Keep this message");
    expect(store.activeMessages).toHaveLength(0);
  });

  it.each(["message", "branch-panel"])("forks an image-only prompt from %s without stealing the original draft", async (source) => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    thread.started = true;
    thread.sessionFile = "C:\\sessions\\one.jsonl";
    const originalImage = { id: "draft-image", name: "Draft", data: "ZHJhZnQ=", mimeType: "image/png", previewUrl: "data:image/png;base64,ZHJhZnQ=" };
    store.draftsByThread[thread.id] = "Unsent original draft";
    store.attachmentsByThread[thread.id] = [originalImage];
    store.messagesByThread[thread.id] = [{ id: "photo", entryId: "photo-entry", role: "user", text: "", thinking: "", timestamp: "", streaming: false, tools: [] }];
    mocks.forkSessionAt.mockResolvedValueOnce({ cancelled: false, text: "", images: [{ type: "image", data: "aW1hZ2U=", mimeType: "image/png" }] });
    mocks.getState.mockResolvedValueOnce({ sessionId: "fork", sessionFile: "C:\\sessions\\fork.jsonl" });
    mocks.getSessionSnapshot.mockResolvedValueOnce({ messages: [] });
    if (source === "message") expect(await store.forkFromMessage("photo")).toBe(true);
    else await store.forkActiveSession("photo-entry", "Do not insert fallback text for an image-only prompt");
    expect(store.activeDraft).toBe("");
    expect(store.activeAttachments).toHaveLength(1);
    expect(store.activeAttachments[0].data).toBe("aW1hZ2U=");
    const original = store.threads.find((candidate) => candidate.sessionFile === "C:\\sessions\\one.jsonl")!;
    expect(store.draftsByThread[original.id]).toBe("Unsent original draft");
    expect(store.attachmentsByThread[original.id]).toEqual([originalImage]);
  });

  it("binds the confirmed fork even when the subsequent state query fails", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    thread.started = true;
    thread.sessionFile = "C:\\sessions\\one.jsonl";
    store.messagesByThread[thread.id] = [{ id: "user", entryId: "entry", role: "user", text: "Prompt", thinking: "", timestamp: "", streaming: false, tools: [] }];
    mocks.forkSessionAt.mockResolvedValueOnce({ cancelled: false, text: "Prompt", sessionFile: "C:\\sessions\\fork.jsonl", sessionId: "fork-id" });
    mocks.getState.mockRejectedValueOnce(new Error("state read timed out"));
    mocks.getSessionSnapshot.mockResolvedValueOnce({ messages: [] });
    expect(await store.forkFromMessage("user")).toBe(true);
    expect(thread.sessionFile).toBe("C:\\sessions\\fork.jsonl");
    expect(thread.sessionId).toBe("fork-id");
    expect(store.activeMessages).toHaveLength(0);
    expect(store.activeDraft).toBe("Prompt");
  });

  it("ignores a pre-fork state response from the same Pi generation", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    thread.started = true;
    thread.sessionFile = "C:\\sessions\\original.jsonl";
    let finish!: (value: unknown) => void;
    mocks.getState.mockReturnValueOnce(new Promise((resolve) => { finish = resolve; }));
    const pending = store.refreshState(thread.id);
    thread.sessionFile = "C:\\sessions\\fork.jsonl";
    store.sessionStateByThread[thread.id] = { sessionId: "fork", sessionFile: thread.sessionFile };
    finish({ sessionId: "original", sessionFile: "C:\\sessions\\original.jsonl" });
    await pending;
    expect(store.activeSessionState?.sessionId).toBe("fork");
  });

  it("does not keep original message IDs when loading a new fork fails", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    thread.started = true;
    thread.sessionFile = "C:\\sessions\\one.jsonl";
    store.messagesByThread[thread.id] = [{ id: "latest", entryId: "entry", role: "user", text: "Continue", thinking: "", timestamp: "", streaming: false, tools: [] }];
    mocks.forkSessionAt.mockResolvedValueOnce({ cancelled: false, text: "Continue" });
    mocks.getState.mockResolvedValueOnce({ sessionId: "fork", sessionFile: "C:\\sessions\\fork.jsonl" });
    mocks.getSessionSnapshot.mockRejectedValueOnce(new Error("snapshot failed"));
    expect(await store.forkFromMessage("latest")).toBe(false);
    expect(thread.sessionFile).toBe("C:\\sessions\\fork.jsonl");
    expect(store.activeMessages.some((message) => message.entryId === "entry")).toBe(false);
    expect(store.transcriptStateByThread[thread.id]).toBe("error");
    expect(store.activeDraft).toBe("Continue");
  });

  it("keeps a replay draft on its original task when navigating away", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    thread.started = true;
    thread.sessionFile = "C:\\sessions\\one.jsonl";
    store.messagesByThread[thread.id] = [{ id: "latest", entryId: "entry", role: "user", text: "Continue", thinking: "", timestamp: "", streaming: false, tools: [] }];
    let finish!: () => void;
    mocks.replaySessionMessage.mockReturnValueOnce(new Promise<void>((resolve) => { finish = resolve; }));
    mocks.getSessionSnapshot.mockResolvedValueOnce({ messages: [] });
    const pending = store.resendEditedMessage("latest", "Recover this edit");
    await vi.waitFor(() => expect(mocks.replaySessionMessage).toHaveBeenCalled());
    store.activeThreadId = "another-task";
    finish();
    expect(await pending).toBe(false);
    expect(store.draftsByThread[thread.id]).toBe("Recover this edit");
    expect(mocks.sendPrompt).not.toHaveBeenCalled();
  });

  it("rejects an old initial history load after a session replacement", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    thread.sessionFile = "C:\\sessions\\one.jsonl";
    store.transcriptStateByThread[thread.id] = "idle";
    let finish!: (value: unknown) => void;
    mocks.getSessionSnapshot.mockReturnValueOnce(new Promise((resolve) => { finish = resolve; }));
    const pending = store.loadThreadTranscript(thread.id);
    thread.sessionFile = "C:\\sessions\\fork.jsonl";
    store.invalidateSessionReads(thread.id);
    finish({ messages: [{ role: "user", content: "Old branch", piDeskEntryId: "old-entry" }] });
    await pending;
    expect(store.activeMessages.some((message) => message.entryId === "old-entry")).toBe(false);
  });

  it("never repeats a mutation with an unknown outcome, even if Pi is no longer running", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    thread.started = true;
    thread.sessionFile = "C:\\sessions\\one.jsonl";
    const operation = vi.fn().mockRejectedValue(new Error("outcome-unknown: thread is not running"));
    await expect(store.callWithSession(thread, operation)).rejects.toThrow("outcome-unknown");
    expect(operation).toHaveBeenCalledTimes(1);
    expect(mocks.startSession).not.toHaveBeenCalled();
    expect(thread.started).toBe(false);
    expect(store.transcriptStateByThread[thread.id]).toBe("error");
  });

  it("does not reload intermediate history or dispatch queued prompts during a mutation", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    thread.sessionFile = "C:\\sessions\\one.jsonl";
    store.sessionOperationByThread[thread.id] = "Deleting message";
    store.pendingPromptsByThread[thread.id] = [{ id: "queued", text: "Keep queued", images: [], createdAt: "" }];
    await store.reloadSessionTranscript(thread);
    await store.dispatchNextPendingPrompt(thread.id);
    expect(mocks.getSessionSnapshot).not.toHaveBeenCalled();
    expect(mocks.sendPrompt).not.toHaveBeenCalled();
    expect(store.pendingPromptsByThread[thread.id]).toHaveLength(1);
  });

  it("replays the latest user message in the existing session", async () => {
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-1", path: "C:\\sessions\\one.jsonl", cwd: "D:\\work\\repo", title: "Runtime audit",
      firstMessage: "Inspect runtime", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z", messageCount: 2,
    }]);
    mocks.getSessionSnapshot
      .mockResolvedValueOnce({ messages: [
        { role: "user", content: "Inspect runtime", piDeskEntryId: "entry-1" },
        { role: "assistant", content: "Old response", piDeskEntryId: "entry-2" },
      ] })
      .mockResolvedValueOnce({ messages: [] });
    mocks.replaySessionMessage.mockResolvedValueOnce({});
    const store = useAppStore();
    await store.initialize();
    await store.loadThreadTranscript(store.activeThreadId);
    const userMessage = store.activeMessages[0];

    expect(await store.resendEditedMessage(userMessage.id, "Inspect the updated runtime")).toBe(true);

    expect(mocks.replaySessionMessage).toHaveBeenCalledWith({
      threadId: "session-session-1", path: "C:\\sessions\\one.jsonl", entryId: "entry-1",
    });
    expect(mocks.forkSessionAt).not.toHaveBeenCalled();
    expect(store.activeThread?.sessionFile).toBe("C:\\sessions\\one.jsonl");
    expect(store.threads).toHaveLength(1);
    expect(mocks.sendPrompt).toHaveBeenCalledWith({
      threadId: "session-session-1", message: "Inspect the updated runtime", streamingBehavior: undefined,
    });
    expect(store.activeMessages.at(-1)?.text).toBe("Inspect the updated runtime");
    expect(store.activeDraft).toBe("");
  });

  it("restores the active model before resending an edited latest message", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    thread.started = true;
    thread.generation = 1;
    thread.sessionFile = "C:\\sessions\\one.jsonl";
    const model = { provider: "custom", id: "gpt-5.6", name: "GPT 5.6" };
    store.sessionStateByThread[thread.id] = { model };
    store.transcriptStateByThread[thread.id] = "loaded";
    store.messagesByThread[thread.id] = [{ id: "latest", entryId: "entry", role: "user", text: "Continue", thinking: "", timestamp: "", streaming: false, tools: [] }];
    mocks.replaySessionMessage.mockResolvedValueOnce({});
    mocks.getSessionSnapshot.mockResolvedValueOnce({ messages: [] });
    mocks.getState.mockResolvedValueOnce({ model });

    expect(await store.resendEditedMessage("latest", "Continue again")).toBe(true);

    expect(mocks.setModel).toHaveBeenCalledWith({ threadId: thread.id, provider: model.provider, modelId: model.id });
    expect(mocks.setModel.mock.invocationCallOrder[0]).toBeLessThan(mocks.sendPrompt.mock.invocationCallOrder[0]);
    expect(mocks.sendPrompt).toHaveBeenCalledWith(expect.objectContaining({ message: "Continue again" }));
  });

  it("reports a failed resend after rewind and keeps the edited draft for recovery", async () => {
    const store = useAppStore();
    await store.createThread("D:\\work\\repo", "approve");
    const thread = store.activeThread!;
    thread.started = true;
    thread.sessionFile = "C:\\sessions\\one.jsonl";
    store.messagesByThread[thread.id] = [{ id: "latest", entryId: "entry", role: "user", text: "Continue", thinking: "", timestamp: "", streaming: false, tools: [] }];
    mocks.replaySessionMessage.mockResolvedValueOnce({});
    mocks.getSessionSnapshot.mockResolvedValueOnce({ messages: [] });
    mocks.sendPrompt.mockRejectedValueOnce(new Error("send failed"));
    expect(await store.resendEditedMessage("latest", "Continue again")).toBe(false);
    expect(store.activeDraft).toBe("Continue again");
    expect(thread.error).toBe("send failed");
    expect(store.activeMessages.some((message) => message.error === "send failed")).toBe(true);
    expect(mocks.forkSessionAt).not.toHaveBeenCalled();
  });

  it("forks after an assistant entry and keeps the composer empty", async () => {
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-1", path: "C:\\sessions\\one.jsonl", cwd: "D:\\work\\repo", title: "Runtime audit",
      firstMessage: "Inspect runtime", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z", messageCount: 2,
    }]);
    mocks.getSessionSnapshot
      .mockResolvedValueOnce({ messages: [
        { role: "user", content: "Inspect runtime", piDeskEntryId: "entry-1" },
        { role: "assistant", content: "Done", piDeskEntryId: "entry-2" },
      ] })
      .mockResolvedValueOnce({ messages: [
        { role: "user", content: "Inspect runtime", piDeskEntryId: "entry-1" },
        { role: "assistant", content: "Done", piDeskEntryId: "entry-2" },
      ] });
    mocks.forkSessionAt.mockResolvedValueOnce({ cancelled: false, text: "Inspect runtime" });
    mocks.getState.mockResolvedValueOnce({ sessionId: "session-2", sessionFile: "C:\\sessions\\fork.jsonl", isStreaming: false });
    const store = useAppStore();
    await store.initialize();
    await store.loadThreadTranscript(store.activeThreadId);

    await store.forkFromMessage(store.activeMessages[1].id);

    expect(mocks.forkSessionAt).toHaveBeenCalledWith({
      threadId: "session-session-1", path: "C:\\sessions\\one.jsonl", entryId: "entry-2", before: false,
    });
    expect(store.activeDraft).toBe("");
  });

  it("edits and deletes persisted messages before reloading their local snapshot", async () => {
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-1", path: "C:\\sessions\\one.jsonl", cwd: "D:\\work\\repo", title: "Runtime audit",
      firstMessage: "Inspect runtime", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z", messageCount: 2,
    }]);
    mocks.getSessionSnapshot
      .mockResolvedValueOnce({ messages: [
        { role: "user", content: "Before", piDeskEntryId: "entry-1" },
        { role: "assistant", content: "Reply", piDeskEntryId: "entry-2" },
      ] })
      .mockResolvedValueOnce({ messages: [
        { role: "user", content: "After", piDeskEntryId: "entry-1" },
        { role: "assistant", content: "Reply", piDeskEntryId: "entry-2" },
      ] })
      .mockResolvedValueOnce({ messages: [
        { role: "user", content: "After", piDeskEntryId: "entry-1" },
      ] });
    mocks.editSessionMessage.mockResolvedValueOnce({});
    mocks.deleteSessionMessage.mockResolvedValueOnce({});
    const store = useAppStore();
    await store.initialize();
    await store.loadThreadTranscript(store.activeThreadId);

    expect(await store.editMessage(store.activeMessages[0].id, "After")).toBe(true);
    expect(mocks.editSessionMessage).toHaveBeenCalledWith({
      threadId: "session-session-1", path: "C:\\sessions\\one.jsonl", entryId: "entry-1", text: "After",
    });
    expect(store.activeMessages[0].text).toBe("After");

    expect(await store.deleteMessage(store.activeMessages[1].id)).toBe(true);
    expect(mocks.deleteSessionMessage).toHaveBeenCalledWith({
      threadId: "session-session-1", path: "C:\\sessions\\one.jsonl", entryId: "entry-2",
    });
    expect(store.activeMessages).toHaveLength(1);
  });

  it("stops Pi and removes a deleted task while retaining its recovery path", async () => {
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-1", path: "C:\\sessions\\one.jsonl", cwd: "D:\\work\\repo", title: "Runtime audit",
      firstMessage: "Inspect runtime", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z", messageCount: 1,
    }]);
    const store = useAppStore();
    await store.initialize();
    const thread = store.activeThread!;
    thread.started = true;
    store.requestDeleteActiveSession();

    await store.confirmDeleteSession();

    expect(mocks.stopSession).toHaveBeenCalledWith(thread.id);
    expect(mocks.deleteSession).toHaveBeenCalledWith("C:\\sessions\\one.jsonl");
    expect(store.threads).toHaveLength(0);
    expect(store.deletedRecoveryPath).toBe("C:\\sessions\\one.jsonl.deleted-test");
    expect(store.deleteDialogOpen).toBe(true);
  });

  it("does not move a session when its Pi process cannot be stopped", async () => {
    mocks.listSessions.mockResolvedValueOnce([{
      id: "session-1", path: "C:\\sessions\\one.jsonl", cwd: "D:\\work\\repo", title: "Runtime audit",
      firstMessage: "Inspect runtime", createdAt: "2026-08-10T08:00:00Z", modifiedAt: "2026-08-10T09:00:00Z", messageCount: 1,
    }]);
    mocks.stopSession.mockRejectedValueOnce(new Error("process busy"));
    const store = useAppStore();
    await store.initialize();
    store.activeThread!.started = true;
    store.requestDeleteActiveSession();

    await store.confirmDeleteSession();

    expect(mocks.deleteSession).not.toHaveBeenCalled();
    expect(store.threads).toHaveLength(1);
    expect(store.deleteSessionError).toBe("process busy");
  });

  it("runs a due one-time task in the background and keeps the scheduled-task page open", async () => {
    const store = useAppStore();
    store.$patch({
      catalogReady: true,
      activePage: "scheduledTasks",
      workspaces: [{ id: "workspace-1", name: "repo", path: "D:\\work\\repo", trust: "approve" }],
      scheduledTasks: [{
        id: "schedule-1",
        name: "Dependency review",
        prompt: "Review dependency updates.",
        workspaceId: "workspace-1",
        modelProvider: "openai",
        modelId: "gpt-5.6",
        modelName: "GPT 5.6",
        thinkingLevel: "medium",
        frequency: "once",
        runAt: "2026-09-03T01:00:00.000Z",
        enabled: true,
        nextRunAt: "2026-09-03T01:00:00.000Z",
        createdAt: "2026-09-02T01:00:00.000Z",
        updatedAt: "2026-09-02T01:00:00.000Z",
      }],
    });
    mocks.getState
      .mockResolvedValueOnce({ sessionId: "session-1", model: { provider: "openai", id: "gpt-5.6", name: "GPT 5.6" }, isStreaming: false })
      .mockResolvedValueOnce({ sessionId: "session-1", model: { provider: "openai", id: "gpt-5.6", name: "GPT 5.6" }, thinkingLevel: "medium", isStreaming: false });

    await store.checkScheduledTasks(new Date("2026-09-03T02:00:00.000Z"));

    expect(mocks.setModel).toHaveBeenCalledWith(expect.objectContaining({ provider: "openai", modelId: "gpt-5.6" }));
    expect(mocks.setThinkingLevel).toHaveBeenCalledWith(expect.objectContaining({ level: "medium" }));
    expect(mocks.setModel.mock.invocationCallOrder[0]).toBeLessThan(mocks.setThinkingLevel.mock.invocationCallOrder[0]);
    expect(mocks.setThinkingLevel.mock.invocationCallOrder[0]).toBeLessThan(mocks.sendPrompt.mock.invocationCallOrder[0]);
    expect(mocks.sendPrompt).toHaveBeenCalledWith(expect.objectContaining({ message: "Review dependency updates." }));
    expect(store.threads[0].title).toBe("Dependency review");
    expect(store.activePage).toBe("scheduledTasks");
    expect(store.scheduledTasks[0]).toMatchObject({ enabled: false, lastStatus: "started", nextRunAt: undefined });
  });

  it("persists scheduled tasks with desktop state", async () => {
    const store = useAppStore();
    store.catalogReady = true;
    store.workspaces = [{ id: "workspace-1", name: "repo", path: "D:\\work\\repo", trust: "approve" }];
    store.configuredModels = [{ provider: "openai", id: "gpt-5.6", name: "GPT 5.6" }];
    store.saveScheduledTask({
      name: "Daily review",
      prompt: "Review open changes.",
      workspaceId: "workspace-1",
      modelProvider: "openai",
      modelId: "gpt-5.6",
      modelName: "GPT 5.6",
      thinkingLevel: "high",
      frequency: "daily",
      time: "09:00",
      weekday: 1,
      runAt: "",
    });

    await store.persistDesktopState();

    expect(mocks.saveDesktopState).toHaveBeenCalledWith(expect.objectContaining({
      scheduledTasks: [expect.objectContaining({ name: "Daily review", frequency: "daily", modelProvider: "openai", modelId: "gpt-5.6", thinkingLevel: "high" })],
    }));
  });

  it("does not send a scheduled prompt when the saved thinking level is unavailable", async () => {
    const store = useAppStore();
    store.$patch({
      workspaces: [{ id: "workspace-1", name: "repo", path: "D:\\work\\repo", trust: "approve" }],
      scheduledTasks: [{
        id: "schedule-unsupported", name: "Deep review", prompt: "Review deeply.", workspaceId: "workspace-1",
        modelProvider: "openai", modelId: "gpt-5.6", thinkingLevel: "max", frequency: "daily", time: "09:00",
        enabled: true, nextRunAt: "2026-09-04T01:00:00.000Z", createdAt: "2026-09-03T01:00:00.000Z", updatedAt: "2026-09-03T01:00:00.000Z",
      }],
    });
    mocks.getAvailableThinkingLevels.mockResolvedValueOnce({ levels: ["off", "medium"] });
    mocks.getState.mockResolvedValueOnce({ model: { provider: "openai", id: "gpt-5.6" }, isStreaming: false });

    await store.runScheduledTask("schedule-unsupported");

    expect(mocks.setThinkingLevel).not.toHaveBeenCalled();
    expect(mocks.sendPrompt).not.toHaveBeenCalled();
    expect(store.scheduledTasks[0]).toMatchObject({ lastStatus: "failed" });
    expect(store.scheduledTasks[0].lastError).toContain("max");
  });
  it("loads session file changes when refreshing the repository", async () => {
    const store = useAppStore();
    store.threads = [{ id: "t1", title: "Task", workspace: "repo", workspacePath: "D:\\work\\repo", trust: "approve", status: "idle", started: false, generation: 0, sessionFile: "D:\\sessions\\repo\\a.jsonl" }];
    store.activeThreadId = "t1";
    mocks.snapshotRepository.mockResolvedValue({ files: [], git: { isRepository: true, files: [] } });
    mocks.sessionFileChanges.mockResolvedValue([{ path: "src/main.go", editCalls: 2, writeCalls: 0, plan: "revert-edits" }]);

    await store.refreshActiveRepository();

    expect(mocks.sessionFileChanges).toHaveBeenCalledWith("D:\\work\\repo", "D:\\sessions\\repo\\a.jsonl");
    expect(store.activeSessionChanges).toEqual([{ path: "src/main.go", editCalls: 2, writeCalls: 0, plan: "revert-edits" }]);
    expect(store.activeSessionChangesError).toBe("");
  });

  it("rolls back a session file and refreshes the workspace", async () => {
    const store = useAppStore();
    store.threads = [{ id: "t1", title: "Task", workspace: "repo", workspacePath: "D:\\work\\repo", trust: "approve", status: "idle", started: false, generation: 0, sessionFile: "D:\\sessions\\repo\\a.jsonl" }];
    store.activeThreadId = "t1";
    mocks.rollbackSessionFile.mockResolvedValue(undefined);
    mocks.snapshotRepository.mockResolvedValue({ files: [], git: { isRepository: true, files: [] } });
    mocks.sessionFileChanges.mockResolvedValue([]);

    await store.rollbackSessionFile("src/main.go");

    expect(mocks.rollbackSessionFile).toHaveBeenCalledWith("D:\\work\\repo", "D:\\sessions\\repo\\a.jsonl", "src/main.go");
    expect(store.activeSessionChangesError).toBe("");
    expect(mocks.snapshotRepository).toHaveBeenCalled();
  });

  it("surfaces rollback failures without refreshing", async () => {
    const store = useAppStore();
    store.threads = [{ id: "t1", title: "Task", workspace: "repo", workspacePath: "D:\\work\\repo", trust: "approve", status: "idle", started: false, generation: 0, sessionFile: "D:\\sessions\\repo\\a.jsonl" }];
    store.activeThreadId = "t1";
    mocks.rollbackSessionFile.mockRejectedValueOnce(new Error("recorded change no longer matches the file"));

    await store.rollbackSessionFile("src/main.go");

    expect(store.activeSessionChangesError).toContain("recorded change no longer matches");
    expect(mocks.snapshotRepository).not.toHaveBeenCalled();
  });

  it("skips session file changes for remote workspaces", async () => {
    const store = useAppStore();
    store.workspaces = [{ id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }];
    store.threads = [{ id: "t2", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0, sessionFile: "/tmp/a.jsonl" }];
    store.activeThreadId = "t2";
    store.sessionChangesByThread["t2"] = [{ path: "x", editCalls: 1, writeCalls: 0, plan: "revert-edits" }];

    await store.refreshSessionChanges("t2");

    expect(mocks.sessionFileChanges).not.toHaveBeenCalled();
    expect(store.sessionChangesByThread["t2"]).toBeUndefined();
  });
});
