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
    expect(tokens).toContain("--font-size-delta: -1.5px");
    expect(tokens).toContain("--font-size-delta: 2px");
    expect(tokens).toContain("--font-size-root: 16px");
    expect(tokens).toContain("--font-size-meta: calc(11px + var(--font-size-delta))");
    expect(tokens).toContain("--font-size-control: calc(13px + var(--font-size-delta))");
    expect(tokens).toContain("--font-size-body: calc(14px + var(--font-size-delta))");
  });

  it("publishes the Codex-style code preview palette", async () => {
    const tokens = await tokensText();
    expect(tokens).toContain("--preview-keyword: #d53538");
    expect(tokens).toContain("--preview-declaration: #751ed9");
    expect(tokens).toContain("--preview-symbol: #bd5800");
    expect(tokens).toContain("--preview-string: #008809");
    expect(tokens).toContain("--preview-operator: #0071ea");
  });

  it("scales Tailwind text utilities with the selected interface size", async () => {
    const tailwind = await tailwindText();
    expect(tailwind).toContain("--text-xs: var(--font-size-meta)");
    expect(tailwind).toContain("--text-sm: var(--font-size-control)");
    expect(tailwind).toContain("--text-base: var(--font-size-body)");
  });
});
