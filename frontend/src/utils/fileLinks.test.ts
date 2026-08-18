import { describe, expect, it } from "vitest";
import { resolveWorkspaceFileLink } from "./fileLinks";

describe("resolveWorkspaceFileLink", () => {
  it("resolves relative, absolute, file URL, and source-location links inside a Windows workspace", () => {
    expect(resolveWorkspaceFileLink("results/tg_groups.csv", "D:\\repo")).toEqual({
      relativePath: "results/tg_groups.csv",
      absolutePath: "D:\\repo\\results\\tg_groups.csv",
      name: "tg_groups.csv",
      line: undefined,
    });
    expect(resolveWorkspaceFileLink("D:/repo/scripts/join.py:19", "D:\\repo")).toMatchObject({
      relativePath: "scripts/join.py", absolutePath: "D:\\repo\\scripts\\join.py", line: 19,
    });
    expect(resolveWorkspaceFileLink("file:///D:/repo/docs/Guide%20One.md#L8", "D:\\repo")).toMatchObject({
      relativePath: "docs/Guide One.md", name: "Guide One.md", line: 8,
    });
  });

  it("resolves POSIX paths and rejects external links and workspace escapes", () => {
    expect(resolveWorkspaceFileLink("./src/main.ts", "/work/repo")).toMatchObject({
      relativePath: "src/main.ts", absolutePath: "/work/repo/src/main.ts",
    });
    expect(resolveWorkspaceFileLink("https://example.com/file.ts", "D:\\repo")).toBeUndefined();
    expect(resolveWorkspaceFileLink("mailto:user@example.com", "D:\\repo")).toBeUndefined();
    expect(resolveWorkspaceFileLink("../outside.txt", "D:\\repo")).toBeUndefined();
    expect(resolveWorkspaceFileLink("D:/other/outside.txt", "D:\\repo")).toBeUndefined();
  });
});
