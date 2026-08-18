import { describe, expect, it } from "vitest";
import type { TimelineMessage } from "../stores/app";
import { groupConversationTurns } from "./conversationGrouping";

function message(partial: Partial<TimelineMessage> & Pick<TimelineMessage, "id" | "role">): TimelineMessage {
  return { text: "", thinking: "", timestamp: "08/12 14:00", streaming: false, tools: [], ...partial };
}

describe("groupConversationTurns", () => {
  it("folds consecutive assistant execution into the final answer", () => {
    const grouped = groupConversationTurns([
      message({ id: "user", role: "user", text: "Change it", timestampMs: 1000 }),
      message({ id: "work", role: "assistant", thinking: "Inspect", timestampMs: 1500, tools: [{ id: "edit", name: "edit", output: "ok", status: "complete" }] }),
      message({ id: "final", entryId: "entry-final", role: "assistant", text: "Done", thinking: "Verify", timestampMs: 4000 }),
    ]);

    expect(grouped).toHaveLength(2);
    expect(grouped[1]).toMatchObject({ id: "final", entryId: "entry-final", text: "Done", durationMs: 3000, thinkingCount: 2 });
    expect(grouped[1].executionSteps?.map((step) => step.kind)).toEqual(["thinking", "tools", "thinking"]);
  });

  it("keeps compaction markers in place and separates assistant runs", () => {
    const grouped = groupConversationTurns([
      message({ id: "user", role: "user", timestampMs: 1000 }),
      message({ id: "before", role: "assistant", text: "Before compaction", timestampMs: 2000 }),
      message({
        id: "compact", role: "system", timestampMs: 3000,
        compaction: { summary: "Condensed context", tokensBefore: 120000 },
      }),
      message({ id: "after", role: "assistant", text: "After compaction", timestampMs: 4000 }),
    ]);

    expect(grouped.map((item) => item.id)).toEqual(["user", "before", "compact", "after"]);
    expect(grouped[2].compaction).toEqual({ summary: "Condensed context", tokensBefore: 120000 });
  });

  it("keeps a recovered provider failure before the later successful response", () => {
    const grouped = groupConversationTurns([
      message({ id: "user", role: "user", text: "Upload it", timestampMs: 1000 }),
      message({ id: "failed", role: "assistant", error: "OpenAI API error (520)", timestampMs: 2000 }),
      message({ id: "success", role: "assistant", text: "Upload complete", timestampMs: 4000 }),
    ]);

    expect(grouped).toHaveLength(3);
    expect(grouped[1]).toMatchObject({
      id: "failed",
      error: "OpenAI API error (520)",
      runNotice: { status: "recovered", error: "OpenAI API error (520)" },
    });
    expect(grouped[2]).toMatchObject({ id: "success", text: "Upload complete" });
    expect(grouped[2].runNotice).toBeUndefined();
    expect(grouped[2].error).toBeUndefined();
  });

  it("keeps consecutive failed attempts in order before the recovered response", () => {
    const grouped = groupConversationTurns([
      message({ id: "user", role: "user", text: "Upload it", timestampMs: 1000 }),
      message({ id: "failed-1", role: "assistant", error: "Request timed out.", timestampMs: 2000 }),
      message({ id: "failed-2", role: "assistant", error: "OpenAI API error (520)", timestampMs: 3000 }),
      message({ id: "success", role: "assistant", text: "Upload complete", timestampMs: 4000 }),
    ]);

    expect(grouped.map((item) => item.id)).toEqual(["user", "failed-1", "failed-2", "success"]);
    expect(grouped[1].runNotice).toEqual({ status: "retried", error: "Request timed out." });
    expect(grouped[2].runNotice).toEqual({ status: "recovered", error: "OpenAI API error (520)" });
    expect(grouped[3].runNotice).toBeUndefined();
  });

  it("keeps the latest unresolved provider failure at the bottom of the assistant run", () => {
    const grouped = groupConversationTurns([
      message({ id: "user", role: "user", text: "Continue", timestampMs: 1000 }),
      message({ id: "partial", role: "assistant", text: "Working", timestampMs: 2000 }),
      message({ id: "failed", role: "assistant", error: "Request timed out.", timestampMs: 3000 }),
    ]);

    expect(grouped[1]).toMatchObject({
      id: "partial",
      text: "Working",
      timestampMs: 3000,
      runNotice: { status: "failed", error: "Request timed out." },
    });
  });

  it("does not merge assistant runs across user messages", () => {
    const grouped = groupConversationTurns([
      message({ id: "u1", role: "user" }),
      message({ id: "a1", role: "assistant", text: "One" }),
      message({ id: "u2", role: "user" }),
      message({ id: "a2", role: "assistant", text: "Two" }),
    ]);
    expect(grouped.map((item) => item.id)).toEqual(["u1", "a1", "u2", "a2"]);
  });
});
