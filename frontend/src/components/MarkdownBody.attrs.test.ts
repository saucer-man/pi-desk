import { mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import { describe, expect, it } from "vitest";
import MarkdownBody from "./MarkdownBody.vue";

// 回归：MarkdownBody 是多根组件，若不显式 v-bind="$attrs"，调用方传的 class 会被 Vue 静默丢弃，
// 导致 .file-markdown-preview 的 overflow/flex 布局失效（markdown 预览无法滚动）。
describe("MarkdownBody attribute fallthrough", () => {
  it("lands the external class on the rendered markdown container", () => {
    const wrapper = mount(MarkdownBody, {
      props: { text: "# heading\n\nbody" },
      attrs: { class: "file-markdown-preview" },
      global: { plugins: [createPinia()] },
      attachTo: document.body,
    });
    // 多根组件的 wrapper.element 是 Fragment，只能按 DOM 查
    expect(document.querySelector(".file-markdown-preview.markdown-body")).not.toBeNull();
    expect(document.querySelectorAll(".file-markdown-preview")).toHaveLength(1);
    wrapper.unmount();
  });

  it("lands the external class on the oversized fallback <pre>", () => {
    const wrapper = mount(MarkdownBody, {
      props: { text: "x".repeat(100_001) },
      attrs: { class: "file-markdown-preview" },
      global: { plugins: [createPinia()] },
      attachTo: document.body,
    });
    expect(document.querySelector("pre.file-markdown-preview.oversized-message")).not.toBeNull();
    wrapper.unmount();
  });
});
