import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import CodePreview from "./CodePreview.vue";

describe("CodePreview", () => {
  it("highlights recognized source files and keeps unknown files readable", async () => {
    const wrapper = mount(CodePreview, {
      props: { path: "main.go", content: "package main\n\nfunc main() {}", label: "File preview content" },
    });

    await vi.waitFor(() => expect(wrapper.find(".tok-keyword").exists()).toBe(true));
    expect(wrapper.text()).toContain("package main");

    await wrapper.setProps({ path: "notes.unknown", content: "plain content" });
    await vi.waitFor(() => expect(wrapper.text()).toBe("plain content"));
    expect(wrapper.find("[class^='tok-']").exists()).toBe(false);
  });
});
