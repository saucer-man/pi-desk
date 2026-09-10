import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { editorViewCtx, type Editor } from "@milkdown/core";
import { TextSelection } from "@milkdown/prose/state";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "../stores/app";
import { prepareImage } from "../utils/imageAttachments";
import { repositoryService } from "../services/repository";
import ComposerBar from "./ComposerBar.vue";
import MarkdownEditorCore from "./MarkdownEditorCore.vue";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/modelconfig", () => ({ modelConfigService: { selectable: vi.fn().mockResolvedValue([]) } }));
vi.mock("../services/repository", () => ({ repositoryService: { clipboardFiles: vi.fn().mockResolvedValue([]) } }));
vi.mock("../utils/imageAttachments", () => ({
  MAX_ATTACHED_IMAGES: 10,
  MAX_SOURCE_IMAGE_BYTES: 10 * 1024 * 1024,
  MAX_IMAGE_BASE64_CHARS: 16_000_000,
  prepareImage: vi.fn(async (file: File) => ({
    id: "pasted-image",
    name: file.name,
    data: "aW1hZ2U=",
    mimeType: file.type,
    previewUrl: `data:${file.type};base64,aW1hZ2U=`,
  })),
}));

function elementRect(left: number, top: number, width: number, height: number): DOMRect {
  return {
    x: left, y: top, left, top, width, height,
    right: left + width, bottom: top + height,
    toJSON: () => ({}),
  } as DOMRect;
}

describe("ComposerBar", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.mocked(repositoryService.clipboardFiles).mockReset().mockResolvedValue([]);
  });

  it("pastes file references into the queue editor selection", async () => {
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "paste", title: "Paste", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "running", started: true, generation: 1 }],
      activeThreadId: "paste",
      pendingPromptsByThread: { paste: [{ id: "queued", text: "Review old please", images: [], createdAt: "2026-09-04T00:00:00Z" }] },
    });
    vi.mocked(repositoryService.clipboardFiles).mockResolvedValue([{ path: "a b.pdf", name: "a b.pdf" }]);
    const wrapper = mount(ComposerBar);
    await flushPromises();
    await wrapper.get('.queue-actions button[title="Edit queued message"]').trigger("click");
    const input = wrapper.get<HTMLInputElement>('input[aria-label="Edit queued message"]');
    input.element.setSelectionRange(7, 11);
    await input.trigger("paste", { clipboardData: { items: [], getData: () => "" } });
    await flushPromises();
    expect(input.element.value).toBe('Review @"a b.pdf" please');
    expect(store.activeDraft).toBe("");
    wrapper.unmount();
  });

  it("pastes Explorer files at the caret even when the browser exposes no File objects", async () => {
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "paste", title: "Paste", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: true, generation: 1 }],
      activeThreadId: "paste", draftsByThread: { paste: "Review:" },
    });
    vi.mocked(repositoryService.clipboardFiles).mockResolvedValue([{ path: "docs/需求 文档.pdf", name: "需求 文档.pdf" }, { path: "main.go", name: "main.go" }]);
    const wrapper = mount(ComposerBar);
    await flushPromises();
    const setup = wrapper.findComponent(MarkdownEditorCore).vm.$ as unknown as { setupState: { get(): Editor | undefined } };
    const view = setup.setupState.get()?.action((ctx) => ctx.get(editorViewCtx));
    if (!view) throw new Error("editor not ready");
    view.dispatch(view.state.tr.setSelection(TextSelection.atEnd(view.state.doc)));
    await wrapper.get("[contenteditable='true']").trigger("paste", { clipboardData: { items: [], getData: () => "" } });
    await flushPromises();
    expect(wrapper.get("[contenteditable='true']").text()).toBe('Review: @"docs/需求 文档.pdf" @main.go');
    expect(store.activeDraft).toContain('@"docs/需求 文档.pdf" @main.go');
    expect(store.activeAttachments).toEqual([]);
    wrapper.unmount();
  });

  it("does not overwrite a newer draft or insert a delayed paste into another task", async () => {
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "paste", title: "Paste", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: true, generation: 1 }],
      activeThreadId: "paste", draftsByThread: { paste: "original" },
    });
    let complete!: (files: { path: string; name: string }[]) => void;
    vi.mocked(repositoryService.clipboardFiles).mockImplementation(() => new Promise((resolve) => { complete = resolve; }));
    const wrapper = mount(ComposerBar);
    await flushPromises();
    await wrapper.get("[contenteditable='true']").trigger("paste", { clipboardData: { items: [], getData: () => "" } });
    expect(wrapper.get(".send-button").attributes("disabled")).toBeDefined();
    store.updateDraft("newer");
    await flushPromises();
    complete([{ path: "main.go", name: "main.go" }]);
    await flushPromises();
    expect(store.activeDraft).toBe("newer");
    expect(wrapper.get(".attachment-error").text()).toMatch(/draft changed|输入内容发生/);
    await wrapper.get("[contenteditable='true']").trigger("paste", { clipboardData: { items: [], getData: () => "" } });
    store.activeThreadId = "another";
    store.activeThreadId = "paste";
    complete([{ path: "main.go", name: "main.go" }]);
    await flushPromises();
    expect(store.activeDraft).toBe("newer");
    wrapper.unmount();
  });

  it("keeps plain path text editable and surfaces a host rejection without inserting files", async () => {
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "paste", title: "Paste", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: true, generation: 1 }],
      activeThreadId: "paste",
    });
    const wrapper = mount(ComposerBar);
    await flushPromises();
    await wrapper.get("[contenteditable='true']").trigger("paste", { clipboardData: { items: [], getData: () => "/plain/path.txt" } });
    await flushPromises();
    expect(store.activeDraft).toBe("/plain/path.txt");
    vi.mocked(repositoryService.clipboardFiles).mockRejectedValue(new Error("copied files must be inside the current workspace"));
    await wrapper.get("[contenteditable='true']").trigger("paste", { clipboardData: { items: [], getData: () => "" } });
    await flushPromises();
    expect(store.activeDraft).toBe("/plain/path.txt");
    expect(wrapper.get(".attachment-error").text()).toContain("inside the current workspace");
    wrapper.unmount();
  });

  it("disables sending while a history operation is active", async () => {
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "thread", title: "Task", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: true, generation: 1 }],
      activeThreadId: "thread",
      draftsByThread: { thread: "Do not send during deletion" },
      sessionOperationByThread: { thread: "Deleting message" },
    });
    store.sendActivePrompt = vi.fn();
    const wrapper = mount(ComposerBar);
    expect(wrapper.get(".send-button").attributes("disabled")).toBeDefined();
    await wrapper.get(".send-button").trigger("click");
    expect(store.sendActivePrompt).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("navigates commands and exposes the editable local queue and retry controls", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-1", title: "Audit", workspace: "repo", workspacePath: "D:\\repo", trust: "deny",
        status: "running", started: true, generation: 1,
      }],
      activeThreadId: "thread-1",
      draftsByThread: { "thread-1": "/" },
      commandsByThread: { "thread-1": [
        { name: "compact", description: "Compact context", source: "extension" },
        { name: "review", description: "Review changes", source: "prompt" },
      ] },
      pendingPromptsByThread: { "thread-1": [
        { id: "pending-1", text: "Inspect logs", images: [], createdAt: "2026-08-12T00:00:00Z" },
        { id: "pending-2", text: "Run tests", images: [], createdAt: "2026-08-12T00:00:01Z" },
      ] },
      retryByThread: { "thread-1": { attempt: 2, maxAttempts: 4, delayMs: 500, errorMessage: "rate limited" } },
      repositoryByWorkspace: { "d:/repo": {
        files: [{ path: "src/main.ts", name: "main.ts" }, { path: "src/view.ts", name: "view.ts" }],
        git: { isRepository: true, branch: "main", files: [] },
      } },
    });
    store.sendActivePrompt = vi.fn().mockResolvedValue(undefined);
    store.steerPendingPrompt = vi.fn().mockResolvedValue(undefined);
    store.abortActiveRetry = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(ComposerBar, { global: { plugins: [pinia] } });

    expect(wrapper.find("textarea").exists()).toBe(false);
    expect(wrapper.find(".composer-markdown-layer").exists()).toBe(false);
    expect(wrapper.text()).toContain("Inspect logs");
    expect(wrapper.text()).toContain("Run tests");
    expect(wrapper.text()).toContain("Retry 2 of 4");
    expect(wrapper.find(".retry-banner .is-spinning").exists()).toBe(false);
    // Tailwind utilities are !important and would override the banner's vertical centering.
    expect(wrapper.get(".retry-banner").classes()).not.toContain("items-start");
    expect(wrapper.get(".retry-banner").element.nextElementSibling).toBe(wrapper.get(".composer-input-stack").element);
    expect(wrapper.get(".queue-panel").element.nextElementSibling).toBe(wrapper.get(".composer").element);
    expect(wrapper.get(".queue-text").attributes("title")).toBe("Inspect logs");
    const firstQueueActions = wrapper.findAll(".queue-actions")[0];
    expect(firstQueueActions.findAll("button").map((button) => button.attributes("title"))).toEqual([
      "Send this message now",
      "Edit queued message",
      "Delete queued message",
    ]);

    await flushPromises();
    const editor = wrapper.get("[contenteditable='true']");
    for (let index = 0; index < 3; index += 1) await editor.trigger("keydown", { key: "ArrowDown" });
    await editor.trigger("keydown", { key: "Enter" });
    expect(editor.element.textContent).toBe("/review ");
    expect(store.activeDraft).toBe("/review ");
    expect(store.sendActivePrompt).not.toHaveBeenCalled();

    store.updateDraft("Check @view");
    await flushPromises();
    await editor.trigger("keydown", { key: "Enter" });
    expect(store.activeDraft).toBe("Check @src/view.ts ");
    expect(store.sendActivePrompt).not.toHaveBeenCalled();

    await wrapper.get('button[title="Edit queued message"]').trigger("click");
    await wrapper.get('input[aria-label="Edit queued message"]').setValue("Inspect build logs");
    await wrapper.get('button[title="Save edit"]').trigger("click");
    expect(store.activePendingPrompts[0].text).toBe("Inspect build logs");

    await wrapper.get('button[title="Send this message now"]').trigger("click");
    expect(store.steerPendingPrompt).toHaveBeenCalledWith("pending-1");

    await wrapper.get('button[title="Delete queued message"]').trigger("click");
    expect(store.activePendingPrompts).toHaveLength(1);

    await wrapper.get('button[title="Stop retry"]').trigger("click");
    expect(store.abortActiveRetry).toHaveBeenCalledOnce();
  });

  it("stacks the dedicated ordered todo above a matching queue with no intervening element", () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-todo", title: "Todo", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "running", started: true, generation: 1,
      }],
      activeThreadId: "thread-todo",
      extensionWidgetsByThread: { "thread-todo": {
        notes: { key: "notes", lines: ["Generic notes"], placement: "aboveEditor" },
        "pi-desk-todo": {
          key: "pi-desk-todo", instance: "turn-2", placement: "aboveEditor",
          lines: ["── 待办 ──", "[ ] #2 second", "── 已完成 ──", "[x] #1 first"],
        },
      } },
      pendingPromptsByThread: { "thread-todo": [
        { id: "pending", text: "Queued work", images: [], createdAt: "2026-08-18T00:00:00Z" },
      ] },
      retryByThread: { "thread-todo": { attempt: 1, maxAttempts: 3, delayMs: 100 } },
    });

    const wrapper = mount(ComposerBar, { global: { plugins: [pinia] } });
    const generic = wrapper.get(".extension-widget");
    const retry = wrapper.get(".retry-banner");
    const stack = wrapper.get(".composer-input-stack");
    const todo = wrapper.get(".pi-desk-todo-panel");
    const queue = wrapper.get(".queue-panel");
    const composer = wrapper.get(".composer");

    expect(generic.text()).toBe("Generic notes");
    expect(generic.element.nextElementSibling).toBe(retry.element);
    expect(retry.element.nextElementSibling).toBe(stack.element);
    expect(stack.classes()).toEqual(expect.arrayContaining(["has-todo", "has-queue"]));
    expect(todo.element.nextElementSibling).toBe(queue.element);
    expect(queue.element.nextElementSibling).toBe(composer.element);
    expect(wrapper.findAll(".pi-desk-todo-row").map((row) => row.text())).toEqual(["#1first", "#2second"]);
    expect(wrapper.findAll('.extension-widget pre')).toHaveLength(1);
  });

  it("shows supported RPC commands with their sources and omits todo and llama", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-commands", title: "Commands", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: true, generation: 1,
      }],
      activeThreadId: "thread-commands",
      draftsByThread: { "thread-commands": "/" },
      commandsByThread: { "thread-commands": [
        { name: "todo", description: "Show todos", source: "extension" },
        { name: "llama", description: "Manage llama.cpp router models", source: "extension" },
        ...Array.from({ length: 12 }, (_, index) => ({
          name: `command-${index + 1}`,
          description: `Command ${index + 1}`,
          source: index === 11 ? "skill" as const : "extension" as const,
          path: index === 11 ? "C:\\skills\\command-12\\SKILL.md" : `C:\\extensions\\command-${index + 1}.ts`,
        })),
      ] },
    });

    const wrapper = mount(ComposerBar, { global: { plugins: [pinia] } });
    vi.spyOn(wrapper.get(".composer").element, "getBoundingClientRect").mockReturnValue(elementRect(100, 500, 820, 108));
    const commandButton = wrapper.get('.composer-command-button[title="Commands"]');

    expect(wrapper.get(".composer").classes()).toContain("!overflow-visible");
    expect(wrapper.find(".completion-menu").classes()).toContain("!overflow-y-auto");
    await commandButton.trigger("click");
    await flushPromises();
    const commandMenu = wrapper.get<HTMLElement>('.completion-menu[aria-label="Commands"]');
    const commands = wrapper.findAll('.completion-menu[aria-label="Commands"] button');
    expect(commandMenu.classes()).toEqual(expect.arrayContaining(["!fixed", "!overflow-y-auto"]));
    expect(commandMenu.element.style.bottom).toBe(`${window.innerHeight - 500 + 8}px`);
    expect(window.innerHeight - Number.parseFloat(commandMenu.element.style.bottom)).toBeLessThan(500);

    expect(commands).toHaveLength(14);
    expect(wrapper.text()).not.toContain("/todo");
    expect(wrapper.text()).not.toContain("/llama");
    expect(commands[0].text()).toContain("/skill");
    expect(commands[0].text()).toContain("Pi Desk");
    expect(commands[1].text()).toContain("/prompt");
    expect(commands[13].text()).toContain("/command-12");
    expect(commands[13].text()).toContain("Skill");
    expect(commands[13].attributes("title")).toBe("Skill · C:\\skills\\command-12\\SKILL.md");

    const editor = wrapper.get(".composer-editor");
    for (let index = 0; index < 13; index += 1) await editor.trigger("keydown", { key: "ArrowDown" });
    expect(commands[13].attributes("aria-selected")).toBe("true");
  });

  it("opens skill and prompt management from local slash commands without sending a prompt", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-local-commands", title: "Commands", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: true, generation: 1,
      }],
      activeThreadId: "thread-local-commands",
      draftsByThread: { "thread-local-commands": "/skill" },
    });
    store.sendActivePrompt = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(ComposerBar, { global: { plugins: [pinia] } });

    await wrapper.get('.completion-menu[aria-label="Commands"] button').trigger("click");
    expect(store.settingsOpen).toBe(true);
    expect(store.settingsSection).toBe("skillManagement");
    expect(store.activeDraft).toBe("");
    expect(store.sendActivePrompt).not.toHaveBeenCalled();

    store.settingsOpen = false;
    store.updateDraft("/prompt");
    await flushPromises();
    await wrapper.get('.completion-menu[aria-label="Commands"] button').trigger("click");
    expect(store.settingsOpen).toBe(true);
    expect(store.settingsSection).toBe("promptManagement");
    expect(store.activeDraft).toBe("");
    expect(store.sendActivePrompt).not.toHaveBeenCalled();
  });

  it("opens the command button without changing the draft and adds multiple runtime commands", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-command-button", title: "Commands", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: true, generation: 1,
      }],
      activeThreadId: "thread-command-button",
      draftsByThread: { "thread-command-button": "Prepare focused tests" },
      commandsByThread: { "thread-command-button": [
        { name: "review", description: "Review changes", source: "prompt" },
        { name: "compact", description: "Compact context", source: "extension" },
      ] },
    });
    store.refreshCommands = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(ComposerBar, { global: { plugins: [pinia] } });
    const commandButton = wrapper.get('.composer-command-button[title="Commands"]');

    expect(wrapper.get(".composer-tools").element.firstElementChild).toBe(commandButton.element);
    await commandButton.trigger("click");
    await flushPromises();
    expect(store.activeDraft).toBe("Prepare focused tests");
    expect(wrapper.findAll('.completion-menu[aria-label="Commands"] button')).toHaveLength(4);

    const review = wrapper.findAll('.completion-menu[aria-label="Commands"] button').find((button) => button.text().includes("/review"));
    if (!review) throw new Error("review command was not rendered");
    await review.trigger("click");
    expect(store.activeDraft).toBe("/review Prepare focused tests");

    store.updateDraft("/review keep these arguments");
    await flushPromises();
    await commandButton.trigger("click");
    const compact = wrapper.findAll('.completion-menu[aria-label="Commands"] button').find((button) => button.text().includes("/compact"));
    if (!compact) throw new Error("compact command was not rendered");
    await compact.trigger("click");
    expect(store.activeDraft).toBe("/review /compact keep these arguments");
    expect(store.refreshCommands).toHaveBeenCalledTimes(2);
  });

  it("completes multiple slash commands without changing the surrounding draft", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-multiple-commands", title: "Commands", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: true, generation: 1,
      }],
      activeThreadId: "thread-multiple-commands",
      draftsByThread: { "thread-multiple-commands": "/review /ski" },
      commandsByThread: { "thread-multiple-commands": [
        { name: "review", description: "Review changes", source: "prompt" },
        { name: "skill:grill", description: "Stress-test a plan", source: "skill" },
        { name: "compact", description: "Compact context", source: "extension" },
      ] },
    });
    const wrapper = mount(ComposerBar, { global: { plugins: [pinia] } });

    const skill = wrapper.findAll('.completion-menu[aria-label="Commands"] button').find((button) => button.text().includes("/skill:grill"));
    if (!skill) throw new Error("skill command was not rendered");
    await skill.trigger("click");
    expect(store.activeDraft).toBe("/review /skill:grill ");

    store.updateDraft("/review /skill:grill Keep the request /com");
    await flushPromises();
    const compact = wrapper.findAll('.completion-menu[aria-label="Commands"] button').find((button) => button.text().includes("/compact"));
    if (!compact) throw new Error("compact command was not rendered");
    await compact.trigger("click");
    expect(store.activeDraft).toBe("/review /skill:grill Keep the request /compact ");
  });

  it("preserves a normal draft when the command button opens Pi Desk management", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-desktop-command-button", title: "Commands", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: true, generation: 1,
      }],
      activeThreadId: "thread-desktop-command-button",
      draftsByThread: { "thread-desktop-command-button": "Keep this draft" },
    });
    store.refreshCommands = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(ComposerBar, { global: { plugins: [pinia] } });
    const commandButton = wrapper.get('.composer-command-button[title="Commands"]');

    await commandButton.trigger("click");
    document.dispatchEvent(new Event("pointerdown", { bubbles: true }));
    await flushPromises();
    expect(wrapper.find('.completion-menu[aria-label="Commands"]').exists()).toBe(false);
    expect(store.activeDraft).toBe("Keep this draft");

    await commandButton.trigger("click");
    await wrapper.get('.completion-menu[aria-label="Commands"] button').trigger("click");
    expect(store.settingsOpen).toBe(true);
    expect(store.settingsSection).toBe("skillManagement");
    expect(store.activeDraft).toBe("Keep this draft");
  });

  it("edits queued images without mutating them before save", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    const originalImage = { id: "original-image", name: "original.png", data: "b3JpZ2luYWw=", mimeType: "image/png", previewUrl: "data:image/png;base64,b3JpZ2luYWw=" };
    store.$patch({
      threads: [{
        id: "thread-1", title: "Audit", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "running", started: true, generation: 1,
      }],
      activeThreadId: "thread-1",
      pendingPromptsByThread: { "thread-1": [{ id: "pending-image", text: "Inspect capture", images: [originalImage], createdAt: "2026-08-12T00:00:00Z" }] },
    });
    const wrapper = mount(ComposerBar, { global: { plugins: [pinia] } });

    await wrapper.get('button[title="Edit queued message"]').trigger("click");
    expect(wrapper.get(".queue-editor-image img").attributes("alt")).toBe("original.png");
    await wrapper.get('button[title="Remove queued image"]').trigger("click");
    expect(store.activePendingPrompts[0].images).toEqual([originalImage]);
    await wrapper.get('button[title="Cancel edit"]').trigger("click");
    expect(store.activePendingPrompts[0].images).toEqual([originalImage]);

    await wrapper.get('button[title="Edit queued message"]').trigger("click");
    await wrapper.get('button[title="Remove queued image"]').trigger("click");
    const pasted = new File(["replacement"], "replacement.png", { type: "image/png" });
    await wrapper.get('input[aria-label="Edit queued message"]').trigger("paste", {
      clipboardData: { items: [{ kind: "file", type: "image/png", getAsFile: () => pasted }] },
    });
    await flushPromises();

    expect(wrapper.get(".queue-editor-image img").attributes("alt")).toBe("replacement.png");
    await wrapper.get('button[title="Save edit"]').trigger("click");
    expect(store.activePendingPrompts[0]).toMatchObject({
      text: "Inspect capture",
      images: [expect.objectContaining({ id: "pasted-image", name: "replacement.png" })],
    });
  });

  it("moves an edited queued message into the composer without discarding the current draft", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    const queuedImage = { id: "queued-image", name: "queued.png", data: "cXVldWVk", mimeType: "image/png", previewUrl: "data:image/png;base64,cXVldWVk" };
    const draftImage = { id: "draft-image", name: "draft.png", data: "ZHJhZnQ=", mimeType: "image/png", previewUrl: "data:image/png;base64,ZHJhZnQ=" };
    store.$patch({
      threads: [{
        id: "thread-move-queue", title: "Queue", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "running", started: true, generation: 1,
      }],
      activeThreadId: "thread-move-queue",
      draftsByThread: { "thread-move-queue": "Keep this draft" },
      attachmentsByThread: { "thread-move-queue": [draftImage] },
      pendingPromptsByThread: { "thread-move-queue": [{ id: "pending-move", text: "Queued text", images: [queuedImage], createdAt: "2026-08-12T00:00:00Z" }] },
    });
    const wrapper = mount(ComposerBar, { global: { plugins: [pinia] } });

    expect(wrapper.get(".queue-steer span").text()).toBe("Send now");
    await wrapper.get('button[title="Edit queued message"]').trigger("click");
    await wrapper.get('input[aria-label="Edit queued message"]').setValue("Edited queued text");
    await wrapper.get('button[title="Send to the editor for further editing"]').trigger("click");
    await flushPromises();

    expect(store.activeDraft).toBe("Edited queued text");
    expect(store.activeAttachments).toEqual([queuedImage]);
    expect(store.activePendingPrompts).toHaveLength(1);
    expect(store.activePendingPrompts[0]).toMatchObject({ text: "Keep this draft", images: [draftImage] });
    expect(wrapper.find(".queue-editor").exists()).toBe(false);
  });

  it("opens an input attachment preview and keeps removal separate", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    const image = {
      id: "input-image", name: "diagram.png", data: "ZGlhZ3JhbQ==", mimeType: "image/png",
      previewUrl: "data:image/png;base64,ZGlhZ3JhbQ==",
    };
    store.$patch({
      threads: [{
        id: "thread-image", title: "Image", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: true, generation: 1,
      }],
      activeThreadId: "thread-image",
      attachmentsByThread: { "thread-image": [image] },
    });

    const host = document.createElement("div");
    document.body.appendChild(host);
    const wrapper = mount(ComposerBar, { attachTo: host, global: { plugins: [pinia] } });

    await wrapper.get(".attachment-preview-open").trigger("click");
    await flushPromises();
    const dialog = document.body.querySelector<HTMLElement>(".image-preview-dialog");
    expect(dialog?.getAttribute("role")).toBe("dialog");
    expect(dialog?.querySelector("h2")?.textContent).toBe("diagram.png");
    expect(dialog?.querySelector("img")?.getAttribute("src")).toBe(image.previewUrl);

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await flushPromises();
    expect(document.body.querySelector(".image-preview-dialog")).toBeNull();

    await wrapper.get(".attachment-preview-remove").trigger("click");
    expect(store.activeAttachments).toHaveLength(0);
    expect(document.body.querySelector(".image-preview-dialog")).toBeNull();
    wrapper.unmount();
    host.remove();
  });

  it("keeps very long queued text in the shrinkable text column", () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    const longText = "执行到一半".repeat(400);
    store.$patch({
      threads: [{
        id: "thread-1", title: "Audit", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "running", started: true, generation: 1,
      }],
      activeThreadId: "thread-1",
      pendingPromptsByThread: { "thread-1": [{
        id: "pending-long", text: longText, images: [{ id: "image-1", name: "capture.png", previewUrl: "data:image/png;base64,aW1hZ2U=", mimeType: "image/png", data: "aW1hZ2U=" }], createdAt: "2026-08-12T00:00:00Z",
      }] },
    });

    const wrapper = mount(ComposerBar, { global: { plugins: [pinia] } });
    const queuePanel = wrapper.get(".queue-panel");
    const composer = wrapper.get(".composer");
    const row = wrapper.get(".queue-row");
    const text = row.get(".queue-text");

    expect(queuePanel.element.parentElement).toBe(composer.element.parentElement);
    expect(queuePanel.element.nextElementSibling).toBe(composer.element);
    expect(queuePanel.element.contains(composer.element)).toBe(false);
    expect(text.attributes("title")).toBe(longText);
    expect(text.text()).toBe(longText);
    expect(row.findAll(".queue-text")).toHaveLength(1);
    expect(row.findAll(".queue-actions button")).toHaveLength(3);
    expect(row.get(".queue-thumbnail").attributes("alt")).toBe("capture.png");
  });

  it("queues a normal send while running", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-1", title: "Audit", workspace: "repo", workspacePath: "D:\\repo", trust: "deny",
        status: "running", started: true, generation: 1,
      }],
      activeThreadId: "thread-1",
      draftsByThread: { "thread-1": "Run the focused tests" },
    });
    store.sendActivePrompt = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(ComposerBar, { global: { plugins: [pinia] } });

    await wrapper.get(".send-button").trigger("click");

    expect(store.sendActivePrompt).toHaveBeenCalledOnce();
    expect(wrapper.get(".send-button").attributes("title")).toBe("Queue message");
  });

  it("sends with Enter, keeps Shift Enter for editing, and ignores composition and repeats", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-keyboard", title: "Keyboard", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: true, generation: 1,
      }],
      activeThreadId: "thread-keyboard",
      draftsByThread: { "thread-keyboard": "Keep editing" },
    });
    store.sendActivePrompt = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(ComposerBar, { global: { plugins: [pinia] } });
    await flushPromises();
    const editor = wrapper.get("[contenteditable='true']");

    await editor.trigger("keydown", { key: "Enter" });
    expect(store.sendActivePrompt).toHaveBeenCalledOnce();

    await editor.trigger("keydown", { key: "Enter", shiftKey: true });
    await editor.trigger("keydown", { key: "Enter", keyCode: 229 });
    await editor.trigger("keydown", { key: "Enter", isComposing: true });
    await editor.trigger("keydown", { key: "Enter", repeat: true });
    expect(store.sendActivePrompt).toHaveBeenCalledOnce();
  });

  it.each(["", "typescript", "c++", "objective-c", "CSharp"])("converts a typed fence (%s) on Enter and keeps code newlines in the draft", async (language) => {
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "code", title: "Code", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: true, generation: 1 }],
      activeThreadId: "code",
    });
    store.sendActivePrompt = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(ComposerBar);
    await flushPromises();
    const setup = wrapper.findComponent(MarkdownEditorCore).vm.$ as unknown as { setupState: { get(): Editor | undefined } };
    const view = setup.setupState.get()?.action((ctx) => ctx.get(editorViewCtx));
    if (!view) throw new Error("Milkdown editor did not start");
    view.dispatch(view.state.tr.insertText(`\`\`\`${language}`));
    await flushPromises();

    const editor = wrapper.get("[contenteditable='true']");
    await editor.trigger("keydown", { key: "Enter", code: "Enter" });
    expect(editor.find("pre code").exists()).toBe(true);
    expect(view.state.selection.$from.parent.attrs.language).toBe(language);
    view.dispatch(view.state.tr.insertText("first"));
    await editor.trigger("keydown", { key: "Enter", code: "Enter" });
    view.dispatch(view.state.tr.insertText("second"));
    await editor.trigger("keydown", { key: "Enter", code: "Enter", shiftKey: true });
    view.dispatch(view.state.tr.insertText("third"));
    await flushPromises();

    expect(editor.get("pre code").element.textContent).toBe("first\nsecond\nthird");
    expect(store.draftsByThread.code).toBe(`\`\`\`${language}\nfirst\nsecond\nthird\n\`\`\``);
    expect(store.sendActivePrompt).not.toHaveBeenCalled();
    await wrapper.get(".send-button").trigger("click");
    expect(store.sendActivePrompt).toHaveBeenCalledOnce();
    wrapper.unmount();
  });

  it("creates a code block after a soft line break and exits it with a closing fence", async () => {
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "code-lines", title: "Code lines", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: true, generation: 1 }],
      activeThreadId: "code-lines", draftsByThread: { "code-lines": "Before" },
    });
    store.sendActivePrompt = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(ComposerBar);
    await flushPromises();
    const setup = wrapper.findComponent(MarkdownEditorCore).vm.$ as unknown as { setupState: { get(): Editor | undefined } };
    const view = setup.setupState.get()?.action((ctx) => ctx.get(editorViewCtx));
    if (!view) throw new Error("Milkdown editor did not start");
    view.dispatch(view.state.tr.setSelection(TextSelection.atEnd(view.state.doc)));
    const editor = wrapper.get("[contenteditable='true']");
    await editor.trigger("keydown", { key: "Enter", code: "Enter", shiftKey: true });
    view.dispatch(view.state.tr.insertText("```"));
    await editor.trigger("keydown", { key: "Enter", code: "Enter" });

    expect(editor.findAll("p")[0].text()).toBe("Before");
    expect(editor.find("pre code").exists()).toBe(true);
    view.dispatch(view.state.tr.insertText("const ready = true;"));
    await editor.trigger("keydown", { key: "Enter", code: "Enter" });
    view.dispatch(view.state.tr.insertText("```"));
    await editor.trigger("keydown", { key: "Enter", code: "Enter" });

    expect(editor.findAll("pre")).toHaveLength(1);
    expect(editor.get("pre code").element.textContent).toBe("const ready = true;\n");
    expect(editor.findAll("p").at(-1)?.text()).toBe("");
    view.dispatch(view.state.tr.insertText("After"));
    expect(editor.findAll("p").at(-1)?.text()).toBe("After");
    expect(store.sendActivePrompt).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("creates a code block at the start of a new line inside a list item", async () => {
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "list-code", title: "List code", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: true, generation: 1 }],
      activeThreadId: "list-code", draftsByThread: { "list-code": "1. Before" },
    });
    const wrapper = mount(ComposerBar);
    await flushPromises();
    const setup = wrapper.findComponent(MarkdownEditorCore).vm.$ as unknown as { setupState: { get(): Editor | undefined } };
    const view = setup.setupState.get()?.action((ctx) => ctx.get(editorViewCtx));
    if (!view) throw new Error("Milkdown editor did not start");
    view.dispatch(view.state.tr.setSelection(TextSelection.atEnd(view.state.doc)));
    const editor = wrapper.get("[contenteditable='true']");
    await editor.trigger("keydown", { key: "Enter", code: "Enter", shiftKey: true });
    view.dispatch(view.state.tr.insertText("```"));
    await editor.trigger("keydown", { key: "Enter", code: "Enter" });
    expect(editor.find("li pre code").exists()).toBe(true);
    wrapper.unmount();
  });

  it.each(["1. first", "- first"])("continues and exits a list (%s) with Enter without sending", async (draft) => {
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "list", title: "List", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: true, generation: 1 }],
      activeThreadId: "list", draftsByThread: { list: draft },
    });
    store.sendActivePrompt = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(ComposerBar);
    await flushPromises();
    const setup = wrapper.findComponent(MarkdownEditorCore).vm.$ as unknown as { setupState: { get(): Editor | undefined } };
    const view = setup.setupState.get()?.action((ctx) => ctx.get(editorViewCtx));
    if (!view) throw new Error("Milkdown editor did not start");
    view.dispatch(view.state.tr.setSelection(TextSelection.atEnd(view.state.doc)));
    const editor = wrapper.get("[contenteditable='true']");
    await editor.trigger("keydown", { key: "Enter", code: "Enter" });
    view.dispatch(view.state.tr.insertText("second"));
    expect(editor.findAll("li").map((item) => item.text())).toEqual(["first", "second"]);
    await editor.trigger("keydown", { key: "Enter", code: "Enter" });
    await editor.trigger("keydown", { key: "Enter", code: "Enter" });
    expect(view.state.selection.$from.depth).toBe(1);
    expect(store.sendActivePrompt).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("renders pasted Markdown lists, bold, and code without interpreting code contents", async () => {
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "markdown", title: "Markdown", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "idle", started: true, generation: 1 }],
      activeThreadId: "markdown",
    });
    const wrapper = mount(ComposerBar);
    await flushPromises();
    const editor = wrapper.get("[contenteditable='true']");
    const text = "1. **first**\n2. second\n\n```html\n<br> **literal**\n```";
    await editor.trigger("paste", { clipboardData: { items: [], getData: () => text } });
    await flushPromises();
    expect(editor.findAll("ol > li")).toHaveLength(2);
    expect(editor.get("strong").text()).toBe("first");
    expect(editor.get("pre code").text()).toBe("<br> **literal**");
    expect(store.activeDraft).toContain("**first**");
    expect(store.activeDraft).toContain("<br> **literal**");
    wrapper.unmount();
  });

  it("shows Pi startup progress in the lower-left composer toolbar", () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-1", title: "Audit", workspace: "repo", workspacePath: "D:\\repo", trust: "deny",
        status: "starting", started: false, generation: 0,
      }],
      activeThreadId: "thread-1",
    });

    const wrapper = mount(ComposerBar, { global: { plugins: [pinia] } });

    expect(wrapper.get(".composer-starting").text()).toBe("Pi is starting");
    expect(wrapper.get(".send-button").attributes("disabled")).toBeDefined();
  });

  it("accepts pasted images without rendering an upload button", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-1", title: "Audit", workspace: "repo", workspacePath: "D:\\repo", trust: "deny",
        status: "idle", started: true, generation: 1,
      }],
      activeThreadId: "thread-1",
    });
    const wrapper = mount(ComposerBar, { global: { plugins: [pinia] } });
    const image = new File(["image"], "clipboard.png", { type: "image/png" });

    expect(wrapper.find('input[type="file"]').exists()).toBe(false);
    expect(wrapper.find('button[title="Attach images"]').exists()).toBe(false);

    await wrapper.get(".composer-editor").trigger("paste", {
      clipboardData: {
        items: [{ kind: "file", type: "image/png", getAsFile: () => image }],
      },
    });
    await flushPromises();

    expect(store.activeAttachments).toEqual([expect.objectContaining({ name: "clipboard.png", mimeType: "image/png" })]);
    expect(wrapper.get(".attachment-preview img").attributes("alt")).toBe("clipboard.png");
  });

  it("shows a readable error when pasted image preparation rejects with a browser event", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-1", title: "Audit", workspace: "repo", workspacePath: "D:\\repo", trust: "deny",
        status: "idle", started: true, generation: 1,
      }],
      activeThreadId: "thread-1",
    });
    vi.mocked(prepareImage).mockRejectedValueOnce(new Event("error"));
    const wrapper = mount(ComposerBar, { global: { plugins: [pinia] } });
    const image = new File(["image"], "clipboard.png", { type: "image/png" });

    await wrapper.get(".composer-editor").trigger("paste", {
      clipboardData: {
        items: [{ kind: "file", type: "image/png", getAsFile: () => image }],
      },
    });
    await flushPromises();

    expect(wrapper.get(".attachment-error").text()).toBe("Unable to prepare clipboard.png");
    expect(wrapper.text()).not.toContain("[object Event]");
  });

  it("keeps the combined menu open and replaces efforts after selecting a model", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    const gpt = { id: "gpt-5.6-sol", name: "GPT 5.6 Sol", provider: "openai", contextWindow: 200_000 };
    const grok = { id: "grok-4.6", name: "Grok 4.6", provider: "grok", contextWindow: 1_000_000 };
    store.$patch({
      threads: [{
        id: "thread-model-menu", title: "Models", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: true, generation: 1,
      }],
      activeThreadId: "thread-model-menu",
      sessionStateByThread: { "thread-model-menu": { model: gpt, thinkingLevel: "xhigh" } },
      modelsByThread: { "thread-model-menu": [gpt, grok] },
      thinkingLevelsByThread: { "thread-model-menu": ["off", "minimal", "low", "medium", "high", "xhigh"] },
    });
    store.refreshConfiguredModels = vi.fn().mockResolvedValue(undefined);
    let finishModelSelection!: () => void;
    store.chooseModel = vi.fn((model) => new Promise<void>((resolve) => {
      finishModelSelection = () => {
        store.sessionStateByThread["thread-model-menu"] = { model, thinkingLevel: "high" };
        store.thinkingLevelsByThread["thread-model-menu"] = ["low", "medium", "high"];
        resolve();
      };
    }));
    const wrapper = mount(ComposerBar, { global: { plugins: [pinia] } });
    vi.spyOn(wrapper.get(".composer").element, "getBoundingClientRect").mockReturnValue(elementRect(100, 500, 820, 108));
    vi.spyOn(wrapper.get(".model-button").element.parentElement as HTMLElement, "getBoundingClientRect").mockReturnValue(elementRect(300, 570, 180, 32));

    await wrapper.get(".model-button").trigger("click");
    await flushPromises();
    const modelMenu = wrapper.get<HTMLElement>(".model-menu");
    expect(modelMenu.classes()).toContain("!fixed");
    expect(modelMenu.element.style.bottom).toBe(`${window.innerHeight - 500 + 8}px`);
    expect(window.innerHeight - Number.parseFloat(modelMenu.element.style.bottom)).toBeLessThan(500);
    expect(store.refreshConfiguredModels).toHaveBeenCalledOnce();
    expect(wrapper.get(".model-button").text()).toContain("xhigh");
    expect(wrapper.findAll(".thinking-level-grid button").map((button) => button.text())).toContain("xhigh");
    await wrapper.findAll(".model-menu-options button")[1].trigger("click");

    expect(wrapper.find(".model-menu").exists()).toBe(true);
    expect(wrapper.get(".thinking-level-grid").attributes("aria-busy")).toBe("true");
    expect(wrapper.find(".thinking-level-loading").exists()).toBe(true);
    expect(wrapper.findAll(".thinking-level-grid button")).toHaveLength(0);
    expect(wrapper.findAll(".model-menu-options button").every((button) => button.attributes("disabled") !== undefined)).toBe(true);
    expect(wrapper.get(".menu-section-label").text()).toBe("Model");
    finishModelSelection();
    await flushPromises();

    expect(wrapper.find(".model-menu").exists()).toBe(true);
    expect(wrapper.findAll(".model-menu-options button")[1].attributes("aria-checked")).toBe("true");
    expect(wrapper.get(".thinking-level-grid").attributes("aria-busy")).toBe("false");
    expect(wrapper.find(".thinking-level-loading").exists()).toBe(false);
    expect(wrapper.findAll(".thinking-level-grid button").map((button) => button.text())).toEqual(["low", "medium", "high"]);
    expect(wrapper.get(".model-button").text()).toContain("Grok 4.6 · high");
  });

  it("shows context, input, output, and cache token metrics below the composer", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-stats", title: "Audit", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: true, generation: 1,
      }],
      activeThreadId: "thread-stats",
      draftsByThread: { "thread-stats": "/" },
      commandsByThread: { "thread-stats": [{ name: "compact", description: "Compact context", source: "extension" }] },
      sessionStateByThread: { "thread-stats": {
        model: { id: "gpt-5.6", name: "GPT 5.6", provider: "openai", contextWindow: 200_000 },
        thinkingLevel: "minimal",
      } },
      modelsByThread: { "thread-stats": [
        { id: "gpt-5.6", name: "GPT 5.6", provider: "openai", contextWindow: 200_000 },
        { id: "claude-sonnet", name: "Claude Sonnet", provider: "anthropic", contextWindow: 200_000 },
      ] },
      thinkingLevelsByThread: { "thread-stats": ["off", "minimal", "low", "medium", "high"] },
      sessionStatsByThread: { "thread-stats": {
        contextUsage: { tokens: 60_000, contextWindow: 200_000, percent: 30, estimated: true },
        tokens: { input: 50_000, output: 10_000, cacheRead: 40_000, cacheWrite: 5_000, total: 105_000 },
      } },
    });

    const wrapper = mount(ComposerBar, { global: { plugins: [pinia] } });
    const metrics = wrapper.get(".composer-token-metrics");
    expect(metrics.element.previousElementSibling?.classList.contains("composer-input-stack")).toBe(true);
    expect(wrapper.get(".composer-input-stack").element.lastElementChild).toBe(wrapper.get(".composer").element);
    expect(metrics.findAll(".composer-token-metric")).toHaveLength(4);
    const context = metrics.get(".is-context");
    expect(context.text()).toContain("Context~60K / 200K");
    expect(context.get(".context-token-meter b").attributes("style")).toContain("width: 30%");
    expect(context.attributes("title")).toContain("~60,000 / 200,000");
    expect(metrics.get(".is-input").text()).toContain("Input50K");
    expect(metrics.get(".is-output").text()).toContain("Output10K");
    expect(metrics.get(".is-cache").text()).toContain("Cache45K");
    expect(metrics.get(".is-cache").attributes("title")).toContain("Cache read: 40,000");
    expect(metrics.get(".is-cache").attributes("title")).toContain("Cache write: 5,000");
    expect(wrapper.find(".composer-context-summary").exists()).toBe(false);

    expect(wrapper.find(".completion-menu").exists()).toBe(true);
    await wrapper.get(".model-button").trigger("click");
    await flushPromises();
    expect(wrapper.find(".completion-menu").exists()).toBe(false);
    expect(wrapper.findAll(".model-menu-options button")).toHaveLength(2);
    expect(wrapper.findAll(".thinking-level-grid button")).toHaveLength(5);
    document.dispatchEvent(new Event("pointerdown", { bubbles: true }));
    await flushPromises();
    expect(wrapper.find(".model-menu").exists()).toBe(false);
    expect(wrapper.find(".completion-menu").exists()).toBe(true);
  });

  it("shows and changes workspace access below the conversation", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-1", title: "Audit", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: false, generation: 0,
      }],
      activeThreadId: "thread-1",
    });
    store.setActiveWorkspaceTrust = vi.fn().mockResolvedValue(true);
    const wrapper = mount(ComposerBar, { global: { plugins: [pinia] } });
    vi.spyOn(wrapper.get(".composer").element, "getBoundingClientRect").mockReturnValue(elementRect(100, 500, 820, 108));
    vi.spyOn(wrapper.get(".access-button").element.parentElement as HTMLElement, "getBoundingClientRect").mockReturnValue(elementRect(140, 570, 170, 32));

    expect(wrapper.get(".access-button").text()).toContain("Trust project resources");
    await wrapper.get(".access-button").trigger("click");
    await flushPromises();
    const accessMenu = wrapper.get<HTMLElement>(".access-menu");
    expect(accessMenu.classes()).toEqual(expect.arrayContaining(["!fixed", "!overflow-y-auto"]));
    expect(accessMenu.element.style.bottom).toBe(`${window.innerHeight - 500 + 8}px`);
    expect(window.innerHeight - Number.parseFloat(accessMenu.element.style.bottom)).toBeLessThan(500);
    expect(wrapper.get(".access-menu").text()).toContain("Applies to every task in this workspace");
    expect(wrapper.get(".access-menu").text()).toContain("Pi's normal tools can still modify the workspace");
    const restricted = wrapper.findAll('.access-menu [role="menuitemradio"]').find((item) => item.text().includes("Ignore project resources"));
    if (!restricted) throw new Error("restricted access option was not rendered");
    await restricted.trigger("click");
    expect(store.setActiveWorkspaceTrust).toHaveBeenCalledWith("deny");
  });
});
