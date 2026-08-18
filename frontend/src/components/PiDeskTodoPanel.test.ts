import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import PiDeskTodoPanel from "./PiDeskTodoPanel.vue";

describe("PiDeskTodoPanel", () => {
  it("renders compact ordered read-only tasks with progress and local collapse", async () => {
    const wrapper = mount(PiDeskTodoPanel, {
      props: {
        todo: {
          completed: 1,
          total: 3,
          items: [
            { id: 1, text: "Inspect repository", done: true },
            { id: 2, text: "Implement layout", done: false },
            { id: 3, text: "Run verification", done: false },
          ],
        },
      },
    });

    expect(wrapper.get(".pi-desk-todo-heading").text()).toContain("Todo");
    expect(wrapper.get(".pi-desk-todo-heading").text()).toContain("1/3");
    expect(wrapper.findAll(".pi-desk-todo-row").map((row) => row.text())).toEqual([
      "#1Inspect repository", "#2Implement layout", "#3Run verification",
    ]);
    expect(wrapper.findAll(".pi-desk-todo-row")[0].classes()).toContain("is-done");
    expect(Number.parseFloat(wrapper.get<HTMLElement>(".pi-desk-todo-progress span").element.style.width)).toBeCloseTo(33.33, 1);
    expect(wrapper.findAll("button")).toHaveLength(1);

    const toggle = wrapper.get('button[aria-label="Collapse todo list"]');
    await toggle.trigger("click");
    expect(wrapper.classes()).toContain("is-collapsed");
    expect(wrapper.get(".pi-desk-todo-content").attributes("aria-hidden")).toBe("true");
    expect(wrapper.get("button").attributes("aria-label")).toBe("Expand todo list");
  });

  it("marks fully completed progress without reordering or hiding rows", () => {
    const wrapper = mount(PiDeskTodoPanel, {
      props: {
        todo: {
          completed: 2,
          total: 2,
          items: [
            { id: 1, text: "First", done: true },
            { id: 2, text: "Second", done: true },
          ],
        },
      },
    });

    expect(wrapper.classes()).toContain("is-complete");
    expect(wrapper.findAll(".pi-desk-todo-row").map((row) => row.text())).toEqual(["#1First", "#2Second"]);
    expect(wrapper.get<HTMLElement>(".pi-desk-todo-progress span").element.style.width).toBe("100%");
  });
});
