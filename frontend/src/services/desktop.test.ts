import { beforeEach, describe, expect, it, vi } from "vitest";
import { notifyDesktop } from "./desktop";

const notificationMocks = vi.hoisted(() => ({
  checkAuthorization: vi.fn(),
  requestAuthorization: vi.fn(),
  sendNotification: vi.fn(),
}));

vi.mock("../../bindings/github.com/wailsapp/wails/v3/pkg/services/notifications", () => ({
  NotificationService: {
    CheckNotificationAuthorization: notificationMocks.checkAuthorization,
    RequestNotificationAuthorization: notificationMocks.requestAuthorization,
    SendNotification: notificationMocks.sendNotification,
  },
}));

describe("desktop notifications", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    notificationMocks.checkAuthorization.mockResolvedValue(true);
    notificationMocks.requestAuthorization.mockResolvedValue(true);
    notificationMocks.sendNotification.mockResolvedValue(undefined);
  });

  it("uses the native Pi Desk notification service and compacts toast text", async () => {
    const body = `  Review\n\n${"capture ".repeat(60)}  `;

    await expect(notifyDesktop("  Task\ncompleted  ", body)).resolves.toBe(true);

    expect(notificationMocks.requestAuthorization).not.toHaveBeenCalled();
    expect(notificationMocks.sendNotification).toHaveBeenCalledOnce();
    expect(notificationMocks.sendNotification).toHaveBeenCalledWith(expect.objectContaining({
      id: expect.stringMatching(/^pi-desk-task-\d+-\d+$/),
      title: "Task completed",
      interruptionLevel: "active",
    }));
    const options = notificationMocks.sendNotification.mock.calls[0][0] as { body: string };
    expect(options.body).not.toMatch(/\s{2,}/);
    expect(Array.from(options.body)).toHaveLength(240);
    expect(options.body.endsWith("…")).toBe(true);
  });

  it("requests native authorization when needed and skips denied notifications", async () => {
    notificationMocks.checkAuthorization.mockResolvedValue(false);
    notificationMocks.requestAuthorization.mockResolvedValue(false);

    await expect(notifyDesktop("Task completed", "Build")).resolves.toBe(false);

    expect(notificationMocks.requestAuthorization).toHaveBeenCalledOnce();
    expect(notificationMocks.sendNotification).not.toHaveBeenCalled();
  });
});
