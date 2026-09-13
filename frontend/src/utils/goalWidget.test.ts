import { describe, expect, it } from "vitest";
import { parsePiDeskGoalWidget } from "./goalWidget";

describe("Pi Desk goal widget projection", () => {
  it("parses the bundled extension format with a token budget", () => {
    const projection = parsePiDeskGoalWidget({
      key: "pi-desk-goal",
      lines: ["-- Goal --", "status:active", "iteration:3", "tokens:12000/50000", "Ship the release", "and verify CI"],
    });

    expect(projection).toEqual({
      status: "active",
      iteration: 3,
      tokensUsed: 12000,
      tokenBudget: 50000,
      text: "Ship the release\nand verify CI",
    });
  });

  it("parses goals without a token budget and tolerates unknown lines in the objective", () => {
    expect(parsePiDeskGoalWidget({
      key: "PI-DESK-GOAL",
      lines: ["status:budget_limited", "iteration:0", "tokens:0", "Fix the flaky test"],
    })).toEqual({ status: "budget_limited", iteration: 0, tokensUsed: 0, text: "Fix the flaky test" });
  });

  it("does not claim generic widgets or malformed goal widgets", () => {
    expect(parsePiDeskGoalWidget({ key: "plan", lines: ["status:active", "iteration:1", "tokens:0", "x"] })).toBeUndefined();
    expect(parsePiDeskGoalWidget({ key: "pi-desk-goal", lines: ["-- Goal --", "status:running", "iteration:1", "tokens:0", "x"] })).toBeUndefined();
    expect(parsePiDeskGoalWidget({ key: "pi-desk-goal", lines: ["status:active", "iteration:1", "tokens:0"] })).toBeUndefined();
    expect(parsePiDeskGoalWidget({ key: "pi-desk-goal", lines: ["status:active", "iteration:1", "tokens:600/500", "x"] })).toBeUndefined();
  });
});
