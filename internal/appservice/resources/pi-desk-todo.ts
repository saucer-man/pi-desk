/**
 * Pi Desk Todo Extension
 *
 * Registers a todo tool for the model and projects its current state through
 * the RPC Extension UI Protocol. Todo snapshots are stored as custom entries
 * in the active Pi session, so they follow session branches without entering
 * model context.
 *
 * A todo list belongs to one user turn. before_agent_start clears the previous
 * turn's list before the next model run begins; tool-loop continuations inside
 * the same run keep their current list.
 */

import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { StringEnum } from "@earendil-works/pi-ai";
import { Type } from "typebox";

interface Todo {
	id: number;
	text: string;
	done: boolean;
}

interface TodoState {
	todos: Todo[];
	nextId: number;
}

interface TodoDetails extends TodoState {
	action: "list" | "add" | "toggle" | "clear";
	error?: string;
}

const TodoParams = Type.Object({
	action: StringEnum(["list", "add", "toggle", "clear"] as const),
	text: Type.Optional(Type.String({ description: "Todo text (for add)" })),
	id: Type.Optional(Type.Number({ description: "Todo ID (for toggle)" })),
});

const WIDGET_KEY = "pi-desk-todo";
const ENTRY_TYPE = "pi-desk-todo";
const SELF_MARKER = "pi-desk-todo";

export default function (pi: ExtensionAPI) {
	let todos: Todo[] = [];
	let nextId = 1;
	let widgetCollapsed = false;
	let yielded = false;

	function isOwnTodo(): boolean {
		const tool = pi.getAllTools().find((candidate) => candidate.name === "todo");
		const source = tool?.sourceInfo as { path?: string; source?: string } | undefined;
		return Boolean(source?.path?.includes(SELF_MARKER) || source?.source?.includes(SELF_MARKER));
	}

	function orderedTodos(): Todo[] {
		return [...todos].sort((left, right) => left.id - right.id);
	}

	function persistState(): void {
		// The session snapshot is canonicalized too, so every later projection
		// preserves #1, #2, #3... regardless of completion state.
		todos = orderedTodos();
		pi.appendEntry(ENTRY_TYPE, { todos, nextId });
	}

	function updateWidget(ctx: ExtensionContext): void {
		if (todos.length === 0) {
			ctx.ui.setWidget(WIDGET_KEY, undefined);
			return;
		}
		const ordered = orderedTodos();
		const completed = ordered.filter((todo) => todo.done).length;
		const lines = widgetCollapsed
			? [`${completed}/${ordered.length}`]
			: ["-- Todo --", ...ordered.map((todo) => `${todo.done ? "[x]" : "[ ]"} #${todo.id} ${todo.text}`)];
		ctx.ui.setWidget(WIDGET_KEY, lines);
	}

	function reconstructState(ctx: ExtensionContext): void {
		const last = ctx.sessionManager
			.getEntries()
			.filter((entry: { type: string; customType?: string }) => entry.type === "custom" && entry.customType === ENTRY_TYPE)
			.pop() as { data?: TodoState } | undefined;
		todos = [...(last?.data?.todos ?? [])].sort((left, right) => left.id - right.id);
		nextId = last?.data?.nextId ?? 1;
	}

	function clearPreviousTurn(ctx: ExtensionContext): void {
		if (yielded || !isOwnTodo()) {
			yielded = true;
			return;
		}
		const changed = todos.length > 0 || nextId !== 1;
		todos = [];
		nextId = 1;
		widgetCollapsed = false;
		if (changed) persistState();
		updateWidget(ctx);
	}

	pi.registerTool({
		name: "todo",
		label: "Todo",
		description: "Manage a todo list for the current user turn. Actions: list, add, toggle, and clear.",
		promptSnippet: "Manage a todo list for the current user turn (add / toggle / clear)",
		promptGuidelines: [
			"Use the todo tool to track multi-step work: add items before starting, toggle done as each step completes, and clear when finished.",
			"Todo items are scoped to the current user turn and are reset before the next user turn begins.",
			"Always present todo items in numeric ID order (#1, #2, #3...) and never regroup completed items separately.",
			"Toggle todo items by id; call todo list first if the ids are unknown.",
		],
		parameters: TodoParams,

		async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
			let error: string | undefined;

			switch (params.action) {
				case "add": {
					const text = params.text?.trim();
					if (!text) {
						error = "text required for add";
						break;
					}
					todos.push({ id: nextId++, text, done: false });
					break;
				}
				case "toggle": {
					if (params.id === undefined) {
						error = "id required for toggle";
						break;
					}
					const target = todos.find((todo) => todo.id === params.id);
					if (!target) {
						error = `#${params.id} not found`;
						break;
					}
					target.done = !target.done;
					break;
				}
				case "clear":
					todos = [];
					nextId = 1;
					break;
				case "list":
				default:
					break;
			}

			if (params.action !== "list" && !error) persistState();
			updateWidget(ctx);

			const ordered = orderedTodos();
			const details: TodoDetails = {
				action: params.action,
				todos: ordered,
				nextId,
				...(error ? { error } : {}),
			};

			let text: string;
			if (error) {
				text = `Error: ${error}`;
			} else if (params.action === "list") {
				text = ordered.length
					? ordered.map((todo) => `[${todo.done ? "x" : " "}] #${todo.id}: ${todo.text}`).join("\n")
					: "No todos";
			} else if (params.action === "add") {
				const added = todos[todos.length - 1];
				text = `Added todo #${added.id}: ${added.text}`;
			} else if (params.action === "toggle") {
				const todo = todos.find((candidate) => candidate.id === params.id);
				text = `Todo #${params.id} ${todo?.done ? "completed" : "uncompleted"}`;
			} else {
				text = "Cleared all todos";
			}

			return { content: [{ type: "text" as const, text }], details };
		},
	});

	pi.registerCommand("todo", {
		description: "Show, collapse, or expand the current turn's todo list",
		handler: async (args, ctx) => {
			if (!isOwnTodo()) {
				ctx.ui.notify("Another extension provides the todo tool; use that extension's command.", "info");
				return;
			}
			if (args === "collapse") {
				widgetCollapsed = true;
				updateWidget(ctx);
				return;
			}
			if (args === "expand") {
				widgetCollapsed = false;
				updateWidget(ctx);
				return;
			}
			if (todos.length === 0) {
				ctx.ui.notify("No todos in the current turn.", "info");
				return;
			}
			const ordered = orderedTodos();
			const completed = ordered.filter((todo) => todo.done).length;
			ctx.ui.notify([
				`Todos ${completed}/${ordered.length}`,
				...ordered.map((todo) => `${todo.done ? "[x]" : "[ ]"} #${todo.id} ${todo.text}`),
			].join("\n"), "info");
		},
	});

	pi.on("session_start", async (_event, ctx) => {
		if (!isOwnTodo()) {
			yielded = true;
			return;
		}
		yielded = false;
		reconstructState(ctx);
		updateWidget(ctx);
	});

	pi.on("before_agent_start", async (_event, ctx) => {
		clearPreviousTurn(ctx);
	});

	pi.on("session_tree", async (_event, ctx) => {
		if (yielded || !isOwnTodo()) {
			yielded = true;
			return;
		}
		reconstructState(ctx);
		updateWidget(ctx);
	});
}
