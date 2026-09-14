/**
 * Compact projection of the subagent extension's tool result details.
 *
 * The extension streams per-task state (mode + results) in tool result
 * details; this derives the live per-delegate status the transcript shows,
 * without retaining the full child transcripts.
 */

export interface SubagentTaskState {
  agent: string;
  status: "running" | "ok" | "error";
  step?: number;
  model?: string;
  inputTokens: number;
  outputTokens: number;
  cost: number;
}

export interface SubagentTaskSummary {
  mode: string;
  tasks: SubagentTaskState[];
}

export function subagentSummaryFromResult(result: unknown): SubagentTaskSummary | undefined {
  if (!result || typeof result !== "object") return undefined;
  const details = (result as { details?: unknown }).details;
  if (!details || typeof details !== "object") return undefined;
  const record = details as { mode?: unknown; results?: unknown };
  if (!Array.isArray(record.results) || record.results.length === 0) return undefined;
  const tasks: SubagentTaskState[] = [];
  for (const raw of record.results) {
    if (!raw || typeof raw !== "object") continue;
    const item = raw as {
      agent?: unknown;
      exitCode?: unknown;
      step?: unknown;
      model?: unknown;
      stopReason?: unknown;
      usage?: { input?: unknown; output?: unknown; cost?: unknown };
    };
    // Streaming updates carry exitCode -1 placeholders (parallel) or a
    // non-terminal stopReason; "toolUse" means the child is still in its
    // tool loop, so only a terminal stopReason settles a task's outcome.
    const running = item.exitCode === -1
      || item.stopReason === undefined
      || item.stopReason === "toolUse";
    const failed = (typeof item.exitCode === "number" && item.exitCode !== 0 && item.exitCode !== -1)
      || item.stopReason === "error" || item.stopReason === "aborted";
    tasks.push({
      agent: typeof item.agent === "string" ? item.agent : "",
      status: running ? "running" : failed ? "error" : "ok",
      ...(typeof item.step === "number" ? { step: item.step } : {}),
      ...(typeof item.model === "string" && item.model ? { model: item.model } : {}),
      inputTokens: typeof item.usage?.input === "number" ? item.usage.input : 0,
      outputTokens: typeof item.usage?.output === "number" ? item.usage.output : 0,
      cost: typeof item.usage?.cost === "number" ? item.usage.cost : 0,
    });
  }
  if (tasks.length === 0) return undefined;
  return { mode: typeof record.mode === "string" ? record.mode : "single", tasks };
}
