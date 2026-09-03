import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import ConversationMessage from "./ConversationMessage.vue";
import { useAppStore } from "../stores/app";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

describe("ConversationMessage", () => {
  beforeEach(() => setActivePinia(createPinia()));

  it("shows time above the response and collapses completed execution", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const wrapper = mount(ConversationMessage, {
      props: {
        message: {
          id: "assistant-1",
          role: "assistant",
          text: "Result",
          thinking: "Inspect the existing implementation first.",
          timestamp: "10:00",
          timestampMs: 1000,
          durationMs: 2500,
          streaming: false,
          tools: [{ id: "read-1", name: "read", output: "source", status: "complete" }],
        },
      },
      global: { plugins: [pinia] },
    });

    const details = wrapper.get(".execution-process");
    expect(wrapper.get(".message-row").classes()).toContain("message-row--compact");
    expect(details.attributes("open")).toBeUndefined();
    expect(wrapper.get(".message-header").text()).toContain("Pi");
    expect(wrapper.get(".message-header").text()).toContain("10:00");
    expect(wrapper.get(".message-header").text()).toContain("2.5s");
    expect(details.get("summary").text()).toContain("Execution: 1 tools · 1 thoughts");
    await details.get("summary").trigger("click");
    expect(details.attributes("open")).toBeDefined();
    expect(details.get(".thinking-block").text()).toContain("Reasoning");
    expect(details.get(".thinking-block .thinking-icon").attributes("aria-hidden")).toBe("true");
  });

  it("renders GPT think tags through the existing reasoning UI", async () => {
    const pinia = createPinia();
    const wrapper = mount(ConversationMessage, {
      props: {
        message: {
          id: "assistant-tagged-thinking", role: "assistant",
          text: "<think>Inspect the existing UI</think>Final answer", thinking: "",
          timestamp: "10:00", streaming: false, tools: [],
        },
      },
      global: { plugins: [pinia] },
    });

    expect(wrapper.get(".markdown-body").text()).toBe("Final answer");
    expect(wrapper.text()).not.toContain("<think>");
    expect(wrapper.get(".execution-process").attributes("open")).toBeUndefined();
    await wrapper.get(".execution-process > summary").trigger("click");
    expect(wrapper.get(".thinking-block pre").text()).toBe("Inspect the existing UI");

    await wrapper.setProps({ message: {
      ...wrapper.props("message"), text: "<think>Still inspecting", streaming: true,
    } });
    expect(wrapper.get(".thinking-block").attributes("open")).toBeDefined();
    expect(wrapper.get(".thinking-block pre").text()).toBe("Still inspecting");
  });

  it("renders Todo, reasoning, and other tools as peers in one execution grid", () => {
    const pinia = createPinia();
    const wrapper = mount(ConversationMessage, {
      props: {
        message: {
          id: "assistant-steps",
          role: "assistant",
          text: "Done",
          thinking: "",
          timestamp: "10:02",
          streaming: false,
          tools: [],
          executionSteps: [
            { id: "todo-step", kind: "tools", tools: [
              { id: "todo-1", name: "todo", arguments: { action: "add" }, output: "", status: "complete" },
              { id: "todo-2", name: "todo", arguments: { action: "toggle" }, output: "", status: "complete" },
            ] },
            { id: "thinking-step", kind: "thinking", text: "Inspect the layout." },
            { id: "read-step", kind: "tools", tools: [
              { id: "read-1", name: "read", arguments: { path: "App.vue" }, output: "", status: "complete" },
            ] },
          ],
        },
      },
      global: { plugins: [pinia] },
    });

    const children = Array.from(wrapper.get(".execution-process-details").element.children);
    expect(children.map((child) => child.className)).toEqual([
      "tool-call",
      "tool-call",
      "thinking-block",
      "tool-call",
    ]);
  });

  it("keeps streamed intermediate output on the final answer axis", () => {
    const pinia = createPinia();
    const wrapper = mount(ConversationMessage, {
      props: {
        message: {
          id: "assistant-streaming-output",
          role: "assistant",
          text: "Final answer",
          thinking: "",
          timestamp: "10:03",
          streaming: true,
          tools: [],
          executionSteps: [
            { id: "partial-output", kind: "message", text: "Partial answer" },
          ],
        },
      },
      global: { plugins: [pinia] },
    });

    const streamedOutput = wrapper.get(".execution-process-details .markdown-body");
    expect(streamedOutput.text()).toBe("Partial answer");
  });

  it("keeps execution expanded until all assistant output finishes", async () => {
    const pinia = createPinia();
    const wrapper = mount(ConversationMessage, {
      props: {
        message: {
          id: "assistant-live",
          role: "assistant",
          text: "Partial answer",
          thinking: "Working",
          timestamp: "10:01",
          streaming: true,
          tools: [{ id: "read-live", name: "read", output: "source", status: "complete" }],
        },
      },
      global: { plugins: [pinia] },
    });
    const details = wrapper.get(".execution-process");
    expect(details.attributes("open")).toBeDefined();
    expect(wrapper.find(".message-actions").exists()).toBe(false);

    await details.get("summary").trigger("click");
    await wrapper.vm.$nextTick();
    expect(details.attributes("open")).toBeDefined();

    await wrapper.setProps({ message: { ...wrapper.props("message"), streaming: false } });
    await wrapper.vm.$nextTick();
    expect(details.attributes("open")).toBeUndefined();
    expect(wrapper.find(".message-actions").exists()).toBe(true);
    expect(wrapper.findAll(".message-action")).toHaveLength(4);
  });

  it("expands only the active reasoning step and collapses it when reasoning finishes", async () => {
    const pinia = createPinia();
    const message = {
      id: "assistant-thinking-live", role: "assistant" as const, text: "", thinking: "Inspecting",
      timestamp: "10:01", streaming: true, activeExecution: "thinking" as const, tools: [],
      executionSteps: [{ id: "thinking-live", kind: "thinking" as const, text: "Inspecting", active: true }],
    };
    const wrapper = mount(ConversationMessage, { props: { message }, global: { plugins: [pinia] } });

    expect(wrapper.get(".thinking-block").attributes("open")).toBeDefined();
    await wrapper.setProps({ message: {
      ...message, text: "Answer", activeExecution: "text" as const,
      executionSteps: [{ ...message.executionSteps[0], active: false }],
    } });
    expect(wrapper.get(".thinking-block").attributes("open")).toBeUndefined();
  });

  it("keeps message actions visible while disabling persisted mutations during a model run", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-running", title: "Running", workspace: "repo", workspacePath: "D:\\repo",
        trust: "approve", status: "running", started: true, generation: 1,
      }],
      activeThreadId: "thread-running",
    });
    const wrapper = mount(ConversationMessage, {
      props: {
        message: {
          id: "assistant-complete-fragment", entryId: "entry-1", role: "assistant",
          text: "Partial run fragment", thinking: "", timestamp: "10:03", streaming: false, tools: [],
        },
      },
      global: { plugins: [pinia] },
    });

    expect(wrapper.find(".message-actions").exists()).toBe(true);
    expect(wrapper.get('button[title="Copy message"]').attributes("disabled")).toBeUndefined();
    expect(wrapper.get('button[title="Edit message"]').attributes("disabled")).toBeDefined();
    expect(wrapper.get('button[title="Delete message"]').attributes("disabled")).toBeDefined();
    expect(wrapper.get('button[title="Fork from this message"]').attributes("disabled")).toBeDefined();
    store.activeThread!.status = "idle";
    await wrapper.vm.$nextTick();
    expect(wrapper.findAll(".message-action").every((button) => button.attributes("disabled") === undefined)).toBe(true);
  });

  it("places the user timestamp before the actions below the bubble", () => {
    const pinia = createPinia();
    const wrapper = mount(ConversationMessage, {
      props: {
        message: {
          id: "user-1",
          entryId: "entry-user-1",
          role: "user",
          text: "Update the request implementation",
          thinking: "",
          timestamp: "08/04 00:10",
          streaming: false,
          tools: [],
        },
      },
      global: { plugins: [pinia] },
    });

    expect(wrapper.find(".message-header").exists()).toBe(false);
    const meta = wrapper.get(".message-meta");
    expect(meta.element.firstElementChild?.tagName).toBe("TIME");
    expect(meta.get(".message-meta-time").text()).toBe("08/04 00:10");
    expect(meta.get(".message-action--copy").attributes("title")).toBe("Copy message");
  });

  it("opens session images in the same preview dialog as composer attachments", async () => {
    const pinia = createPinia();
    const image = {
      id: "history-image-1",
      name: "Image 1",
      data: "aW1hZ2U=",
      mimeType: "image/png",
      previewUrl: "data:image/png;base64,aW1hZ2U=",
    };
    const host = document.createElement("div");
    document.body.appendChild(host);
    const wrapper = mount(ConversationMessage, {
      attachTo: host,
      props: {
        message: {
          id: "user-image",
          role: "user",
          text: "Review this capture",
          thinking: "",
          timestamp: "08/04 00:11",
          streaming: false,
          images: [image],
          tools: [],
        },
      },
      global: { plugins: [pinia] },
    });

    const openButton = wrapper.get('.message-image-open[title="View image"]');
    expect(openButton.attributes("type")).toBe("button");
    await openButton.trigger("click");
    await flushPromises();

    const dialog = document.body.querySelector<HTMLElement>(".image-preview-dialog");
    expect(dialog?.getAttribute("role")).toBe("dialog");
    expect(dialog?.querySelector("h2")?.textContent).toBe("Image 1");
    expect(dialog?.querySelector("img")?.getAttribute("src")).toBe(image.previewUrl);

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await flushPromises();
    expect(document.body.querySelector(".image-preview-dialog")).toBeNull();
    wrapper.unmount();
    host.remove();
  });

  it("renders a compact compaction divider with an expandable summary", async () => {
    const pinia = createPinia();
    const wrapper = mount(ConversationMessage, {
      props: {
        message: {
          id: "history-compaction-compact-1",
          role: "system",
          text: "",
          thinking: "",
          timestamp: "08/14 11:24",
          streaming: false,
          tools: [],
          compaction: {
            summary: "## Compacted context\n\nThe previous implementation details were retained.",
            tokensBefore: 241443,
            estimatedTokensAfter: 32000,
          },
        },
      },
      global: { plugins: [pinia] },
    });

    const details = wrapper.get(".compaction-divider");
    expect(wrapper.get(".message-row").classes()).toContain("message-row--compaction");
    expect(details.get("summary").text()).toContain("Context compacted");
    expect(details.get("summary").text()).toContain("241,443 tokens before");
    expect(details.get("summary").text()).toContain("about 32,000 tokens after");
    expect(details.get("summary").text()).toContain("08/14 11:24");
    expect(details.attributes("open")).toBeUndefined();
    expect(wrapper.find(".message-actions").exists()).toBe(false);

    await details.get("summary").trigger("click");
    expect(details.attributes("open")).toBeDefined();
    expect(details.get(".compaction-summary").text()).toContain("The previous implementation details were retained.");
  });

  it("offers copy, inline edit, delete confirmation, and fork for a persisted message", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.editMessage = vi.fn().mockResolvedValue(true);
    store.deleteMessage = vi.fn().mockResolvedValue(true);
    store.forkFromMessage = vi.fn().mockResolvedValue(undefined);
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    const wrapper = mount(ConversationMessage, {
      props: {
        message: {
          id: "assistant-1",
          entryId: "entry-1",
          role: "assistant",
          text: "Original response",
          thinking: "",
          timestamp: "10:00",
          streaming: false,
          tools: [],
        },
      },
      global: { plugins: [pinia] },
    });

    await wrapper.get('button[title="Copy message"]').trigger("click");
    expect(writeText).toHaveBeenCalledWith("Original response");

    await wrapper.get('button[title="Edit message"]').trigger("click");
    await wrapper.get("textarea").setValue("Updated response");
    await wrapper.get('button[title="Save message"]').trigger("click");
    expect(store.editMessage).toHaveBeenCalledWith("assistant-1", "Updated response");

    await wrapper.get('button[title="Delete message"]').trigger("click");
    expect(wrapper.get(".message-delete-confirm").text()).toContain("Delete this message");
    await wrapper.get(".message-delete-confirm .is-danger").trigger("click");
    expect(store.deleteMessage).toHaveBeenCalledWith("assistant-1");

    await wrapper.get('button[title="Fork from this message"]').trigger("click");
    expect(store.forkFromMessage).toHaveBeenCalledWith("assistant-1");
  });

  it("sends an edited latest user message instead of only saving it", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    const message = {
      id: "user-latest", entryId: "entry-latest", role: "user" as const,
      text: "Original request", thinking: "", timestamp: "10:06", streaming: false, tools: [],
    };
    store.$patch({
      threads: [{
        id: "thread-edit", title: "Edit", workspace: "repo", workspacePath: "D:\\repo",
        sessionFile: "C:\\sessions\\edit.jsonl", trust: "approve", status: "idle", started: true, generation: 1,
      }],
      activeThreadId: "thread-edit",
      messagesByThread: { "thread-edit": [message] },
    });
    store.editMessage = vi.fn().mockResolvedValue(true);
    store.resendEditedMessage = vi.fn().mockResolvedValue(true);
    const wrapper = mount(ConversationMessage, {
      props: { message },
      global: { plugins: [pinia] },
    });

    await wrapper.get('button[title="Edit message"]').trigger("click");
    await wrapper.get("textarea").setValue("Updated request");
    await wrapper.get('button[title="Send edited message"]').trigger("click");

    expect(store.resendEditedMessage).toHaveBeenCalledWith("user-latest", "Updated request");
    expect(store.editMessage).not.toHaveBeenCalled();
    expect(wrapper.find("textarea").exists()).toBe(false);
  });

  it("renders retry, continued retry, recovery, and terminal failure on an assistant fragment", async () => {
    const pinia = createPinia();
    const baseMessage = {
      id: "assistant-retry",
      role: "assistant" as const,
      text: "Partial output",
      thinking: "",
      timestamp: "10:05",
      streaming: false,
      tools: [],
    };
    const wrapper = mount(ConversationMessage, {
      props: {
        message: {
          ...baseMessage,
          runNotice: {
            status: "retrying" as const,
            error: "OpenAI API error (520)",
            attempt: 2,
            maxAttempts: 4,
            delayMs: 1500,
          },
        },
      },
      global: { plugins: [pinia] },
    });

    const retrying = wrapper.get('.message-run-notice[data-status="retrying"]');
    expect(retrying.text()).toContain("Retrying in 1.5s (2/4)");
    expect(retrying.text()).toContain("OpenAI API error (520)");
    expect(retrying.attributes("role")).toBe("status");

    await wrapper.setProps({
      message: {
        ...baseMessage,
        runNotice: { status: "retried", error: "OpenAI API error (520)", attempt: 1, maxAttempts: 4 },
      },
    });
    const retried = wrapper.get('.message-run-notice[data-status="retried"]');
    expect(retried.text()).toContain("continued with another retry");
    expect(retried.find(".is-spinning").exists()).toBe(false);

    await wrapper.setProps({
      message: {
        ...baseMessage,
        runNotice: { status: "recovered", error: "OpenAI API error (520)", attempt: 2, maxAttempts: 4 },
      },
    });
    expect(wrapper.find('.message-run-notice[data-status="recovered"]').exists()).toBe(false);
    expect(wrapper.find(".message-actions").exists()).toBe(true);

    await wrapper.setProps({
      message: { ...baseMessage, text: "", runNotice: { status: "recovered", error: "OpenAI API error (520)" } },
    });
    expect(wrapper.find(".message-run-notice").exists()).toBe(false);
    expect(wrapper.find(".message-actions").exists()).toBe(false);

    await wrapper.setProps({
      message: { ...baseMessage, error: "Request timed out." },
    });
    const failed = wrapper.get('.message-run-notice[data-status="failed"]');
    expect(failed.text()).toContain("this run stopped");
    expect(failed.text()).toContain("Request timed out.");
    expect(failed.attributes("role")).toBe("alert");
  });

  it("counts down to the automatic retry deadline", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-09-02T12:00:00Z"));
    try {
      const wrapper = mount(ConversationMessage, {
        props: {
          message: {
            id: "assistant-retry-countdown", role: "assistant" as const, text: "Partial output", thinking: "",
            timestamp: "10:05", streaming: false, tools: [],
            runNotice: {
              status: "retrying" as const, attempt: 2, maxAttempts: 3, delayMs: 4000,
              retryAt: Date.now() + 4000,
            },
          },
        },
        global: { plugins: [createPinia()] },
      });

      expect(wrapper.get(".message-run-notice").text()).toContain("Retrying in 4s");
      await vi.advanceTimersByTimeAsync(1100);
      expect(wrapper.get(".message-run-notice").text()).toContain("Retrying in 3s");
      wrapper.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it("renders expanded skill context as a compact invocation and preserves it when editing", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.editMessage = vi.fn().mockResolvedValue(true);
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    const expanded = `<skill name="grill-me" location="C:\\Users\\yanq\\.agents\\skills\\grill-me\\SKILL.md">\nReferences are relative to C:\\Users\\yanq\\.agents\\skills\\grill-me.\n\nRun a \`/grilling\` session.\n</skill>\n\nReview the image generation plan.`;
    const wrapper = mount(ConversationMessage, {
      props: {
        message: {
          id: "user-skill", entryId: "entry-skill", role: "user", text: expanded,
          thinking: "", timestamp: "10:04", streaming: false, tools: [],
        },
      },
      global: { plugins: [pinia] },
    });

    expect(wrapper.get(".message-skill-invocation").text()).toBe("/skill:grill-me");
    expect(wrapper.get(".markdown-body").text()).toBe("Review the image generation plan.");
    expect(wrapper.text()).not.toContain("<skill");
    expect(wrapper.text()).not.toContain("References are relative");
    expect(wrapper.text()).not.toContain("Run a /grilling session");

    await wrapper.get('button[title="Copy message"]').trigger("click");
    expect(writeText).toHaveBeenCalledWith("/skill:grill-me Review the image generation plan.");

    await wrapper.get('button[title="Edit message"]').trigger("click");
    expect(wrapper.get("textarea").element.value).toBe("Review the image generation plan.");
    await wrapper.get("textarea").setValue("Audit the revised plan.");
    await wrapper.get('button[title="Save message"]').trigger("click");
    expect(store.editMessage).toHaveBeenCalledWith(
      "user-skill",
      expect.stringMatching(/<skill name="grill-me"[\s\S]*<\/skill>\n\nAudit the revised plan\.$/),
    );
  });

});
