import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";
import { Window } from "happy-dom";

const distUrl = new URL("../dist/", import.meta.url);
const html = await readFile(new URL("index.html", distUrl), "utf8");
const entry = html.match(/<script[^>]+src="([^"]+)"/u)?.[1];
assert(entry, "Production entry script is missing from dist/index.html");

const browser = new Window({ url: "http://127.0.0.1/" });
browser.document.write(html);

for (const name of [
  "CSS",
  "Document",
  "Element",
  "Event",
  "HTMLElement",
  "HTMLInputElement",
  "HTMLTextAreaElement",
  "MouseEvent",
  "MutationObserver",
  "Node",
  "ResizeObserver",
  "SVGElement",
]) {
  Object.defineProperty(globalThis, name, { configurable: true, value: browser[name] });
}
Object.defineProperties(globalThis, {
  document: { configurable: true, value: browser.document },
  location: { configurable: true, value: browser.location },
  navigator: { configurable: true, value: browser.navigator },
  window: { configurable: true, value: browser },
});

const entryPath = fileURLToPath(new URL(entry.replace(/^\//u, ""), distUrl));
await import(pathToFileURL(entryPath).href);

const deadline = Date.now() + 2_000;
while (!browser.document.querySelector("#app")?.children.length && Date.now() < deadline) {
  await new Promise((resolve) => setTimeout(resolve, 20));
}

assert(browser.document.querySelector("#app")?.children.length, "Production bundle did not mount Vue");
await browser.close();
console.log("Production bundle mounted successfully");
process.exit(0);
