import { createPinia, setActivePinia } from "pinia";
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore, type TimelineMessage } from "../stores/app";
import ConversationPane from "./ConversationPane.vue";

vi.mock("../services/agent", () => ({
  agentService: {},
  onPiEvent: () => vi.fn(),
}));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

function transcript(length: number): TimelineMessage[] {
  return Array.from({ length }, (_, index) => ({
    id: `message-${index}`,
    role: index % 2 ? "assistant" : "user",
    text: `Message ${index}`,
    thinking: "",
    timestamp: "10:00",
    streaming: false,
    tools: [],
  }));
}

describe("ConversationPane", () => {
  beforeEach(() => setActivePinia(createPinia()));
  afterEach(() => vi.unstubAllGlobals());

  function mountTranscript(length: number) {
    const store = useAppStore();
    store.threads = [{
      id: "thread-1", title: "Long task", workspace: "repo", workspacePath: "D:\\repo",
      trust: "deny", status: "idle", started: false, generation: 0,
    }];
    store.activeThreadId = "thread-1";
    store.messagesByThread["thread-1"] = transcript(length);
    return mount(ConversationPane, {
      global: {
        stubs: {
          ComposerBar: true,
          ConversationMessage: { props: ["message"], template: '<article class="stub-message" :data-message-id="message.id" :data-role="message.role">{{ message.text }}</article>' },
        },
      },
    });
  }

  it("keeps the workspace blank when no task is selected", () => {
    const wrapper = mount(ConversationPane, { global: { stubs: { ComposerBar: true } } });

    expect(wrapper.get(".empty-workspace").text()).toBe("");
    expect(wrapper.find(".welcome-empty").exists()).toBe(false);
    expect(wrapper.find(".empty-logo").exists()).toBe(false);
    expect(wrapper.find("composer-bar-stub").exists()).toBe(false);
  });

  it("keeps short transcripts on the exact DOM path", () => {
    const wrapper = mountTranscript(80);
    expect(wrapper.find("[data-virtualized]").exists()).toBe(false);
    expect(wrapper.findAll(".stub-message")).toHaveLength(80);
    expect(wrapper.find("composer-bar-stub").exists()).toBe(true);
  });

  it("keeps the latest user and assistant turn outside history virtualization", async () => {
    const wrapper = mountTranscript(82);
    await flushPromises();
    expect(wrapper.find("[data-virtualized]").exists()).toBe(true);
    for (const id of ["message-80", "message-81"]) {
      const row = wrapper.get(`[data-message-id="${id}"]`).element;
      expect(row.closest(".virtual-message-row")).toBeNull();
      expect(row.parentElement).toBe(wrapper.get(".timeline-content").element);
    }
    wrapper.unmount();
  });

  it("opens an already selected conversation at its latest message", async () => {
    const wrapper = mountTranscript(4);
    const timeline = wrapper.get(".timeline").element as HTMLElement;
    Object.defineProperty(timeline, "scrollHeight", { configurable: true, value: 1200 });
    timeline.scrollTop = 300;

    await flushPromises();

    expect(timeline.scrollTop).toBe(1200);
  });

  it("opens cached conversations at the bottom, including reselecting the current conversation", async () => {
    const wrapper = mountTranscript(4);
    await flushPromises();
    const store = useAppStore();
    store.threads.push({ ...store.threads[0], id: "thread-2" });
    store.messagesByThread["thread-2"] = transcript(6);
    const timeline = wrapper.get(".timeline").element as HTMLElement;
    Object.defineProperty(timeline, "scrollHeight", {
      get: () => wrapper.findAll(".stub-message").length * 300,
    });

    for (const threadId of ["thread-1", "thread-2", "thread-1"]) {
      await wrapper.get(".timeline").trigger("wheel", { deltaY: -100 });
      timeline.scrollTop = 100;
      store.selectThread(threadId);
      await flushPromises();
      expect(timeline.scrollTop).toBe(timeline.scrollHeight);
    }

    wrapper.unmount();
    timeline.scrollTop = 100;
    store.selectThread("thread-1");
    await flushPromises();
    expect(timeline.scrollTop).toBe(100);
  });

  it("measures the composer stack and follows its growth only while pinned to the bottom", async () => {
    const observers: Array<{ callback: () => void; target?: Element; disconnect: () => void }> = [];
    const frames: FrameRequestCallback[] = [];
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => frames.push(callback));
    vi.stubGlobal("cancelAnimationFrame", vi.fn());
    vi.stubGlobal("ResizeObserver", class {
      record: (typeof observers)[number];
      constructor(callback: () => void) {
        this.record = { callback, disconnect: vi.fn() };
        observers.push(this.record);
      }
      observe(target: Element) { this.record.target = target; }
      unobserve() {}
      disconnect() { this.record.disconnect(); }
    });
    const wrapper = mountTranscript(4);
    await flushPromises();
    const composer = wrapper.get("composer-bar-stub").element;
    const observer = observers.find((item) => item.target === composer)!;
    expect(observer).toBeDefined();
    const timeline = wrapper.get(".timeline").element as HTMLElement;
    Object.defineProperties(timeline, {
      clientHeight: { configurable: true, value: 600 },
      scrollHeight: { configurable: true, value: 1200 },
    });
    timeline.scrollTop = 600;
    await wrapper.get(".timeline").trigger("scroll");
    const rect = vi.spyOn(composer, "getBoundingClientRect").mockReturnValue({ height: 320 } as DOMRect);

    observer.callback();
    frames.at(-1)?.(0);
    await flushPromises();

    expect((wrapper.element as HTMLElement).style.getPropertyValue("--composer-overlay-reserve")).toBe("320px");
    expect(timeline.scrollTop).toBe(1200);

    timeline.scrollTop = 100;
    await wrapper.get(".timeline").trigger("wheel", { deltaY: -100 });
    await wrapper.get(".timeline").trigger("scroll");
    rect.mockReturnValue({ height: 400 } as DOMRect);
    observer.callback();
    frames.at(-1)?.(0);
    await flushPromises();

    expect((wrapper.element as HTMLElement).style.getPropertyValue("--composer-overlay-reserve")).toBe("400px");
    expect(timeline.scrollTop).toBe(100);
    wrapper.unmount();
    expect(observer.disconnect).toHaveBeenCalled();
  });

  it("follows measured content growth without mistaking layout scrolls for an upward wheel", async () => {
    const observers: Array<{ callback: () => void; target?: Element; disconnect: () => void }> = [];
    vi.stubGlobal("ResizeObserver", class {
      record: (typeof observers)[number];
      constructor(callback: () => void) {
        this.record = { callback, disconnect: vi.fn() };
        observers.push(this.record);
      }
      observe(target: Element) { this.record.target = target; }
      unobserve() {}
      disconnect() { this.record.disconnect(); }
    });
    const wrapper = mountTranscript(4);
    await flushPromises();
    const observer = observers.find((item) => item.target === wrapper.get(".timeline-content").element)!;
    const timeline = wrapper.get(".timeline").element as HTMLElement;
    Object.defineProperties(timeline, {
      clientHeight: { configurable: true, value: 600 },
      scrollHeight: { configurable: true, value: 1200 },
    });
    timeline.scrollTop = 600;
    await wrapper.get(".timeline").trigger("scroll");

    Object.defineProperty(timeline, "scrollHeight", { configurable: true, value: 2000 });
    await wrapper.get(".timeline").trigger("scroll");
    observer.callback();
    expect(timeline.scrollTop).toBe(2000);

    await wrapper.get(".timeline").trigger("wheel", { deltaY: -5 });
    timeline.scrollTop = 1395;
    await wrapper.get(".timeline").trigger("scroll");
    Object.defineProperty(timeline, "scrollHeight", { configurable: true, value: 3000 });
    observer.callback();
    expect(timeline.scrollTop).toBe(1395);

    timeline.scrollTop = 2400;
    await wrapper.get(".timeline").trigger("scroll");
    Object.defineProperty(timeline, "scrollHeight", { configurable: true, value: 3200 });
    observer.callback();
    expect(timeline.scrollTop).toBe(3200);
    wrapper.unmount();
    expect(observer.disconnect).toHaveBeenCalled();
  });

  it("keeps the first answer DOM in place through the empty start of a tool follow-up", async () => {
    const store = useAppStore();
    store.threads = [{ id: "thread-1", title: "First turn", workspace: "repo", workspacePath: "D:\\repo", trust: "deny", status: "running", started: true, generation: 1 }];
    store.activeThreadId = "thread-1";
    store.messagesByThread["thread-1"] = [];
    const live = mount(ConversationPane, { global: { stubs: { ComposerBar: true } } });
    try {
      await flushPromises();
      expect(live.find(".empty-thread").exists()).toBe(true);
      store.messagesByThread["thread-1"] = [
        { ...transcript(1)[0], id: "first-user", text: "Inspect" },
        { ...transcript(2)[1], id: "first-work", text: "Reading the project", streaming: true, tools: [{ id: "read", name: "read", output: "source", status: "complete" }] },
      ];
      await live.vm.$nextTick();
      const body = live.get('[data-role="assistant"] .message-content > .markdown-body').element;
      store.messagesByThread["thread-1"][1].streaming = false;
      store.messagesByThread["thread-1"].push({ ...transcript(2)[1], id: "next-work", text: "", thinking: "Checking the next file", streaming: true });
      await live.vm.$nextTick();
      expect(live.get('[data-role="assistant"] .message-content > .markdown-body').element).toBe(body);
      expect(live.get('[data-role="assistant"] .message-content > .markdown-body').text()).toBe("Reading the project");
      expect(live.findAll(".execution-process-details > .markdown-body")).toHaveLength(0);
    } finally {
      live.unmount();
    }
  });

  it("preserves the assistant row and open tools through streaming, completion and history refresh", async () => {
    const store = useAppStore();
    store.threads = [{
      id: "thread-1", title: "Live task", workspace: "repo", workspacePath: "D:\\repo",
      trust: "deny", status: "running", started: false, generation: 0,
    }];
    store.activeThreadId = "thread-1";
    store.messagesByThread["thread-1"] = [
      { ...transcript(1)[0], id: "request", text: "Inspect" },
      { ...transcript(2)[1], id: "work", thinking: "Check sources", streaming: true, tools: [{ id: "tool", name: "read", output: "source", status: "complete" }] },
    ];
    const wrapper = mount(ConversationPane, { global: { stubs: { ComposerBar: true } } });
    const row = wrapper.get('[data-role="assistant"]').element;
    const tool = wrapper.get(".tool-call").element;
    const thinking = wrapper.get(".thinking-block").element;
    await wrapper.get(".thinking-block > summary").trigger("click");
    await wrapper.get(".tool-call > summary").trigger("click");
    expect(wrapper.get(".tool-call").attributes("open")).toBeDefined();

    store.messagesByThread["thread-1"].push({ ...transcript(2)[1], id: "answer", text: "Done", streaming: true });
    await wrapper.vm.$nextTick();

    expect(wrapper.get('[data-role="assistant"]').element).toBe(row);
    expect(wrapper.get('[data-role="assistant"]').attributes("data-message-id")).toBe("answer");
    expect(wrapper.get(".tool-call").element).toBe(tool);
    expect(wrapper.get(".tool-call").attributes("open")).toBeDefined();
    store.activeAssistantByThread["thread-1"] = "answer";
    store.finishAssistant("thread-1");
    store.threads[0].status = "idle";
    await wrapper.vm.$nextTick();
    expect(wrapper.get(".execution-process").attributes("open")).toBeDefined();
    const waitingSlot = wrapper.get(".waiting-for-output").element;
    store.applySessionSnapshot(store.threads[0], { messages: [
      { role: "user", piDeskEntryId: "request", content: "Inspect" },
      { role: "assistant", piDeskEntryId: "work", content: [
        { type: "thinking", thinking: "Check sources" },
        { type: "text", text: "Message 1" }, { type: "toolCall", id: "tool", name: "read" },
      ] },
      { role: "toolResult", toolCallId: "tool", piDeskEntryId: "result", content: "source" },
      { role: "assistant", piDeskEntryId: "answer", content: "Done" },
    ], messageCount: 4 });
    await wrapper.vm.$nextTick();
    expect(wrapper.get('[data-role="assistant"]').element).toBe(row);
    expect(wrapper.get('[data-role="assistant"]').attributes("data-message-id")).toBe("history-answer");
    expect(wrapper.get(".tool-call").element).toBe(tool);
    expect(wrapper.get(".tool-call").attributes("open")).toBeDefined();
    expect(wrapper.get(".thinking-block").element).toBe(thinking);
    expect(wrapper.get(".thinking-block").attributes("open")).toBeDefined();
    expect(wrapper.get(".waiting-for-output").element).toBe(waitingSlot);
    expect(wrapper.get(".waiting-for-output").classes()).toContain("invisible");
    wrapper.unmount();
  });

  it("shows the temporary thinking status only while waiting for backend output", async () => {
    const store = useAppStore();
    store.threads = [{
      id: "thread-1", title: "Waiting task", workspace: "repo", workspacePath: "D:\\repo",
      trust: "deny", status: "running", started: true, generation: 1,
    }];
    store.activeThreadId = "thread-1";
    store.messagesByThread["thread-1"] = transcript(2);
    store.waitingForOutputByThread["thread-1"] = true;
    const wrapper = mount(ConversationPane, {
      global: {
        stubs: {
          ComposerBar: true,
          ConversationMessage: { props: ["message"], template: '<article class="stub-message">{{ message.text }}</article>' },
        },
      },
    });

    const status = wrapper.get(".waiting-for-output");
    expect(status.text()).toBe("");
    expect(status.attributes("aria-label")).toBe("Thinking");
    expect(status.find("svg.is-spinning").exists()).toBe(true);
    expect(wrapper.get(".timeline-content").element.lastElementChild?.classList.contains("waiting-for-output")).toBe(true);

    store.retryByThread["thread-1"] = { attempt: 1, maxAttempts: 3, delayMs: 4000 };
    await wrapper.vm.$nextTick();
    expect(wrapper.get(".waiting-for-output").classes()).toContain("invisible");

    store.waitingForOutputByThread["thread-1"] = false;
    await wrapper.vm.$nextTick();
    expect(wrapper.get(".waiting-for-output").classes()).toContain("invisible");
  });

  it("opens conversation search with Ctrl+F and navigates results", async () => {
    const wrapper = mountTranscript(3);
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "f", ctrlKey: true, bubbles: true, cancelable: true }));
    await wrapper.vm.$nextTick();

    expect(wrapper.find(".conversation-search").exists()).toBe(true);
    const input = wrapper.get(".conversation-search-input");
    await input.setValue("Message");
    expect(wrapper.get(".conversation-search-result").text()).toBe("1 / 3");

    await wrapper.findAll(".conversation-search-control")[1].trigger("click");
    expect(wrapper.get(".conversation-search-result").text()).toBe("2 / 3");

    await input.trigger("keydown", { key: "Escape" });
    expect(wrapper.find(".conversation-search").exists()).toBe(false);
  });

  it("does not jump back to a search result when streaming output changes", async () => {
    const wrapper = mountTranscript(4);
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "f", ctrlKey: true, bubbles: true, cancelable: true }));
    await wrapper.vm.$nextTick();
    await wrapper.get(".conversation-search-input").setValue("Message 0");
    await flushPromises();

    const scrollIntoView = vi.fn();
    wrapper.findAll(".stub-message")[0].element.scrollIntoView = scrollIntoView;
    const store = useAppStore();
    store.messagesByThread["thread-1"][3].text += " streaming";
    await wrapper.vm.$nextTick();
    await flushPromises();

    expect(scrollIntoView).not.toHaveBeenCalled();
  });

  it("shows a left conversation outline preview and jumps to a turn", async () => {
    const wrapper = mountTranscript(3);
    const items = wrapper.findAll(".conversation-outline-item");
    expect(items).toHaveLength(2);
    expect(wrapper.get(".conversation-outline").attributes("aria-label")).toBe("Conversation outline");

    await items[0].trigger("mouseenter");
    expect(wrapper.get(".conversation-outline-preview").text()).toContain("Message 0");
    expect(wrapper.get(".conversation-outline-preview").text()).toContain("Message 1");

    await items[1].trigger("click");
    expect(items[1].classes()).toContain("is-active");
  });

  it("renders an empty historical session without a card frame", () => {
    const store = useAppStore();
    store.threads = [{
      id: "thread-1", title: "History", workspace: "repo", workspacePath: "D:\\repo",
      trust: "deny", status: "idle", started: false, generation: 0, sessionFile: "session.jsonl",
    }];
    store.activeThreadId = "thread-1";
    store.messagesByThread["thread-1"] = [];
    const wrapper = mount(ConversationPane, { global: { stubs: { ComposerBar: true } } });

    const emptyThread = wrapper.get(".empty-thread");
    expect(emptyThread.classes()).not.toContain("border");
    expect(emptyThread.classes()).not.toContain("rounded-xl");
    expect(emptyThread.classes()).not.toContain("shadow-sm");
  });

  it("removes recovered retry noise before the later assistant response", () => {
    const store = useAppStore();
    store.threads = [{
      id: "thread-1", title: "Recovered task", workspace: "repo", workspacePath: "D:\\repo",
      trust: "deny", status: "idle", started: false, generation: 0,
    }];
    store.activeThreadId = "thread-1";
    store.messagesByThread["thread-1"] = [
      { ...transcript(1)[0], id: "user-1", role: "user", text: "Upload it" },
      { ...transcript(1)[0], id: "failed-1", role: "assistant", text: "", error: "Request timed out." },
      { ...transcript(1)[0], id: "success-1", role: "assistant", text: "Upload complete" },
    ];
    const wrapper = mount(ConversationPane, {
      global: {
        stubs: {
          ComposerBar: true,
          ConversationMessage: {
            props: ["message"],
            template: '<article class="stub-message"><span v-if="message.runNotice" class="stub-notice">{{ message.runNotice.status }}</span><span>{{ message.text }}</span></article>',
          },
        },
      },
    });

    const rows = wrapper.findAll(".stub-message");
    expect(rows).toHaveLength(2);
    expect(rows[1].text()).toContain("Upload complete");
    expect(rows[1].find(".stub-notice").exists()).toBe(false);
  });

  it("virtualizes a long transcript", async () => {
    const wrapper = mountTranscript(2);
    const store = useAppStore();
    store.activeThread!.messageCount = 200;

    await wrapper.vm.$nextTick();

    expect(wrapper.get("[data-virtualized]").attributes("data-virtualized")).toBe("true");
  });

  it("switches every long transcript to the virtualized path", () => {
    const wrapper = mountTranscript(81);
    expect(wrapper.get("[data-virtualized]").attributes("data-virtualized")).toBe("true");
    expect(wrapper.findAll(".stub-message").length).toBeLessThan(81);
    expect(wrapper.find("composer-bar-stub").exists()).toBe(true);
  });
});
