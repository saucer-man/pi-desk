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
  it("keeps the dynamic workbench out of the shell grid flow", async () => {
    const css = await workbenchText();
    expect(css).toMatch(/\.inspector\.panel-workbench\s*{[^}]*position:\s*absolute[^}]*display:\s*flex/s);
    expect(css).toMatch(/\.app-shell\.is-inspector-expanded \.panel-workbench\s*{[^}]*width:\s*auto/s);
    expect(css).toMatch(/\.panel-body\.has-tree\s*{[^}]*grid-template-columns:\s*minmax\(0, 1fr\) minmax\(160px, 32%\)/s);
  });
  it("uses one application topbar and a two-column shell", async () => {
    const css = await workbenchText();
    expect(css).toMatch(/--topbar-height:\s*44px/);
    expect(css).toMatch(/\.app-menubar\s*{\s*display:\s*none/);
    expect(css).toMatch(/\.topbar\s*{[^}]*background:\s*var\(--bg-conversation\)/s);
    expect(css).toMatch(/grid-template-columns:\s*var\(--sidebar-width\) minmax\(0, 1fr\)/);
    expect(css).toMatch(/grid-template-rows:\s*var\(--topbar-height\) minmax\(0, 1fr\)/);
    expect(css).toMatch(/\.inspector\s*{[^}]*grid-template-rows:\s*34px minmax\(0, 1fr\)/s);
    expect(css).toMatch(/\.inspector-header\s*{[^}]*min-height:\s*34px/s);
  });

  it("styles the workspace application control as a compact split button", async () => {
    const css = await workbenchText();
    expect(css).toMatch(/\.workspace-application-split\s*{[^}]*display:\s*inline-flex[^}]*border:\s*1px solid/s);
    expect(css).toMatch(/\.workspace-application-toggle\s*{[^}]*border-left:\s*1px solid/s);
    expect(css).toMatch(/@media \(max-width: 760px\)[\s\S]*\.topbar-actions \.workspace-application-anchor\s*{\s*display:\s*none/);
  });

  it("keeps the reading and composer axes bounded", async () => {
    const css = await workbenchText();
    expect(css).toMatch(/--conversation-content-width:\s*880px/);
    expect(css).toMatch(/\.conversation-pane\s*{[^}]*--bg-workspace:\s*var\(--bg-conversation\)/s);
    expect(css).toMatch(/--composer-overlay-reserve:\s*0px/);
    expect(css).toMatch(/\.conversation-scroll-region\s*{[^}]*grid-column:\s*1[^}]*grid-row:\s*1 \/ -1/s);
    expect(css).toMatch(/\.timeline\s*{[^}]*width:\s*100%[^}]*padding:\s*24px var\(--conversation-inline-space\) calc\(var\(--composer-overlay-reserve\) \+ 20px\)[^}]*scroll-padding-bottom:\s*var\(--composer-overlay-reserve\)/s);
    expect(css).toMatch(/\.composer-wrap\s*{[^}]*--composer-max-width:\s*var\(--conversation-content-width\)[^}]*--composer-stack-total-inset:\s*20px[^}]*--conversation-inline-space:/s);
    expect(css).toMatch(/\.composer-wrap\s*{[^}]*grid-column:\s*1[^}]*grid-row:\s*2[^}]*background:\s*linear-gradient\(to right, var\(--bg-workspace\) calc\(100% - 15px\), transparent calc\(100% - 15px\)\)[^}]*pointer-events:\s*none/s);
    expect(css).toMatch(/\.composer-wrap\s*{[^}]*padding:\s*8px var\(--conversation-inline-space\) 14px/s);
    expect(css).toMatch(/\.composer,[^}]*width:\s*min\(var\(--composer-max-width\), 100%\)/s);
    expect(css).toMatch(/\.composer-token-metrics\s*{[^}]*display:\s*grid[^}]*width:\s*min\(var\(--composer-max-width\), 100%\)/s);
    expect(css).toMatch(/\.message-row\[data-role="assistant"\] \.message-content,[^}]*max-width:\s*100%/s);
  });

  it("keeps the conversation scrollbar at the workspace edge when the inspector opens", async () => {
    const css = await workbenchText();
    expect(css).toMatch(/\.app-shell\.is-inspector-open \.workspace-shell\s*{[^}]*padding-right:\s*0/s);
    expect(css).toMatch(/\.app-shell\.is-inspector-open \.timeline\s*{[^}]*--inspector-width\) - var\(--conversation-content-width\)[^}]*padding-right:\s*calc\(var\(--inspector-width\) \+ var\(--conversation-inline-space\)\)/s);
    expect(css).toMatch(/\.app-shell\.is-inspector-open \.composer-wrap\s*{[^}]*--inspector-width\) - var\(--conversation-content-width\)[^}]*padding-right:\s*calc\(var\(--inspector-width\) \+ var\(--conversation-inline-space\)\)/s);
    expect(css).toMatch(/\.composer,[^}]*pointer-events:\s*auto/s);
  });

  it("restores hit testing for queue and todo panels while keeping the outer scrollbar accessible", async () => {
    const css = await workbenchText();
    expect(css).toMatch(/\.composer-wrap\s*{[^}]*pointer-events:\s*none/s);
    expect(css).toMatch(/\.composer-stack-panel\s*{[^}]*pointer-events:\s*auto/s);
    expect(css).toMatch(/@media \(max-width: 760px\)[\s\S]*\.timeline\s*{[^}]*padding:\s*26px 16px calc\(var\(--composer-overlay-reserve\) \+ 18px\)/);
  });

  it("defines inspector drawer and compact sidebar breakpoints", async () => {
    const css = await workbenchText();
    expect(css).toContain("@media (max-width: 1279px)");
    expect(css).toContain("@media (max-width: 760px)");
    expect(css).toContain("@media (max-width: 520px)");
    expect(css).toMatch(/@media \(max-width: 520px\)[\s\S]*--composer-stack-total-inset:\s*12px/);
  });

  it("matches the neutral sidebar density and active surface", async () => {
    const css = await workbenchText();
    expect(css).toMatch(/\.sidebar\s*{[^}]*background:\s*var\(--bg-sidebar\)/s);
    expect(css).toMatch(/:root\[data-theme="light"\] \.sidebar,[^}]*--bg-active:\s*#e1e1e3/s);
    expect(css).toMatch(/\.primary-nav button\s*{[^}]*height:\s*40px[^}]*font-size:\s*calc\(15px \+ var\(--font-size-delta\)\)/s);
    expect(css).toMatch(/\.thread-time\s*{[^}]*font-variant-numeric:\s*tabular-nums/s);
  });

  it("gives markdown file previews a calm reading layout", async () => {
    const css = await workbenchText();
    expect(css).toMatch(/\.file-preview-toolbar\s*{[^}]*min-height:\s*40px[^}]*padding:\s*0 10px/s);
    expect(css).toMatch(/\.markdown-preview-toggle\s*{[^}]*min-height:\s*40px[^}]*background:\s*var\(--bg-conversation\)/s);
    expect(css).toMatch(/\.file-markdown-preview\s*{[^}]*padding:\s*32px max\(28px, calc\(\(100% - var\(--conversation-content-width\)\) \/ 2\)\) 72px[^}]*scrollbar-gutter:\s*stable/s);
    expect(css).toMatch(/\.file-markdown-preview h1\s*{[^}]*font-size:\s*calc\(24px \+ var\(--font-size-delta\)\)[^}]*letter-spacing:\s*-0\.02em/s);
    expect(css).toMatch(/\.file-markdown-preview h2\s*{[^}]*margin:\s*32px 0 12px[^}]*font-size:\s*calc\(19px \+ var\(--font-size-delta\)\)/s);
  });

  it("keeps the inspector resize target on the panel edge without a visible rail", async () => {
    const css = await workbenchText();
    expect(css).toMatch(/\.pane-resizer\.is-right\s*{[^}]*right:\s*calc\(var\(--inspector-width\) - 3px\)/s);
    expect(css).toMatch(/\.pane-resizer\.is-right::after\s*{[^}]*content:\s*none/s);
  });

  it("gives settings the ZCode-style hierarchy and spacious rows", async () => {
    const css = await workbenchText();
    expect(css).toMatch(/\.dialog-body\.settings-layout\s*{[^}]*padding:\s*0 !important/s);
    expect(css).toMatch(/\.dialog-body\.settings-layout\s*{[^}]*padding-block:\s*0 !important[^}]*padding-inline:\s*0 !important/s);
    expect(css).toMatch(/\.settings-layout\s*{[^}]*grid-template-columns:\s*264px minmax\(0, 1fr\)/s);
    expect(css).toMatch(/\.settings-sidebar\s*{[^}]*border-right:\s*0[^}]*background:\s*var\(--bg-sidebar\)/s);
    expect(css).toMatch(/\.settings-nav\s*{[^}]*border-right:\s*0/s);
    expect(css).toMatch(/\.settings-main\s*{[^}]*border:\s*0[^}]*background:\s*var\(--bg-settings\)/s);
    expect(css).not.toContain(".settings-project-path");
    expect(css).toMatch(/\.settings-nav button\s*{[^}]*width:\s*100%[^}]*min-height:\s*42px[^}]*border-radius:\s*var\(--radius-lg\)[^}]*font-size:\s*var\(--font-size-body\)/s);
    expect(css).toMatch(/\.settings-view-header h1\s*{[^}]*font-size:\s*calc\(32px \+ var\(--font-size-delta\)\)[^}]*letter-spacing:\s*-0\.035em/s);
    expect(css).toMatch(/\.settings-main \.setting-row\s*{[^}]*min-height:\s*72px[^}]*padding:\s*var\(--space-md\) var\(--space-lg\)/s);
    expect(css).toMatch(/\.settings-main > \.settings-content\s*{[^}]*background:\s*var\(--bg-settings\) !important/s);
    expect(css).toMatch(/\.settings-main \.settings-sections\s*{[^}]*grid-auto-rows:\s*max-content[^}]*gap:\s*var\(--space-xl\)/s);
    expect(css).toMatch(/\.settings-main \.settings-sections > section\s*{[^}]*display:\s*grid[^}]*gap:\s*var\(--space-md\)[^}]*border:\s*0[^}]*background:\s*transparent/s);
    expect(css).toMatch(/\.settings-main \.settings-card\s*{[^}]*border:\s*1px solid var\(--border\)[^}]*border-radius:\s*var\(--radius-lg\)[^}]*background:\s*var\(--bg-card\)[^}]*overflow:\s*hidden/s);
    expect(css).toMatch(/\.settings-main \.setting-row\s*{[^}]*background:\s*transparent/s);
    expect(css).toMatch(/\.settings-main \.settings-section-title\s*{[^}]*margin:\s*0[^}]*padding:\s*0[^}]*border:\s*0/s);
    expect(css).toMatch(/\.settings-main \.settings-card > \.setting-row:last-child\s*{[^}]*border-bottom:\s*0/s);
    expect(css).toMatch(/\.settings-main \.setting-row-select select\s*{[^}]*width:\s*256px !important[^}]*height:\s*40px !important[^}]*flex:\s*0 0 256px !important/s);
    expect(css).toMatch(/\.settings-page\.settings-dialog \.settings-main \.setting-row > input\[type="checkbox"\]\s*{[^}]*width:\s*38px !important[^}]*height:\s*22px !important[^}]*margin:\s*0/s);
    expect(css).toMatch(/\.settings-page\.settings-dialog \.settings-main \.setting-row > input\[type="checkbox"\]\s*{[^}]*background-position:\s*left 1px center[^}]*background-size:\s*18px 18px/s);
    expect(css).toMatch(/input\[type="checkbox"\]:checked\s*{[^}]*background-position:\s*right 1px center/s);
    expect(css).not.toMatch(/\.settings-page\.settings-dialog \.settings-main \.setting-row > input\[type="checkbox"\]::after/);
    expect(css).toMatch(/\.settings-dialog \.provider-header-row\s*{[^}]*grid-template-columns:\s*repeat\(2, minmax\(0, 1fr\)\) 32px/s);
  });

  it("keeps management screens aligned with the wider settings rhythm", async () => {
    const css = await workbenchText();
    expect(css).toMatch(/\.settings-dialog \.model-manager-layout,[^}]*grid-template-columns:\s*248px minmax\(0, 1fr\) !important/s);
    expect(css).toMatch(/\.settings-dialog \.model-manager-layout,[^}]*background:\s*var\(--bg-settings\)/s);
    expect(css).toMatch(/\.settings-dialog \.model-config-list,[^}]*background:\s*var\(--bg-settings\) !important/s);
    expect(css).toMatch(/\.settings-dialog \.model-editor,[^}]*background:\s*var\(--bg-settings\) !important/s);
    expect(css).toMatch(/\.settings-dialog \.model-config-list > button,[^}]*min-height:\s*42px !important/s);
    expect(css).toMatch(/\.settings-dialog \.skill-list \.prompt-config-scope > button,[^}]*min-height:\s*62px !important/s);
    expect(css).toMatch(/\.settings-dialog \.model-field > textarea\s*{[^}]*min-height:\s*96px !important/s);
    expect(css).toMatch(/\.settings-dialog \.model-editor-actions\s*{[^}]*padding:\s*var\(--space-md\) var\(--space-xl\)/s);
  });
});
