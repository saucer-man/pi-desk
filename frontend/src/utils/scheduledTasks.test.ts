import { describe, expect, it } from "vitest";
import { localDateTimeToISO, nextScheduledRun, newScheduledTaskDraft, toLocalDateTimeInput } from "./scheduledTasks";

describe("scheduled task recurrence", () => {
  it("schedules hourly work on the next matching minute", () => {
    const next = new Date(nextScheduledRun({ frequency: "hourly", time: "00:15" }, new Date(2026, 8, 3, 10, 20)));
    expect([next.getHours(), next.getMinutes()]).toEqual([11, 15]);
  });

  it("skips weekends for weekday schedules", () => {
    const next = new Date(nextScheduledRun({ frequency: "weekdays", time: "09:00" }, new Date(2026, 8, 4, 18, 0)));
    expect([next.getDay(), next.getHours(), next.getMinutes()]).toEqual([1, 9, 0]);
  });

  it("advances weekly schedules instead of returning the current run", () => {
    const next = new Date(nextScheduledRun({ frequency: "weekly", time: "09:00", weekday: 4 }, new Date(2026, 8, 3, 9, 0)));
    expect(next.getDate()).toBe(10);
    expect(next.getDay()).toBe(4);
  });

  it("disables an expired one-time schedule", () => {
    expect(nextScheduledRun({ frequency: "once", runAt: new Date(2026, 8, 3, 8, 0).toISOString() }, new Date(2026, 8, 3, 9, 0))).toBe("");
  });

  it("round-trips local date-time inputs", () => {
    const local = toLocalDateTimeInput(new Date(2026, 8, 3, 14, 25));
    expect(toLocalDateTimeInput(localDateTimeToISO(local))).toBe(local);
    expect(newScheduledTaskDraft("workspace-1", new Date(2026, 8, 3, 14, 0)).workspaceId).toBe("workspace-1");
  });
});
