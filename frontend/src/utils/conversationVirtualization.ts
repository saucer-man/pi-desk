export const CONVERSATION_VIRTUALIZATION_THRESHOLD = 80;

interface VirtualizationCandidate {
  role: "user" | "assistant" | "system";
  text: string;
  thinking: string;
  images?: unknown[];
  tools: unknown[];
  executionSteps?: Array<{ kind: "thinking" | "tools" | "message"; tools?: unknown[] }>;
  runNotice?: { status: "retrying" | "retried" | "recovered" | "failed" };
  changes?: { files: unknown[] };
  compaction?: { summary: string; tokensBefore?: number };
}

export function shouldVirtualizeMessages(messages: VirtualizationCandidate[]): boolean {
  return messages.length > CONVERSATION_VIRTUALIZATION_THRESHOLD;
}

export function estimateMessageSize(message: VirtualizationCandidate): number {
  if (message.compaction) return 48;
  if (message.changes) return 240;
  const textLines = message.text.length ? Math.max(1, Math.ceil(message.text.length / 90)) : 0;
  const groupedSections = message.executionSteps?.reduce((count, step) => count + (step.kind === "tools" ? step.tools?.length ?? 0 : 1), 0) ?? 0;
  const compactSections = Math.max(groupedSections, (message.thinking ? 1 : 0) + message.tools.length);
  const noticeSize = message.runNotice ? 48 : 0;
  const baseSize = message.role === "system"
    ? 62
    : message.role === "user"
      ? 86
      : compactSections > 0
        ? 0
        : 28;
  return Math.min(420, Math.max(22, baseSize + textLines * 22 + compactSections * 22 + noticeSize));
}
