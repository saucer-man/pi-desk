import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { PromptTemplateScope } from "../../bindings/pi-desk/internal/domain";
import { useAppStore } from "../stores/app";
import PromptTemplateManager from "./PromptTemplateManager.vue";

const promptMocks = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), upsert: vi.fn(), delete: vi.fn() }));

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/modelconfig", () => ({ modelConfigService: { selectable: vi.fn().mockResolvedValue([]) } }));
vi.mock("../services/prompts", () => ({ promptTemplateService: promptMocks }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

describe("PromptTemplateManager", () => {
  beforeEach(() => {
    promptMocks.list.mockReset();
    promptMocks.get.mockReset();
    promptMocks.upsert.mockReset();
    promptMocks.delete.mockReset();
  });

  it("lists and edits Pi native global prompt templates", async () => {
    promptMocks.list.mockResolvedValue({
      globalDirectory: "C:\\Users\\dev\\.pi\\agent\\prompts",
      projectEnabled: false,
      projectNotice: "trust project resources before managing project prompt templates",
      templates: [{ scope: PromptTemplateScope.PromptTemplateScopeGlobal, name: "review", description: "Review changes", path: "C:\\Users\\dev\\.pi\\agent\\prompts\\review.md" }],
    });
    promptMocks.get.mockResolvedValue({
      scope: PromptTemplateScope.PromptTemplateScopeGlobal, name: "review", description: "Review changes",
      path: "C:\\Users\\dev\\.pi\\agent\\prompts\\review.md", content: "---\ndescription: Review changes\n---\nReview $@\n",
    });
    promptMocks.upsert.mockResolvedValue({
      scope: PromptTemplateScope.PromptTemplateScopeGlobal, name: "review", description: "Review changes",
      path: "C:\\Users\\dev\\.pi\\agent\\prompts\\review.md", content: "---\ndescription: Review changes\n---\nReview changed files\n",
    });
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.refreshCommands = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(PromptTemplateManager, { global: { plugins: [pinia] } });
    await flushPromises();

    expect(wrapper.text()).toContain("/review");
    await wrapper.get('textarea').setValue("---\ndescription: Review changes\n---\nReview changed files\n");
    await wrapper.get('form').trigger("submit");
    await flushPromises();

    expect(promptMocks.upsert).toHaveBeenCalledWith(expect.objectContaining({
      scope: PromptTemplateScope.PromptTemplateScopeGlobal,
      originalName: "review",
      name: "review",
    }));
    expect(wrapper.text()).toContain("Prompt template saved.");
  });

  it("keeps project prompt editing disabled until the workspace is trusted", async () => {
    promptMocks.list.mockResolvedValue({
      globalDirectory: "C:\\Users\\dev\\.pi\\agent\\prompts",
      projectDirectory: "D:\\repo\\.pi\\prompts",
      projectEnabled: false,
      projectNotice: "trust project resources before managing project prompt templates",
      templates: [],
    });
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "thread-1", title: "Runtime", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: true, generation: 1 }],
      activeThreadId: "thread-1",
      commandsByThread: { "thread-1": [{ name: "package-prompt", description: "Loaded from a package", source: "prompt", path: "C:\\package\\prompts\\package-prompt.md" }] },
    });
    const wrapper = mount(PromptTemplateManager, { global: { plugins: [pinia] } });
    await flushPromises();

    expect(wrapper.text()).toContain("trust project resources before managing project prompt templates");
    const scopeSelect = wrapper.get('select');
    const projectOption = scopeSelect.findAll('option').find((option) => option.element.value === PromptTemplateScope.PromptTemplateScopeProject);
    expect(projectOption?.attributes("disabled")).toBeDefined();
    expect(wrapper.get(".runtime-resource-scope").text()).toContain("/package-prompt");
    expect(wrapper.get(".runtime-resource-scope").text()).toContain("Loaded by Pi (read-only)");
  });
});
