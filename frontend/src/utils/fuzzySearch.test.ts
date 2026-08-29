import { describe, expect, it } from "vitest";
import { fuzzyScore, rankFuzzy } from "./fuzzySearch";

describe("fuzzy search", () => {
  it("matches ordered characters and prefers file-name and boundary matches", () => {
    expect(fuzzyScore("components/ComposerBar.vue", "cmpbar")).toBeGreaterThanOrEqual(0);
    expect(fuzzyScore("components/ComposerBar.vue", "zbar")).toBe(-1);
    const files = [
      { name: "bar.ts", path: "deep/bar.ts" },
      { name: "other.ts", path: "components/ComposerBar.vue" },
    ];
    expect(rankFuzzy(files, "bar", (file) => [file.name, file.path])[0].name).toBe("bar.ts");
  });
});
