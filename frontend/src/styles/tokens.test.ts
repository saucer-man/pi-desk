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
    expect(tokens).toContain("--bg-settings: #f8f8f8");
    expect(tokens).toContain("--bg-conversation: #f8f8f8");
    expect(tokens).toContain("--bg-card: #ffffff");
    expect(tokens).toContain("--bg-composer: var(--bg-card)");
  });

  it("publishes global font-family and root-size preference tokens", async () => {
    const tokens = await tokensText();
    expect(tokens).toContain('--font-interface-body: "PingFang SC",');
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

  it("publishes the selectable code preview palettes and sampled ZCode surfaces", async () => {
    const tokens = await tokensText();
    for (const theme of ["github-light", "github-dark", "vitesse-light", "vitesse-dark", "minimal-light", "minimal-dark", "github-hc-light", "github-hc-dark", "catppuccin-latte", "catppuccin-mocha"]) {
      expect(tokens).toContain(`data-code-theme="${theme}"`);
    }
    expect(tokens).toContain("--preview-toolbar-bg: #f6f6f6");
    expect(tokens).toContain("--preview-bg: #ffffff");
    expect(tokens).toContain("--panel-surface: #ffffff");
    expect(tokens).toContain("--panel-selected: #f3f3f3");
    expect(tokens).toContain("--preview-border: #e0e0e0");
    expect(tokens).toContain("--diff-add-bg: #e3f2e3");
    expect(tokens).toContain("--diff-delete-bg: #ffe4e0");
    expect(tokens).toContain("--sheet-bg: #f8f8f8");
    expect(tokens).toContain("--sheet-header-bg: #f6f6f6");
    expect(tokens).toContain("--sheet-cell-bg: #ffffff");
    expect(tokens).toContain("--sheet-border: #e0e0e0");
    expect(tokens).toContain("--sheet-text: #242424");
  });

  it("scales Tailwind text utilities with the selected interface size", async () => {
    const tailwind = await tailwindText();
    expect(tailwind).toContain("--text-xs: var(--font-size-meta)");
    expect(tailwind).toContain("--text-sm: var(--font-size-control)");
    expect(tailwind).toContain("--text-base: var(--font-size-body)");
  });
});
