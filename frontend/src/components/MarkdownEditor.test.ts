import { flushPromises, mount } from "@vue/test-utils";
import { editorViewCtx, type Editor } from "@milkdown/core";
import { TextSelection } from "@milkdown/prose/state";
import type { EditorView } from "@milkdown/prose/view";
import { describe, expect, it } from "vitest";
import MarkdownEditor from "./MarkdownEditor.vue";
import MarkdownEditorCore from "./MarkdownEditorCore.vue";

function typeText(view: EditorView, text: string) {
  for (const char of text) {
    const { from, to } = view.state.selection;
    const handled = view.someProp("handleTextInput", (handler) => handler(view, from, to, char, () => view.state.tr.insertText(char)));
    if (!handled) view.dispatch(view.state.tr.insertText(char));
  }
}

describe("MarkdownEditor", () => {
  it.each([
    ["- outer\n  - **nested**", "li li strong", "nested"],
    ["> ```html\n> <br> **literal**\n> ```", "blockquote pre code", "<br> **literal**"],
    ["1. code\n\n   ```html\n   <br> **literal**\n   ```", "li pre code", "<br> **literal**"],
  ])("preserves nested Markdown through editing and draft restoration (%s)", async (source, selector, expected) => {
    const wrapper = mount(MarkdownEditor, { props: { modelValue: source, placeholder: "Write", ariaLabel: "Prompt" } });
    await flushPromises();
    const setup = wrapper.findComponent(MarkdownEditorCore).vm.$ as unknown as { setupState: { get(): Editor | undefined } };
    const view = setup.setupState.get()?.action((ctx) => ctx.get(editorViewCtx));
    if (!view) throw new Error("Milkdown editor did not start");
    view.dispatch(view.state.tr.setSelection(TextSelection.atEnd(view.state.doc)).insertText("!"));
    const draft = String(wrapper.emitted("update:modelValue")?.at(-1)?.[0]);
    await wrapper.setProps({ modelValue: "another task" });
    await wrapper.setProps({ modelValue: draft });
    expect(wrapper.get(selector).text()).toBe(`${expected}!`);
    wrapper.unmount();
  });

  it.each([" inserted ", "**bold**", " **bold** ", "\nfirst\nsecond\n"])("pastes text at the captured caret while preserving its contents (%s)", async (text) => {
    const wrapper = mount(MarkdownEditor, { props: { modelValue: "beforeafter", placeholder: "Write", ariaLabel: "Prompt" } });
    await flushPromises();
    const setup = wrapper.findComponent(MarkdownEditorCore).vm.$ as unknown as { setupState: { get(): Editor | undefined } };
    const view = setup.setupState.get()?.action((ctx) => ctx.get(editorViewCtx));
    if (!view) throw new Error("Milkdown editor did not start");
    view.dispatch(view.state.tr.setSelection(TextSelection.create(view.state.doc, 7)));
    const insert = (wrapper.vm as unknown as { captureTextInsertion(): (text: string) => boolean }).captureTextInsertion();
    expect(insert(text)).toBe(true);
    const content = wrapper.get("[contenteditable='true']").element.textContent;
    expect(content).toBe(`before${text.replace("**bold**", "bold")}after`);
    wrapper.unmount();
  });

  it("inserts asynchronous clipboard text at the captured selection without overwriting new content", async () => {
    const wrapper = mount(MarkdownEditor, { props: { modelValue: "Review old please", placeholder: "Write", ariaLabel: "Prompt" } });
    await flushPromises();
    const setup = wrapper.findComponent(MarkdownEditorCore).vm.$ as unknown as { setupState: { get(): Editor | undefined } };
    const view = setup.setupState.get()?.action((ctx) => ctx.get(editorViewCtx));
    if (!view) throw new Error("editor not ready");
    view.dispatch(view.state.tr.setSelection(TextSelection.create(view.state.doc, 8, 12)));
    const editor = wrapper.vm as unknown as { captureTextInsertion(): (text: string, separate?: boolean) => boolean };
    const insert = editor.captureTextInsertion();
    view.dispatch(view.state.tr.setSelection(TextSelection.atEnd(view.state.doc)));
    expect(insert('@"a b.pdf" ', true)).toBe(true);
    expect(wrapper.get("[contenteditable='true']").text()).toBe('Review @"a b.pdf" please');
    const staleInsert = editor.captureTextInsertion();
    view.dispatch(view.state.tr.insertText("!"));
    expect(staleInsert("discarded")).toBe(false);
    expect(wrapper.get("[contenteditable='true']").text()).not.toContain("discarded");
    wrapper.unmount();
  });
  it.each(["**加粗文字**", "__bold text__", "**变量_name**", "__some_value__"])("renders typed bold (%s), ends the mark, and restores the draft", async (source) => {
    const wrapper = mount(MarkdownEditor, { props: { modelValue: "", placeholder: "Write", ariaLabel: "Prompt" } });
    await flushPromises();
    const setup = wrapper.findComponent(MarkdownEditorCore).vm.$ as unknown as { setupState: { get(): Editor | undefined } };
    const view = setup.setupState.get()?.action((ctx) => ctx.get(editorViewCtx));
    if (!view) throw new Error("Milkdown editor did not start");
    typeText(view, source);
    expect(wrapper.get("strong").text()).toBe(source.slice(2, -2));
    typeText(view, " plain");
    expect(wrapper.get("strong").text()).toBe(source.slice(2, -2));
    const draft = String(wrapper.emitted("update:modelValue")?.at(-1)?.[0]);
    await wrapper.setProps({ modelValue: "another task" });
    await wrapper.setProps({ modelValue: draft });
    expect(wrapper.get("strong").text()).toBe(source.slice(2, -2));
    expect(wrapper.get("[contenteditable='true']").text()).toBe(`${source.slice(2, -2)} plain`);
    wrapper.unmount();
  });

  it.each(["`ab`", "```text\nab\n```"])("keeps pasted Markdown literal inside code (%s)", async (source) => {
    const wrapper = mount(MarkdownEditor, { props: { modelValue: source, placeholder: "Write", ariaLabel: "Prompt" } });
    await flushPromises();
    const setup = wrapper.findComponent(MarkdownEditorCore).vm.$ as unknown as { setupState: { get(): Editor | undefined } };
    const view = setup.setupState.get()?.action((ctx) => ctx.get(editorViewCtx));
    if (!view) throw new Error("Milkdown editor did not start");
    view.dispatch(view.state.tr.setSelection(TextSelection.create(view.state.doc, 2)));
    const insert = (wrapper.vm as unknown as { captureTextInsertion(): (text: string) => boolean }).captureTextInsertion();
    expect(insert("**literal**")).toBe(true);
    expect(wrapper.get("code").text()).toBe("a**literal**b");
    expect(wrapper.find("strong").exists()).toBe(false);
    wrapper.unmount();
  });

  it.each(["**word__", "__word**", "** word**"])("does not turn invalid bold delimiters into bold (%s)", async (source) => {
    const wrapper = mount(MarkdownEditor, { props: { modelValue: "", placeholder: "Write", ariaLabel: "Prompt" } });
    await flushPromises();
    const setup = wrapper.findComponent(MarkdownEditorCore).vm.$ as unknown as { setupState: { get(): Editor | undefined } };
    const view = setup.setupState.get()?.action((ctx) => ctx.get(editorViewCtx));
    if (!view) throw new Error("Milkdown editor did not start");
    typeText(view, source);
    expect(wrapper.find("strong").exists()).toBe(false);
    wrapper.unmount();
  });

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

  it("shows saved soft line breaks as newlines when restoring a draft", async () => {
    const wrapper = mount(MarkdownEditor, {
      props: { modelValue: "first\nsecond", placeholder: "Write", ariaLabel: "Prompt" },
    });
    await flushPromises();
    expect(wrapper.find("p br").exists()).toBe(true);
    expect(wrapper.find('span[data-type="hardbreak"]').exists()).toBe(false);
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

  it("turns a typed numbered prefix into a list and preserves its number in Markdown", async () => {
    const wrapper = mount(MarkdownEditor, {
      props: { modelValue: "", placeholder: "Write", ariaLabel: "Prompt" },
    });
    await flushPromises();
    const setup = wrapper.findComponent(MarkdownEditorCore).vm.$ as unknown as { setupState: { get(): Editor | undefined } };
    const view = setup.setupState.get()?.action((ctx) => ctx.get(editorViewCtx));
    if (!view) throw new Error("Milkdown editor did not start");
    view.dispatch(view.state.tr.insertText("1."));
    const { from, to } = view.state.selection;
    view.someProp("handleTextInput", (handler) => handler(view, from, to, " ", () => view.state.tr.insertText(" ")));
    expect(wrapper.find("ol > li").exists()).toBe(true);
    view.dispatch(view.state.tr.insertText("first item"));
    expect(String(wrapper.emitted("update:modelValue")?.at(-1)?.[0])).toMatch(/^1\.\s+first item$/);
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
