import { describe, expect, it } from "vitest";
import { parsePiDeskTodoWidget } from "./todoWidget";

describe("Pi Desk todo widget projection", () => {
  it("ignores status headings and always restores numeric id order", () => {
    const projection = parsePiDeskTodoWidget({
      key: "pi-desk-todo",
      lines: [
        "── 待办 ──",
        "☐ #2 second",
        "☐ #4 fourth",
        "── 已完成 ──",
        "☑ #1 first",
        "[x] #3: third",
      ],
    });

    expect(projection).toEqual({
      completed: 2,
      total: 4,
      items: [
        { id: 1, text: "first", done: true },
        { id: 2, text: "second", done: false },
        { id: 3, text: "third", done: true },
        { id: 4, text: "fourth", done: false },
      ],
    });
  });

  it("parses the bundled extension format and collapsed progress summary", () => {
    expect(parsePiDeskTodoWidget({
      key: "pi-desk-todo",
      lines: ["-- Todo --", "[x] #1 done", "[ ] #2 pending"],
    })).toMatchObject({ completed: 1, total: 2 });
    expect(parsePiDeskTodoWidget({ key: "pi-desk-todo", lines: ["3/5"] })).toEqual({ items: [], completed: 3, total: 5 });
  });

  it("does not claim generic or malformed extension widgets", () => {
    expect(parsePiDeskTodoWidget({ key: "plan", lines: ["[ ] #1 inspect"] })).toBeUndefined();
    expect(parsePiDeskTodoWidget({ key: "pi-desk-todo", lines: ["arbitrary extension output"] })).toBeUndefined();
  });
});
