import { describe, expect, it } from "vitest";
import { buildToolDiff } from "./toolDiff";

describe("buildToolDiff", () => {
  it("prefers the display diff returned by Pi edit tool results", () => {
    const diff = buildToolDiff(
      "edit",
      { path: "main.go", edits: [{ oldText: "old", newText: "new" }] },
      { diff: "- 1 old\n+ 1 new", patch: "ignored" },
    );

    expect(diff).toEqual({ path: "main.go", text: "- 1 old\n+ 1 new" });
  });

  it("recovers legacy edit and write diffs from persisted arguments", () => {
    expect(buildToolDiff("edit", { path: "main.go", oldText: "old", newText: "new" })?.text).toBe("- old\n+ new");
    expect(buildToolDiff("write", { path: "new.go", content: "one\ntwo" })?.text).toBe("+   1 one\n+   2 two");
  });
});
