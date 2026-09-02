import { createPinia, setActivePinia } from "pinia";
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
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
          ConversationMessage: { props: ["message"], template: '<article class="stub-message">{{ message.text }}</article>' },
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
    expect(wrapper.get(".timeline").element.lastElementChild?.classList.contains("waiting-for-output")).toBe(true);

    store.waitingForOutputByThread["thread-1"] = false;
    await wrapper.vm.$nextTick();
    expect(wrapper.find(".waiting-for-output").exists()).toBe(false);
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
