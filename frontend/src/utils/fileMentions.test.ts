import { describe, expect, it } from "vitest";
import { buildRepositoryTree, formatFileMention } from "./fileMentions";

describe("file mentions", () => {
  it("quotes paths containing spaces and marks directories", () => {
    expect(formatFileMention("src/main.go")).toBe("@src/main.go");
    expect(formatFileMention("my docs/read me.md")).toBe('@"my docs/read me.md"');
    expect(formatFileMention("src", true)).toBe("@src/");
  });

  it("builds a directory-first tree from bounded flat paths", () => {
    const tree = buildRepositoryTree(["README.md", "src/view.ts", "src/main.ts"]);
    expect(tree.map((node) => node.name)).toEqual(["src", "README.md"]);
    expect(tree[0].children.map((node) => node.name)).toEqual(["main.ts", "view.ts"]);
  });
});
