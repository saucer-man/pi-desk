import { AsyncLocalStorage } from "node:async_hooks";
import { findPackageJSON } from "node:module";
import { basename, dirname, join } from "node:path";
import { pathToFileURL } from "node:url";
import { DefaultPackageManager, ExtensionRunner, getPackageDir } from "@earendil-works/pi-coding-agent";

interface TimingContext { sessionId: string; traceId: number; server?: string }
const context = new AsyncLocalStorage<TimingContext>();
const enabled = process.env.PI_DESK_SESSION_TIMING === "1";
let serial = 0;

export function traceSessionLoad<T>(sessionId: string, run: () => T): T {
  if (!enabled || context.getStore()?.sessionId === sessionId) return run();
  return context.run({ sessionId, traceId: ++serial }, run);
}

export function measureSessionLoad<T>(phase: string, run: () => T, detail?: Record<string, string>): T {
  const active = context.getStore();
  if (!active) return run();
  const spanId = ++serial;
  const started = performance.now();
  const log = (event: string) => console.error("[session-timing] " + JSON.stringify({
    at: new Date().toISOString(), ...active, ...detail, spanId, phase, event,
    elapsedMs: Math.round(performance.now() - started),
  }));
  log("start");
  const timer = setInterval(() => log("pending"), 5000).unref();
  const finish = (event: string) => { clearInterval(timer); log(event); };
  try {
    const result = run();
    if (result instanceof Promise) {
      void result.then(() => finish("end"), () => finish("error"));
    } else finish("end");
    return result;
  } catch (error) {
    finish("error");
    throw error;
  }
}

type SdkMethod = (this: object, ...args: unknown[]) => unknown;
function instrument(target: object, name: string, around: (self: object, args: unknown[], next: () => unknown) => unknown) {
  const methods = target as Record<string, SdkMethod>;
  const original = methods[name];
  if (typeof original !== "function") return;
  methods[name] = function (...args) {
    const next = () => original.apply(this, args);
    return context.getStore() ? around(this, args, next) : next();
  };
}

function hookExtensions(runner: object, event: string, next: () => unknown) {
  // Pi SDK 1.4 snapshots the handler arrays synchronously before awaiting a handler.
  const { extensions } = runner as { extensions: { path: string; handlers: Map<string, SdkMethod[]> }[] };
  const restore: (() => void)[] = [];
  for (const { path, handlers } of extensions) {
    const original = handlers.get(event);
    if (!original?.length) continue;
    const file = basename(path);
    const plugin = /^index\.[cm]?[jt]s$/.test(file) ? `${basename(dirname(path))}/${file}` : file;
    handlers.set(event, original.map(handler => function (...args) {
      return measureSessionLoad(`plugin.${event}`, () => handler.apply(this, args), { plugin });
    }));
    restore.push(() => handlers.set(event, original));
  }
  try { return next(); } finally { restore.forEach(reset => reset()); }
}

export async function installSessionTiming() {
  if (!enabled) return;
  instrument(DefaultPackageManager.prototype, "resolve", (_self, _args, next) => measureSessionLoad("packages.resolve", next));
  instrument(DefaultPackageManager.prototype, "installParsedSource", (_self, [source], next) => {
    const { type, name } = source as { type: string; name?: string };
    return measureSessionLoad("packages.install", next, {
      sourceType: type, ...(type === "npm" && name && /^[@a-z0-9._/-]+$/i.test(name) ? { package: name } : {}),
    });
  });
  instrument(ExtensionRunner.prototype, "emit", (self, [event], next) => {
    const { type } = event as { type: string };
    return ["session_start", "session_shutdown", "model_select"].includes(type) ? hookExtensions(self, type, next) : next();
  });
  instrument(ExtensionRunner.prototype, "emitResourcesDiscover", (self, _args, next) => hookExtensions(self, "resources_discover", next));

  // Use the same package resolution as Pi SDK, including when npm nests its dependencies.
  const mcpRuntime = pathToFileURL(join(getPackageDir(), "dist/extensions/mcp/runtime.js"));
  const { McpServerConnection } = await import(mcpRuntime.href) as typeof import("../node_modules/@earendil-works/pi-coding-agent/dist/extensions/mcp/runtime.js");
  const mcpPackage = findPackageJSON("@earendil-works/pi-mcp", mcpRuntime);
  if (!mcpPackage) return;
  const client = pathToFileURL(join(dirname(mcpPackage), "dist/index.js"));
  const { McpClient, StdioTransport, StreamableHttpTransport } = await import(client.href) as typeof import("@earendil-works/pi-mcp");
  for (const method of ["open", "connectOnce", "close"]) {
    instrument(McpServerConnection.prototype, method, (self, _args, next) => {
      const active = context.getStore()!;
      return context.run({ ...active, server: (self as { name: string }).name }, () => measureSessionLoad(`mcp.${method}`, next));
    });
  }
  for (const [transport, phase] of [[StdioTransport, "mcp.stdio.start"], [StreamableHttpTransport, "mcp.http.start"]] as const) {
    instrument(transport.prototype, "start", (_self, _args, next) => context.getStore()?.server ? measureSessionLoad(phase, next) : next());
  }
  instrument(McpClient.prototype, "requestInternal", (_self, [method], next) => {
    return context.getStore()?.server && (method === "initialize" || method === "tools/list")
      ? measureSessionLoad(`mcp.${method}`, next)
      : next();
  });
}
