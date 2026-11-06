import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { resolve, extname, sep } from "node:path";
import { openExternal } from "./browser.ts";
import {attachTerminals} from "./terminals.ts";
import { Runtime } from "./runtime.ts";
import { api, json, workspace } from "./api.ts";
import { safeError } from "./storage.ts";

const runtime = new Runtime();
const root = resolve(fileURLToPath(new URL("../frontend/dist/", import.meta.url)));
const dev = process.argv.includes("--dev");
const vite = dev ? await (await import("vite")).createServer({ configLoader: "native", configFile: fileURLToPath(new URL("../frontend/vite.config.ts", import.meta.url)), server: { middlewareMode: true } }) : undefined;
const mime: Record<string,string> = { ".html": "text/html; charset=utf-8", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".woff2": "font/woff2", ".png": "image/png", ".ico": "image/x-icon" };
let port = Number(process.env.PORT || process.env.PI_DESK_PORT || 30140);
const portArg = process.argv.indexOf("--port"); if (portArg >= 0) port = Number(process.argv[portArg + 1]);
if (!Number.isInteger(port) || port < 0 || port > 65535) throw new Error("无效端口");
const origins = () => new Set([`http://127.0.0.1:${port}`, `http://localhost:${port}`]);
function allowed(req: import("node:http").IncomingMessage) {
  const navigation = req.method === "GET" && req.headers["sec-fetch-mode"] === "navigate" && req.headers["sec-fetch-dest"] === "document" && !req.url?.startsWith("/api/");
  return Boolean(req.headers.host && origins().has("http://" + req.headers.host) && (!req.headers.origin || origins().has(req.headers.origin)) && (req.headers["sec-fetch-site"] !== "cross-site" || navigation));
}
const server = createServer(async (req, res) => {
  res.setHeader("X-Content-Type-Options", "nosniff"); res.setHeader("Referrer-Policy", "no-referrer"); res.setHeader("X-Frame-Options", "DENY");
  if (!allowed(req)) { json(res, { error: "只允许本机同源访问" }, 403); return; }
  try {
    const url = new URL(req.url || "/", `http://127.0.0.1:${port}`);
    if (url.pathname.startsWith("/api/")) { await api(runtime, req, res, url); return; }
    if (vite) { vite.middlewares(req, res); return; }
    if (!["GET", "HEAD"].includes(req.method || "")) { res.writeHead(405).end(); return; }
    const path = resolve(root, "." + decodeURIComponent(url.pathname));
    if (path !== root && !path.startsWith(root + sep)) { res.writeHead(403).end(); return; }
    const target = extname(path) ? path : resolve(root, "index.html");
    res.setHeader("Content-Security-Policy", `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; connect-src 'self' ws://127.0.0.1:${port} ws://localhost:${port}; frame-src blob:; frame-ancestors 'none'; object-src 'none'; base-uri 'none'`);
    res.setHeader("Content-Type", mime[extname(target)] || "application/octet-stream");
    res.end(await readFile(target));
  } catch (error) { if (!res.headersSent) json(res, { error: safeError(error) }, 400); else res.end(); }
});
const closeTerminals=attachTerminals(server,runtime,allowed);
server.listen(port, "127.0.0.1", () => {
  port = (server.address() as { port: number }).port; const url = `http://127.0.0.1:${port}`; console.log(`Pi Desk ${url}`);
  if (!process.argv.includes("--no-open")) void openExternal(url).catch(error=>console.error(safeError(error)));
});
server.on("error", error => { console.error(safeError(error)); process.exitCode = 1; });
let closing = false;
async function shutdown() { if (closing) return; closing = true; closeTerminals(); await runtime.shutdown(); await vite?.close(); server.close(); server.closeAllConnections(); }
process.on("SIGINT", () => void shutdown()); process.on("SIGTERM", () => void shutdown());
