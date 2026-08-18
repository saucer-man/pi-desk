export const PI_DESK_TODO_WIDGET_KEY = "pi-desk-todo";
const MAX_TODO_ITEMS = 100;
const MAX_TODO_TEXT = 4096;

export interface TodoWidgetItem {
  id: number;
  text: string;
  done: boolean;
}

export interface TodoWidgetProjection {
  items: TodoWidgetItem[];
  completed: number;
  total: number;
}

interface WidgetLike {
  key: string;
  lines: string[];
}

const TODO_LINE = /^(?:\[(x| )\]|(☑|☐))\s*#(\d+)\s*:?[ \t]+(.+)$/i;
const SUMMARY_LINE = /^(\d+)\s*\/\s*(\d+)$/;

export function parsePiDeskTodoWidget(widget: WidgetLike | undefined): TodoWidgetProjection | undefined {
  if (!widget || widget.key.toLocaleLowerCase() !== PI_DESK_TODO_WIDGET_KEY) return undefined;

  const items = new Map<number, TodoWidgetItem>();
  let summary: { completed: number; total: number } | undefined;
  for (const rawLine of widget.lines.slice(0, MAX_TODO_ITEMS + 8)) {
    const line = rawLine.trim();
    const summaryMatch = SUMMARY_LINE.exec(line);
    if (summaryMatch) {
      const completed = Number(summaryMatch[1]);
      const total = Number(summaryMatch[2]);
      if (Number.isSafeInteger(completed) && Number.isSafeInteger(total) && completed >= 0 && total >= completed) {
        summary = { completed, total };
      }
      continue;
    }
    const match = TODO_LINE.exec(line);
    if (!match) continue;
    const id = Number(match[3]);
    const text = match[4].trim().slice(0, MAX_TODO_TEXT);
    if (!Number.isSafeInteger(id) || id <= 0 || !text || items.has(id)) continue;
    items.set(id, { id, text, done: match[1]?.toLocaleLowerCase() === "x" || match[2] === "☑" });
  }

  const ordered = [...items.values()].sort((left, right) => left.id - right.id);
  if (ordered.length) {
    return {
      items: ordered,
      completed: ordered.filter((item) => item.done).length,
      total: ordered.length,
    };
  }
  return summary ? { items: [], ...summary } : undefined;
}
