import { describe, expect, it } from "vitest";
import { subagentSummaryFromResult } from "./subagentTasks";

describe("subagentSummaryFromResult", () => {
  it("returns undefined for non-subagent results", () => {
    expect(subagentSummaryFromResult(undefined)).toBeUndefined();
    expect(subagentSummaryFromResult({ content: [{ type: "text", text: "hi" }] })).toBeUndefined();
    expect(subagentSummaryFromResult({ details: { mode: "single", results: [] } })).toBeUndefined();
    expect(subagentSummaryFromResult({ details: { mode: "single", results: [null, 7] } })).toBeUndefined();
  });

  it("keeps streaming tasks running until a terminal stopReason arrives", () => {
    const streaming = subagentSummaryFromResult({
      details: { mode: "single", results: [{ agent: "scout", exitCode: 0, stopReason: "toolUse", usage: { input: 10, output: 5, cost: 0.001 } }] },
    });
    expect(streaming?.tasks[0].status).toBe("running");

    const placeholder = subagentSummaryFromResult({
      details: { mode: "parallel", results: [{ agent: "scout", exitCode: -1 }] },
    });
    expect(placeholder?.tasks[0].status).toBe("running");

    const noReasonYet = subagentSummaryFromResult({
      details: { mode: "single", results: [{ agent: "scout", exitCode: 0 }] },
    });
    expect(noReasonYet?.tasks[0].status).toBe("running");
  });

  it("settles finished and failed tasks with usage and model metadata", () => {
    const summary = subagentSummaryFromResult({
      details: {
        mode: "parallel",
        results: [
          { agent: "scout", exitCode: 0, stopReason: "end", model: "z-ai/glm-5.3", usage: { input: 1200, output: 345, cost: 0.0042 } },
          { agent: "writer", exitCode: 1, stopReason: "end" },
          { agent: "runner", exitCode: 0, stopReason: "aborted" },
        ],
      },
    });
    expect(summary?.mode).toBe("parallel");
    expect(summary?.tasks[0]).toMatchObject({ agent: "scout", status: "ok", model: "z-ai/glm-5.3", inputTokens: 1200, outputTokens: 345, cost: 0.0042 });
    expect(summary?.tasks[1]).toMatchObject({ agent: "writer", status: "error" });
    expect(summary?.tasks[2]).toMatchObject({ agent: "runner", status: "error" });
  });

  it("defaults missing mode and drops unknown field shapes", () => {
    const summary = subagentSummaryFromResult({
      details: { results: [{ agent: "scout", exitCode: 0, stopReason: "end", step: 2, model: 42, usage: { input: "x" } }] },
    });
    expect(summary?.mode).toBe("single");
    expect(summary?.tasks[0]).toMatchObject({ agent: "scout", status: "ok", step: 2, inputTokens: 0 });
    expect(summary?.tasks[0].model).toBeUndefined();
  });
});
