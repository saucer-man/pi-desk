import workbenchFile from "./workbench.css?inline";
import { describe, expect, it } from "vitest";

async function workbenchText(): Promise<string> {
  if (workbenchFile.includes(".app-shell")) return workbenchFile.replace(/\r\n?/g, "\n");
  const moduleName = ["node", "fs/promises"].join(":");
  const { readFile } = await import(/* @vite-ignore */ moduleName) as {
    readFile(path: string, encoding: "utf8"): Promise<string>;
  };
  return (await readFile("src/styles/workbench.css", "utf8")).replace(/\r\n?/g, "\n");
}

describe("responsive workbench layout", () => {
  it("uses one application topbar and a two-column shell", async () => {
    const css = await workbenchText();
    expect(css).toMatch(/--topbar-height:\s*52px/);
    expect(css).toMatch(/\.app-menubar\s*{\s*display:\s*none/);
    expect(css).toMatch(/grid-template-columns:\s*var\(--sidebar-width\) minmax\(0, 1fr\)/);
    expect(css).toMatch(/grid-template-rows:\s*var\(--topbar-height\) minmax\(0, 1fr\)/);
  });

  it("styles the workspace application control as a compact split button", async () => {
    const css = await workbenchText();
    expect(css).toMatch(/\.workspace-application-split\s*{[^}]*display:\s*inline-flex[^}]*border:\s*1px solid/s);
    expect(css).toMatch(/\.workspace-application-toggle\s*{[^}]*border-left:\s*1px solid/s);
    expect(css).toMatch(/@media \(max-width: 760px\)[\s\S]*\.topbar-actions \.workspace-application-anchor\s*{\s*display:\s*none/);
  });

  it("keeps the reading and composer axes bounded", async () => {
    const css = await workbenchText();
    expect(css).toMatch(/\.timeline\s*{[^}]*width:\s*min\(960px, 100%\)/s);
    expect(css).toMatch(/\.composer-wrap\s*{[^}]*--composer-max-width:\s*880px[^}]*--composer-stack-total-inset:\s*24px/s);
    expect(css).toMatch(/\.composer,[^}]*width:\s*min\(var\(--composer-max-width\), 100%\)/s);
    expect(css).toMatch(/\.composer-token-metrics\s*{[^}]*display:\s*grid[^}]*width:\s*min\(860px, calc\(100% - 20px\)\)/s);
  });

  it("defines inspector drawer and compact sidebar breakpoints", async () => {
    const css = await workbenchText();
    expect(css).toContain("@media (max-width: 1279px)");
    expect(css).toMatch(/@media \(max-width: 1279px\)[\s\S]*\.app-shell\.is-inspector-open \.conversation-outline\s*{[^}]*right:\s*min\(var\(--inspector-width\), calc\(100% - 58px\)\)/);
    expect(css).toContain("@media (max-width: 760px)");
    expect(css).toContain("@media (max-width: 520px)");
    expect(css).toMatch(/@media \(max-width: 520px\)[\s\S]*--composer-stack-total-inset:\s*12px/);
  });
});
