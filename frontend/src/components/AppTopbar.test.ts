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

  it("keeps task lifecycle actions out of the topbar", () => {
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

    expect(wrapper.find('button[title="Task actions"]').exists()).toBe(false);
    expect(wrapper.find('button[title="Session branches"]').exists()).toBe(false);
    expect(wrapper.find(".inspector-toggle").exists()).toBe(true);
  });
});
