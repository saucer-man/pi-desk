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
});
