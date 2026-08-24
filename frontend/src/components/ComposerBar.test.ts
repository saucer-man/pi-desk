import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "../stores/app";
import ComposerBar from "./ComposerBar.vue";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/modelconfig", () => ({ modelConfigService: { selectable: vi.fn().mockResolvedValue([]) } }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));
vi.mock("../utils/imageAttachments", () => ({
  MAX_ATTACHED_IMAGES: 10,
  MAX_IMAGE_BASE64_CHARS: 16_000_000,
  prepareImage: vi.fn(async (file: File) => ({
    id: "pasted-image",
    name: file.name,
    data: "aW1hZ2U=",
    mimeType: file.type,
    previewUrl: `data:${file.type};base64,aW1hZ2U=`,
  })),
}));

describe("ComposerBar", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
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
    expect(wrapper.get(".retry-banner").element.nextElementSibling).toBe(wrapper.get(".composer-input-stack").element);
    expect(wrapper.get(".queue-panel").element.nextElementSibling).toBe(wrapper.get(".composer").element);
    expect(wrapper.get(".queue-text").attributes("title")).toBe("Inspect logs");
    const firstQueueActions = wrapper.findAll(".queue-actions")[0];
    expect(firstQueueActions.findAll("button").map((button) => button.attributes("title"))).toEqual([
      "Send this message now",
      "Edit queued message",
      "Delete queued message",
    ]);

    const editor = wrapper.get(".composer-editor");
    for (let index = 0; index < 3; index += 1) await editor.trigger("keydown", { key: "ArrowDown" });
    await editor.trigger("keydown", { key: "Enter" });
    expect(store.activeDraft).toBe("/review ");
    expect(store.sendActivePrompt).not.toHaveBeenCalled();

    store.updateDraft("Check @view");
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
    const commands = wrapper.findAll('.completion-menu[aria-label="Commands"] button');

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

  it("opens the command button without changing the draft and prefixes or replaces runtime commands", async () => {
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
    expect(store.activeDraft).toBe("/compact keep these arguments");
    expect(store.refreshCommands).toHaveBeenCalledTimes(3);
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

    await wrapper.get(".model-button").trigger("click");
    await flushPromises();
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

    expect(wrapper.get(".access-button").text()).toContain("Trust project resources");
    await wrapper.get(".access-button").trigger("click");
    expect(wrapper.get(".access-menu").text()).toContain("Applies to every task in this workspace");
    expect(wrapper.get(".access-menu").text()).toContain("Pi's normal tools can still modify the workspace");
    const restricted = wrapper.findAll('.access-menu [role="menuitemradio"]').find((item) => item.text().includes("Ignore project resources"));
    if (!restricted) throw new Error("restricted access option was not rendered");
    await restricted.trigger("click");
    expect(store.setActiveWorkspaceTrust).toHaveBeenCalledWith("deny");
  });
});
