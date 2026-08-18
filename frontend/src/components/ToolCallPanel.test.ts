import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import ToolCallPanel from "./ToolCallPanel.vue";

describe("ToolCallPanel", () => {
  it("summarizes commands and exposes input and output copy actions", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    const wrapper = mount(ToolCallPanel, {
      props: { tool: { id: "tool-1", name: "bash", arguments: { command: "npm test" }, output: "all passed", status: "complete" } },
    });

    expect(wrapper.find("summary").text()).toContain("bash npm test");
    expect(wrapper.find("summary").text()).toContain("Complete");
    expect(wrapper.get(".tool-summary").element.nextElementSibling).toBe(wrapper.get(".tool-status").element);
    await wrapper.get('[aria-label="Copy tool input"]').trigger("click");
    await wrapper.get('[aria-label="Copy tool output"]').trigger("click");
    expect(writeText).toHaveBeenNthCalledWith(1, JSON.stringify({ command: "npm test" }, null, 2));
    expect(writeText).toHaveBeenNthCalledWith(2, "all passed");
  });

  it("keeps failed calls collapsed by default and labels projected output truncation", async () => {
    const wrapper = mount(ToolCallPanel, {
      props: { tool: { id: "tool-2", name: "read", arguments: { path: "main.go" }, output: "partial", truncated: true, status: "error" } },
    });

    expect(wrapper.get("details").attributes("open")).toBeUndefined();
    expect(wrapper.text()).toContain("Failed");
    await wrapper.get("summary").trigger("click");
    expect(wrapper.get("details").attributes("open")).toBeDefined();
    expect(wrapper.text()).toContain("Truncated in view");
  });

  it("contains clipboard permission failures", async () => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: vi.fn().mockRejectedValue(new Error("denied")) },
    });
    const wrapper = mount(ToolCallPanel, {
      props: { tool: { id: "tool-3", name: "read", output: "content", status: "complete" } },
    });

    await expect(wrapper.get('[aria-label="Copy tool output"]').trigger("click")).resolves.toBeUndefined();
  });

  it("shows duration and a colored inline diff for edit and write tools", () => {
    const wrapper = mount(ToolCallPanel, {
      props: {
        tool: {
          id: "tool-4", name: "edit", arguments: { path: "main.go" }, output: "ok", status: "complete", durationMs: 16700,
          diff: { path: "main.go", text: "- 1 old\n+ 1 new" },
        },
      },
    });

    expect(wrapper.get("summary").text()).toContain("17s");
    expect(wrapper.get(".tool-diff-badge").text()).toBe("diff");
    expect(wrapper.get(".tool-diff-section").text()).toContain("main.go");
    expect(wrapper.findAll(".diff-line")[0].classes()).toContain("is-removed");
    expect(wrapper.findAll(".diff-line")[1].classes()).toContain("is-added");
  });
});
