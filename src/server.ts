#!/usr/bin/env node
import { createServer } from "node:http";
import { readFile, realpath, stat } from "node:fs/promises";
import { mkdirSync, watch } from "node:fs";
import { fileURLToPath } from "node:url";
import { resolve, extname, sep, dirname } from "node:path";
import { openExternal } from "./browser.ts";
import {attachTerminals} from "./terminals.ts";
import { Runtime } from "./runtime.ts";
import { api, json, workspace } from "./api.ts";
import { inside, safeError, sessionRoot } from "./storage.ts";
import { parseArgs } from "node:util";

const { values, positionals } = parseArgs({ allowPositionals: true, options: { port: { type: "string" }, "no-open": { type: "boolean" }, dev: { type: "boolean" }, help: { type: "boolean", short: "h" }, version: { type: "boolean", short: "v" } } });
if (values.help || values.version) {
  const pkg = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8"));
  console.log(values.version ? pkg.version : `Pi Desk\n\nUsage: pi-desk [web] [options]\n\n  --port <number>  Port (default: 30140)\n  --no-open        Do not open a browser\n  -h, --help       Show help\n  -v, --version    Show version`);
  process.exit(0);
}
if (positionals.length && (positionals.length !== 1 || positionals[0] !== "web")) throw new Error("仅支持 web 命令，使用 --help 查看用法");
let port = Number(values.port ?? (process.env.PORT || process.env.PI_DESK_PORT || 30140));
if (!Number.isInteger(port) || port < 0 || port > 65535) throw new Error("无效端口");
const runtime = new Runtime();
mkdirSync(sessionRoot, { recursive: true });
let syncTimer: ReturnType<typeof setTimeout> | undefined;
const sessionWatcher = watch(sessionRoot, { recursive: true }, () => {
  clearTimeout(syncTimer);
  syncTimer = setTimeout(() => void runtime.discover().catch(error => runtime.emit({ type: "notice", data: { message: safeError(error) } })), 300);
});
sessionWatcher.on("error", error => console.error(safeError(error)));
await runtime.discover();
const root = resolve(fileURLToPath(new URL("../frontend/dist/", import.meta.url)));
const dev = values.dev;
const vite = dev ? await (await import("vite")).createServer({ configLoader: "native", configFile: fileURLToPath(new URL("../frontend/vite.config.ts", import.meta.url)), server: { middlewareMode: true } }) : undefined;
const mime: Record<string,string> = { ".html": "text/html; charset=utf-8", ".htm":"text/html; charset=utf-8", ".js": "text/javascript", ".mjs":"text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".woff2": "font/woff2", ".woff":"font/woff", ".ttf":"font/ttf", ".png": "image/png", ".ico": "image/x-icon", ".jpg":"image/jpeg", ".jpeg":"image/jpeg", ".gif":"image/gif", ".webp":"image/webp", ".json":"application/json", ".wasm":"application/wasm" };
const origins = () => new Set([`http://127.0.0.1:${port}`, `http://localhost:${port}`]);
function allowed(req: import("node:http").IncomingMessage) {
  const navigation = req.method === "GET" && req.headers["sec-fetch-mode"] === "navigate" && req.headers["sec-fetch-dest"] === "document" && !req.url?.startsWith("/api/");
  return Boolean(req.headers.host && origins().has("http://" + req.headers.host) && (!req.headers.origin || origins().has(req.headers.origin)) && (req.headers["sec-fetch-site"] !== "cross-site" || navigation));
}
const server = createServer(async (req, res) => {
  res.setHeader("X-Content-Type-Options", "nosniff"); res.setHeader("Referrer-Policy", "no-referrer"); res.setHeader("X-Frame-Options", "DENY");
  const previewRequest=req.url?.startsWith("/preview/")&&["GET","HEAD"].includes(req.method||"")&&origins().has("http://"+req.headers.host);
  if (!allowed(req)&&!previewRequest) { json(res, { error: "只允许本机同源访问" }, 403); return; }
  try {
    const url = new URL(req.url || "/", `http://127.0.0.1:${port}`);
    if(url.pathname.startsWith("/preview/")){
      const [, ,token,...parts]=url.pathname.split("/"),file=runtime.htmlPreviews.get(token);if(!file||!["GET","HEAD"].includes(req.method||"")){res.writeHead(404).end();return;}
      const folder=dirname(file),target=await realpath(resolve(folder,decodeURIComponent(parts.join("/"))));
      if(!inside(folder,target)||!mime[extname(target).toLowerCase()]||!(await stat(target)).isFile()){res.writeHead(403).end();return;}
      if((await stat(target)).size>2*1024*1024){res.writeHead(413).end();return;}
      res.removeHeader("X-Frame-Options");res.setHeader("Access-Control-Allow-Origin","null");res.setHeader("Content-Security-Policy","sandbox allow-scripts; default-src 'self' data: blob: http: https:; script-src 'self' 'unsafe-inline' 'unsafe-eval' blob: http: https:; style-src 'self' 'unsafe-inline' http: https:; frame-ancestors 'self'; object-src 'none'; base-uri 'self'");res.setHeader("Cache-Control","no-store");res.setHeader("Content-Type",mime[extname(target).toLowerCase()]);res.end(req.method==="HEAD"?undefined:await readFile(target));return;
    }
    if (url.pathname.startsWith("/api/")) { await api(runtime, req, res, url); return; }
    if (vite) { vite.middlewares(req, res); return; }
    if (!["GET", "HEAD"].includes(req.method || "")) { res.writeHead(405).end(); return; }
    const path = resolve(root, "." + decodeURIComponent(url.pathname));
    if (path !== root && !path.startsWith(root + sep)) { res.writeHead(403).end(); return; }
    const target = extname(path) ? path : resolve(root, "index.html");
    res.setHeader("Content-Security-Policy", `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; connect-src 'self' ws://127.0.0.1:${port} ws://localhost:${port}; frame-src 'self' blob:; frame-ancestors 'none'; object-src 'none'; base-uri 'none'`);
    res.setHeader("Content-Type", mime[extname(target)] || "application/octet-stream");
    res.end(await readFile(target));
  } catch (error) { if (!res.headersSent) json(res, { error: safeError(error) }, 400); else res.end(); }
});
const closeTerminals=attachTerminals(server,runtime,allowed);
server.listen(port, "127.0.0.1", () => {
  port = (server.address() as { port: number }).port; const url = `http://127.0.0.1:${port}`; console.log(`Pi Desk ${url}`);
  if (!values["no-open"]) void openExternal(url).catch(error=>console.error(safeError(error)));
});
server.on("error", error => { console.error(safeError(error)); process.exitCode = 1; });
let closing = false;
async function shutdown() { if (closing) return; closing = true; sessionWatcher.close(); clearTimeout(syncTimer); closeTerminals(); await runtime.shutdown(); await vite?.close(); server.close(); server.closeAllConnections(); }
process.on("SIGINT", () => void shutdown()); process.on("SIGTERM", () => void shutdown());
