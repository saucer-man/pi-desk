import net from "node:net";
import path from "node:path";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";

const socketPath = process.env.PI_DESK_REMOTE_SOCKET;
const token = process.env.PI_DESK_REMOTE_TOKEN;
const root = process.env.PI_DESK_REMOTE_ROOT;
const maxFrame = 32 * 1024 * 1024;
const protocol = "pi-desk.remote-tools.v1";
const coverage = ["bash", "edit", "find", "grep", "ls", "read", "user_bash", "write"] as const;
let nextID = 0;

if (!socketPath || !token || !root || !path.posix.isAbsolute(root) || path.posix.normalize(root) !== root) {
	throw new Error("Pi Desk remote adapter configuration is invalid");
}

type BrokerError = { code: string; message: string };
type BrokerResponse<T> = { id: string; ok: boolean; result?: T; error?: BrokerError };
class RemoteBrokerError extends Error {
	constructor(readonly code: string, message: string) { super(`${code}: ${message}`); }
}

function logicalPath(value: string | undefined, allowRoot = true): string {
	const input = value === undefined || value === "" ? "." : value;
	if (input.includes("\\") || /[\p{Cc}\p{Cf}]/u.test(input)) throw new Error("Remote path is invalid");
	let relative = input;
	if (path.posix.isAbsolute(input)) {
		if (input === root) relative = ".";
		else if (root === "/") relative = input.slice(1);
		else if (input.startsWith(`${root}/`)) relative = input.slice(root.length + 1);
		else throw new Error("Remote path is outside the workspace");
	}
	const normalized = path.posix.normalize(relative);
	if (normalized === ".." || normalized.startsWith("../") || path.posix.isAbsolute(normalized) || normalized.includes("\\")) {
		throw new Error("Remote path is outside the workspace");
	}
	if (!allowRoot && normalized === ".") throw new Error("Remote file path is required");
	return normalized;
}

function call<T>(operation: string, payload: Record<string, unknown>, signal?: AbortSignal): Promise<T> {
	return new Promise((resolve, reject) => {
		if (signal?.aborted) return reject(new Error("Operation aborted"));
		const id = `tool-${++nextID}`;
		const body = Buffer.from(JSON.stringify({ id, token, operation, ...payload }), "utf8");
		if (body.length > maxFrame) return reject(new Error("Remote tool request exceeds the safety limit"));
		const header = Buffer.allocUnsafe(4);
		header.writeUInt32BE(body.length);
		const socket = net.createConnection(socketPath);
		let buffer = Buffer.alloc(0);
		let expected = -1;
		let settled = false;
		let dispatched = false;
		const mutationUnknown = () => new RemoteBrokerError("REMOTE_OUTCOME_UNKNOWN", "Remote mutation outcome is unknown; inspect the workspace before retrying");
		const finish = (error?: Error, value?: T) => {
			if (settled) return;
			settled = true;
			signal?.removeEventListener("abort", abort);
			socket.destroy();
			if (error) reject(error); else resolve(value as T);
		};
		const abort = () => finish(dispatched && ["write", "edit", "bash"].includes(operation) ? mutationUnknown() : new Error("Operation aborted"));
		signal?.addEventListener("abort", abort, { once: true });
		socket.once("connect", () => {
			dispatched = true;
			socket.write(Buffer.concat([header, body]));
		});
		socket.on("data", (chunk: Buffer) => {
			buffer = Buffer.concat([buffer, chunk]);
			if (expected < 0 && buffer.length >= 4) {
				expected = buffer.readUInt32BE(0);
				buffer = buffer.subarray(4);
				if (expected < 1 || expected > maxFrame) return finish(new Error("Remote broker response is invalid"));
			}
			if (expected >= 0 && buffer.length >= expected) {
				if (buffer.length !== expected) return finish(new Error("Remote broker response is invalid"));
				try {
					const response = JSON.parse(buffer.toString("utf8")) as BrokerResponse<T>;
					if (response.id !== id || response.ok !== true || response.error) {
						return finish(response.error ? new RemoteBrokerError(response.error.code, response.error.message) : new Error("Remote broker response is invalid"));
					}
					finish(undefined, response.result);
				} catch (error) {
					finish(error instanceof Error ? error : new Error("Remote broker response is invalid"));
				}
			}
		});
		socket.once("error", () => finish(dispatched && ["write", "edit", "bash"].includes(operation) ? mutationUnknown() : new RemoteBrokerError("REMOTE_DISCONNECTED", "Remote workspace is disconnected or stale")));
		socket.once("end", () => {
			if (!settled) finish(dispatched && ["write", "edit", "bash"].includes(operation) ? mutationUnknown() : new RemoteBrokerError("REMOTE_DISCONNECTED", "Remote workspace is disconnected or stale"));
		});
	});
}

const readParameters = Type.Object({
	path: Type.String(),
	offset: Type.Optional(Type.Number()),
	limit: Type.Optional(Type.Number()),
});
const writeParameters = Type.Object({ path: Type.String(), content: Type.String() });
const editParameters = Type.Object({
	path: Type.String(),
	edits: Type.Array(Type.Object({ oldText: Type.String(), newText: Type.String() })),
});
const findParameters = Type.Object({ pattern: Type.String(), path: Type.Optional(Type.String()), limit: Type.Optional(Type.Number()) });
const grepParameters = Type.Object({
	pattern: Type.String(), path: Type.Optional(Type.String()), glob: Type.Optional(Type.String()),
	ignoreCase: Type.Optional(Type.Boolean()), literal: Type.Optional(Type.Boolean()),
	context: Type.Optional(Type.Number()), limit: Type.Optional(Type.Number()),
});
const lsParameters = Type.Object({ path: Type.Optional(Type.String()), limit: Type.Optional(Type.Number()) });
const bashParameters = Type.Object({ command: Type.String(), timeout: Type.Optional(Type.Number()) });

export default function (pi: ExtensionAPI) {
	pi.on("project_trust", () => ({ trusted: "yes", remember: false }));
	pi.on("session_start", async (_event, ctx) => {
		await call("hello", { protocol, coverage });
		ctx.ui.setStatus("pi-desk-remote", `SSH: ${root}`);
	});
	pi.on("before_agent_start", async (event, ctx) => {
		const remoteContext = await call<{ content: string; hash: string }>("context", {});
		const localCwd = event.systemPromptOptions?.cwd || ctx.cwd;
		const context = [
			"Remote workspace context (managed by Pi Desk):",
			`- The actual workspace root is: ${root}`,
			`- Pi's local process cwd is: ${localCwd}`,
			"- Use the registered remote tools for workspace files and commands; do not treat the local cwd or anchor path as the remote workspace.",
			"",
			"<remote-project-context>",
			remoteContext.content,
			"</remote-project-context>",
		].join("\n");
		return { systemPrompt: `${event.systemPrompt}\n\n${context}` };
	});

	pi.registerTool({
		name: "read", label: "read",
		description: "Read a remote workspace file. Supports text and images. Text output is limited to 2000 lines or 50KB.",
		promptSnippet: "Read remote file contents", parameters: readParameters,
		async execute(_id, params, signal) {
			const offset = Number.isInteger(params.offset) ? params.offset! : 1;
			const limit = Number.isInteger(params.limit) ? params.limit! : 2000;
			const result = await call<any>("read", { path: logicalPath(params.path, false), offset, limit }, signal);
			if (typeof result.base64 === "string") return { content: [{ type: "image" as const, data: result.base64, mimeType: result.mime }] };
			let text = result.content as string;
			if (result.truncated && result.nextLine > 0) text += `\n\n[Showing lines ${result.startLine}-${result.endLine}. Use offset=${result.nextLine} to continue.]`;
			return { content: [{ type: "text" as const, text }] };
		},
	});
	pi.registerTool({
		name: "write", label: "write", description: "Create or atomically overwrite a remote workspace file.",
		promptSnippet: "Create or overwrite remote files", parameters: writeParameters, executionMode: "sequential",
		async execute(_id, params, signal) {
			const result = await call<any>("write", { path: logicalPath(params.path, false), content: params.content }, signal);
			return { content: [{ type: "text" as const, text: `Wrote ${result.size} bytes to ${result.path}` }] };
		},
	});
	pi.registerTool({
		name: "edit", label: "edit", description: "Apply exact, unique, non-overlapping replacements to one remote file atomically.",
		promptSnippet: "Make precise remote file edits", parameters: editParameters, executionMode: "sequential",
		async execute(_id, params, signal) {
			const result = await call<any>("edit", { path: logicalPath(params.path, false), edits: params.edits }, signal);
			return { content: [{ type: "text" as const, text: `Edited ${result.path}` }] };
		},
	});
	pi.registerTool({
		name: "find", label: "find", description: "Find remote workspace files by glob pattern. Respects Git candidate boundaries.",
		promptSnippet: "Find remote files by glob", parameters: findParameters,
		async execute(_id, params, signal) {
			const base = logicalPath(params.path);
			const limit = Number.isInteger(params.limit) ? params.limit! : 1000;
			const result = await call<any>("find", { path: base, pattern: params.pattern, limit }, signal);
			const prefix = base === "." ? "" : `${base}/`;
			let text = result.paths.length ? result.paths.map((item: string) => item.startsWith(prefix) ? item.slice(prefix.length) : item).join("\n") : "No files found matching pattern";
			if (result.budgetReached) text += "\n\n[Remote search budget reached; refine the pattern.]";
			return { content: [{ type: "text" as const, text }] };
		},
	});
	pi.registerTool({
		name: "grep", label: "grep", description: "Search remote workspace text with a Go/RE2 regular expression or literal string.",
		promptSnippet: "Search remote file contents", parameters: grepParameters,
		async execute(_id, params, signal) {
			const result = await call<any>("grep", {
				path: logicalPath(params.path), pattern: params.pattern, glob: params.glob || "", limit: Number.isInteger(params.limit) ? params.limit! : 100,
				context: Number.isInteger(params.context) ? params.context! : 0, ignoreCase: params.ignoreCase === true, literal: params.literal === true,
			}, signal);
			let text = result.output as string;
			if (result.budgetReached) text += "\n\n[Remote search budget reached; refine the query.]";
			if (result.lineTruncated) text += "\n\n[Some matching lines were truncated. Use read for full lines.]";
			return { content: [{ type: "text" as const, text }] };
		},
	});
	pi.registerTool({
		name: "ls", label: "ls", description: "List one remote workspace directory.", promptSnippet: "List remote directory contents", parameters: lsParameters,
		async execute(_id, params, signal) {
			const limit = Number.isInteger(params.limit) ? params.limit! : 500;
			const result = await call<any>("list", { path: logicalPath(params.path) }, signal);
			const entries = result.entries.slice(0, limit).map((entry: any) => path.posix.basename(entry.path) + (entry.kind === "directory" ? "/" : ""));
			let text = entries.length ? entries.join("\n") : "(empty directory)";
			if (result.truncated || result.entries.length > limit) text += `\n\n[${limit} entries limit reached.]`;
			return { content: [{ type: "text" as const, text }] };
		},
	});
	const bashOperations = {
		exec: async (command: string, _cwd: string, options: { onData: (data: Buffer) => void; signal?: AbortSignal; timeout?: number }) => {
			const timeout = Math.min(120, Math.max(1, Math.ceil(options.timeout || 120)));
			const result = await call<any>("bash", { content: command, timeout }, options.signal);
			if (result.output) options.onData(Buffer.from(result.output, "utf8"));
			return { exitCode: result.exitCode as number };
		},
	};
	pi.registerTool({
		name: "bash", label: "bash", description: "Execute a command in the remote workspace with bounded output and timeout.",
		promptSnippet: "Execute remote shell commands", parameters: bashParameters, executionMode: "sequential",
		async execute(_id, params, signal, onUpdate) {
			const chunks: Buffer[] = [];
			const result = await call<any>("bash", { content: params.command, timeout: Math.min(120, Math.max(1, Math.ceil(params.timeout || 120))) }, signal);
			if (result.output) chunks.push(Buffer.from(result.output, "utf8"));
			const text = Buffer.concat(chunks).toString("utf8") || `(command exited with code ${result.exitCode})`;
			onUpdate?.({ content: [{ type: "text" as const, text }] });
			return { content: [{ type: "text" as const, text }] };
		},
	});
	pi.on("user_bash", () => ({ operations: bashOperations }));
}
