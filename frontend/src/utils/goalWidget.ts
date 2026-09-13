export const PI_DESK_GOAL_WIDGET_KEY = "pi-desk-goal";
const MAX_GOAL_TEXT = 4096;

export type GoalWidgetStatus = "active" | "paused" | "blocked" | "usage_limited" | "budget_limited" | "complete";

const GOAL_STATUSES: readonly GoalWidgetStatus[] = ["active", "paused", "blocked", "usage_limited", "budget_limited", "complete"];

export interface GoalWidgetProjection {
  status: GoalWidgetStatus;
  iteration: number;
  tokensUsed: number;
  tokenBudget?: number;
  text: string;
}

interface WidgetLike {
  key: string;
  lines: string[];
}

const STATUS_LINE = /^status:([a-z_]+)$/i;
const ITERATION_LINE = /^iteration:(\d+)$/;
const TOKENS_LINE = /^tokens:(\d+)(?:\/(\d+))?$/;
const HEADER_LINE = /^--\s*goal\s*--$/i;

export function parsePiDeskGoalWidget(widget: WidgetLike | undefined): GoalWidgetProjection | undefined {
  if (!widget || widget.key.toLocaleLowerCase() !== PI_DESK_GOAL_WIDGET_KEY) return undefined;

  let status: GoalWidgetStatus | undefined;
  let iteration = -1;
  let tokensUsed = -1;
  let tokenBudget: number | undefined;
  const textLines: string[] = [];
  for (const rawLine of widget.lines) {
    const line = rawLine.trim();
    if (HEADER_LINE.test(line)) continue;
    const statusMatch = STATUS_LINE.exec(line);
    if (statusMatch) {
      const candidate = statusMatch[1].toLowerCase() as GoalWidgetStatus;
      if (GOAL_STATUSES.includes(candidate)) status = candidate;
      continue;
    }
    const iterationMatch = ITERATION_LINE.exec(line);
    if (iterationMatch) {
      iteration = Number(iterationMatch[1]);
      continue;
    }
    const tokensMatch = TOKENS_LINE.exec(line);
    if (tokensMatch) {
      tokensUsed = Number(tokensMatch[1]);
      if (tokensMatch[2] !== undefined) tokenBudget = Number(tokensMatch[2]);
      continue;
    }
    textLines.push(line);
  }
  if (!status || iteration < 0 || tokensUsed < 0 || textLines.length === 0) return undefined;
  if (tokenBudget !== undefined && tokenBudget < tokensUsed) return undefined;
  return {
    status,
    iteration,
    tokensUsed,
    ...(tokenBudget !== undefined ? { tokenBudget } : {}),
    text: textLines.join("\n").slice(0, MAX_GOAL_TEXT),
  };
}
