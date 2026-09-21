import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import CodePreview from "./CodePreview.vue";

describe("CodePreview", () => {
  it("highlights recognized source files and keeps unknown files readable", async () => {
    const wrapper = mount(CodePreview, {
      props: { path: "main.go", content: "package main\n\nfunc main() {}", label: "File preview content", flush: true },
    });

    await vi.waitFor(() => expect(wrapper.find(".tok-keyword").exists()).toBe(true));
    expect(wrapper.get("pre").classes()).not.toContain("p-0!");
    expect(wrapper.get("pre").classes()).not.toContain("text-xs");
    expect(wrapper.text()).toContain("package main");
    expect(wrapper.findAll(".file-preview-line-number").map((line) => line.text())).toEqual(["1", "2", "3"]);

    await wrapper.setProps({ path: "sample.ts", content: "const value = computed(() => appStore.name);" });
    await vi.waitFor(() => expect(wrapper.find(".tok-definitionKeyword").text()).toBe("const"));
    expect(wrapper.find(".tok-function").text()).toBe("computed");
    expect(wrapper.find(".tok-propertyName").text()).toBe("name");

    await wrapper.setProps({ path: "notes.unknown", content: "plain\ncontent" });
    await vi.waitFor(() => expect(wrapper.findAll(".file-preview-line-text").map((line) => line.text())).toEqual(["plain", "content"]));
    expect(wrapper.findAll(".file-preview-line-number").map((line) => line.text())).toEqual(["1", "2"]);
    expect(wrapper.find("[class^='tok-']").exists()).toBe(false);
  });
});
