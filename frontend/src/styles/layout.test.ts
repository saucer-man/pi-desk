import layoutFile from "./layout.css?inline";
import { describe, expect, it } from "vitest";

async function layoutText(): Promise<string> {
  if (layoutFile.includes(".workspace-shell")) return layoutFile;
  const moduleName = ["node", "fs/promises"].join(":");
  const { readFile } = await import(/* @vite-ignore */ moduleName) as {
    readFile(path: string, encoding: "utf8"): Promise<string>;
  };
  return readFile("src/styles/layout.css", "utf8");
}

function ruleBodies(layout: string, selector: string): string[] {
  const result: string[] = [];
  let cursor = 0;
  while (cursor < layout.length) {
    const open = layout.indexOf("{", cursor);
    if (open < 0) break;
    const close = layout.indexOf("}", open + 1);
    if (close < 0) break;
    const previousClose = layout.lastIndexOf("}", open - 1);
    const candidate = layout.slice(previousClose + 1, open).trim();
    if (candidate === selector) result.push(layout.slice(open + 1, close));
    cursor = close + 1;
  }
  return result;
}

function firstRuleBody(layout: string, selector: string): string {
  const body = ruleBodies(layout, selector)[0];
  if (body === undefined) throw new Error(`Missing CSS rule: ${selector}`);
  return body;
}

describe("application rail alignment", () => {
  it("pins both workspace rails to the same grid row without a top offset", async () => {
    const layout = await layoutText();
    const shared = firstRuleBody(layout, `.workspace-shell,
.inspector`);
    expect(shared).toMatch(/margin-top:\s*0/);
    expect(shared).toMatch(/grid-row:\s*2/);
    expect(firstRuleBody(layout, ".workspace-shell")).toMatch(/grid-column:\s*2/);
    expect(firstRuleBody(layout, ".inspector")).toMatch(/grid-column:\s*3/);
    expect(firstRuleBody(layout, ".inspector")).not.toMatch(/margin-top/);
    expect(ruleBodies(layout, ".inspector").some((body) => /margin-top:\s*[1-9]/.test(body))).toBe(false);
  });
});

describe("message editor theme colors", () => {
  it("uses defined foreground and background tokens in light and dark themes", async () => {
    const layout = await layoutText();
    expect(firstRuleBody(layout, ".message-edit textarea")).toMatch(/background:\s*var\(--bg-raised\)/);
    expect(firstRuleBody(layout, ".message-edit textarea")).toMatch(/color:\s*var\(--text\)/);
    expect(firstRuleBody(layout, ".message-edit-button--primary")).toMatch(/color:\s*var\(--text-inverse\)/);
  });
});

describe("skill invocation messages", () => {
  it("renders skill metadata as a compact row that cannot expand the user bubble", async () => {
    const layout = await layoutText();
    const invocation = firstRuleBody(layout, ".message-skill-invocation");
    expect(invocation).toMatch(/display:\s*flex/);
    expect(invocation).toMatch(/min-width:\s*0/);
    expect(firstRuleBody(layout, ".message-skill-invocation > code")).toMatch(/text-overflow:\s*ellipsis/);
  });
});

describe("streamed assistant output alignment", () => {
  it("cancels the execution panel indent for intermediate assistant text", async () => {
    const layout = await layoutText();
    const output = firstRuleBody(layout, ".execution-process-details > .markdown-body");
    expect(output).toMatch(/margin-left:\s*-4px/);
    expect(output).toMatch(/padding:\s*4px 0/);
  });
});

describe("assistant output waiting indicator", () => {
  it("keeps the spinner on the conversation content axis without a visible label", async () => {
    const layout = await layoutText();
    const status = firstRuleBody(layout, ".waiting-for-output");
    expect(status).toMatch(/width:\s*100%/);
    expect(status).toMatch(/justify-content:\s*flex-start/);
    expect(status).toMatch(/margin:\s*0/);
    expect(status).not.toMatch(/margin:\s*0\s+auto/);
  });
});

describe("session list state indicators", () => {
  it("uses process state for bold text and separate output and unread markers", async () => {
    const layout = await layoutText();
    expect(firstRuleBody(layout, ".thread-title.is-started")).toMatch(/font-weight:\s*650/);
    expect(firstRuleBody(layout, ".thread-status")).toMatch(/animation:\s*spin/);
    expect(firstRuleBody(layout, ".thread-unread")).toMatch(/background:\s*var\(--blue\)/);
    expect(layout).not.toContain(".thread-title.is-unread");
  });
});

describe("composer todo and queue stack", () => {
  it("keeps todo and queue narrower than the composer with zero vertical gaps", async () => {
    const layout = await layoutText();
    const stackPanel = firstRuleBody(layout, ".composer-stack-panel");
    expect(stackPanel).toMatch(/width:\s*min\(calc\(var\(--composer-max-width\) - var\(--composer-stack-total-inset\)\), calc\(100% - var\(--composer-stack-total-inset\)\)\)/);
    const queue = firstRuleBody(layout, ".queue-panel");
    expect(queue).toMatch(/margin-bottom:\s*0/);
    expect(queue).toMatch(/border-bottom:\s*0/);
    expect(queue).toMatch(/box-shadow:\s*none/);
    expect(firstRuleBody(layout, ".retry-banner")).toMatch(/margin-bottom:\s*8px/);
    expect(firstRuleBody(layout, ".extension-widget")).toMatch(/margin-bottom:\s*8px/);
  });

  it("uses the agreed compact row, image, scrolling, progress, and motion budgets", async () => {
    const layout = await layoutText();
    expect(firstRuleBody(layout, ".queue-list")).toMatch(/max-height:\s*128px/);
    expect(firstRuleBody(layout, ".queue-row")).toMatch(/min-height:\s*32px/);
    expect(firstRuleBody(layout, ".queue-thumbnail")).toMatch(/width:\s*24px[\s\S]*height:\s*24px/);
    expect(firstRuleBody(layout, ".pi-desk-todo-list")).toMatch(/max-height:\s*160px/);
    expect(firstRuleBody(layout, ".pi-desk-todo-row")).toMatch(/height:\s*32px/);
    expect(firstRuleBody(layout, ".pi-desk-todo-progress")).toMatch(/height:\s*2px/);
    expect(layout).toContain("@media (prefers-reduced-motion: reduce)");
  });
});

describe("batch extension questions", () => {
  it("uses a bounded wide dialog with scrollable tabs and full-width option cards", async () => {
    const layout = await layoutText();
    expect(firstRuleBody(layout, ".batch-extension-dialog")).toMatch(/width:\s*min\(720px, 100%\)/);
    expect(firstRuleBody(layout, ".batch-question-tabs")).toMatch(/overflow-x:\s*auto/);
    expect(firstRuleBody(layout, ".batch-question-options > button")).toMatch(/grid-template-columns:\s*16px minmax\(0, 1fr\)/);
    expect(firstRuleBody(layout, ".batch-question-review-item div > span")).toMatch(/overflow-wrap:\s*anywhere/);
  });
});

describe("model menu stability", () => {
  it("keeps the popup height fixed while model capabilities refresh", async () => {
    const layout = await layoutText();
    const menu = firstRuleBody(layout, ".model-menu");
    expect(menu).toMatch(/display:\s*flex/);
    expect(menu).toMatch(/height:\s*min\(360px,\s*calc\(100vh - 96px\)\)/);
    expect(menu).not.toMatch(/max-height/);
    expect(firstRuleBody(layout, ".model-menu-options")).toMatch(/flex:\s*1 1 auto/);
    expect(firstRuleBody(layout, ".thinking-level-grid")).toMatch(/min-height:\s*62px/);
  });
});
