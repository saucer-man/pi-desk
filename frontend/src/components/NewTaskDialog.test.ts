import { mount, type VueWrapper } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "../stores/app";
import NewTaskDialog from "./NewTaskDialog.vue";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));
const remoteWorkspaceService = vi.hoisted(() => ({
  discover: vi.fn(), connectNew: vi.fn(),
  prepareRoot: vi.fn(), decideRoot: vi.fn(), disconnect: vi.fn(),
}));
vi.mock("../services/remoteWorkspaces", () => ({ remoteWorkspaceService }));

async function chooseRemoteAlias(wrapper: VueWrapper) {
  await wrapper.get("#remote-alias").trigger("click");
  await wrapper.get('[role="option"]').trigger("click");
}

describe("NewTaskDialog", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
    remoteWorkspaceService.discover.mockResolvedValue([{ name: "work", risky: false }]);
    remoteWorkspaceService.disconnect.mockResolvedValue(undefined);
  });

  it("always exposes the SSH workspace entry", () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.newTaskOpen = true;
    const wrapper = mount(NewTaskDialog, { global: { plugins: [pinia] } });

    expect(wrapper.findAll(".segmented-control button")).toHaveLength(2);
    expect(wrapper.get("#workspace-path").classes()).toContain("h-full");
    expect(wrapper.get("#workspace-path").classes()).not.toContain("min-h-11");
    expect(wrapper.get('.path-input .icon-button').classes()).not.toContain("pointer-coarse:size-11");
  });

  it("reports SSH alias discovery failures", async () => {
    remoteWorkspaceService.discover.mockRejectedValue(new Error("SSH config discovery failed"));
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.newTaskOpen = true;
    const wrapper = mount(NewTaskDialog, { global: { plugins: [pinia] } });

    await wrapper.findAll(".segmented-control button")[1].trigger("click");

    await vi.waitFor(() => expect(wrapper.text()).toContain("SSH config discovery failed"));
  });

  it("defaults a new workspace to full access", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.newTaskOpen = true;
    store.createThread = vi.fn().mockImplementation(async () => { store.activeThreadId = "thread-default"; });
    store.startThreadInBackground = vi.fn();
    const wrapper = mount(NewTaskDialog, { global: { plugins: [pinia] } });
    await wrapper.get("#workspace-path").setValue("D:\\work\\new-repo");

    expect((wrapper.get('input[value="approve"]').element as HTMLInputElement).checked).toBe(true);
    await wrapper.get("footer .primary").trigger("click");

    await vi.waitFor(() => expect(store.createThread).toHaveBeenCalledWith("D:\\work\\new-repo", "approve"));
  });

  it("preserves the saved access mode when opening an existing workspace", () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      newTaskOpen: true,
      workspaces: [{ id: "workspace-1", name: "repo", path: "D:\\work\\repo", trust: "deny" }],
      bootstrap: { workingDirectory: "D:\\work\\repo" },
    });

    const wrapper = mount(NewTaskDialog, { global: { plugins: [pinia] } });

    expect((wrapper.get('input[value="deny"]').element as HTMLInputElement).checked).toBe(true);
  });

  it("uses the native folder result and submits the selected trust mode", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.newTaskOpen = true;
    store.pickWorkspace = vi.fn().mockResolvedValue("D:\\work\\selected");
    store.createThread = vi.fn().mockImplementation(async () => { store.activeThreadId = "thread-new"; });
    store.startThreadInBackground = vi.fn();
    const wrapper = mount(NewTaskDialog, { global: { plugins: [pinia] } });

    await wrapper.get('button[title="Browse folders"]').trigger("click");
    await vi.waitFor(() => expect(wrapper.get("#workspace-path").element).toHaveProperty("value", "D:\\work\\selected"));
    await wrapper.get('input[value="approve"]').setValue(true);
    await wrapper.get("footer .primary").trigger("click");

    await vi.waitFor(() => expect(store.createThread).toHaveBeenCalledWith("D:\\work\\selected", "approve"));
    expect(store.startThreadInBackground).toHaveBeenCalledWith("thread-new");
  });

  it("requires a separate root identity approval before creating an SSH task", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.newTaskOpen = true;
    store.createRemoteThread = vi.fn().mockImplementation(async () => { store.activeThreadId = "thread-remote"; });
    store.refreshConfiguredModels = vi.fn().mockResolvedValue(undefined);
    store.startThreadInBackground = vi.fn();
    remoteWorkspaceService.connectNew.mockResolvedValue("target-1");
    remoteWorkspaceService.prepareRoot.mockResolvedValue({
      token: "a".repeat(64), targetId: "target-1", hostAlias: "work",
      hostKeyAlgorithm: "ssh-ed25519", hostKeySha256: "SHA256:test", canonicalRoot: "/srv/repo", device: "1", inode: "2",
    });
    remoteWorkspaceService.decideRoot.mockResolvedValue({
      id: "workspace-1", name: "repo", path: "", kind: "ssh", targetId: "target-1",
      remoteRoot: "/srv/repo", trust: "approve", addedAt: "", lastOpenedAt: "",
    });
    const wrapper = mount(NewTaskDialog, { global: { plugins: [pinia] } });

    await vi.waitFor(() => expect(wrapper.findAll(".segmented-control button")).toHaveLength(2));
    await wrapper.findAll(".segmented-control button")[1].trigger("click");
    await chooseRemoteAlias(wrapper);
    await wrapper.get("#remote-name").setValue("repo");
    await wrapper.get("#remote-root").setValue("/srv/repo");
    await wrapper.get("footer .primary").trigger("click");

    await vi.waitFor(() => expect(wrapper.text()).toContain("/srv/repo"));
    expect(store.createRemoteThread).not.toHaveBeenCalled();
    await wrapper.get("footer .primary").trigger("click");
    await vi.waitFor(() => expect(store.createRemoteThread).toHaveBeenCalled());
    expect(remoteWorkspaceService.connectNew).toHaveBeenCalledWith("repo", "work");
    expect(remoteWorkspaceService.decideRoot).toHaveBeenCalledWith("a".repeat(64), "approve");
    expect(store.startThreadInBackground).toHaveBeenCalledWith("thread-remote");
  });

  it("disconnects a target when root preparation fails after connect", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.newTaskOpen = true;
    const disconnect = vi.spyOn(store, "disconnectRemoteTarget");
    remoteWorkspaceService.connectNew.mockResolvedValue("target-1");
    remoteWorkspaceService.prepareRoot.mockRejectedValue(new Error("root probe failed"));
    const wrapper = mount(NewTaskDialog, { global: { plugins: [pinia] } });

    await wrapper.findAll(".segmented-control button")[1].trigger("click");
    await chooseRemoteAlias(wrapper);
    await wrapper.get("#remote-name").setValue("repo");
    await wrapper.get("#remote-root").setValue("/srv/repo");
    await wrapper.get("footer .primary").trigger("click");

    await vi.waitFor(() => expect(wrapper.text()).toContain("root probe failed"));
    expect(disconnect).toHaveBeenCalledWith("target-1");
    expect(remoteWorkspaceService.disconnect).toHaveBeenCalledWith("target-1");
  });

  it("does not persist root approval when model refresh fails", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.newTaskOpen = true;
    store.refreshConfiguredModels = vi.fn().mockRejectedValue(new Error("Model catalog is unavailable"));
    remoteWorkspaceService.connectNew.mockResolvedValue("target-1");
    remoteWorkspaceService.prepareRoot.mockResolvedValue({
      token: "c".repeat(64), targetId: "target-1", hostAlias: "work",
      hostKeyAlgorithm: "ssh-ed25519", hostKeySha256: "SHA256:test", canonicalRoot: "/srv/repo", device: "1", inode: "2",
    });
    const wrapper = mount(NewTaskDialog, { global: { plugins: [pinia] } });

    await wrapper.findAll(".segmented-control button")[1].trigger("click");
    await chooseRemoteAlias(wrapper);
    await wrapper.get("#remote-name").setValue("repo");
    await wrapper.get("#remote-root").setValue("/srv/repo");
    await wrapper.get("footer .primary").trigger("click");
    await vi.waitFor(() => expect(wrapper.text()).toContain("/srv/repo"));
    await wrapper.get("footer .primary").trigger("click");

    await vi.waitFor(() => expect(wrapper.text()).toContain("Model catalog is unavailable"));
    expect(remoteWorkspaceService.decideRoot).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("/srv/repo");
  });

  it("clears an expired root candidate when approval fails", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.newTaskOpen = true;
    store.refreshConfiguredModels = vi.fn().mockResolvedValue(undefined);
    remoteWorkspaceService.connectNew.mockResolvedValue("target-1");
    remoteWorkspaceService.prepareRoot.mockResolvedValue({
      token: "d".repeat(64), targetId: "target-1", hostAlias: "work",
      hostKeyAlgorithm: "ssh-ed25519", hostKeySha256: "SHA256:test", canonicalRoot: "/srv/repo", device: "1", inode: "2",
    });
    remoteWorkspaceService.decideRoot.mockRejectedValue(new Error("remote root trust candidate expired"));
    const wrapper = mount(NewTaskDialog, { global: { plugins: [pinia] } });

    await wrapper.findAll(".segmented-control button")[1].trigger("click");
    await chooseRemoteAlias(wrapper);
    await wrapper.get("#remote-name").setValue("repo");
    await wrapper.get("#remote-root").setValue("/srv/repo");
    await wrapper.get("footer .primary").trigger("click");
    await vi.waitFor(() => expect(wrapper.text()).toContain("SHA256:test"));
    await wrapper.get("footer .primary").trigger("click");

    await vi.waitFor(() => expect(wrapper.text()).toContain("remote root trust candidate expired"));
    expect(remoteWorkspaceService.disconnect).toHaveBeenCalledWith("target-1");
    expect(wrapper.text()).not.toContain("SHA256:test");
  });

  it("disconnects an approved target when creating its desktop thread fails", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.newTaskOpen = true;
    store.refreshConfiguredModels = vi.fn().mockResolvedValue(undefined);
    store.createRemoteThread = vi.fn().mockRejectedValue(new Error("desktop thread failed"));
    remoteWorkspaceService.connectNew.mockResolvedValue("target-1");
    remoteWorkspaceService.prepareRoot.mockResolvedValue({
      token: "e".repeat(64), targetId: "target-1", hostAlias: "work",
      hostKeyAlgorithm: "ssh-ed25519", hostKeySha256: "SHA256:test", canonicalRoot: "/srv/repo", device: "1", inode: "2",
    });
    remoteWorkspaceService.decideRoot.mockResolvedValue({
      id: "workspace-1", name: "repo", path: "", kind: "ssh", targetId: "target-1",
      remoteRoot: "/srv/repo", trust: "approve", addedAt: "", lastOpenedAt: "",
    });
    const wrapper = mount(NewTaskDialog, { global: { plugins: [pinia] } });

    await wrapper.findAll(".segmented-control button")[1].trigger("click");
    await chooseRemoteAlias(wrapper);
    await wrapper.get("#remote-name").setValue("repo");
    await wrapper.get("#remote-root").setValue("/srv/repo");
    await wrapper.get("footer .primary").trigger("click");
    await vi.waitFor(() => expect(wrapper.text()).toContain("SHA256:test"));
    await wrapper.get("footer .primary").trigger("click");

    await vi.waitFor(() => expect(wrapper.text()).toContain("desktop thread failed"));
    expect(remoteWorkspaceService.decideRoot).toHaveBeenCalledWith("e".repeat(64), "approve");
    expect(remoteWorkspaceService.disconnect).toHaveBeenCalledWith("target-1");
    expect(wrapper.text()).not.toContain("SHA256:test");
  });

  it("keeps connecting a new target when an unrelated target becomes stale", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.newTaskOpen = true;
    let finishConnect!: (targetID: string) => void;
    remoteWorkspaceService.connectNew.mockReturnValueOnce(new Promise((resolve) => { finishConnect = resolve; }));
    remoteWorkspaceService.prepareRoot.mockResolvedValue({
      token: "0".repeat(64), targetId: "target-1", hostAlias: "work",
      hostKeyAlgorithm: "ssh-ed25519", hostKeySha256: "SHA256:test", canonicalRoot: "/srv/repo", device: "1", inode: "2",
    });
    const wrapper = mount(NewTaskDialog, { global: { plugins: [pinia] } });

    await wrapper.findAll(".segmented-control button")[1].trigger("click");
    await chooseRemoteAlias(wrapper);
    await wrapper.get("#remote-name").setValue("repo");
    await wrapper.get("#remote-root").setValue("/srv/repo");
    await wrapper.get("footer .primary").trigger("click");
    await vi.waitFor(() => expect(remoteWorkspaceService.connectNew).toHaveBeenCalled());
    store.markRemoteTargetStale("target-2");
    finishConnect("target-1");

    await vi.waitFor(() => expect(wrapper.text()).toContain("SHA256:test"));
    expect(remoteWorkspaceService.prepareRoot).toHaveBeenCalledWith("target-1", "repo", "/srv/repo");
    expect(remoteWorkspaceService.disconnect).not.toHaveBeenCalled();
  });

  it("disconnects a prepared root response after its target becomes stale", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.newTaskOpen = true;
    remoteWorkspaceService.connectNew.mockResolvedValue("target-1");
    let finishPrepare!: (value: Record<string, unknown>) => void;
    remoteWorkspaceService.prepareRoot.mockReturnValueOnce(new Promise((resolve) => { finishPrepare = resolve; }));
    const wrapper = mount(NewTaskDialog, { global: { plugins: [pinia] } });

    await wrapper.findAll(".segmented-control button")[1].trigger("click");
    await chooseRemoteAlias(wrapper);
    await wrapper.get("#remote-name").setValue("repo");
    await wrapper.get("#remote-root").setValue("/srv/repo");
    await wrapper.get("footer .primary").trigger("click");
    await vi.waitFor(() => expect(remoteWorkspaceService.prepareRoot).toHaveBeenCalled());
    store.markRemoteTargetStale("target-1");
    finishPrepare({
      token: "f".repeat(64), targetId: "target-1", hostAlias: "work",
      hostKeyAlgorithm: "ssh-ed25519", hostKeySha256: "SHA256:test", canonicalRoot: "/srv/repo", device: "1", inode: "2",
    });

    await vi.waitFor(() => expect(wrapper.text()).toContain("REMOTE_DISCONNECTED"));
    expect(wrapper.text()).not.toContain("SHA256:test");
    expect(remoteWorkspaceService.disconnect).toHaveBeenCalledWith("target-1");
  });

  it("revokes sibling tasks and records an explicit remote root rejection without reloading the catalog", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      newTaskOpen: true,
      workspaces: [{ id: "workspace-existing", name: "Existing", path: "", kind: "ssh", targetId: "target-1", remoteRoot: "/srv/existing", trust: "approve" }],
      threads: [{
        id: "thread-running", title: "Running", workspace: "Existing", workspaceId: "workspace-existing", workspacePath: "", trust: "approve",
        status: "running", started: true, generation: 7,
      }],
      remoteReadyByWorkspace: { "workspace-existing": true },
    });
    store.stopRemoteTargetThreads = vi.fn().mockImplementation(async (targetID: string) => {
      store.threads[0].started = false;
      store.threads[0].status = "idle";
      store.markRemoteTargetStale(targetID);
      return "";
    });
    remoteWorkspaceService.connectNew.mockResolvedValue("target-1");
    remoteWorkspaceService.prepareRoot.mockResolvedValue({
      token: "b".repeat(64), targetId: "target-1", hostAlias: "work",
      hostKeyAlgorithm: "ssh-ed25519", hostKeySha256: "SHA256:test", canonicalRoot: "/srv/repo", device: "1", inode: "2",
    });
    remoteWorkspaceService.decideRoot.mockResolvedValue({
      id: "workspace-1", name: "repo", path: "", kind: "ssh", targetId: "target-1",
      remoteRoot: "/srv/repo", trust: "deny", addedAt: "", lastOpenedAt: "",
    });
    const wrapper = mount(NewTaskDialog, { global: { plugins: [pinia] } });

    await vi.waitFor(() => expect(wrapper.findAll(".segmented-control button")).toHaveLength(2));
    await wrapper.findAll(".segmented-control button")[1].trigger("click");
    await chooseRemoteAlias(wrapper);
    await wrapper.get("#remote-name").setValue("repo");
    await wrapper.get("#remote-root").setValue("/srv/repo");
    await wrapper.get("footer .primary").trigger("click");
    await vi.waitFor(() => expect(wrapper.text()).toContain("/srv/repo"));
    await wrapper.get("footer .danger-button").trigger("click");

    await vi.waitFor(() => expect(remoteWorkspaceService.decideRoot).toHaveBeenCalledWith("b".repeat(64), "deny"));
    expect(store.stopRemoteTargetThreads).toHaveBeenCalledWith("target-1");
    expect(store.threads[0]).toMatchObject({ id: "thread-running", started: false, status: "idle", generation: 7 });
    expect(store.workspaces.find((workspace) => workspace.id === "workspace-1")).toMatchObject({ kind: "ssh", trust: "deny" });
    expect(store.remoteReadyByWorkspace).toMatchObject({ "workspace-existing": false, "workspace-1": false });
      expect(store.newTaskOpen).toBe(false);
  });

  it("keeps the dialog open and renders backend validation errors", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.newTaskOpen = true;
    store.createThread = vi.fn().mockRejectedValue(new Error("Workspace path is not a directory"));
    const wrapper = mount(NewTaskDialog, { global: { plugins: [pinia] } });
    await wrapper.get("#workspace-path").setValue("D:\\missing");

    await wrapper.get("footer .primary").trigger("click");

    await vi.waitFor(() => expect(wrapper.text()).toContain("Workspace path is not a directory"));
    expect(store.newTaskOpen).toBe(true);
  });
});
