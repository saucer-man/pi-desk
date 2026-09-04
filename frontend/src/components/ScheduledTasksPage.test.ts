import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "../stores/app";
import ScheduledTasksPage from "./ScheduledTasksPage.vue";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/modelconfig", () => ({ modelConfigService: { selectable: vi.fn().mockResolvedValue([]) } }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

describe("ScheduledTasksPage", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
  });

  it("shows scheduled work and exposes direct controls", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      workspaces: [{ id: "workspace-1", name: "pi-desk", path: "D:\\repo", trust: "approve" }],
      scheduledTasks: [{
        id: "schedule-1",
        name: "Review dependency updates",
        prompt: "Inspect dependency updates and report breaking changes.",
        workspaceId: "workspace-1",
        modelProvider: "openai",
        modelId: "gpt-5.6",
        modelName: "GPT 5.6",
        thinkingLevel: "high",
        frequency: "daily",
        time: "09:00",
        enabled: true,
        nextRunAt: new Date(Date.now() + 60_000).toISOString(),
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
      }],
    });
    store.runScheduledTask = vi.fn().mockResolvedValue("thread-1");
    const wrapper = mount(ScheduledTasksPage, { global: { plugins: [pinia] } });

    expect(wrapper.get(".scheduled-task-row").text()).toContain("Review dependency updates");
    expect(wrapper.get(".scheduled-task-row").text()).toContain("pi-desk");
    expect(wrapper.get(".scheduled-task-row").text()).toContain("GPT 5.6");
    expect(wrapper.get(".scheduled-task-row").text()).toContain("High");
    expect(wrapper.get(".scheduled-task-row").text()).toContain("Daily · 09:00");

    await wrapper.get('button[aria-label="Run now"]').trigger("click");
    expect(store.runScheduledTask).toHaveBeenCalledWith("schedule-1");

    await wrapper.get('input[type="checkbox"]').setValue(false);
    expect(store.scheduledTasks[0].enabled).toBe(false);
  });

  it("explains why creation is unavailable without a trusted local workspace", () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.workspaces = [{ id: "remote-1", name: "remote", path: "", kind: "ssh", trust: "approve" }];
    const wrapper = mount(ScheduledTasksPage, { global: { plugins: [pinia] } });

    expect(wrapper.get(".scheduled-tasks-notice").text()).toContain("trust a local workspace");
    expect(wrapper.get(".scheduled-tasks-header button").attributes("disabled")).toBeDefined();
  });

  it("defaults a new schedule to the current Pi task model", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      workspaces: [{ id: "workspace-1", name: "pi-desk", path: "D:\\repo", trust: "approve" }],
      threads: [{
        id: "thread-1", title: "Current", workspace: "pi-desk", workspaceId: "workspace-1", workspacePath: "D:\\repo",
        trust: "approve", status: "idle", started: true, generation: 1, createdAt: new Date().toISOString(), modifiedAt: new Date().toISOString(), unread: false,
      }],
      activeThreadId: "thread-1",
      knownRuntimeModels: [
        { provider: "anthropic", id: "claude", name: "Claude" },
        { provider: "openai", id: "gpt-5.6", name: "GPT 5.6" },
      ],
      sessionStateByThread: { "thread-1": { model: { provider: "openai", id: "gpt-5.6", name: "GPT 5.6" }, thinkingLevel: "high" } },
      thinkingLevelsByThread: { "thread-1": ["low", "medium", "high"] },
    });
    const wrapper = mount(ScheduledTasksPage, { global: { plugins: [pinia] } });

    await wrapper.get(".scheduled-tasks-header button").trigger("click");
    await vi.waitFor(() => expect(wrapper.get(".scheduled-task-dialog").attributes("open")).toBeDefined());

    const modelSelect = wrapper.findAll(".scheduled-task-form select")[1];
    expect((modelSelect.element as HTMLSelectElement).selectedOptions[0].textContent).toContain("GPT 5.6 · openai");
    const thinkingSelect = wrapper.findAll(".scheduled-task-form select")[2];
    expect((thinkingSelect.element as HTMLSelectElement).value).toBe("high");
    expect(wrapper.findAll(".scheduled-task-parameter-field").map((field) => field.get("span").text())).toEqual(["Model", "Thinking level"]);
  });

  it("requires an explicit choice when multiple models are available without a current model", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      workspaces: [{ id: "workspace-1", name: "pi-desk", path: "D:\\repo", trust: "approve" }],
      knownRuntimeModels: [
        { provider: "anthropic", id: "claude", name: "Claude" },
        { provider: "openai", id: "gpt-5.6", name: "GPT 5.6" },
      ],
    });
    const wrapper = mount(ScheduledTasksPage, { global: { plugins: [pinia] } });

    await wrapper.get(".scheduled-tasks-header button").trigger("click");
    await vi.waitFor(() => expect(wrapper.get(".scheduled-task-dialog").attributes("open")).toBeDefined());

    expect((wrapper.findAll(".scheduled-task-form select")[1].element as HTMLSelectElement).value).toBe("");
  });
});
