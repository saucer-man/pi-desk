import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { type PanelTab, useAppStore } from "../stores/app";
import InspectorPanel from "./InspectorPanel.vue";
import { buildToolDiff } from "../utils/toolDiff";

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
function panelState(threadId: string, tab: Partial<PanelTab> & Pick<PanelTab, "kind" | "path">) {
  const id = threadId + ":" + tab.path;
  return { inspectorByThread: { [threadId]: { tabs: [{ id, title: tab.path || "", ...tab }], activeId: id, open: true, width: 600, expanded: false } } };
}
describe("InspectorPanel", () => {
  beforeEach(() => vi.clearAllMocks());

  it("reorders a pointer-dragged tab without activating the drop target", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.activeThreadId = "drag";
    store.refreshActiveRepository = vi.fn().mockResolvedValue(undefined);
    store.scheduleDesktopStateSave = vi.fn();
    store.openPanelTab({ id: "one", kind: "files", title: "One" });
    store.openPanelTab({ id: "two", kind: "files", title: "Two" });
    const wrapper = mount(InspectorPanel, { global: { plugins: [pinia] } });
    const tab = wrapper.findAll('[role="tab"]')[1];
    tab.element.setPointerCapture = vi.fn();
    const point = vi.spyOn(document, "elementFromPoint").mockReturnValue(wrapper.findAll(".panel-tab")[0].element);
    await tab.trigger("pointerdown", { button: 0, pointerId: 1, clientX: 200, clientY: 20 });
    await tab.trigger("pointerup", { pointerId: 1, clientX: 20, clientY: 20 });
    await tab.trigger("click");
    expect(store.activePanel?.tabs.map((tab) => tab.id)).toEqual(["two", "one"]);
    expect(store.activePanel?.activeId).toBe("two");
    point.mockRestore();
    wrapper.unmount();
  });

  it("keeps the directory node mounted while previewing and pinning a file, and toggles it for diffs", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "tree", title: "Tree", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: false, generation: 0 }],
      activeThreadId: "tree",
      repositoryByWorkspace: { "d:/repo": { files: [{ path: "main.py", name: "main.py" }], git: { isRepository: false, files: [] } } },
    });
    store.refreshActiveRepository = vi.fn().mockResolvedValue(undefined);
    store.scheduleDesktopStateSave = vi.fn();
    repositoryMocks.previewFile.mockResolvedValue({ path: "main.py", content: "print(1)" });
    const wrapper = mount(InspectorPanel, { global: { plugins: [pinia] } });
    await flushPromises();
    const fileButton = wrapper.get('.file-tree-open');
    await fileButton.trigger('click');
    await flushPromises();
    expect(wrapper.get('.file-tree-open').element).toBe(fileButton.element);
    expect(store.activePanelTab?.pinned).toBe(false);
    await fileButton.trigger('dblclick');
    await flushPromises();
    expect(store.activePanelTab?.pinned).toBe(true);
    expect(wrapper.get('[aria-label="文件目录"]').attributes('aria-expanded')).toBe('true');
    await wrapper.get('[aria-label="文件目录"]').trigger('click');
    expect(wrapper.find('.panel-directory').exists()).toBe(false);
    await store.openRepositoryDiff('main.py', '@@ -1 +1 @@\n-print(1)\n+print(2)', 'message');
    await flushPromises();
    Element.prototype.scrollIntoView = vi.fn();
    await wrapper.get('[aria-label="文件目录"]').trigger('click');
    expect(wrapper.find('.panel-directory').exists()).toBe(true);
    expect(wrapper.find('.repository-diff').exists()).toBe(true);
    wrapper.unmount();
  });

  it("separates Pi write/edit display gutters from source indentation when switching conversation cards", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "gutter", title: "Gutter", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: false, generation: 0 }],
      activeThreadId: "gutter",
    });
    store.refreshActiveRepository = vi.fn().mockResolvedValue(undefined);
    store.scheduleDesktopStateSave = vi.fn();
    const wrapper = mount(InspectorPanel, { global: { plugins: [pinia] } });
    await flushPromises();
    const write = buildToolDiff("write", { path: "main.py", content: "import argparse\n\n    records = []" })!;
    const edit = buildToolDiff("edit", { path: "main.py", oldText: "    records = []", newText: "    records = [1]" }, {
      diff: "    ...\n 12     records = []\n 13 \n-14     records = []\n+14     records = [1]\n 15         return records\n    ...",
    })!;
    for (const [text, expected] of [
      [write.text, [["1", "import argparse"], ["2", ""], ["3", "    records = []"]]],
      [edit.text, [["12", "    records = []"], ["13", ""], ["14", "    records = []"], ["14", "    records = [1]"], ["15", "        return records"]]],
      ["@@ -1 +1 @@\n-123 old\n+456 new", [["1", "123 old"], ["1", "456 new"]]],
      [write.text, [["1", "import argparse"], ["2", ""], ["3", "    records = []"]]],
    ] as const) {
      await store.openRepositoryDiff("main.py", text);
      await flushPromises();
      const rows = wrapper.findAll(".diff-line:not(.is-hunk):not(.is-meta)");
      expect(rows.map((row) => [row.get(".diff-line-number").text(), row.get(".diff-line-text").element.textContent])).toEqual(expected);
      await vi.waitFor(() => expect(wrapper.find(".diff-line-text [class^='tok-']").exists()).toBe(true));
      expect(rows.map((row) => row.get(".diff-line-text").element.textContent)).toEqual(expected.map((row) => row[1]));
    }
    wrapper.unmount();
  });

  it("identifies a recorded conversation diff as this session", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-session-diff", title: "Session diff", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: false, generation: 0,
      }],
      activeThreadId: "thread-session-diff",
      ...panelState("thread-session-diff", { kind: "diff", path: "main.go", diff: { path: "main.go", working: "@@ -1 +1 @@\n-old\n+new", session: true } }),
    });
    store.refreshActiveRepository = vi.fn().mockResolvedValue(undefined);

    const wrapper = mount(InspectorPanel, { global: { plugins: [pinia] } });
    await flushPromises();

    expect(wrapper.get(".diff-section header").text()).toBe("This session");
    expect(wrapper.get('[aria-label="File path"]').text()).toContain("main.go");
    expect(wrapper.classes()).toContain("panel-workbench");
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
    await flushPromises();

    expect(wrapper.findAll('[role="tab"]')[0].text()).toBe("Files");
    expect(wrapper.find('[title="Show branches"]').exists()).toBe(false);
    expect(wrapper.find('button[title="Preview src/main.go"]').exists()).toBe(true);
    expect(wrapper.get('button[title="Preview src/main.go"]').classes()).toContain("is-changed");
    expect(wrapper.get('button[title="Preview src/main.go"]').attributes("data-status")).toBe("M");
    expect(wrapper.find('button[title="Preview README.md"]').exists()).toBe(true);

    await wrapper.get('button[title="Preview README.md"]').trigger("click");
    await flushPromises();
    expect(repositoryMocks.previewFile).toHaveBeenCalledWith("D:\\repo", "README.md");
    expect(wrapper.findAll('[role="tab"]')).toHaveLength(2);
    expect(wrapper.find(".panel-pathbar").exists()).toBe(true);
    expect(wrapper.get('[aria-label="File path"]').text()).toContain("repo");
    expect(wrapper.get('[aria-label="File path"]').text()).toContain("README.md");
    expect(wrapper.text()).toContain("# Repo");
    await wrapper.get('.panel-tab.is-active .panel-tab-close').trigger("click");
    expect(wrapper.find(".panel-pathbar").exists()).toBe(false);

    expect(wrapper.find(".repository-file-controls").exists()).toBe(false);
    expect(wrapper.find('input[type="search"]').exists()).toBe(true);
    expect(wrapper.find('button[title="Preview README.md"]').exists()).toBe(true);
    expect(wrapper.find('button[title="Preview src/main.go"]').exists()).toBe(true);
    await wrapper.get('button[title="Mention file"]').trigger("click");
    expect(store.activeDraft).toBe("@src/main.go ");

    await wrapper.get('button[title="View diff for src/main.go"]').trigger("click");
    await flushPromises();
    expect(repositoryMocks.diff).toHaveBeenCalledWith("D:\\repo", "src/main.go");
    expect(wrapper.get('[aria-label="Working tree diff"]').text()).toContain("new");
    const deletion = wrapper.get(".diff-line.is-deletion");
    const addition = wrapper.get(".diff-line.is-addition");
    const context = wrapper.findAll(".diff-line").find((line) => line.get(".diff-line-text").text() === "keep");
    expect(context?.get(".diff-line-number").text()).toBe("4");
    expect(deletion.get(".diff-line-number").text()).toBe("5");
    expect(deletion.get(".diff-line-text").text()).toBe("old");
    expect(addition.get(".diff-line-number").text()).toBe("5");
    expect(addition.get(".diff-line-text").text()).toBe("new");
    await vi.waitFor(() => expect(wrapper.find(".diff-line-text [class^='tok-']").exists()).toBe(true));
    expect(wrapper.get('[aria-label="File path"]').text()).toContain("src");
    await wrapper.get(".panel-tab.is-active .panel-tab-close").trigger("click");

    expect(wrapper.find(".context-panel").exists()).toBe(false);
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
    await flushPromises();

    expect(wrapper.find('button[title="Preview new.png"]').exists()).toBe(true);
    expect(wrapper.get('button[title="View diff for new.png"]').text()).toBe("R");
    store.repositoryStaleByWorkspace["d:/repo"] = true;
    store.repositoryErrorByWorkspace["d:/repo"] = "remote repository is disconnected or stale";
    await wrapper.vm.$nextTick();
    expect(wrapper.text()).toContain("remote repository is disconnected or stale");
    store.repositoryErrorByWorkspace["d:/repo"] = "";

    store.openPanelTab({ id: "binary", kind: "diff", title: "new.png", path: "new.png", loading: true });
    await wrapper.vm.$nextTick();
    expect(wrapper.find(".repository-diff .is-spinning").exists()).toBe(true);

    store.activePanelTab!.loading = false;
    store.activePanelTab!.error = "diff unavailable";
    await wrapper.vm.$nextTick();
    expect(wrapper.text()).toContain("diff unavailable");

    store.activePanelTab!.error = "";
    store.activePanelTab!.diff = {
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
      ...panelState("thread-remote", { kind: "file", path: "README.md", preview: { path: "README.md", absolutePath: "", content: "remote", size: 6, binary: false, truncated: false } }),
    });
    store.refreshActiveRepository = vi.fn().mockResolvedValue(undefined);

    const wrapper = mount(InspectorPanel, { global: { plugins: [pinia] } });
    await flushPromises();

    expect(wrapper.find('button[title="Open file"]').exists()).toBe(false);
    expect(wrapper.find('button[title="Show in file manager"]').exists()).toBe(false);
    expect(wrapper.find(".panel-open-split").exists()).toBe(false);
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
      ...panelState("thread-preview", { kind: "file", path: "scripts/join_groups.py", preview: {
        path: "scripts/join_groups.py", absolutePath: "D:\\repo\\scripts\\join_groups.py", content: "print('ok')", size: 12, binary: false, truncated: false,
      } }),
    });
    store.refreshActiveRepository = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(InspectorPanel, { global: { plugins: [pinia] } });
    await flushPromises();

    expect(wrapper.text()).toContain("join_groups.py");
    expect(wrapper.get('[aria-label="File path"]').text()).toContain("scripts");
    expect(wrapper.text()).not.toContain("D:\\repo\\scripts\\join_groups.py");
    expect(wrapper.text()).toContain("print('ok')");
    expect(wrapper.find(".file-preview-tabs").exists()).toBe(false);
    expect(wrapper.find(".file-preview-meta").exists()).toBe(false);
    expect(wrapper.get('[aria-label="File preview content"]').classes()).toEqual(expect.arrayContaining(["rounded-none!", "border-0!"]));
    await wrapper.get('.panel-tab.is-active .panel-tab-close').trigger("click");
    expect(store.activeRepositoryFilePreviewPath).toBe("");
  });

  it("renders media previews without redundant file metadata", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "thread-media", title: "Media", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: false, generation: 0 }],
      activeThreadId: "thread-media",
      ...panelState("thread-media", { kind: "file", path: "image.png", preview: {
        path: "image.png", absolutePath: "D:\\repo\\image.png", mediaType: "image/png", dataUrl: "data:image/png;base64,iVBORw0KGgo=", size: 8, binary: true, truncated: false,
      } }),
    });
    store.refreshActiveRepository = vi.fn().mockResolvedValue(undefined);
    repositoryMocks.previewFile.mockResolvedValue({ path: "README.md", absolutePath: "D:\\repo\\README.md", mediaType: "text/markdown", content: "# Repo", size: 6 });
    const wrapper = mount(InspectorPanel, { global: { plugins: [pinia] } });
    await flushPromises();

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

  it("renders spreadsheet cells and switches worksheets", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "thread-sheet", title: "Workbook", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: false, generation: 0 }],
      activeThreadId: "thread-sheet",
      ...panelState("thread-sheet", { kind: "file", path: "reports/summary.xlsx", preview: {
        path: "reports/summary.xlsx",
        absolutePath: "D:\\repo\\reports\\summary.xlsx",
        mediaType: "application/x-pi-desk-spreadsheet",
        content: JSON.stringify({ sheets: [
          { name: "Summary", columns: 2, rows: [["Name", "Amount"], ["Alpha", "12"]] },
          { name: "Details", columns: 2, rows: [["Item", "State"], ["Browser", "Done"]] },
        ] }),
        size: 128,
        binary: true,
        truncated: false,
      } }),
    });
    store.refreshActiveRepository = vi.fn().mockResolvedValue(undefined);

    const wrapper = mount(InspectorPanel, { global: { plugins: [pinia] } });
    await flushPromises();

    expect(wrapper.get('[aria-label="File path"]').text()).toContain("reports");
    expect(wrapper.get('[aria-label="File path"]').text()).toContain("summary.xlsx");
    expect(wrapper.get(".spreadsheet-grid").text()).toContain("Alpha");
    expect(wrapper.findAll('.spreadsheet-tabs [role="tab"]')).toHaveLength(2);
    expect(wrapper.findAll('.spreadsheet-tabs [role="tab"]')[0].attributes("aria-selected")).toBe("true");

    await wrapper.findAll('.spreadsheet-tabs [role="tab"]')[1].trigger("click");

    expect(wrapper.get(".spreadsheet-grid").text()).toContain("Browser");
    expect(wrapper.get(".spreadsheet-grid").text()).not.toContain("Alpha");
    expect(wrapper.findAll('.spreadsheet-tabs [role="tab"]')[1].attributes("aria-selected")).toBe("true");
  });

  it("requires a second click before rolling back a session-touched file", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-rollback", title: "Rollback", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: false, generation: 0,
      }],
      activeThreadId: "thread-rollback",
      repositoryByWorkspace: { "d:/repo": {
        files: [{ path: "src/main.go", name: "main.go" }],
        git: { isRepository: true, branch: "main", files: [{ path: "src/main.go", indexStatus: " ", worktreeStatus: "M" }] },
      } },
      sessionChangesByThread: { "thread-rollback": [{ path: "src/main.go", editCalls: 1, writeCalls: 0, plan: "revert-edits" }] },
    });
    store.refreshActiveRepository = vi.fn().mockResolvedValue(undefined);
    store.rollbackSessionFile = vi.fn().mockResolvedValue(undefined);

    const wrapper = mount(InspectorPanel, { global: { plugins: [pinia] } });
    await flushPromises();
    const rollback = wrapper.get('button.file-tree-rollback');
    expect(rollback.attributes("title")).toBe("Roll back session changes");

    await rollback.trigger("click");
    expect(store.rollbackSessionFile).not.toHaveBeenCalled();
    expect(rollback.attributes("title")).toContain("Click again to reverse this session's edits");

    await rollback.trigger("click");
    expect(store.rollbackSessionFile).toHaveBeenCalledWith("src/main.go");
  });

  it("describes the delete plan for session-created files", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-created", title: "Created", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: false, generation: 0,
      }],
      activeThreadId: "thread-created",
      repositoryByWorkspace: { "d:/repo": {
        files: [{ path: "notes.md", name: "notes.md" }],
        git: { isRepository: true, branch: "main", files: [{ path: "notes.md", indexStatus: "?", worktreeStatus: "?" }] },
      } },
      sessionChangesByThread: { "thread-created": [{ path: "notes.md", editCalls: 0, writeCalls: 1, plan: "delete" }] },
    });
    store.refreshActiveRepository = vi.fn().mockResolvedValue(undefined);
    store.rollbackSessionFile = vi.fn().mockResolvedValue(undefined);

    const wrapper = mount(InspectorPanel, { global: { plugins: [pinia] } });
    await flushPromises();
    const rollback = wrapper.get('button.file-tree-rollback');
    expect(rollback.attributes("title")).toBe("Roll back session changes");

    await rollback.trigger("click");
    expect(rollback.attributes("title")).toBe("Click again to delete the file created by this session");
  });
});
