export type ScheduledTaskFrequency = "once" | "hourly" | "daily" | "weekdays" | "weekly";
export type ScheduledTaskRunStatus = "started" | "failed";
export const scheduledTaskThinkingLevels = ["off", "minimal", "low", "medium", "high", "xhigh", "max"] as const;

export interface ScheduledTask {
  id: string;
  name: string;
  prompt: string;
  workspaceId: string;
  modelProvider?: string;
  modelId?: string;
  modelName?: string;
  thinkingLevel?: string;
  frequency: ScheduledTaskFrequency;
  time?: string;
  weekday?: number;
  runAt?: string;
  enabled: boolean;
  nextRunAt?: string;
  lastRunAt?: string;
  lastThreadId?: string;
  lastStatus?: ScheduledTaskRunStatus;
  lastError?: string;
  createdAt: string;
  updatedAt: string;
}

export type ScheduledTaskDraft = Pick<ScheduledTask, "name" | "prompt" | "workspaceId" | "frequency"> & {
  modelProvider: string;
  modelId: string;
  modelName: string;
  thinkingLevel: string;
  time: string;
  weekday: number;
  runAt: string;
};

function clockParts(value: string): [number, number] | undefined {
  const match = /^(\d{2}):(\d{2})$/.exec(value);
  if (!match) return undefined;
  const hour = Number(match[1]);
  const minute = Number(match[2]);
  return hour <= 23 && minute <= 59 ? [hour, minute] : undefined;
}

export function nextScheduledRun(task: Pick<ScheduledTask, "frequency" | "time" | "weekday" | "runAt">, after = new Date()): string {
  if (!Number.isFinite(after.getTime())) return "";
  if (task.frequency === "once") {
    const runAt = new Date(task.runAt || "");
    return Number.isFinite(runAt.getTime()) && runAt > after ? runAt.toISOString() : "";
  }

  const clock = clockParts(task.time || "");
  if (!clock) return "";
  const [hour, minute] = clock;
  const candidate = new Date(after);
  candidate.setSeconds(0, 0);

  if (task.frequency === "hourly") {
    candidate.setMinutes(minute);
    if (candidate <= after) candidate.setHours(candidate.getHours() + 1);
    return candidate.toISOString();
  }

  candidate.setHours(hour, minute, 0, 0);
  if (candidate <= after) candidate.setDate(candidate.getDate() + 1);
  if (task.frequency === "daily") return candidate.toISOString();

  if (task.frequency === "weekdays") {
    while (candidate.getDay() === 0 || candidate.getDay() === 6) candidate.setDate(candidate.getDate() + 1);
    return candidate.toISOString();
  }

  const weekday = Number.isInteger(task.weekday) && task.weekday! >= 0 && task.weekday! <= 6 ? task.weekday! : 1;
  const days = (weekday - candidate.getDay() + 7) % 7;
  candidate.setDate(candidate.getDate() + days);
  if (candidate <= after) candidate.setDate(candidate.getDate() + 7);
  return candidate.toISOString();
}

export function toLocalDateTimeInput(value: string | Date): string {
  const date = value instanceof Date ? value : new Date(value);
  if (!Number.isFinite(date.getTime())) return "";
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function localDateTimeToISO(value: string): string {
  const date = new Date(value);
  return Number.isFinite(date.getTime()) ? date.toISOString() : "";
}

export function newScheduledTaskDraft(workspaceId = "", now = new Date()): ScheduledTaskDraft {
  const runAt = new Date(now);
  runAt.setMinutes(runAt.getMinutes() + 30, 0, 0);
  return {
    name: "",
    prompt: "",
    workspaceId,
    modelProvider: "",
    modelId: "",
    modelName: "",
    thinkingLevel: "",
    frequency: "daily",
    time: "09:00",
    weekday: 1,
    runAt: toLocalDateTimeInput(runAt),
  };
}
