import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "../stores/app";
import SessionStatistics from "./SessionStatistics.vue";

const catalogMocks = vi.hoisted(() => ({ getSessionUsage: vi.fn() }));

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: catalogMocks }));
vi.mock("../services/modelconfig", () => ({ modelConfigService: { selectable: vi.fn().mockResolvedValue([]) } }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

describe("SessionStatistics", () => {
  beforeEach(() => {
    catalogMocks.getSessionUsage.mockReset();
    catalogMocks.getSessionUsage.mockResolvedValue({
      sessions: 3, messages: 12, userMessages: 3, assistantMessages: 4, toolResults: 5,
      tokens: { input: 1000, output: 200, cacheRead: 3000, cacheWrite: 40, reasoning: 80, total: 4240 },
      cost: 1.25,
      models: [
        { provider: "openai", model: "gpt-5", assistantMessages: 3, tokens: { input: 900, output: 180, cacheRead: 3000, cacheWrite: 40, reasoning: 70, total: 4120 }, cost: 1.2 },
        { provider: "deepseek", model: "deepseek-v3", assistantMessages: 1, tokens: { input: 100, output: 20, cacheRead: 0, cacheWrite: 0, reasoning: 10, total: 120 }, cost: 0.05 },
      ],
    });
  });

  it("shows Pi-persisted token and model usage without starting a thread", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const wrapper = mount(SessionStatistics, { global: { plugins: [pinia] } });
    await flushPromises();

    expect(catalogMocks.getSessionUsage).toHaveBeenCalledWith(undefined);
    expect(wrapper.text()).toContain("4,240");
    expect(wrapper.text()).toContain("$1.2500");
    expect(wrapper.text()).toContain("gpt-5");
    expect(wrapper.text()).toContain("Reasoning is included in output tokens");
  });

  it("filters statistics to the active workspace", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "thread-1", title: "Usage", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: false, generation: 0 }],
      activeThreadId: "thread-1",
    });
    const wrapper = mount(SessionStatistics, { global: { plugins: [pinia] } });
    await flushPromises();

    const workspaceTab = wrapper.findAll('[role="tab"]').find((tab) => tab.text() === "Current workspace")!;
    await workspaceTab.trigger("click");
    await flushPromises();

    expect(catalogMocks.getSessionUsage).toHaveBeenLastCalledWith("D:\\repo");
    expect(wrapper.text()).toContain("D:\\repo");
  });
});
