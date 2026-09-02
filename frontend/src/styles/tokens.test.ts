import tokensFile from "./tokens.css?inline";
import tailwindFile from "./tailwind.css?inline";
import { describe, expect, it } from "vitest";

async function tokensText(): Promise<string> {
  if (tokensFile.includes("--bg-app")) return tokensFile.replace(/\r\n?/g, "\n");
  const moduleName = ["node", "fs/promises"].join(":");
  const { readFile } = await import(/* @vite-ignore */ moduleName) as {
    readFile(path: string, encoding: "utf8"): Promise<string>;
  };
  return (await readFile("src/styles/tokens.css", "utf8")).replace(/\r\n?/g, "\n");
}

async function tailwindText(): Promise<string> {
  if (tailwindFile.includes("--text-xs")) return tailwindFile.replace(/\r\n?/g, "\n");
  const moduleName = ["node", "fs/promises"].join(":");
  const { readFile } = await import(/* @vite-ignore */ moduleName) as {
    readFile(path: string, encoding: "utf8"): Promise<string>;
  };
  return (await readFile("src/styles/tailwind.css", "utf8")).replace(/\r\n?/g, "\n");
}

describe("teleported dialog theme inheritance", () => {
  it("publishes light theme variables from the document root", async () => {
    const tokens = await tokensText();
    expect(tokens).toContain(":root[data-theme=\"light\"],\n.app-shell[data-theme=\"light\"]");
    expect(tokens).toContain(":root[data-theme=\"system\"],\n  .app-shell[data-theme=\"system\"]");
  });

  it("publishes global font-family and root-size preference tokens", async () => {
    const tokens = await tokensText();
    expect(tokens).toContain(':root[data-font-family="system"]');
    expect(tokens).toContain(':root[data-font-family="serif"]');
    expect(tokens).toContain(':root[data-font-family="mono"]');
    expect(tokens).toContain(':root[data-font-size="12"]');
    expect(tokens).toContain(':root[data-font-size="18"]');
    expect(tokens).toContain("--font-size-delta: -2px");
    expect(tokens).toContain("--font-size-delta: 4px");
    expect(tokens).toContain("--font-size-root: 16px");
    expect(tokens).toContain("--font-size-body: calc(14px + var(--font-size-delta))");
  });

  it("scales Tailwind text utilities with the selected interface size", async () => {
    const tailwind = await tailwindText();
    expect(tailwind).toContain("--text-xs: calc(12px + var(--font-size-delta))");
    expect(tailwind).toContain("--text-sm: calc(14px + var(--font-size-delta))");
    expect(tailwind).toContain("--text-base: calc(16px + var(--font-size-delta))");
  });
});
