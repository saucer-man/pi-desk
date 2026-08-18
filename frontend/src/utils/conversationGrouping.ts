import type { ExecutionStep, TimelineMessage } from "../stores/app";

function executionSteps(messages: TimelineMessage[], finalIndex: number): ExecutionStep[] {
  const steps: ExecutionStep[] = [];
  for (let index = 0; index < messages.length; index += 1) {
    const message = messages[index];
    if (message.thinking) steps.push({ id: `${message.id}-thinking`, kind: "thinking", text: message.thinking });
    if (message.tools.length) steps.push({ id: `${message.id}-tools`, kind: "tools", tools: message.tools });
    if (index !== finalIndex && message.text) steps.push({ id: `${message.id}-message`, kind: "message", text: message.text });
  }
  return steps;
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
    error: messages.map((message) => message.error).findLast(Boolean),
  };
}

export function groupConversationTurns(messages: TimelineMessage[]): TimelineMessage[] {
  const result: TimelineMessage[] = [];
  let assistantRun: TimelineMessage[] = [];
  let turnStartedAt: number | undefined;

  const flushAssistantRun = () => {
    const merged = mergeAssistantRun(assistantRun, turnStartedAt);
    if (merged) result.push(merged);
    assistantRun = [];
  };

  for (const message of messages) {
    if (message.role === "assistant") {
      assistantRun.push(message);
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
