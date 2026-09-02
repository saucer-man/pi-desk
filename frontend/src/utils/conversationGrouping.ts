import type { ExecutionStep, TimelineMessage, TimelineRunNotice } from "../stores/app";

function executionSteps(messages: TimelineMessage[], finalIndex: number): ExecutionStep[] {
  const steps: ExecutionStep[] = [];
  for (let index = 0; index < messages.length; index += 1) {
    const message = messages[index];
    if (message.thinking) steps.push({
      id: `${message.id}-thinking`, kind: "thinking", text: message.thinking,
      active: message.streaming && message.activeExecution === "thinking",
    });
    if (message.tools.length) steps.push({ id: `${message.id}-tools`, kind: "tools", tools: message.tools });
    if (index !== finalIndex && message.text) steps.push({ id: `${message.id}-message`, kind: "message", text: message.text });
  }
  return steps;
}

function inferredRunNotice(messages: TimelineMessage[], index: number): TimelineRunNotice | undefined {
  const message = messages[index];
  if (message.runNotice) {
    return message.runNotice.status === "retrying" || message.runNotice.status === "failed"
      ? { ...message.runNotice }
      : undefined;
  }
  if (!message.error) return undefined;
  const candidate = messages[index + 1];
  const next = candidate?.role === "assistant" ? candidate : undefined;
  return next ? undefined : { status: "failed", error: message.error };
}

function mergedRunNotice(messages: TimelineMessage[]): TimelineRunNotice | undefined {
  const explicit = messages.map((message) => message.runNotice)
    .findLast((notice) => notice?.status === "retrying" || notice?.status === "failed");
  if (explicit) return { ...explicit };
  if (messages.some((message) => message.runNotice?.status === "recovered")) return undefined;
  const lastErrorIndex = messages.findLastIndex((message) => Boolean(message.error));
  const recovered = lastErrorIndex >= 0 && messages.slice(lastErrorIndex + 1)
    .some((message) => !message.error && Boolean(message.text || message.thinking || message.tools.length));
  if (recovered) return undefined;
  const error = messages.map((message) => message.error).findLast(Boolean);
  return error ? { status: "failed", error } : undefined;
}

function mergeAssistantRun(messages: TimelineMessage[], turnStartedAt?: number): TimelineMessage | undefined {
  if (!messages.length) return undefined;
  const streaming = messages.some((message) => message.streaming);
  let finalIndex = -1;
  if (streaming) {
    finalIndex = messages.length - 1;
  } else {
    for (let index = messages.length - 1; index >= 0; index -= 1) {
      if (messages[index].text) {
        finalIndex = index;
        break;
      }
    }
  }
  if (finalIndex < 0) finalIndex = messages.length - 1;

  const finalMessage = messages[finalIndex];
  const lastMessage = messages.at(-1) ?? finalMessage;
  const steps = executionSteps(messages, finalIndex);
  const runNotice = mergedRunNotice(messages);
  const endedAt = lastMessage.timestampMs;
  const startedAt = turnStartedAt ?? messages[0].timestampMs;
  return {
    ...finalMessage,
    id: finalMessage.id,
    thinking: "",
    thinkingCount: steps.filter((step) => step.kind === "thinking").length,
    tools: [],
    executionSteps: steps,
    timestamp: lastMessage.timestamp || finalMessage.timestamp,
    timestampMs: endedAt ?? finalMessage.timestampMs,
    durationMs: startedAt !== undefined && endedAt !== undefined ? Math.max(0, endedAt - startedAt) : undefined,
    streaming,
    error: runNotice?.status === "failed" ? messages.map((message) => message.error).findLast(Boolean) : undefined,
    runNotice,
  };
}

export function groupConversationTurns(messages: TimelineMessage[]): TimelineMessage[] {
  const result: TimelineMessage[] = [];
  let assistantRun: TimelineMessage[] = [];
  let turnStartedAt: number | undefined;

  const flushAssistantRun = () => {
    const merged = mergeAssistantRun(assistantRun, turnStartedAt);
    if (merged && (
      merged.text
      || merged.executionSteps?.length
      || merged.images?.length
      || merged.runNotice
    )) result.push(merged);
    assistantRun = [];
  };

  for (let index = 0; index < messages.length; index += 1) {
    const message = messages[index];
    if (message.role === "assistant") {
      const runNotice = inferredRunNotice(messages, index);
      assistantRun.push(runNotice ? { ...message, runNotice } : message);
      if (runNotice) flushAssistantRun();
      continue;
    }
    flushAssistantRun();
    result.push(message);
    if (message.role === "user") turnStartedAt = message.timestampMs;
    if (message.role === "system") turnStartedAt = undefined;
  }
  flushAssistantRun();
  return result;
}
