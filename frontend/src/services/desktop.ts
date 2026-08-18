import { NotificationService } from "../../bindings/github.com/wailsapp/wails/v3/pkg/services/notifications";
import { DesktopService, PiMaintenanceService } from "../../bindings/pi-desk/internal/appservice";
import type { BootstrapState, PiMaintenanceAction, PiMaintenanceResult, PiRuntimeStatus, UpdateCheckResult } from "../../bindings/pi-desk/internal/domain";

let notificationSequence = 0;

function compactNotificationText(value: string, limit: number): string {
  const normalized = value.replace(/\s+/g, " ").trim();
  const characters = Array.from(normalized);
  if (characters.length <= limit) return normalized;
  return `${characters.slice(0, Math.max(1, limit - 1)).join("")}…`;
}

export async function getBootstrapState(): Promise<BootstrapState> {
  return DesktopService.GetBootstrapState();
}

export async function toggleDebugMode(): Promise<boolean> {
  return DesktopService.ToggleDebugMode();
}

export async function checkRuntime(): Promise<PiRuntimeStatus> {
  return DesktopService.CheckRuntime();
}

export async function checkForUpdates(): Promise<UpdateCheckResult> {
	return DesktopService.CheckForUpdates();
}

export async function maintainPi(action: PiMaintenanceAction): Promise<PiMaintenanceResult> {
	return PiMaintenanceService.MaintainPi({ action });
}

export async function notifyDesktop(title: string, body: string): Promise<boolean> {
  try {
    let authorized = await NotificationService.CheckNotificationAuthorization();
    if (!authorized) authorized = await NotificationService.RequestNotificationAuthorization();
    if (!authorized) return false;

    const normalizedTitle = compactNotificationText(title, 80) || "Pi Desk";
    const normalizedBody = compactNotificationText(body, 240);
    notificationSequence += 1;
    await NotificationService.SendNotification({
      id: `pi-desk-task-${Date.now()}-${notificationSequence}`,
      title: normalizedTitle,
      body: normalizedBody,
      interruptionLevel: "active",
    });
    return true;
  } catch {
    return false;
  }
}
