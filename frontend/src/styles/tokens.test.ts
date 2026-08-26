import tokensFile from "./tokens.css?inline";
import { describe, expect, it } from "vitest";

async function tokensText(): Promise<string> {
  if (tokensFile.includes("--bg-app")) return tokensFile.replace(/\r\n?/g, "\n");
  const moduleName = ["node", "fs/promises"].join(":");
  const { readFile } = await import(/* @vite-ignore */ moduleName) as {
    readFile(path: string, encoding: "utf8"): Promise<string>;
  };
  return (await readFile("src/styles/tokens.css", "utf8")).replace(/\r\n?/g, "\n");
}

describe("teleported dialog theme inheritance", () => {
  it("publishes light theme variables from the document root", async () => {
    const tokens = await tokensText();
    expect(tokens).toContain(":root[data-theme=\"light\"],\n.app-shell[data-theme=\"light\"]");
    expect(tokens).toContain(":root[data-theme=\"system\"],\n  .app-shell[data-theme=\"system\"]");
  });
});
