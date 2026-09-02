import { flushPromises, mount } from "@vue/test-utils";
import { editorViewCtx, type Editor } from "@milkdown/core";
import { TextSelection } from "@milkdown/prose/state";
import { describe, expect, it } from "vitest";
import MarkdownEditor from "./MarkdownEditor.vue";
import MarkdownEditorCore from "./MarkdownEditorCore.vue";

describe("MarkdownEditor", () => {
  it("uses one editable Markdown surface and syncs its serialized value", async () => {
    const wrapper = mount(MarkdownEditor, {
      props: { modelValue: "**ready**", placeholder: "Write", ariaLabel: "Prompt" },
    });
    await flushPromises();

    const editor = wrapper.find("[contenteditable='true']");
    expect(editor.exists()).toBe(true);
    expect(editor.classes()).toContain("markdown-body");
    expect(editor.attributes("aria-label")).toBe("Prompt");
    expect(editor.text()).toBe("ready");
    expect(editor.find("strong").exists()).toBe(true);

    (wrapper.vm as unknown as { replaceMarkdown(value: string): void }).replaceMarkdown("**changed**");
    await flushPromises();
    expect(wrapper.find("strong").text()).toBe("changed");
    expect(wrapper.emitted("update:modelValue")?.at(-1)).toEqual(["**changed**"]);

    await wrapper.setProps({ modelValue: "" });
    await flushPromises();
    expect(wrapper.get(".markdown-editor").classes()).toContain("is-empty");
    expect(wrapper.get("[contenteditable='true']").attributes("data-placeholder")).toBe("Write");
    wrapper.unmount();
  });

  it("inserts and preserves a Markdown hard break on Shift Enter", async () => {
    const wrapper = mount(MarkdownEditor, {
      props: { modelValue: "first", placeholder: "Write", ariaLabel: "Prompt" },
    });
    await flushPromises();

    const editor = wrapper.get<HTMLElement>("[contenteditable='true']");
    const core = wrapper.findComponent(MarkdownEditorCore);
    const setup = core.vm.$ as unknown as { setupState: { get(): Editor | undefined } };
    const milkdown = setup.setupState.get();
    milkdown?.action((ctx) => {
      const view = ctx.get(editorViewCtx);
      view.dispatch(view.state.tr.setSelection(TextSelection.atEnd(view.state.doc)));
    });

    await editor.trigger("keydown", { key: "Enter", code: "Enter", shiftKey: true });

    expect(editor.find("br").exists()).toBe(true);
    const emitted = String(wrapper.emitted("update:modelValue")?.at(-1)?.[0]);
    expect(emitted).toBe("first\n");
    wrapper.unmount();
  });

  it("serializes consecutive Shift Enter line breaks as Markdown text", async () => {
    const wrapper = mount(MarkdownEditor, {
      props: { modelValue: "first", placeholder: "Write", ariaLabel: "Prompt" },
    });
    await flushPromises();

    const editor = wrapper.get<HTMLElement>("[contenteditable='true']");
    const core = wrapper.findComponent(MarkdownEditorCore);
    const setup = core.vm.$ as unknown as { setupState: { get(): Editor | undefined } };
    const milkdown = setup.setupState.get();
    const view = milkdown?.action((ctx) => ctx.get(editorViewCtx));
    if (!view) throw new Error("Milkdown editor did not start");
    view.dispatch(view.state.tr.setSelection(TextSelection.atEnd(view.state.doc)));

    await editor.trigger("keydown", { key: "Enter", code: "Enter", shiftKey: true });
    await editor.trigger("keydown", { key: "Enter", code: "Enter", shiftKey: true });
    view.dispatch(view.state.tr.insertText("second"));

    const emitted = String(wrapper.emitted("update:modelValue")?.at(-1)?.[0]);
    expect(emitted).toBe("first\n\nsecond");
    expect(emitted).not.toMatch(/<\/?br\s*\/?>/i);
    wrapper.unmount();
  });

  it("normalizes browser break tags before updating the draft", async () => {
    const wrapper = mount(MarkdownEditor, {
      props: { modelValue: "first</br>second", placeholder: "Write", ariaLabel: "Prompt" },
    });
    await flushPromises();

    const core = wrapper.findComponent(MarkdownEditorCore);
    const setup = core.vm.$ as unknown as { setupState: { get(): Editor | undefined } };
    const milkdown = setup.setupState.get();
    const view = milkdown?.action((ctx) => ctx.get(editorViewCtx));
    if (!view) throw new Error("Milkdown editor did not start");
    view.dispatch(view.state.tr.setSelection(TextSelection.atEnd(view.state.doc)).insertText("!"));

    const emitted = String(wrapper.emitted("update:modelValue")?.at(-1)?.[0]);
    expect(emitted).toBe("first\nsecond!");
    expect(emitted).not.toContain("</br>");
    wrapper.unmount();
  });

  it("renders GFM tables and strikethrough", async () => {
    const markdown = "| Name | Done |\n| --- | --- |\n| Build | yes |\n\n~~obsolete~~";
    const wrapper = mount(MarkdownEditor, {
      props: { modelValue: markdown, placeholder: "Write", ariaLabel: "Prompt" },
    });
    await flushPromises();

    expect(wrapper.get("table").text()).toContain("Build");
    expect(wrapper.get("del").text()).toBe("obsolete");
    wrapper.unmount();
  });

  it("replays the newest model value when it changes during startup", async () => {
    const wrapper = mount(MarkdownEditor, {
      props: { modelValue: "old draft", placeholder: "Write", ariaLabel: "Prompt" },
    });
    const update = wrapper.setProps({ modelValue: "new draft " });
    await update;
    await flushPromises();

    expect(wrapper.get("[contenteditable='true']").element.textContent).toBe("new draft ");
    wrapper.unmount();
  });
});
