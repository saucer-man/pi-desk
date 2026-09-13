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
  openFile: vi.fn(),
  revealFile: vi.fn(),
}));
vi.mock("../services/repository", () => ({ repositoryService: repositoryMocks }));
vi.mock("../services/terminal", () => ({ terminalService: {}, onTerminalEvent: () => () => undefined }));
describe("InspectorPanel", () => {
  beforeEach(() => vi.clearAllMocks());

  it("identifies a recorded conversation diff as this session", () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-session-diff", title: "Session diff", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: false, generation: 0,
      }],
      activeThreadId: "thread-session-diff",
      repositoryDiffPathByWorkspace: { "d:/repo": "main.go" },
      repositoryDiffByWorkspace: { "d:/repo": { path: "main.go", working: "@@ -1 +1 @@\n-old\n+new", session: true } },
    });
    store.refreshActiveRepository = vi.fn().mockResolvedValue(undefined);

    const wrapper = mount(InspectorPanel, { global: { plugins: [pinia] } });

    expect(wrapper.get(".diff-section header").text()).toBe("This session");
    expect(wrapper.get(".diff-stats").text()).toBe("+1-1");
  });

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
      working: "diff --git a/src/main.go b/src/main.go\n@@ -4,2 +4,2 @@\n keep\n-old\n+new\n\\ No newline at end of file\n",
      content: "",
      binary: false,
      truncated: false,
    });
    repositoryMocks.previewFile.mockResolvedValue({
      path: "README.md", absolutePath: "D:\\repo\\README.md", content: "# Repo", size: 6, binary: false, truncated: false,
    });
    const wrapper = mount(InspectorPanel, { global: { plugins: [pinia] } });

    expect(wrapper.findAll('[role="tab"]')[0].text()).toBe("Files");
    expect(wrapper.find('[title="Show branches"]').exists()).toBe(false);
    expect(wrapper.find('button[title="Preview src/main.go"]').exists()).toBe(true);
    expect(wrapper.get('button[title="Preview src/main.go"]').classes()).toContain("is-changed");
    expect(wrapper.get('button[title="Preview src/main.go"]').attributes("data-status")).toBe("M");
    expect(wrapper.find('button[title="Preview README.md"]').exists()).toBe(true);

    await wrapper.get('button[title="Preview README.md"]').trigger("click");
    await flushPromises();
    expect(repositoryMocks.previewFile).toHaveBeenCalledWith("D:\\repo", "README.md");
    expect(wrapper.findAll('[role="tab"]')).toHaveLength(4);
    expect(wrapper.find(".inspector-file-header").exists()).toBe(true);
    expect(wrapper.text()).toContain("# Repo");
    await wrapper.get('button[title="Close file preview"]').trigger("click");
    expect(wrapper.find(".inspector-file-header").exists()).toBe(false);

    expect(wrapper.find(".repository-file-controls").exists()).toBe(false);
    expect(wrapper.find('input[type="search"]').exists()).toBe(false);
    expect(wrapper.find('button[title="Preview README.md"]').exists()).toBe(true);
    expect(wrapper.find('button[title="Preview src/main.go"]').exists()).toBe(true);
    await wrapper.get('button[title="Mention file"]').trigger("click");
    expect(store.activeDraft).toBe("@src/main.go ");

    await wrapper.get('button[title="View diff for src/main.go"]').trigger("click");
    await flushPromises();
    expect(repositoryMocks.diff).toHaveBeenCalledWith("D:\\repo", "src/main.go");
    expect(wrapper.get('[aria-label="Working tree diff"]').text()).toContain("+new");
    const deletion = wrapper.get(".diff-line.is-deletion");
    const addition = wrapper.get(".diff-line.is-addition");
    const context = wrapper.findAll(".diff-line").find((line) => line.get(".diff-line-text").text() === "keep");
    expect(context?.get(".diff-line-number--old").text()).toBe("4");
    expect(context?.get(".diff-line-number--new").text()).toBe("4");
    expect(deletion.get(".diff-line-number--old").text()).toBe("5");
    expect(deletion.get(".diff-line-number--new").text()).toBe("");
    expect(deletion.get(".diff-line-marker").text()).toBe("−");
    expect(deletion.get(".diff-line-text").text()).toBe("old");
    expect(addition.get(".diff-line-number--old").text()).toBe("");
    expect(addition.get(".diff-line-number--new").text()).toBe("5");
    expect(addition.get(".diff-line-text").text()).toBe("new");
    expect(wrapper.get(".diff-stats").text()).toBe("+1-1");
    await wrapper.get('button[title="Back to changes"]').trigger("click");

    await wrapper.findAll('[role="tab"]')[1].trigger("click");
    expect(wrapper.text()).toContain("D:\\repo");
    const contextRows = wrapper.findAll(".context-panel dl > div");
    expect(contextRows[2].text()).toContain("SessionAudit");
    expect(contextRows[3].text()).toContain("Session IDCreated on first prompt");
    expect(wrapper.text()).not.toContain("All files");
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

    expect(wrapper.find('button[title="Preview new.png"]').exists()).toBe(true);
    expect(wrapper.get('button[title="View diff for new.png"]').text()).toBe("R");
    store.repositoryStaleByWorkspace["d:/repo"] = true;
    store.repositoryErrorByWorkspace["d:/repo"] = "remote repository is disconnected or stale";
    await wrapper.vm.$nextTick();
    expect(wrapper.text()).toContain("Repository data is stale");
    expect(wrapper.get('button[title="View diff for new.png"]').text()).toBe("R");

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

  it("does not expose local open actions for a remote preview", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      workspaces: [{ id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote", remoteRoot: "/srv/repo", trust: "approve" }],
      threads: [{ id: "thread-remote", title: "Remote", workspace: "remote", workspaceId: "workspace-remote", workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0 }],
      activeThreadId: "thread-remote",
      repositoryFilePreviewPathByThread: { "thread-remote": "README.md" },
      repositoryFilePreviewByThread: { "thread-remote": { path: "README.md", absolutePath: "", content: "remote", size: 6, binary: false, truncated: false } },
    });
    store.refreshActiveRepository = vi.fn().mockResolvedValue(undefined);

    const wrapper = mount(InspectorPanel, { global: { plugins: [pinia] } });

    expect(wrapper.find('button[title="Open file"]').exists()).toBe(false);
    expect(wrapper.find('button[title="Show in file manager"]').exists()).toBe(false);
    store.closeRepositoryFilePreview();
    store.inspectorTab = "context";
    await wrapper.vm.$nextTick();
    expect(wrapper.text()).toContain("/srv/repo");
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
    expect(wrapper.text()).not.toContain("D:\\repo\\scripts\\join_groups.py");
    expect(wrapper.text()).toContain("print('ok')");
    expect(wrapper.text()).toContain(":7");
    expect(wrapper.find(".file-preview-tabs").exists()).toBe(false);
    expect(wrapper.find(".file-preview-meta").exists()).toBe(false);
    expect(wrapper.get('[aria-label="File preview content"]').classes()).toEqual(expect.arrayContaining(["rounded-none!", "border-0!"]));
    await wrapper.get('button[title="Close file preview"]').trigger("click");
    expect(store.activeRepositoryFilePreviewPath).toBe("");
  });

  it("renders media previews without redundant file metadata", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "thread-media", title: "Media", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: false, generation: 0 }],
      activeThreadId: "thread-media",
      repositoryFileTabsByThread: { "thread-media": ["README.md", "image.png"] },
      repositoryFilePreviewPathByThread: { "thread-media": "image.png" },
      repositoryFilePreviewByThread: { "thread-media": {
        path: "image.png", absolutePath: "D:\\repo\\image.png", mediaType: "image/png", dataUrl: "data:image/png;base64,iVBORw0KGgo=", size: 8, binary: true, truncated: false,
      } },
    });
    store.refreshActiveRepository = vi.fn().mockResolvedValue(undefined);
    repositoryMocks.previewFile.mockResolvedValue({ path: "README.md", absolutePath: "D:\\repo\\README.md", mediaType: "text/markdown", content: "# Repo", size: 6 });
    const wrapper = mount(InspectorPanel, { global: { plugins: [pinia] } });

    expect(wrapper.get("img.file-media-preview").attributes("src")).toContain("data:image/png");
    expect(wrapper.find(".file-preview-tabs").exists()).toBe(false);
    expect(wrapper.find(".file-preview-meta").exists()).toBe(false);
    await store.openRepositoryFilePreview("README.md");
    await flushPromises();
    expect(repositoryMocks.previewFile).toHaveBeenCalledWith("D:\\repo", "README.md");
    expect(wrapper.text()).toContain("Repo");
    await wrapper.findAll(".markdown-preview-toggle button")[1].trigger("click");
    expect(wrapper.get('[aria-label="File preview content"]').text()).toContain("# Repo");
  });
});
