import { describe, expect, it } from "vitest";
import { buildToolDiff, mergeToolDiffs } from "./toolDiff";

describe("buildToolDiff", () => {
  it("prefers the display diff returned by Pi edit tool results", () => {
    const diff = buildToolDiff(
      "edit",
      { path: "main.go", edits: [{ oldText: "old", newText: "new" }] },
      { diff: "- 1 old\n+ 1 new", patch: "ignored" },
    );

    expect(diff).toEqual({
      path: "main.go",
      text: "- 1 old\n+ 1 new",
      edits: [{ oldText: "old", newText: "new", firstChangedLine: 1 }],
    });
  });

  it("recovers legacy edit and write diffs from persisted arguments", () => {
    expect(buildToolDiff("edit", { path: "main.go", oldText: "old", newText: "new" })?.text).toBe("- old\n+ new");
    expect(buildToolDiff("write", { path: "new.go", content: "one\ntwo" })?.text).toBe("+   1 one\n+   2 two");
  });

  it("collapses repeated edits into the original-to-final change", () => {
    const first = buildToolDiff("edit", { path: "main.go", oldText: "xxx", newText: "xxx1" }, { diff: "- 1 xxx\n+ 1 xxx1", firstChangedLine: 1 })!;
    const second = buildToolDiff("edit", { path: "main.go", oldText: "xxx1", newText: "xxx2" }, { diff: "- 1 xxx1\n+ 1 xxx2", firstChangedLine: 1 })!;

    expect(mergeToolDiffs([first, second])).toBe("@@ -1,1 +1,1 @@\n-xxx\n+xxx2");
    expect(mergeToolDiffs([first, buildToolDiff("edit", { path: "main.go", oldText: "xxx1", newText: "xxx" }, { diff: "- 1 xxx1\n+ 1 xxx", firstChangedLine: 1 })!])).toBe("");
  });

  it("collapses a later edit that expands around an earlier edited area", () => {
    const first = buildToolDiff("edit", { path: "main.go", oldText: "old", newText: "middle" }, { diff: "- 2 old\n+ 2 middle", firstChangedLine: 2 })!;
    const second = buildToolDiff("edit", { path: "main.go", oldText: "before\nmiddle\nafter", newText: "before\nfinal\nafter" }, { diff: "- 2 middle\n+ 2 final", firstChangedLine: 2 })!;

    expect(mergeToolDiffs([first, second])).toBe("@@ -2,1 +2,1 @@\n-old\n+final");
  });

  it("recovers line starts for multiple edits from Pi's display diff", () => {
    const diff = buildToolDiff("edit", { path: "main.go", edits: [
      { oldText: "old", newText: "new" },
      { oldText: "left", newText: "right" },
    ] }, { diff: "- 10 old\n+ 10 new\n     ...\n- 25 left\n+ 25 right", firstChangedLine: 10 });

    expect(diff?.edits?.map((edit) => edit.firstChangedLine)).toEqual([10, 25]);
  });
});
