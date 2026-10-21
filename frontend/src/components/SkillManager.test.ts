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

  it("displays shared project skills read-only and blocks saving", async () => {
    const skill = { ...skillSummary(), scope: SkillScope.SkillScopeProject, rootDirectory: "D:\\repo\\.agents\\skills", readOnly: true };
    skillMocks.list.mockResolvedValue({ globalDirectory: piSkillRoot, globalDirectories: [piSkillRoot, sharedSkillRoot], projectEnabled: true, skills: [skill] });
    skillMocks.get.mockResolvedValue({ ...skill, content: "# Shared instructions" });
    const pinia = createPinia();
    setActivePinia(pinia);
    const wrapper = mount(SkillManager, { global: { plugins: [pinia] } });
    await flushPromises();
    expect(wrapper.get("textarea").attributes("readonly")).toBeDefined();
    expect(wrapper.text()).toContain("read-only in Pi Desk");
    expect(wrapper.find("footer").exists()).toBe(false);
    await wrapper.get("form").trigger("submit");
    expect(skillMocks.update).not.toHaveBeenCalled();
    expect(skillMocks.delete).not.toHaveBeenCalled();
    expect(skillMocks.get).toHaveBeenCalledWith(expect.objectContaining({ rootDirectory: skill.rootDirectory }));
    wrapper.unmount();
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

    expect(wrapper.find(".settings-content-header").exists()).toBe(false);
    expect(wrapper.get(".skill-config-content").classes()).toContain("model-config-content");
    expect(wrapper.get("textarea").classes()).toEqual(expect.arrayContaining(["h-full", "min-h-0", "resize-none"]));
    expect(wrapper.find("textarea + small").exists()).toBe(false);
    const actionButtons = wrapper.findAll("footer button");
    expect(actionButtons.map((button) => button.text())).toEqual(["Delete skill", "Save skill"]);
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

  it("shows project availability without scope controls", async () => {
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

    expect(wrapper.find("select").exists()).toBe(false);
    expect(wrapper.text()).toContain("Project skills require a trusted workspace.");
    expect(wrapper.get(".runtime-resource-scope").text()).toContain("/package-skill");
    expect(wrapper.get(".runtime-resource-scope").text()).toContain("Loaded by Pi");
  });

  it.each([false, true])("ignores a stale skill response across roots (failure: %s)", async (failStale) => {
    const a = { ...skillSummary(), name: "A" };
    const b = { ...skillSummary(), name: "B", rootDirectory: sharedSkillRoot };
    skillMocks.list.mockResolvedValue({ globalDirectory: piSkillRoot, projectEnabled: false, skills: [a, b] });
    skillMocks.get.mockResolvedValueOnce({ ...a, content: "# A" });
    const pinia = createPinia();
    setActivePinia(pinia);
    const wrapper = mount(SkillManager, { global: { plugins: [pinia] } });
    try {
      await flushPromises();
      let finish!: () => void;
      skillMocks.get.mockImplementationOnce(() => new Promise((resolve, reject) => {
        finish = () => failStale ? reject(new Error("A failed")) : resolve({ ...a, content: "# A" });
      }));
      skillMocks.get.mockResolvedValue({ ...b, content: "# B" });
      const rows = wrapper.findAll(".skill-list .prompt-config-scope button");
      await rows[0].trigger("click");
      await rows[1].trigger("click");
      await flushPromises();
      finish();
      await flushPromises();
      expect((wrapper.get("textarea").element as HTMLTextAreaElement).value).toBe("# B");
      expect(wrapper.find(".form-error").exists()).toBe(false);
      expect(rows[1].classes()).toContain("is-active");
      await wrapper.get("textarea").setValue("# B edited");
      skillMocks.update.mockResolvedValue({ ...b, content: "# B edited" });
      await wrapper.get("form").trigger("submit");
      await flushPromises();
      expect(skillMocks.update).toHaveBeenCalledWith(expect.objectContaining({ rootDirectory: sharedSkillRoot, content: "# B edited" }));
    } finally {
      wrapper.unmount();
    }
  });

  it("keeps the loaded skill selected when another skill fails to load", async () => {
    const a = skillSummary();
    const b = { ...a, name: "B", rootDirectory: sharedSkillRoot };
    skillMocks.list.mockResolvedValue({ globalDirectory: piSkillRoot, projectEnabled: false, skills: [a, b] });
    skillMocks.get.mockResolvedValueOnce({ ...a, content: "# A" }).mockRejectedValueOnce(new Error("B failed"));
    const pinia = createPinia();
    setActivePinia(pinia);
    const wrapper = mount(SkillManager, { global: { plugins: [pinia] } });
    try {
      await flushPromises();
      const rows = wrapper.findAll(".skill-list .prompt-config-scope button");
      await rows[1].trigger("click");
      await flushPromises();
      expect(rows[0].classes()).toContain("is-active");
      expect(wrapper.get(".form-error").text()).toContain("B failed");
      await wrapper.get("textarea").setValue("# A edited");
      skillMocks.get.mockResolvedValue({ ...a, content: "# A edited" });
      skillMocks.update.mockResolvedValue({ ...a, content: "# A edited" });
      await wrapper.get("form").trigger("submit");
      await flushPromises();
      expect(skillMocks.update).toHaveBeenCalledWith(expect.objectContaining({ rootDirectory: piSkillRoot, content: "# A edited" }));
    } finally {
      wrapper.unmount();
    }
  });
});
