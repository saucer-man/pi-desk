import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "../stores/app";
import InspectorPanel from "./InspectorPanel.vue";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
const repositoryMocks = vi.hoisted(() => ({
  diff: vi.fn(),
  previewFile: vi.fn(),
  branches: vi.fn(),
  openFile: vi.fn(),
  revealFile: vi.fn(),
}));
vi.mock("../services/repository", () => ({ repositoryService: repositoryMocks }));
vi.mock("../services/terminal", () => ({ terminalService: {}, onTerminalEvent: () => () => undefined }));
describe("InspectorPanel", () => {
  beforeEach(() => vi.clearAllMocks());

  it("renders repository changes and inserts file mentions", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-1", title: "Audit", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: false, generation: 0,
      }],
      activeThreadId: "thread-1",
      repositoryByWorkspace: { "d:/repo": {
        files: [{ path: "README.md", name: "README.md" }, { path: "src/main.go", name: "main.go" }],
        git: {
          isRepository: true,
          branch: "feature/repo-view",
          files: [{ path: "src/main.go", indexStatus: " ", worktreeStatus: "M" }],
        },
      } },
    });
    store.refreshActiveRepository = vi.fn().mockResolvedValue(undefined);
    repositoryMocks.diff.mockResolvedValue({
      path: "src/main.go",
      staged: "",
      working: "diff --git a/src/main.go b/src/main.go\n@@ -1 +1 @@\n-old\n+new\n",
      content: "",
      binary: false,
      truncated: false,
    });
    repositoryMocks.branches.mockResolvedValue({ branches: [
      { name: "feature/repo-view", fullName: "refs/heads/feature/repo-view", current: true, remote: false, upstream: "origin/feature/repo-view", commit: "abc123", worktreePath: "D:\\repo" },
      { name: "origin/main", fullName: "refs/remotes/origin/main", current: false, remote: true, upstream: "", commit: "def456", worktreePath: "" },
    ] });
    const wrapper = mount(InspectorPanel, { global: { plugins: [pinia] } });

    expect(wrapper.text()).toContain("feature/repo-view");
    expect(wrapper.text()).toContain("src/main.go");
    await wrapper.get('button[title="Mention file"]').trigger("click");
    expect(store.activeDraft).toBe("@src/main.go ");

    await wrapper.get('button[title="Show branches"]').trigger("click");
    await flushPromises();
    expect(repositoryMocks.branches).toHaveBeenCalledWith("D:\\repo");
    expect(wrapper.text()).toContain("origin/feature/repo-view");

    await wrapper.get('button[title="View diff for src/main.go"]').trigger("click");
    await flushPromises();
    expect(repositoryMocks.diff).toHaveBeenCalledWith("D:\\repo", "src/main.go");
    expect(wrapper.get('[aria-label="Working tree diff"]').text()).toContain("+new");
    expect(wrapper.get(".diff-line.is-deletion").text()).toBe("-old");
    await wrapper.get('button[title="Back to changes"]').trigger("click");

    await wrapper.findAll('[role="tab"]')[1].trigger("click");
    expect(wrapper.text()).toContain("Workspace files");
    expect(wrapper.text()).toContain("README.md");
  });

  it("renders renamed, loading, error, and binary diff states", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-2", title: "Assets", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: false, generation: 0,
      }],
      activeThreadId: "thread-2",
      repositoryByWorkspace: { "d:/repo": {
        files: [{ path: "new.png", name: "new.png" }],
        git: {
          isRepository: true,
          branch: "main",
          files: [{ path: "new.png", originalPath: "old.png", indexStatus: "R", worktreeStatus: " " }],
        },
      } },
    });
    store.refreshActiveRepository = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(InspectorPanel, { global: { plugins: [pinia] } });

    expect(wrapper.text()).toContain("old.png -> new.png");
    store.repositoryDiffPathByWorkspace["d:/repo"] = "new.png";
    store.repositoryDiffLoadingByWorkspace["d:/repo"] = true;
    await wrapper.vm.$nextTick();
    expect(wrapper.find(".repository-diff .is-spinning").exists()).toBe(true);

    store.repositoryDiffLoadingByWorkspace["d:/repo"] = false;
    store.repositoryDiffErrorByWorkspace["d:/repo"] = "diff unavailable";
    await wrapper.vm.$nextTick();
    expect(wrapper.text()).toContain("diff unavailable");

    store.repositoryDiffErrorByWorkspace["d:/repo"] = "";
    store.repositoryDiffByWorkspace["d:/repo"] = {
      path: "new.png", staged: "", working: "", content: "", binary: true, truncated: false,
    };
    await wrapper.vm.$nextTick();
    expect(wrapper.text()).toContain("Binary file changed");
  });

  it("renders a linked file in the right inspector preview", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-preview", title: "Preview", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: false, generation: 0,
      }],
      activeThreadId: "thread-preview",
      repositoryFilePreviewPathByThread: { "thread-preview": "scripts/join_groups.py" },
      repositoryFilePreviewByThread: { "thread-preview": {
        path: "scripts/join_groups.py", absolutePath: "D:\\repo\\scripts\\join_groups.py", content: "print('ok')", size: 12, binary: false, truncated: false,
      } },
      repositoryFilePreviewLineByThread: { "thread-preview": 7 },
    });
    store.refreshActiveRepository = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(InspectorPanel, { global: { plugins: [pinia] } });

    expect(wrapper.text()).toContain("join_groups.py");
    expect(wrapper.text()).toContain("D:\\repo\\scripts\\join_groups.py");
    expect(wrapper.text()).toContain("print('ok')");
    expect(wrapper.text()).toContain(":7");
    await wrapper.get('button[title="Close file preview"]').trigger("click");
    expect(store.activeRepositoryFilePreviewPath).toBe("");
  });
});
