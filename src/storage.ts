import { existsSync, readFileSync, writeFileSync, mkdirSync, renameSync, copyFileSync, cpSync } from "node:fs";
import { realpath, stat } from "node:fs/promises";
import { homedir } from "node:os";
import { resolve, join, relative, isAbsolute, dirname } from "node:path";
import { randomUUID, createHash } from "node:crypto";
import type { State, SessionRecord, DraftImage } from "./types.ts";

export const agentDir = resolve((process.env.PI_CODING_AGENT_DIR || join(homedir(), ".pi", "agent")).replace(/^~(?=[/\\]|$)/, homedir()));
export const deskDir = resolve(process.env.PI_DESK_DIR || join(homedir(), ".pi", "desk"));
export const sessionRoot = process.env.PI_CODING_AGENT_SESSION_DIR || join(agentDir, "sessions");
export function inside(root: string, path: string) { const rel = relative(root, path); return rel === "" || (!rel.startsWith("..") && !isAbsolute(rel)); }
export function safeError(error: unknown) { if (error && typeof error === "object" && "error" in error) error = error.error; return String(error instanceof Error ? error.message : error).replace(/(?:\bsk-[\w-]+|Bearer\s+\S+)/g, "[redacted]").slice(0, 2000); }
export async function directory(path: string) { const canonical = await realpath(resolve(path.replace(/^~(?=[/\\]|$)/, homedir()))); if (!(await stat(canonical)).isDirectory()) throw new Error("工作区必须是存在的目录"); return canonical; }

export class Store {
  state: State;
  private saved = "";
  readonly path = join(deskDir, "state.json");
  constructor(legacyStatePath?: string) {
    mkdirSync(deskDir, { recursive: true, mode: 0o700 });
    this.state = existsSync(this.path) ? JSON.parse(this.saved = readFileSync(this.path, "utf8")) : { version: 1, workspaces: [], sessions: [], preferences: { theme: "light" } };
    if (this.state.version !== 1 || !Array.isArray(this.state.sessions) || !Array.isArray(this.state.workspaces)) throw new Error("不支持的 Pi Desk 状态文件，原文件已保留");
    if (!existsSync(this.path) && (legacyStatePath || !process.env.PI_DESK_DIR)) this.migrate(legacyStatePath);
    const preserved = join(deskDir, "legacy-state.json");
    const legacy = existsSync(preserved) ? JSON.parse(readFileSync(preserved, "utf8")) : undefined;
    for(const old of legacy?.workspaces||[]){if(old.location?.kind!=="ssh"||this.state.workspaces.some(w=>w.id===old.id))continue;const target=legacy.targets?.find((t:any)=>t.id===old.location.ssh?.targetId)?.hostAlias;const path=old.location.ssh?.canonicalRoot;if(!target||typeof path!=="string"||!path.startsWith("/"))continue;this.state.workspaces.push({id:old.id,name:old.name||target,path,trusted:true,remote:{target,hostFingerprint:old.location.ssh?.hostKeyBinding?.sha256,connected:false,error:undefined}});}
    if(!this.state.sshLegacyImported&&legacy){
    if(existsSync(this.path)&&!existsSync(join(deskDir,"state-before-go-ssh.json")))copyFileSync(this.path,join(deskDir,"state-before-go-ssh.json"));
    // Old Wails SSH sessions already used local JSONL; retain their IDs and history paths.
    for(const thread of legacy?.desktop?.threads||[]){
      const workspace=this.state.workspaces.find(w=>w.remote&&w.id===thread.workspaceId);
      if(!workspace||this.state.sessions.some(record=>record.id===thread.id))continue;
      this.state.sessions.push({id:thread.id,title:thread.title||"历史会话",workspaceId:workspace.id,cwd:workspace.path,mode:thread.disableSkills?"minimal":"standard",depth:0,status:"paused",sessionPath:thread.sessionPath||undefined,createdAt:thread.createdAt||new Date().toISOString(),updatedAt:thread.updatedAt||new Date().toISOString(),draft:thread.draft||"",archived:Boolean(thread.archived),unread:Boolean(thread.unread)});
    }
    this.state.sshLegacyImported=true;
    }
    if(this.state.schedules===undefined&&Array.isArray(legacy?.desktop?.scheduledTasks))this.state.schedules=legacy.desktop.scheduledTasks.filter((t:any)=>this.state.workspaces.some(w=>w.id===t.workspaceId)).map((t:any)=>({id:t.id||randomUUID(),name:String(t.name||"旧版定时任务"),prompt:String(t.prompt||""),workspaceId:t.workspaceId,model:t.modelProvider&&t.modelId?`${t.modelProvider}/${t.modelId}`:"",thinking:t.thinkingLevel||"off",mode:"standard",frequency:t.frequency,time:t.time,weekday:t.weekday,runAt:t.runAt,enabled:false,lastRunAt:t.lastRunAt,lastSessionId:t.lastThreadId,lastStatus:t.lastStatus==="running"?"paused":t.lastStatus,lastError:"旧版任务已迁移，请检查模型和时间后手动启用",createdAt:t.createdAt||new Date().toISOString(),updatedAt:new Date().toISOString()}));
    for (const record of this.state.sessions) {
      if (record.draftImages === undefined) {
        const old = legacy?.desktop?.threads?.find((t: { id: string }) => t.id === record.id);
        try { const composer = JSON.parse(old?.composerJson || "{}"); record.draftImages = this.cacheImages(composer.attachments || []); record.pending = (composer.pending || []).map((p: any) => ({ id: p.id || randomUUID(), text: String(p.text || ""), images: this.cacheImages(p.images || []), behavior: "followUp", paused: true })); }
        catch { record.draftImages = []; record.error = "旧附件元数据无法恢复，原始状态与图片已保留。"; }
      }
      for (const pending of record.pending || []) pending.paused = true;
      if (record.status === "running") { record.status = "paused"; record.error = "服务已重启，任务已暂停。发送消息继续。"; }
    }
    for(const workspace of this.state.workspaces){workspace.trusted=true;if(workspace.remote?.error?.includes("信任"))workspace.remote.error=undefined;}
    this.save();
  }
  private migrate(sourcePath?: string) {
    const config = process.platform === "win32" ? process.env.APPDATA : process.platform === "darwin" ? join(homedir(), "Library", "Application Support") : process.env.XDG_CONFIG_HOME || join(homedir(), ".config");
    const oldPath = sourcePath || config && join(config, "pi-desk", "state.json");
    if (!oldPath || !existsSync(oldPath)) return;
    // Preserve the complete old metadata before projecting only the supported local fields.
    copyFileSync(oldPath, join(deskDir, "legacy-state.json"));
    const legacy = JSON.parse(readFileSync(oldPath, "utf8"));
    for (const w of legacy.workspaces || []) {
      const path = w.path || w.location?.local?.canonicalPath || w.location?.local?.path || w.location?.path;
      if (!path || w.location?.kind === "ssh") continue;
      this.state.workspaces.push({ id: w.id || randomUUID(), name: w.name || path, path, trusted: true });
    }
    for (const t of legacy.desktop?.threads || []) {
      const workspace = this.state.workspaces.find(w => w.id === t.workspaceId || w.path === t.workspacePath);
      if (!workspace) continue;
      this.state.sessions.push({ id: t.id || randomUUID(), title: t.title || "历史会话", workspaceId: workspace.id, cwd: workspace.path, mode: t.disableSkills ? "minimal" : "standard", depth: 0, status: t.status === "idle" ? "idle" : "paused", sessionPath: t.sessionPath || undefined, createdAt: t.createdAt || new Date().toISOString(), updatedAt: t.updatedAt || new Date().toISOString(), draft: t.draft || "", archived: Boolean(t.archived) });
    }
    const images = join(dirname(oldPath), "composer-images");
    if (existsSync(images)) cpSync(images, join(deskDir, "composer-images"), { recursive: true, force: false });
    const theme = legacy.desktop?.preferences?.appearance;
    if (["light", "dark", "system"].includes(theme)) this.state.preferences.theme = theme;
    this.state.migratedFrom = oldPath;
  }
  save() {
    const latest = new Map<string, string>();
    for (const record of this.state.sessions) { const time = record.updatedAt || record.createdAt || ""; if (time > (latest.get(record.workspaceId) || "")) latest.set(record.workspaceId, time); }
    this.state.workspaces.sort((a, b) => Number(Boolean(b.pinned)) - Number(Boolean(a.pinned)) || (latest.get(b.id) || "").localeCompare(latest.get(a.id) || ""));
    // ponytail: one synchronous metadata file; use an indexed store if 10,000 sessions make saves noticeable.
    const contents = JSON.stringify(this.state, null, 2) + "\n";
    if (contents === this.saved) return;
    const temporary = this.path + ".tmp";
    writeFileSync(temporary, contents, { mode: 0o600 });
    // Windows readers and scanners can briefly deny replacement; keep the previous file intact.
    for (let attempt = 0; ; attempt++) {
      try { renameSync(temporary, this.path); break; }
      catch (error) {
        if (process.platform !== "win32" || attempt >= 5 || !["EPERM", "EACCES", "EBUSY"].includes((error as NodeJS.ErrnoException).code || "")) throw error;
        Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 10);
      }
    }
    this.saved = contents;
  }
  cacheImages(images: DraftImage[]): DraftImage[] {
    if (!Array.isArray(images) || images.length > 20) throw new Error("每条消息最多保存 20 张图片");
    return images.map(image => {
      if (!image || !["image/png", "image/jpeg", "image/webp", "image/gif"].includes(image.mimeType)) throw new Error("图片类型不受支持");
      let key = image.cacheKey;
      if (image.data !== undefined) {
        if (typeof image.data !== "string" || !/^[A-Za-z0-9+/]*={0,2}$/.test(image.data)) throw new Error("图片编码无效");
        const data = Buffer.from(image.data, "base64"); if (data.length > 10 * 1024 * 1024 || !data.length) throw new Error("图片必须在 10 MiB 以内");
        key = createHash("sha256").update(data).digest("hex");
        const folder = join(deskDir, "composer-images"); mkdirSync(folder, { recursive: true, mode: 0o700 });
        writeFileSync(join(folder, key), data, { mode: 0o600 });
      }
      if (typeof key !== "string" || !/^[a-f0-9]{64}$/.test(key)) throw new Error("图片缓存引用无效");
      return { type: "image", id: typeof image.id === "string" ? image.id.slice(0, 100) : randomUUID(), name: String(image.name || "图片").slice(0, 200), mimeType: image.mimeType, cacheKey: key };
    });
  }
  images(images: DraftImage[] = []) {
    return images.map(image => {
      try {
        if (!image.cacheKey || !/^[a-f0-9]{64}$/.test(image.cacheKey)) throw new Error("invalid key");
        const data = readFileSync(join(deskDir, "composer-images", image.cacheKey));
        if (data.length > 10 * 1024 * 1024 || createHash("sha256").update(data).digest("hex") !== image.cacheKey) throw new Error("corrupt image");
        return { ...image, data: data.toString("base64") };
      } catch { return { ...image, missing: true }; }
    });
  }
  record(id: string): SessionRecord { const r = this.state.sessions.find(s => s.id === id); if (!r) throw new Error("会话不存在"); return r; }
}
