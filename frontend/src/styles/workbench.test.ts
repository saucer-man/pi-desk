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
    expect(css).toMatch(/--conversation-content-width:\s*960px/);
    expect(css).toMatch(/--composer-overlay-reserve:\s*164px/);
    expect(css).toMatch(/\.conversation-scroll-region\s*{[^}]*grid-column:\s*1[^}]*grid-row:\s*1 \/ -1/s);
    expect(css).toMatch(/\.timeline\s*{[^}]*width:\s*100%[^}]*padding:\s*36px var\(--conversation-inline-space\) calc\(var\(--composer-overlay-reserve\) \+ 28px\)[^}]*scroll-padding-bottom:\s*var\(--composer-overlay-reserve\)/s);
    expect(css).toMatch(/\.composer-wrap\s*{[^}]*--composer-max-width:\s*var\(--conversation-content-width\)[^}]*--composer-stack-total-inset:\s*24px[^}]*--conversation-inline-space:/s);
    expect(css).toMatch(/\.composer-wrap\s*{[^}]*grid-column:\s*1[^}]*grid-row:\s*2[^}]*background:\s*linear-gradient\(to right, var\(--bg-workspace\) calc\(100% - 15px\), transparent calc\(100% - 15px\)\)[^}]*pointer-events:\s*none/s);
    expect(css).toMatch(/\.composer-wrap\s*{[^}]*padding:\s*10px var\(--conversation-inline-space\) 22px/s);
    expect(css).toMatch(/\.composer,[^}]*width:\s*min\(var\(--composer-max-width\), 100%\)/s);
    expect(css).toMatch(/\.composer-token-metrics\s*{[^}]*display:\s*grid[^}]*width:\s*min\(var\(--composer-max-width\), 100%\)/s);
    expect(css).toMatch(/\.message-row\[data-role="assistant"\] \.message-content,[^}]*max-width:\s*100%/s);
  });

  it("keeps the conversation scrollbar at the workspace edge when the inspector opens", async () => {
    const css = await workbenchText();
    expect(css).toMatch(/\.app-shell\.is-inspector-open \.workspace-shell\s*{[^}]*padding-right:\s*0/s);
    expect(css).toMatch(/\.app-shell\.is-inspector-open \.timeline\s*{[^}]*--inspector-width\) - 18px[^}]*padding-right:\s*calc\(var\(--inspector-width\) \+ 18px \+ var\(--conversation-inline-space\)\)/s);
    expect(css).toMatch(/\.app-shell\.is-inspector-open \.composer-wrap\s*{[^}]*--inspector-width\) - 18px[^}]*padding-right:\s*calc\(var\(--inspector-width\) \+ 18px \+ var\(--conversation-inline-space\)\)/s);
    expect(css).toMatch(/\.composer,[^}]*pointer-events:\s*auto/s);
  });

  it("defines inspector drawer and compact sidebar breakpoints", async () => {
    const css = await workbenchText();
    expect(css).toContain("@media (max-width: 1279px)");
    expect(css).toContain("@media (max-width: 760px)");
    expect(css).toContain("@media (max-width: 520px)");
    expect(css).toMatch(/@media \(max-width: 520px\)[\s\S]*--composer-stack-total-inset:\s*12px/);
  });

  it("keeps the inspector resize target on the panel edge without a visible rail", async () => {
    const css = await workbenchText();
    expect(css).toMatch(/\.pane-resizer\.is-right\s*{[^}]*right:\s*calc\(var\(--inspector-width\) \+ 14px\)/s);
    expect(css).toMatch(/\.pane-resizer\.is-right::after\s*{[^}]*content:\s*none/s);
  });

  it("keeps settings controls compact and flush with the dialog body", async () => {
    const css = await workbenchText();
    expect(css).toMatch(/\.dialog-body\.settings-layout\s*{[^}]*padding:\s*0 !important/s);
    expect(css).toMatch(/\.dialog-body\.settings-layout\s*{[^}]*padding-block:\s*0 !important[^}]*padding-inline:\s*0 !important/s);
    expect(css).toMatch(/\.settings-layout\s*{[^}]*grid-template-columns:\s*156px minmax\(0, 1fr\)/s);
    expect(css).toMatch(/\.settings-nav\s*{[^}]*gap:\s*0[^}]*padding:\s*0/s);
    expect(css).toMatch(/\.settings-nav button\s*{[^}]*width:\s*100%[^}]*height:\s*34px[^}]*border-radius:\s*0[^}]*font-size:\s*calc\(11\.5px \+ var\(--font-size-delta\)\)/s);
    expect(css).not.toMatch(/\.settings-dialog \.text-button\s*{/);
    expect(css).toMatch(/\.settings-dialog \.setting-row-select select\s*{[^}]*width:\s*128px[^}]*flex-basis:\s*128px/s);
    expect(css).not.toMatch(/\.settings-dialog \.setting-row-select select\s*{[^}]*font-size:/s);
    expect(css).toMatch(/\.settings-dialog \.provider-header-row\s*{[^}]*grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\) 28px/s);
  });

  it("uses a dense model management layout", async () => {
    const css = await workbenchText();
    expect(css).toMatch(/\.settings-dialog \.model-config-header\s*{[^}]*margin:\s*0 !important[^}]*padding:\s*9px 12px !important/s);
    expect(css).toMatch(/\.settings-dialog \.model-manager-layout\s*{[^}]*grid-template-columns:\s*200px minmax\(0, 1fr\) !important/s);
    expect(css).toMatch(/\.settings-dialog \.model-config-list > button,[^}]*min-height:\s*32px !important/s);
    expect(css).not.toMatch(/\.settings-dialog \.model-field > input,/);
    expect(css).toMatch(/\.settings-dialog \.model-field > textarea\s*{[^}]*min-height:\s*72px !important/s);
    expect(css).toMatch(/\.settings-dialog \.model-editor-actions\s*{[^}]*padding:\s*6px 14px 8px/s);
  });
});
