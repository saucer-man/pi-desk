import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useAppStore, type ExtensionUIRequest } from "../stores/app";
import ExtensionDialog from "./ExtensionDialog.vue";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

function setup(request: ExtensionUIRequest) {
  const pinia = createPinia();
  setActivePinia(pinia);
  const store = useAppStore();
  store.$patch({
    threads: [{
      id: "thread-1", title: "Extension", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
      status: "idle", started: true, generation: 1,
    }],
    activeThreadId: "thread-1",
    extensionRequestByThread: { "thread-1": request },
  });
  store.respondToExtension = vi.fn().mockResolvedValue(undefined);
  store.dismissExtensionRequest = vi.fn();
  const wrapper = mount(ExtensionDialog, { global: { plugins: [pinia] } });
  return { store, wrapper };
}

describe("ExtensionDialog", () => {
  afterEach(() => vi.useRealTimers());

  it("responds to select, input, editor, confirm, and cancel controls", async () => {
    const { store, wrapper } = setup({ id: "select", method: "select", title: "Pick", options: ["Allow", "Block"] });
    await wrapper.findAll(".select-options button")[0].trigger("click");
    expect(store.respondToExtension).toHaveBeenLastCalledWith("Allow");

    store.extensionRequestByThread["thread-1"] = { id: "input", method: "input", title: "Value", placeholder: "Name" };
    await wrapper.vm.$nextTick();
    await wrapper.get("input").setValue("Pi Desk");
    await wrapper.get("input").trigger("keydown", { key: "Enter" });
    expect(store.respondToExtension).toHaveBeenLastCalledWith("Pi Desk");

    store.extensionRequestByThread["thread-1"] = { id: "editor", method: "editor", title: "Edit", prefill: "one" };
    await wrapper.vm.$nextTick();
    await wrapper.get("textarea").setValue("one\ntwo");
    await wrapper.findAll("footer button").at(-1)!.trigger("click");
    expect(store.respondToExtension).toHaveBeenLastCalledWith("one\ntwo");

    store.extensionRequestByThread["thread-1"] = { id: "confirm", method: "confirm", title: "Continue", message: "Proceed?" };
    await wrapper.vm.$nextTick();
    const footerButtons = wrapper.findAll("footer button");
    await footerButtons[1].trigger("click");
    await footerButtons[2].trigger("click");
    expect(store.respondToExtension).toHaveBeenCalledWith(false);
    expect(store.respondToExtension).toHaveBeenCalledWith(true);
    await wrapper.get('button[title="Cancel"]').trigger("click");
    expect(store.respondToExtension).toHaveBeenLastCalledWith(undefined, true);
  });

  it("renders a batch ask as tabs, preserves option descriptions, reviews, and submits structured answers", async () => {
    const { store, wrapper } = setup({
      id: "batch",
      method: "batch_ask",
      batchReview: true,
      batchQuestions: [
        {
          id: "listen_port", type: "select", question: "服务器对外访问端口使用哪个？", prefill: "8000", allowOther: true,
          placeholder: "也可以填写其他端口",
          options: [
            { label: "8000（推荐）", value: "8000", description: "访问地址为 http://example:8000" },
            { label: "8080", value: "8080", description: "访问地址为 http://example:8080" },
          ],
        },
        {
          id: "data_mode", type: "select", question: "是否迁移现有数据？", allowOther: false,
          options: [
            { label: "完整迁移现有数据（推荐）", value: "migrate", description: "上传配置和数据库" },
            { label: "全新部署", value: "fresh", description: "重新配置" },
          ],
        },
      ],
    });

    expect(wrapper.get("h2").text()).toContain("2");
    expect(wrapper.text()).not.toContain("__piDeckBatchAsk");
    expect(wrapper.get('.batch-question-tabs [role="tab"]').classes()).toContain("answered");
    expect(wrapper.get(".batch-question-options").text()).toContain("访问地址为 http://example:8000");

    await wrapper.findAll('.batch-question-tabs [role="tab"]')[1].trigger("click");
    await wrapper.findAll(".batch-question-options > button")[0].trigger("click");
    await wrapper.get(".batch-question-review-tab").trigger("click");
    expect(wrapper.get(".batch-question-review-list").text()).toContain("完整迁移现有数据（推荐）");
    await wrapper.get(".batch-question-submit").trigger("click");

    const raw = vi.mocked(store.respondToExtension).mock.calls.at(-1)?.[0];
    expect(typeof raw).toBe("string");
    expect(JSON.parse(raw as string)).toEqual({ answers: [
      { id: "listen_port", type: "select", value: "8000", label: "8000（推荐）" },
      { id: "data_mode", type: "select", value: "migrate", label: "完整迁移现有数据（推荐）" },
    ] });
  });

  it("collects confirm, input, and editor questions before enabling submission", async () => {
    const { store, wrapper } = setup({
      id: "mixed-batch",
      method: "batch_ask",
      batchReview: false,
      batchQuestions: [
        { id: "confirm", type: "confirm", question: "Continue?" },
        { id: "name", type: "input", question: "Name?", placeholder: "project" },
        { id: "notes", type: "editor", question: "Notes?", prefill: "existing notes" },
      ],
    });

    await wrapper.findAll(".batch-question-confirm > button")[0].trigger("click");
    await wrapper.findAll('.batch-question-tabs [role="tab"]')[1].trigger("click");
    await wrapper.get('.batch-question-panel > input').setValue("desk");
    await wrapper.findAll('.batch-question-tabs [role="tab"]')[2].trigger("click");
    await wrapper.get(".batch-question-navigation .primary").trigger("click");

    const raw = vi.mocked(store.respondToExtension).mock.calls.at(-1)?.[0];
    expect(JSON.parse(raw as string)).toMatchObject({ answers: [
      { id: "confirm", type: "confirm", value: true },
      { id: "name", type: "input", value: "desk" },
      { id: "notes", type: "editor", value: "existing notes" },
    ] });
  });

  it("submits an allowOther batch selection as a custom answer", async () => {
    const { store, wrapper } = setup({
      id: "custom-batch",
      method: "batch_ask",
      batchReview: false,
      batchQuestions: [{
        id: "port", type: "select", question: "Port?", allowOther: true,
        options: [{ label: "8000", value: "8000" }], placeholder: "Other port",
      }],
    });

    await wrapper.get(".batch-question-custom input").setValue("9000");
    expect(wrapper.get<HTMLButtonElement>(".batch-question-navigation .primary").element.disabled).toBe(true);
    expect(store.respondToExtension).not.toHaveBeenCalled();
    await wrapper.get(".batch-question-custom .text-button").trigger("click");
    await wrapper.get(".batch-question-navigation .primary").trigger("click");
    const raw = vi.mocked(store.respondToExtension).mock.calls.at(-1)?.[0];
    expect(JSON.parse(raw as string)).toEqual({ answers: [
      { id: "port", type: "select", value: "9000", label: "9000", wasCustom: true },
    ] });
  });

  it("dismisses a dialog locally when Pi's timeout elapses", async () => {
    vi.useFakeTimers();
    const { store, wrapper } = setup({ id: "timed", method: "confirm", title: "Continue", message: "Proceed?", timeout: 250 });
    await vi.advanceTimersByTimeAsync(250);
    expect(store.dismissExtensionRequest).toHaveBeenCalledWith("timed");
    wrapper.unmount();
  });
});
