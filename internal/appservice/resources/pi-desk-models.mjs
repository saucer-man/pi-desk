import { realpathSync } from "node:fs";
import { mkdir, readFile, writeFile, rename, unlink } from "node:fs/promises";
import { createHash, randomUUID } from "node:crypto";
import { join, dirname, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import readline from "node:readline";

// The installed Pi owns OAuth, token rotation, auth.json locks and settings writes.
const [piPath, agentDir] = process.argv.slice(1);
const input = readline.createInterface({ input: process.stdin, terminal: false });
const controller = new AbortController();
input.on("close", () => controller.abort(new Error("Pi Desk disconnected")));
let receiver;
const queued = [];
input.on("line", line => receiver ? receiver(line) : queued.push(line));
const receive = () => queued.length ? Promise.resolve(queued.shift()) : new Promise(resolve => { receiver = line => { receiver = undefined; resolve(line); }; });
const send = value => process.stdout.write(JSON.stringify(value) + "\n");
const notify = event => send({ type: "event", event });
let settings, sdkEntry;

function safeError(error) {
  return String(error?.message || error).replace(/(?:eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+|\brt\.[\w.-]+|\bsk-[\w-]+|Bearer\s+\S+)/g, "[redacted]").slice(0, 1600);
}

async function loadSDK() {
  const names = ["@earendil-works/pi-coding-agent", "@mariozechner/pi-coding-agent"];
  for (let directory = dirname(realpathSync(piPath));;) {
    for (const path of [join(directory, "package.json"), ...names.map(name => join(directory, "node_modules", name, "package.json"))]) {
      let manifest;
      try { manifest = JSON.parse(await readFile(path, "utf8")); } catch { continue; }
      if (!names.includes(manifest.name)) continue;
      sdkEntry = resolve(dirname(path), manifest.main || "dist/index.js");
      const sdk = await import(pathToFileURL(sdkEntry).href);
      if (!sdk.ModelRuntime || !sdk.SettingsManager) throw new Error("Installed Pi does not support model/auth management. Update Pi first.");
      return sdk;
    }
    const parent = dirname(directory);
    if (parent === directory) break;
    directory = parent;
  }
  throw new Error("Cannot locate the installed Pi SDK. Install Pi with npm and retry.");
}

async function accountModels(runtime, sdk, force) {
  if (!runtime.isUsingOAuth("openai")) return {};
  const credential = sdk.readStoredCredential("openai", join(agentDir, "auth.json"));
  if (!credential?.access) return {};
  const fingerprint = createHash("sha256").update(credential.access).digest("hex");
  const cachePath = join(agentDir, "cache", "pi-desk-openai-models.json");
  let cached;
  try { cached = JSON.parse(await readFile(cachePath, "utf8")); } catch {}
  if (cached?.fingerprint !== fingerprint || !Array.isArray(cached?.models)) cached = undefined;
  if (!force && cached && Date.now() - cached.updatedAt < 15 * 60_000) return { models: cached.models };
  try {
    const auth = await runtime.getAuth("openai");
    if (!auth?.auth?.apiKey) throw new Error("OpenAI authorization is unavailable");
    // A subscription token is sent only to the official endpoint, never a custom gateway.
    const response = await fetch("https://api.openai.com/v1/models", { headers: { Authorization: `Bearer ${auth.auth.apiKey}` }, redirect: "error", signal: AbortSignal.timeout(8000) });
    if (!response.ok) throw new Error(`OpenAI model catalog returned HTTP ${response.status}`);
    const body = await response.json();
    if (!Array.isArray(body.models)) throw new Error("OpenAI returned an unsupported model catalog");
    const models = body.models.filter(m => m.visibility === "list" && typeof m.slug === "string").map(m => ({ id: m.slug, name: typeof m.display_name === "string" ? m.display_name : m.slug }));
    const latest = sdk.readStoredCredential("openai", join(agentDir, "auth.json"));
    const next = { fingerprint: createHash("sha256").update(latest.access).digest("hex"), updatedAt: Date.now(), models };
    const temporary = cachePath + "." + randomUUID();
    try { await mkdir(join(agentDir, "cache"), { recursive: true }); await writeFile(temporary, JSON.stringify(next), { mode: 0o600 }); await rename(temporary, cachePath); } catch { await unlink(temporary).catch(() => {}); }
    return { models };
  } catch (error) {
    return { models: cached?.models, error: safeError(error) };
  }
}

async function snapshot(runtime, sdk, force = false) {
  const credentials = await runtime.listCredentials();
  const stored = new Map(credentials.map(c => [c.providerId, c.type]));
  const available = runtime.getAvailableSnapshot();
  const account = await accountModels(runtime, sdk, force);
  const accountById = account.models && new Map(account.models.map(m => [m.id, m.name]));
  const providers = runtime.getProviders().map(p => {
    const auth = runtime.getProviderAuthStatus(p.id);
    return { id: p.id, name: p.name, oauth: Boolean(p.auth.oauth?.login), apiKey: Boolean(p.auth.apiKey?.login), configured: auth.configured, stored: stored.has(p.id), authType: stored.get(p.id) || (auth.configured ? "api_key" : ""), authSource: auth.source || "", catalogRestricted: p.id === "openai" && Boolean(accountById), warning: p.id === "openai" ? account.error || "" : "" };
  }).filter(p => p.configured || p.oauth || p.apiKey);
  const models = available.filter(m => m.provider !== "openai" || !accountById || accountById.has(m.id)).map(m => ({ id: m.id, name: m.provider === "openai" && accountById?.get(m.id) || m.name, provider: m.provider, contextWindow: m.contextWindow, reasoning: Boolean(m.reasoning), source: m.provider === "openai" && accountById ? "account" : "pi" }));
  return { agentDir, providers, models, defaultProvider: settings.getDefaultProvider() || "", defaultModel: settings.getDefaultModel() || "", warning: runtime.getError() || "" };
}

try {
  const request = JSON.parse(await receive());
  const sdk = await loadSDK();
  settings = sdk.SettingsManager.create(agentDir, agentDir);
  const runtime = await sdk.ModelRuntime.create({ authPath: join(agentDir, "auth.json"), modelsPath: join(agentDir, "models.json"), allowModelNetwork: false });
  if (runtime.getError()) throw new Error(runtime.getError());
  if (request.operation === "login") {
    if (request.providerId === "openai") {
      const config = JSON.parse(await readFile(join(agentDir, "models.json"), "utf8").catch(e => { if (e.code === "ENOENT") return "{}"; throw e; })).providers?.openai;
      if (config?.baseUrl && config.baseUrl.replace(/\/+$/, "") !== "https://api.openai.com/v1" || config?.api && config.api !== "openai-responses") throw new Error("OpenAI has a custom URL or protocol override in models.json. Use the built-in Responses provider or a separate gateway provider ID before signing in.");
    }
    let promptId = 0;
    await runtime.login(request.providerId, "oauth", {
      signal: controller.signal,
      notify,
      prompt: prompt => new Promise((resolve, reject) => {
        const id = ++promptId;
        const aborted = () => { receiver = undefined; notify({ type: "prompt_cancelled", promptId: id }); reject(new Error("Prompt cancelled")); };
        if (prompt.signal?.aborted) { aborted(); return; }
        prompt.signal?.addEventListener("abort", aborted, { once: true });
        notify({ type: "prompt", promptType: prompt.type, promptId: id, message: prompt.message, placeholder: prompt.placeholder, options: prompt.options });
        receiver = line => {
          const reply = JSON.parse(line);
          if (reply.promptId !== id) return;
          receiver = undefined;
          prompt.signal?.removeEventListener("abort", aborted);
          resolve(String(reply.value));
        };
        while (receiver && queued.length) receiver(queued.shift());
      }),
    }, { getDeviceId: () => settings.getOrCreateDeviceId(), agentName: "Pi Desk" });
  } else if (request.operation === "logout") {
    await runtime.logout(request.providerId);
  } else if (request.operation === "api_key") {
    // Unknown custom providers still share the native credential store and lock.
    const { AuthStorage } = await import(new URL("./core/auth-storage.js", pathToFileURL(sdkEntry)));
    await AuthStorage.create(join(agentDir, "auth.json")).modify(request.providerId, async () => ({ type: "api_key", key: request.apiKey }));
  } else if (request.operation === "default") {
    const catalog = await snapshot(runtime, sdk);
    if (request.providerId && !catalog.models.some(m => m.provider === request.providerId && m.id === request.modelId)) throw new Error("The selected default model is unavailable");
    settings.setDefaultModelAndProvider(request.providerId || undefined, request.modelId || undefined);
  } else if (request.operation !== "catalog") throw new Error("Unknown model management operation");
  await settings.flush();
  const errors = settings.drainErrors();
  if (errors.length) throw new Error("Pi settings could not be saved");
  // Read again after API-key mutations so the availability snapshot is current.
  if (request.operation === "api_key") await runtime.refresh({ allowNetwork: false });
  const result = await snapshot(runtime, sdk, request.refresh || request.operation === "login");
  send({ type: "result", data: result });
} catch (error) {
  send({ type: "error", error: safeError(error) });
  process.exitCode = 1;
} finally {
  input.close();
  process.stdin.destroy();
}
