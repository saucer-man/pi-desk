import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "../stores/app";
import AppTopbar from "./AppTopbar.vue";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

describe("AppTopbar", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
  });

  it("opens the current folder from a split button and lists only discovered applications", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-1", title: "Audit", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: false, generation: 0, sessionFile: "one.jsonl",
      }],
      activeThreadId: "thread-1",
      workspaceApplications: [
        { id: "vscode", name: "Visual Studio Code", iconDataUrl: "data:image/png;base64,dnNjb2Rl" },
        { id: "file-manager", name: "File Explorer", iconDataUrl: "data:image/png;base64,ZmlsZXM=" },
      ],
      workspaceApplicationsLoading: false,
      workspaceApplication: "file-manager",
    });
    store.openActiveWorkspaceWith = vi.fn().mockResolvedValue(true);
    const wrapper = mount(AppTopbar, { global: { plugins: [pinia] } });

    const primaryIcon = wrapper.get<HTMLImageElement>(".workspace-application-primary img.workspace-application-icon-primary");
    expect(primaryIcon.attributes("src")).toBe("data:image/png;base64,ZmlsZXM=");
    await wrapper.get(".workspace-application-primary").trigger("click");
    expect(store.openActiveWorkspaceWith).toHaveBeenCalledWith("");

    await wrapper.get(".workspace-application-toggle").trigger("click");
    const applicationItems = wrapper.findAll('[role="menuitemradio"]');
    expect(applicationItems.map((item) => item.text())).toEqual(["Visual Studio Code", "File Explorer"]);
    expect(applicationItems.map((item) => item.get("img").attributes("src"))).toEqual([
      "data:image/png;base64,dnNjb2Rl", "data:image/png;base64,ZmlsZXM=",
    ]);
    expect(applicationItems[1].classes()).toContain("is-selected");
    await applicationItems[0].trigger("click");
    expect(store.openActiveWorkspaceWith).toHaveBeenLastCalledWith("vscode");
  });

  it("waits for real application icons without rendering a placeholder control", () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-1", title: "Audit", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: false, generation: 0,
      }],
      activeThreadId: "thread-1",
      workspaceApplications: [],
      workspaceApplicationsLoading: true,
    });
    const wrapper = mount(AppTopbar, { global: { plugins: [pinia] } });

    expect(wrapper.find(".workspace-application-anchor").exists()).toBe(false);
  });

  it("disables external application opening for an untrusted workspace", () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-1", title: "Audit", workspace: "repo", workspacePath: "D:\\repo", trust: "deny",
        status: "idle", started: false, generation: 0,
      }],
      activeThreadId: "thread-1",
      workspaceApplications: [{ id: "file-manager", name: "File Explorer", iconDataUrl: "data:image/png;base64,ZmlsZXM=" }],
      workspaceApplicationsLoading: false,
    });
    const wrapper = mount(AppTopbar, { global: { plugins: [pinia] } });

    expect(wrapper.get(".workspace-application-primary").attributes("disabled")).toBeDefined();
    expect(wrapper.get(".workspace-application-toggle").attributes("disabled")).toBeDefined();
  });

  it("keeps destructive task lifecycle actions out of the topbar menu", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-1", title: "Audit", workspace: "repo", workspacePath: "D:\\repo", trust: "deny",
        status: "idle", started: false, generation: 0, sessionFile: "one.jsonl",
      }],
      activeThreadId: "thread-1",
    });
    const wrapper = mount(AppTopbar, { global: { plugins: [pinia] } });

    await wrapper.get('button[title="Task actions"]').trigger("click");
    expect(wrapper.get('[role="menu"]').text()).not.toContain("Archive task");
    expect(wrapper.get('[role="menu"]').text()).not.toContain("Delete task");
  });

  it("opens branches and runs clone and export task actions", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-1", title: "Audit", workspace: "repo", workspacePath: "D:\\repo", trust: "deny",
        status: "idle", started: true, generation: 1, sessionFile: "one.jsonl",
      }],
      activeThreadId: "thread-1",
    });
    store.openBranchPanel = vi.fn().mockResolvedValue(undefined);
    store.cloneActiveSession = vi.fn().mockResolvedValue(undefined);
    store.exportActiveSession = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(AppTopbar, { global: { plugins: [pinia] } });

    await wrapper.get('button[title="Session branches"]').trigger("click");
    expect(store.openBranchPanel).toHaveBeenCalledOnce();

    await wrapper.get('button[title="Task actions"]').trigger("click");
    const clone = wrapper.findAll('[role="menuitem"]').find((item) => item.text().includes("Clone task"));
    if (!clone) throw new Error("clone action not found");
    await clone.trigger("click");
    expect(store.cloneActiveSession).toHaveBeenCalledOnce();

    await wrapper.get('button[title="Task actions"]').trigger("click");
    const exportAction = wrapper.findAll('[role="menuitem"]').find((item) => item.text().includes("Export HTML"));
    if (!exportAction) throw new Error("export action not found");
    await exportAction.trigger("click");
    expect(store.exportActiveSession).toHaveBeenCalledOnce();

  });

  it("exposes keyboard-dismissible task actions and rename dialog", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-1", title: "Audit", workspace: "repo", workspacePath: "D:\\repo", trust: "deny",
        status: "idle", started: false, generation: 0, sessionFile: "one.jsonl",
      }],
      activeThreadId: "thread-1",
    });
    const wrapper = mount(AppTopbar, { attachTo: document.body, global: { plugins: [pinia] } });
    const menuButton = wrapper.get('button[title="Task actions"]');
    await menuButton.trigger("click");
    await wrapper.get('[role="menu"]').trigger("keydown", { key: "Escape" });
    expect(wrapper.find('[role="menu"]').exists()).toBe(false);
    expect(document.activeElement).toBe(menuButton.element);

    await menuButton.trigger("click");
    await wrapper.get('[role="menuitem"]').trigger("click");
    const renameDialog = wrapper.get('[role="dialog"][aria-label="Rename task"]');
    await renameDialog.trigger("keydown", { key: "Escape" });
    expect(wrapper.find('[role="dialog"][aria-label="Rename task"]').exists()).toBe(false);
    wrapper.unmount();
  });
});
