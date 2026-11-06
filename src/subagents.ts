import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { mkdir, readFile, readdir } from "node:fs/promises";
import { join, posix } from "node:path";
import {quote} from "./remote.ts";
import { randomUUID } from "node:crypto";
import { Type } from "typebox";
import { defineTool } from "@earendil-works/pi-coding-agent";
import { parse } from "yaml";
import { agentDir, deskDir, safeError } from "./storage.ts";
import type { Runtime } from "./runtime.ts";
import type { SessionRecord } from "./types.ts";

const exec = promisify(execFile);
export async function git(cwd: string, args: string[], signal?: AbortSignal) { return (await exec("git", ["-C", cwd, ...args], { windowsHide: true, signal, maxBuffer: 8 * 1024 * 1024 })).stdout.trim(); }
const writing = new Map<string, Promise<unknown>>();
async function serial<T>(key: string, operation: () => Promise<T>): Promise<T> {
  const pending = (writing.get(key) || Promise.resolve()).catch(() => {}).then(operation); writing.set(key, pending);
  try { return await pending; } finally { if (writing.get(key) === pending) writing.delete(key); }
}
export async function agentDefinitions(cwd: string) {
  const agents = new Map<string, { name: string; description: string; instructions: string; model?: string; readOnly: boolean; source: string }>();
  for (const dir of [join(agentDir, "agents"), join(cwd, ".pi", "agents")]) {
    for (const file of await readdir(dir).catch(() => [] as string[])) {
      if (!file.endsWith(".md")) continue;
      const source = join(dir, file); const text = await readFile(source, "utf8"); const match = /^---\r?\n([\s\S]*?)\r?\n---\r?\n([\s\S]*)$/.exec(text);
      const meta = match ? parse(match[1]) || {} : {};
      const name = String(meta.name || file.slice(0, -3));
      const tools = Array.isArray(meta.tools) ? meta.tools : typeof meta.tools === "string" ? meta.tools.split(/[, ]+/) : undefined;
      agents.set(name, { name, description: String(meta.description || ""), instructions: match ? match[2] : text, model: meta.model, readOnly: Boolean(tools?.length && tools.every((t: string) => ["read", "grep", "find", "ls"].includes(t))), source });
    }
  }
  return [...agents.values()];
}
interface Task { agent?: string; task: string; access?: "read" | "write"; mode?: "standard" | "minimal"; model?: string }
interface Input { mode?: "single" | "parallel" | "chain"; tasks?: Task[]; task?: string; agent?: string; access?: "read" | "write"; action?: "run" | "integrate"; id?: string }

async function integrate(runtime: Runtime, parent: SessionRecord, id: string) {
  return serial("git:" + parent.workspaceId + ":" + parent.cwd.toLowerCase(), () => applyWorktree(runtime, parent, id));
}
async function applyWorktree(runtime: Runtime, parent: SessionRecord, id: string) {
  const child = runtime.store.record(id);
  if (child.parentId !== parent.id || !child.worktree) throw new Error("只能整合当前会话的直接写入子任务");
  if (child.status !== "idle") throw new Error("子任务尚未成功完成");
  if (child.worktree.integrated) return { id, result: "已整合" };
  const origin = parent.cwd;
  const workspace=runtime.remote.workspaceFor(parent.id);
  const runGit=(cwd:string,args:string[])=>workspace?runtime.remote.git(workspace,cwd,args):git(cwd,args);
  if (await runGit(origin, ["status", "--porcelain"])) throw new Error("主工作区有未提交修改。请先整理变更，子任务 worktree 已保留。");
  for (const ref of ["CHERRY_PICK_HEAD", "MERGE_HEAD"]) if (await runGit(origin, ["rev-parse", "--verify", ref]).then(() => true, () => false)) throw new Error("主工作区已有未结束的 Git 操作，请先完成该操作");
  const branch = await runGit(origin, ["branch", "--show-current"]);
  if (branch !== child.worktree.baseBranch) throw new Error("主工作区分支已改变，请手动检查子任务变更再整合");
  if (await runGit(child.cwd, ["status", "--porcelain"])) {
    await runGit(child.cwd, ["add", "--all"]);
    await runGit(child.cwd, ["-c", "user.name=Pi Desk", "-c", "user.email=pi-desk@localhost", "commit", "-m", `Subagent: ${child.title.slice(0, 100)}`]);
    child.worktree.commit = await runGit(child.cwd, ["rev-parse", "HEAD"]); runtime.changed(child);
  }
  const commit = await runGit(child.cwd, ["rev-parse", "HEAD"]);
  if (commit === child.worktree.baseHead) { child.worktree.integrated = true; runtime.changed(child); return { id, result: "子任务没有文件变更" }; }
  await runGit(child.cwd, ["merge-base", "--is-ancestor", child.worktree.baseHead, commit]);
  child.worktree.commit = commit; runtime.changed(child);
  try { await runGit(origin, ["cherry-pick", `${child.worktree.baseHead}..${commit}`]); }
  catch (error) {
    // This cherry-pick started from a clean checkout; only unwind the operation we own.
    await runGit(origin, ["cherry-pick", "--abort"]).catch(() => {});
    throw new Error(`整合发生冲突，worktree 和提交 ${commit} 已保留。${safeError(error)}`);
  }
  child.worktree.integrated = true; runtime.changed(child); runtime.trace(parent.id, "subagent_integrated", "子任务变更已整合", { id, commit });
  return { id, commit, result: "变更已整合" };
}

export async function delegate(runtime: Runtime, parent: SessionRecord, input: Input, signal?: AbortSignal) {
  if (parent.mode === "minimal") throw new Error("Minimal 仅允许 Pi 四工具，不提供主动委派");
  await runtime.get(parent.id);
  if (input.action === "integrate") { if (!input.id) throw new Error("需要子任务 id"); return integrate(runtime, parent, input.id); }
  if (parent.depth >= 2) throw new Error("子 agent 最多嵌套两层");
  const tasks = input.tasks || (input.task ? [{ task: input.task, agent: input.agent, access: input.access }] : []);
  if (!tasks.length || tasks.some(t => !t.task.trim())) throw new Error("至少需要一个非空子任务");
  const workspace=runtime.remote.workspaceFor(parent.id);
  const runGit=(cwd:string,args:string[],signal?:AbortSignal)=>workspace?runtime.remote.git(workspace,cwd,args,signal):git(cwd,args,signal);
  const definitions = await agentDefinitions(workspace?await runtime.remote.anchor(workspace):parent.cwd);
  const run = async (task: Task, previous = "") => {
    signal?.throwIfAborted();
    const definition = task.agent && definitions.find(a => a.name === task.agent);
    if (task.agent && !definition && !["worker", "scout", "reviewer"].includes(task.agent)) throw new Error(`未找到 agent 定义：${task.agent}`);
    const readOnly = parent.readOnly || task.access === "read" || definition && definition.readOnly || ["scout", "reviewer"].includes(task.agent || "");
    const id = randomUUID(); let cwd = parent.cwd; let worktree: SessionRecord["worktree"];
    let isGit = false;
    if(!readOnly)try { isGit = await runGit(parent.cwd, ["rev-parse", "--is-inside-work-tree"]) === "true"; } catch(error) {if(workspace&&!/not a git repository/i.test(String(error)))throw error;}
    if (!readOnly && isGit) {
      const root = parent.cwd;
      const baseHead = await runGit(parent.cwd, ["rev-parse", "HEAD"]);
      const baseBranch = await runGit(parent.cwd, ["branch", "--show-current"]);
      if(workspace){cwd=await runtime.remote.worktreePath(workspace,id);const result=await runtime.remote.shell(workspace,"mkdir -p "+quote(posix.dirname(cwd)),{signal});if(result.exitCode!==0)throw Error("无法创建远端 worktree 目录");}
      else {cwd = join(deskDir, "worktrees", id); await mkdir(join(deskDir, "worktrees"), { recursive: true });}
      const branch = `pi-desk/${id}`; await runGit(parent.cwd, ["worktree", "add", "-b", branch, cwd, baseHead], signal);
      worktree = { root, baseHead, baseBranch, branch };
    }
    if (!readOnly && !isGit && parent.depth > 0 && !parent.readOnly) throw new Error("非 Git 目录中的嵌套子任务请使用只读访问；写入由当前子 agent 完成");
    const instructions = [definition && definition.instructions || (readOnly ? "Inspect the project and report findings. You have read-only tools." : "Complete the assigned task, run appropriate validation, and report changes and checks."), worktree ? `Your isolated Git worktree is based on commit ${worktree.baseHead}. Uncommitted changes in the parent checkout are not included. Do not merge or remove this worktree. Report the validation commands and result to the parent.` : ""].join("\n");
    const child = runtime.create(parent.workspaceId, task.mode || "standard", { id, cwd, parentId: parent.id, depth: parent.depth + 1, title: `${task.agent || "worker"} · ${task.task.slice(0, 55)}`, readOnly: Boolean(readOnly), model: task.model || definition && definition.model || parent.model, instructions, worktree });
    runtime.trace(parent.id, "subagent_start", "委派子任务", { id });
    const execute = async () => {
      const abort = () => { void runtime.stop(id); }; signal?.addEventListener("abort", abort, { once: true });
      try {
        signal?.throwIfAborted(); await runtime.prompt(id, previous ? `${task.task}\n\n上一任务结果：\n${previous}` : task.task, undefined, "followUp", signal);
        signal?.throwIfAborted();
        const live = await runtime.get(id); const result = live.session.getLastAssistantText() || child.error || "子任务已完成，未返回文本";
        const snapshot = await runtime.snapshot(id);
        const value = { id, status: child.status, result: result.slice(0, 24_000), cwd, worktree, stats: snapshot.stats };
        runtime.trace(parent.id, "subagent_end", "子任务结束", { id, status: child.status }); return value;
      } catch (error) { child.status = signal?.aborted ? "paused" : "error"; child.error = safeError(error); runtime.changed(child); runtime.trace(parent.id,"subagent_error","子任务未完成",{id,status:child.status,error:child.error}); throw error; }
      finally { signal?.removeEventListener("abort", abort); }
    };
    if (!readOnly && !isGit) {
      // ponytail: non-Git writes serialize; nested non-Git writers must stay in the current writer instead of deadlocking a second lease.
      return serial("write:" + parent.workspaceId + ":" + parent.cwd.toLowerCase(), execute);
    }
    return execute();
  };
  if (input.mode === "parallel") return Promise.all(tasks.map(task => run(task)));
  const results: Awaited<ReturnType<typeof run>>[] = [];
  for (const task of tasks) { signal?.throwIfAborted(); results.push(await run(task, input.mode === "chain" ? results.at(-1)?.result : "")); }
  return results;
}

const taskSchema = Type.Object({ task: Type.String({ minLength: 1 }), agent: Type.Optional(Type.String()), access: Type.Optional(Type.Union([Type.Literal("read"), Type.Literal("write")])), mode: Type.Optional(Type.Union([Type.Literal("standard"), Type.Literal("minimal")])), model: Type.Optional(Type.String()) });
export function subagentTool(runtime: Runtime, parent: SessionRecord) {
  return defineTool({ name: "subagent", label: "子 agent", executionMode: "sequential",
    description: "Delegate independent tasks to in-process SDK subagents. Modes: single, parallel (unlimited), chain (passes previous result). Use access=read for analysis; write tasks use isolated Git worktrees. Default agents: worker, scout, reviewer; custom definitions in ~/.pi/agent/agents and .pi/agents. Inspect each result, run validation, then action=integrate with child id to merge a successful write task. Keep worktrees on failures. Maximum nesting: two levels.",
    parameters: Type.Object({ action: Type.Optional(Type.Union([Type.Literal("run"), Type.Literal("integrate")])), id: Type.Optional(Type.String()), mode: Type.Optional(Type.Union([Type.Literal("single"), Type.Literal("parallel"), Type.Literal("chain")])), tasks: Type.Optional(Type.Array(taskSchema, { minItems: 1 })), task: Type.Optional(Type.String()), agent: Type.Optional(Type.String()), access: Type.Optional(Type.Union([Type.Literal("read"), Type.Literal("write")])) }),
    execute: async (_id, params, signal) => { const result = await delegate(runtime, parent, params, signal); return { content: [{ type: "text", text: JSON.stringify(result) }], details: result }; },
  });
}
