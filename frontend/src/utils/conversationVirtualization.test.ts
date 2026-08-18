import { describe, expect, it } from "vitest";
import { CONVERSATION_VIRTUALIZATION_THRESHOLD, estimateMessageSize, shouldVirtualizeMessages } from "./conversationVirtualization";

function message(text = "short") {
  return { role: "assistant" as const, text, thinking: "", images: [] as unknown[], tools: [] };
}

describe("conversation virtualization", () => {
  it("virtualizes every long transcript", () => {
    expect(shouldVirtualizeMessages(Array.from({ length: CONVERSATION_VIRTUALIZATION_THRESHOLD }, () => message()))).toBe(false);
    expect(shouldVirtualizeMessages(Array.from({ length: CONVERSATION_VIRTUALIZATION_THRESHOLD + 1 }, () => message()))).toBe(true);

    const prose = Array.from({ length: CONVERSATION_VIRTUALIZATION_THRESHOLD + 1 }, () => message());
    prose[20] = message("x".repeat(2001));
    expect(shouldVirtualizeMessages(prose)).toBe(true);

    const attachments = Array.from({ length: CONVERSATION_VIRTUALIZATION_THRESHOLD + 1 }, () => message());
    attachments[20].images = [{}];
    expect(shouldVirtualizeMessages(attachments)).toBe(true);
  });

  it("keeps estimated row sizes bounded", () => {
    expect(estimateMessageSize(message())).toBeGreaterThanOrEqual(44);
    expect(estimateMessageSize(message("x".repeat(20_000)))).toBe(420);
  });

  it("reserves space for assistant request status", () => {
    expect(estimateMessageSize({ ...message("result"), runNotice: { status: "failed" } }))
      .toBe(estimateMessageSize(message("result")) + 48);
  });

  it("uses the collapsed divider height for compaction markers", () => {
    expect(estimateMessageSize({
      ...message(""),
      role: "system",
      compaction: { summary: "x".repeat(20_000), tokensBefore: 241443 },
    })).toBe(48);
  });

  it("does not reserve collapsed reasoning height", () => {
    expect(estimateMessageSize({ ...message(""), thinking: "x".repeat(20_000) })).toBe(22);
    expect(estimateMessageSize({ ...message("result"), thinking: "x".repeat(20_000) })).toBe(44);
  });
});
