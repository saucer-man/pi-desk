import { flushPromises, mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import MarkdownEditor from "./MarkdownEditor.vue";

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

  it("continues a list on Enter and emits the new Markdown synchronously", async () => {
    const wrapper = mount(MarkdownEditor, {
      props: { modelValue: "- first", placeholder: "Write", ariaLabel: "Prompt" },
    });
    await flushPromises();

    const editor = wrapper.get<HTMLElement>("[contenteditable='true']");
    const text = editor.get("li p").element.firstChild;
    if (!text) throw new Error("list item text was not rendered");
    editor.element.focus();
    const range = document.createRange();
    range.setStart(text, text.textContent?.length ?? 0);
    range.collapse(true);
    window.getSelection()?.removeAllRanges();
    window.getSelection()?.addRange(range);

    await editor.trigger("keydown", { key: "Enter", code: "Enter" });

    expect(editor.findAll("li")).toHaveLength(2);
    const emitted = String(wrapper.emitted("update:modelValue")?.at(-1)?.[0]);
    expect(emitted).toContain("first");
    expect(emitted.match(/^\* /gm)).toHaveLength(2);
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
