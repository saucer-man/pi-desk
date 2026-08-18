import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { SkillScope } from "../../bindings/pi-desk/internal/domain";
import { useAppStore } from "../stores/app";
import SkillManager from "./SkillManager.vue";

const skillMocks = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), create: vi.fn(), update: vi.fn(), delete: vi.fn() }));
const piSkillRoot = "C:\\Users\\dev\\.pi\\agent\\skills";
const sharedSkillRoot = "C:\\Users\\dev\\.agents\\skills";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/modelconfig", () => ({ modelConfigService: { selectable: vi.fn().mockResolvedValue([]) } }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));
vi.mock("../services/skills", () => ({ managedSkillService: skillMocks }));

function skillSummary() {
  return {
    scope: SkillScope.SkillScopeGlobal,
    name: "code-review",
    description: "Review code",
    rootDirectory: piSkillRoot,
    relativePath: "code-review\\SKILL.md",
    path: `${piSkillRoot}\\code-review\\SKILL.md`,
    directory: `${piSkillRoot}\\code-review`,
    kind: "directory",
    enabled: true,
  };
}

describe("SkillManager", () => {
  beforeEach(() => {
    skillMocks.list.mockReset();
    skillMocks.get.mockReset();
    skillMocks.create.mockReset();
    skillMocks.update.mockReset();
    skillMocks.delete.mockReset();
  });

  it("loads and updates a Pi-native SKILL.md", async () => {
    skillMocks.list.mockResolvedValue({
      globalDirectory: piSkillRoot,
      globalDirectories: [piSkillRoot, sharedSkillRoot],
      projectEnabled: false,
      skills: [skillSummary()],
    });
    skillMocks.get.mockResolvedValue({
      ...skillSummary(),
      content: "---\nname: code-review\ndescription: Review code\n---\n\n# Review\n",
    });
    skillMocks.update.mockResolvedValue({
      ...skillSummary(),
      content: "---\nname: code-review\ndescription: Review code\n---\n\n# Updated\n",
    });
    const pinia = createPinia();
    setActivePinia(pinia);
    const wrapper = mount(SkillManager, { global: { plugins: [pinia] } });
    await flushPromises();

    expect(wrapper.text()).toContain("code-review");
    await wrapper.get("textarea").setValue("---\nname: code-review\ndescription: Review code\n---\n\n# Updated\n");
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    expect(skillMocks.update).toHaveBeenCalledWith(expect.objectContaining({
      rootDirectory: piSkillRoot,
      relativePath: "code-review\\SKILL.md",
    }));
    expect(wrapper.text()).toContain("Skill saved.");
  });

  it("shows one merged skill list without project scope controls", async () => {
    skillMocks.list.mockResolvedValue({
      globalDirectory: piSkillRoot,
      globalDirectories: [piSkillRoot, sharedSkillRoot],
      projectEnabled: false,
      skills: [],
    });
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "thread-1", title: "Runtime", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: true, generation: 1 }],
      activeThreadId: "thread-1",
      commandsByThread: { "thread-1": [{ name: "package-skill", description: "Loaded from a package", source: "skill", path: "C:\\package\\skills\\package-skill\\SKILL.md" }] },
    });
    const wrapper = mount(SkillManager, { global: { plugins: [pinia] } });
    await flushPromises();

    expect(wrapper.text()).toContain(sharedSkillRoot);
    expect(wrapper.find("select").exists()).toBe(false);
    expect(wrapper.text()).not.toContain("Project skills");
    expect(wrapper.get(".runtime-resource-scope").text()).toContain("/package-skill");
    expect(wrapper.get(".runtime-resource-scope").text()).toContain("Loaded by Pi (read-only)");
  });
});
