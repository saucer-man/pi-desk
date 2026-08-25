import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { RuntimeState } from "../../bindings/pi-desk/internal/domain";
import { useAppStore } from "../stores/app";
import AppSidebar from "./AppSidebar.vue";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

describe("AppSidebar", () => {

  it("renders only the expand action while the sidebar is collapsed", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.sidebarCollapsed = true;
    const wrapper = mount(AppSidebar, { global: { plugins: [pinia] } });

    expect(wrapper.findAll("button")).toHaveLength(1);
    expect(wrapper.find('button[aria-label="Open task search"]').exists()).toBe(false);
    expect(wrapper.find('button[aria-label="Review changes"]').exists()).toBe(false);
    expect(wrapper.find(".sidebar-section").exists()).toBe(false);
    expect(wrapper.find(".sidebar-footer").exists()).toBe(false);

    await wrapper.get('button[aria-label="Expand sidebar"]').trigger("click");

    expect(store.sidebarCollapsed).toBe(false);
    expect(wrapper.find('button[aria-label="Open task search"]').exists()).toBe(true);
  });
  it("keeps the expand action in the collapsed sidebar", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    const wrapper = mount(AppSidebar, { global: { plugins: [pinia] } });

    expect(wrapper.find(".sidebar-expand-button").exists()).toBe(false);
    expect(wrapper.get(".primary-nav-row").findAll("button")).toHaveLength(1);
    store.sidebarCollapsed = true;
    await wrapper.vm.$nextTick();

    const expandButton = wrapper.get(".sidebar-expand-button");
    expect(expandButton.attributes("aria-label")).toBe("Expand sidebar");
    await expandButton.trigger("click");
    expect(store.sidebarCollapsed).toBe(false);
    expect(wrapper.find(".sidebar-expand-button").exists()).toBe(false);
  });

  beforeEach(() => {
    setActivePinia(createPinia());
  });

  it("keeps the global new task action prominent and labels the current Pi version", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      bootstrap: {
        appVersion: "0.1.0",
        wailsVersion: "v3.0.0-beta.6",
        workingDirectory: "D:\\repo",
        runtime: { state: RuntimeState.RuntimeReady, version: "0.83.0", command: "pi.cmd" },
      },
      bootstrapLoading: false,
      runtimeCheckLoading: false,
    });

    const wrapper = mount(AppSidebar, { global: { plugins: [pinia] } });

    const newTask = wrapper.get(".new-task-button");
    expect(newTask.text()).toContain("New task");
    await newTask.trigger("click");
    expect(store.newTaskOpen).toBe(true);
    expect(wrapper.get(".runtime-badge").text()).toContain("Current Pi version 0.83.0");
  });

  it("groups tasks by workspace without counters or visible collapse controls", () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    const now = new Date().toISOString();
    store.$patch({
      catalogLoading: false,
      workspaces: [{ id: "workspace-1", name: "pi-desk", path: "D:\\repo", trust: "deny" }],
      threads: [{
        id: "thread-1", title: "Inspect runtime", workspace: "pi-desk", workspacePath: "D:\\repo", trust: "deny",
        status: "idle", started: false, generation: 0, modifiedAt: now,
      }],
    });
    const wrapper = mount(AppSidebar, { global: { plugins: [pinia] } });

    expect(wrapper.find(".sidebar-header").exists()).toBe(false);
    expect(wrapper.find(".brand-mark").exists()).toBe(false);
    expect(wrapper.text()).not.toContain("Today");
    expect(wrapper.find(".date-heading").exists()).toBe(false);
    expect(wrapper.text()).toContain("Inspect runtime");
    expect(wrapper.get(".workspace-header .workspace-row").find("small").exists()).toBe(false);
    expect(wrapper.get(".workspace-header .workspace-row").find(".row-tail").exists()).toBe(false);
  });

  it("groups remote tasks by WorkspaceID instead of their empty local paths", () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      catalogLoading: false,
      workspaces: [
        { id: "workspace-a", name: "remote A", path: "", kind: "ssh", targetId: "target-a", remoteRoot: "/srv/a", trust: "approve" },
        { id: "workspace-b", name: "remote B", path: "", kind: "ssh", targetId: "target-b", remoteRoot: "/srv/b", trust: "approve" },
      ],
      threads: [
        { id: "thread-a", title: "Task A", workspace: "remote A", workspaceId: "workspace-a", workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0 },
        { id: "thread-b", title: "Task B", workspace: "remote B", workspaceId: "workspace-b", workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0 },
      ],
    });
    const wrapper = mount(AppSidebar, { global: { plugins: [pinia] } });
    const groups = wrapper.findAll(".workspace-group");

    expect(groups[0].text()).toContain("Task A");
    expect(groups[0].text()).not.toContain("Task B");
    expect(groups[1].text()).toContain("Task B");
  });

  it("orders workspace tasks by their latest activity and reacts to new replies", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      catalogLoading: false,
      workspaces: [{ id: "workspace-1", name: "pi-desk", path: "D:\\repo", trust: "deny" }],
      threads: [
        {
          id: "thread-old", title: "Old task", workspace: "pi-desk", workspacePath: "D:\\repo", trust: "deny",
          status: "idle", started: false, generation: 0, modifiedAt: "2026-08-14T08:00:00Z",
        },
        {
          id: "thread-new", title: "Latest reply", workspace: "pi-desk", workspacePath: "D:\\repo", trust: "deny",
          status: "idle", started: false, generation: 0, modifiedAt: "2026-08-15T08:00:00Z",
        },
        {
          id: "thread-middle", title: "Middle task", workspace: "pi-desk", workspacePath: "D:\\repo", trust: "deny",
          status: "idle", started: false, generation: 0, modifiedAt: "2026-08-14T12:00:00Z",
        },
      ],
    });
    const wrapper = mount(AppSidebar, { global: { plugins: [pinia] } });

    expect(wrapper.findAll(".thread-title").map((item) => item.text())).toEqual([
      "Latest reply", "Middle task", "Old task",
    ]);

    store.threads[0].modifiedAt = "2026-08-16T08:00:00Z";
    await wrapper.vm.$nextTick();
    expect(wrapper.findAll(".thread-title").map((item) => item.text())).toEqual([
      "Old task", "Latest reply", "Middle task",
    ]);
  });

  it("does not show sessions unavailable after a non-catalog error", () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      catalogLoading: false,
      catalogReady: true,
      catalogError: "",
      settingsError: "state file is locked",
      workspaces: [{ id: "workspace-1", name: "pi-desk", path: "D:\\repo", trust: "deny" }],
      threads: [{
        id: "thread-1", title: "Inspect runtime", workspace: "pi-desk", workspacePath: "D:\\repo", trust: "deny",
        status: "idle", started: false, generation: 0,
      }],
    });

    const wrapper = mount(AppSidebar, { global: { plugins: [pinia] } });

    expect(wrapper.text()).not.toContain("Sessions unavailable");
    expect(wrapper.text()).toContain("Inspect runtime");
  });

  it("toggles a workspace task list by clicking its name", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    const now = new Date().toISOString();
    store.$patch({
      catalogLoading: false,
      workspaces: [{ id: "workspace-1", name: "pi-desk", path: "D:\\repo", trust: "deny" }],
      threads: [{
        id: "thread-1", title: "Inspect runtime", workspace: "pi-desk", workspacePath: "D:\\repo", trust: "deny",
        status: "idle", started: false, generation: 0, modifiedAt: now,
      }],
    });
    const wrapper = mount(AppSidebar, { global: { plugins: [pinia] } });
    const workspace = wrapper.get(".workspace-header .workspace-row");

    expect(workspace.classes()).not.toContain("is-active");
    expect(workspace.attributes("aria-expanded")).toBe("true");
    expect(wrapper.find(".workspace-threads").exists()).toBe(true);
    await workspace.trigger("click");
    expect(workspace.attributes("aria-expanded")).toBe("false");
    expect(wrapper.find(".workspace-threads").exists()).toBe(false);
    await workspace.trigger("click");
    expect(workspace.attributes("aria-expanded")).toBe("true");
    expect(wrapper.find(".workspace-threads").exists()).toBe(true);
  });

  it("creates a new task from the first workspace menu action", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      catalogLoading: false,
      workspaces: [{ id: "workspace-1", name: "pi-desk", path: "D:\\\\repo", trust: "approve" }],
    });
    store.createThread = vi.fn().mockImplementation(async () => { store.activeThreadId = "thread-new"; });
    store.startThreadInBackground = vi.fn();
    const wrapper = mount(AppSidebar, { global: { plugins: [pinia] } });

    expect(wrapper.find(".workspace-new-task").exists()).toBe(false);
    await wrapper.get('button[aria-label="Actions for pi-desk"]').trigger("click", { clientX: 20, clientY: 20 });
    const menuItems = wrapper.get(".workspace-context-menu").findAll('[role="menuitem"]');
    expect(menuItems[0].text()).toContain("New task");
    await menuItems[0].trigger("click");

    expect(store.createThread).toHaveBeenCalledWith("D:\\\\repo", "approve");
    expect(store.startThreadInBackground).toHaveBeenCalledWith("thread-new");
  });

  it("separates the Pi process, model output, and unread task states", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      catalogLoading: false,
      workspaces: [{ id: "workspace-1", name: "pi-desk", path: "D:\\repo", trust: "deny" }],
      threads: [{
        id: "thread-1", title: "Active runtime", workspace: "pi-desk", workspacePath: "D:\\repo", trust: "deny",
        status: "idle", started: true, generation: 2, modifiedAt: new Date().toISOString(), unread: false,
      }],
    });

    const wrapper = mount(AppSidebar, { global: { plugins: [pinia] } });
    const title = wrapper.get(".thread-title");

    expect(title.classes()).toContain("is-started");
    expect(wrapper.find(".thread-status").exists()).toBe(false);
    expect(wrapper.find(".thread-unread").exists()).toBe(false);

    store.threads[0].status = "running";
    await wrapper.vm.$nextTick();
    expect(wrapper.get(".thread-status").attributes("data-state")).toBe("running");
    expect(wrapper.get(".thread-status").attributes("aria-label")).toBe("Model output in progress");
    expect(wrapper.find(".thread-unread").exists()).toBe(false);

    store.threads[0].status = "idle";
    store.threads[0].unread = true;
    await wrapper.vm.$nextTick();
    expect(wrapper.find(".thread-status").exists()).toBe(false);
    expect(wrapper.get(".thread-unread").attributes("aria-label")).toBe("Unread output");

    await wrapper.get(".thread-row").trigger("click");
    expect(store.threads[0].unread).toBe(false);
    expect(wrapper.find(".thread-unread").exists()).toBe(false);
  });

  it("does not bold a task or show a process marker when Pi is not started", () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      catalogLoading: false,
      workspaces: [{ id: "workspace-1", name: "pi-desk", path: "D:\\repo", trust: "deny" }],
      threads: [{
        id: "thread-1", title: "Stopped runtime", workspace: "pi-desk", workspacePath: "D:\\repo", trust: "deny",
        status: "idle", started: false, generation: 0,
      }],
    });

    const wrapper = mount(AppSidebar, { global: { plugins: [pinia] } });

    expect(wrapper.find(".thread-status").exists()).toBe(false);
    expect(wrapper.find(".thread-unread").exists()).toBe(false);
    expect(wrapper.get(".thread-title").classes()).not.toContain("is-started");
  });

  it("opens the workspace action menu and dispatches open and remove actions", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      catalogLoading: false,
      workspaces: [{ id: "workspace-1", name: "pi-desk", path: "D:\\\\repo", trust: "approve" }],
    });
    store.openWorkspace = vi.fn().mockResolvedValue(undefined);
    store.removeWorkspace = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(AppSidebar, { global: { plugins: [pinia] } });

    await wrapper.get('button[aria-label="Actions for pi-desk"]').trigger("click", { clientX: 20, clientY: 20 });
    const menu = wrapper.get(".workspace-context-menu");
    expect(menu.findAll('[role="menuitem"]').map((item) => item.text())).toEqual([
      "New task",
      "Show in File Explorer",
      "Rename workspace",
      "Remove workspace",
    ]);
    await menu.findAll('[role="menuitem"]').find((item) => item.text() === "Show in File Explorer")!.trigger("click");
    expect(store.openWorkspace).toHaveBeenCalledWith("workspace-1");

    await wrapper.get('button[aria-label="Actions for pi-desk"]').trigger("click", { clientX: 20, clientY: 20 });
    await wrapper.get(".workspace-context-menu").findAll('[role="menuitem"]').find((item) => item.text() === "Remove workspace")!.trigger("click");
    expect(store.removeWorkspace).toHaveBeenCalledWith("workspace-1");
  });

  it("filters tasks from the search control", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      catalogLoading: false,
      workspaces: [{ id: "workspace-1", name: "pi-desk", path: "D:\\repo", trust: "deny" }],
      threads: [{
        id: "thread-1", title: "Runtime audit", workspace: "pi-desk", workspacePath: "D:\\repo", trust: "deny",
        status: "idle", started: false, generation: 0,
      }],
    });
    const wrapper = mount(AppSidebar, { global: { plugins: [pinia] } });

    await wrapper.get('button[aria-pressed="false"]').trigger("click");
    const search = wrapper.get('input[type="search"]');
    await search.setValue("missing");
    expect(wrapper.text()).toContain("No matching tasks");
    await search.setValue("runtime");
    expect(wrapper.text()).toContain("Runtime audit");
  });

  it("offers task session, Pi lifecycle, and delete actions from the task menu", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      catalogLoading: false,
      workspaces: [{ id: "workspace-1", name: "pi-desk", path: "D:\\repo", trust: "deny" }],
      threads: [{
        id: "thread-1", title: "Context actions", workspace: "pi-desk", workspacePath: "D:\\repo", trust: "deny",
        status: "idle", started: false, generation: 0, sessionFile: "one.jsonl",
      }],
      activeThreadId: "thread-1",
    });
    store.openWorkspace = vi.fn().mockResolvedValue(undefined);
    store.renameActiveSession = vi.fn().mockResolvedValue(undefined);
    store.openBranchPanel = vi.fn().mockResolvedValue(undefined);
    store.cloneActiveSession = vi.fn().mockResolvedValue(undefined);
    store.exportActiveSession = vi.fn().mockResolvedValue(undefined);
    store.compactActiveSession = vi.fn().mockResolvedValue(undefined);
    store.startThreadInBackground = vi.fn();
    store.requestDeleteThread = vi.fn();
    const wrapper = mount(AppSidebar, { global: { plugins: [pinia] } });

    await wrapper.get(".thread-row").trigger("contextmenu", { clientX: 40, clientY: 60 });
    const menuItems = wrapper.get(".thread-context-menu").findAll('[role="menuitem"]');
    expect(menuItems.map((item) => item.text())).toEqual([
      "Show in File Explorer",
      "Rename task",
      "Session branches",
      "Clone task",
      "Export HTML",
      "Compact context",
      "Start Pi process",
      "Delete task",
    ]);
    expect(wrapper.get(".thread-context-menu").text()).not.toContain("Archive task");

    await menuItems[0].trigger("click");
    expect(store.openWorkspace).toHaveBeenCalledWith("workspace-1");

    for (const [label, action] of [
      ["Session branches", store.openBranchPanel],
      ["Clone task", store.cloneActiveSession],
      ["Export HTML", store.exportActiveSession],
    ] as const) {
      await wrapper.get(".thread-row").trigger("contextmenu", { clientX: 40, clientY: 60 });
      await wrapper.get(".thread-context-menu").findAll('[role="menuitem"]').find((item) => item.text() === label)!.trigger("click");
      expect(action).toHaveBeenCalledOnce();
    }

    await wrapper.get(".thread-row").trigger("contextmenu", { clientX: 40, clientY: 60 });
    await wrapper.get(".thread-context-menu").findAll('[role="menuitem"]').find((item) => item.text() === "Rename task")!.trigger("click");
    await wrapper.get("#task-rename-input").setValue("Renamed task");
    await wrapper.get(".task-rename-menu").trigger("submit");
    expect(store.renameActiveSession).toHaveBeenCalledWith("Renamed task");

    await wrapper.get(".thread-row").trigger("contextmenu", { clientX: 40, clientY: 60 });
    await wrapper.get(".thread-context-menu").findAll('[role="menuitem"]').find((item) => item.text() === "Start Pi process")!.trigger("click");
    expect(store.startThreadInBackground).toHaveBeenCalledWith("thread-1");

    await wrapper.get(".thread-row").trigger("contextmenu", { clientX: 40, clientY: 60 });
    const deleteAction = wrapper.get(".thread-context-menu").findAll('[role="menuitem"]').find((item) => item.text() === "Delete task")!;
    await deleteAction.trigger("click");
    expect(store.requestDeleteThread).toHaveBeenCalledWith("thread-1");
  });
});
