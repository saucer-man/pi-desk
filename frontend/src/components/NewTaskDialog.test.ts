import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "../stores/app";
import NewTaskDialog from "./NewTaskDialog.vue";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

describe("NewTaskDialog", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
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
