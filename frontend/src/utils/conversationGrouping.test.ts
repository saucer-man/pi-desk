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

  it("marks only the currently streaming reasoning step active", () => {
    const grouped = groupConversationTurns([
      message({ id: "user", role: "user", text: "Inspect", timestampMs: 1000 }),
      message({ id: "thinking", role: "assistant", thinking: "Reading", streaming: true, activeExecution: "thinking" }),
    ]);

    expect(grouped[1].executionSteps).toEqual([{
      id: "thinking-thinking", kind: "thinking", text: "Reading", active: true,
    }]);
  });

  it("drops a recovered retry that produced no standalone content", () => {
    const grouped = groupConversationTurns([
      message({ id: "user", role: "user", text: "Retry", timestampMs: 1000 }),
      message({
        id: "recovered", role: "assistant", error: "Rate exceeded",
        runNotice: { status: "recovered", error: "Rate exceeded" },
      }),
    ]);

    expect(grouped.map((item) => item.id)).toEqual(["user"]);
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

  it("removes a recovered provider failure from the rendered conversation", () => {
    const grouped = groupConversationTurns([
      message({ id: "user", role: "user", text: "Upload it", timestampMs: 1000 }),
      message({ id: "failed", role: "assistant", error: "OpenAI API error (520)", timestampMs: 2000 }),
      message({ id: "success", role: "assistant", text: "Upload complete", timestampMs: 4000 }),
    ]);

    expect(grouped).toHaveLength(2);
    expect(grouped[1]).toMatchObject({ id: "success", text: "Upload complete" });
    expect(grouped[1].runNotice).toBeUndefined();
    expect(grouped[1].error).toBeUndefined();
  });

  it("removes consecutive failed attempts after the response recovers", () => {
    const grouped = groupConversationTurns([
      message({ id: "user", role: "user", text: "Upload it", timestampMs: 1000 }),
      message({ id: "failed-1", role: "assistant", error: "Request timed out.", timestampMs: 2000 }),
      message({ id: "failed-2", role: "assistant", error: "OpenAI API error (520)", timestampMs: 3000 }),
      message({ id: "success", role: "assistant", text: "Upload complete", timestampMs: 4000 }),
    ]);

    expect(grouped.map((item) => item.id)).toEqual(["user", "success"]);
    expect(grouped[1].runNotice).toBeUndefined();
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
