import { PiExtensionService } from "../../bindings/pi-desk/internal/appservice";
import type { PiDeskBrowserExtensionStatus, PiDeskComputerUseExtensionStatus, PiDeskGoalExtensionStatus, PiDeskSubagentsExtensionStatus, PiDeskTodoInstallResult, PiExtensionSnapshot, PiPackageRequest, PiPackageSnapshot, SetPiPackageEnabledRequest } from "../../bindings/pi-desk/internal/domain";

export const piExtensionService = {
  list(): Promise<PiExtensionSnapshot> {
    return PiExtensionService.ListExtensions();
  },
  installTodo(): Promise<PiDeskTodoInstallResult> {
    return PiExtensionService.InstallPiDeskTodo();
  },
  removeTodo(): Promise<void> {
    return PiExtensionService.RemovePiDeskTodo();
  },
  installGoal(): Promise<PiDeskGoalExtensionStatus> {
    return PiExtensionService.InstallPiDeskGoal();
  },
  removeGoal(): Promise<void> {
    return PiExtensionService.RemovePiDeskGoal();
  },
  installComputerUse(): Promise<PiDeskComputerUseExtensionStatus> {
    return PiExtensionService.InstallPiDeskComputerUse();
  },
  removeComputerUse(): Promise<void> {
    return PiExtensionService.RemovePiDeskComputerUse();
  },
  installSubagents(): Promise<PiDeskSubagentsExtensionStatus> {
    return PiExtensionService.InstallPiDeskSubagents();
  },
  removeSubagents(): Promise<void> {
    return PiExtensionService.RemovePiDeskSubagents();
  },
  installBrowser(): Promise<PiDeskBrowserExtensionStatus> {
    return PiExtensionService.InstallPiDeskBrowser();
  },
  removeBrowser(): Promise<void> {
    return PiExtensionService.RemovePiDeskBrowser();
  },
  listPackages(workspacePath = ""): Promise<PiPackageSnapshot> {
    return PiExtensionService.ListPackages({ workspacePath });
  },
  installPackage(request: PiPackageRequest) {
    return PiExtensionService.InstallPackage(request);
  },
  updatePackage(request: PiPackageRequest) {
    return PiExtensionService.UpdatePackage(request);
  },
  removePackage(request: PiPackageRequest) {
    return PiExtensionService.RemovePackage(request);
  },
  setPackageEnabled(request: SetPiPackageEnabledRequest): Promise<void> {
    return PiExtensionService.SetPackageEnabled(request);
  },
};

export type {
  PiDeskBrowserExtensionStatus,
  PiDeskComputerUseExtensionStatus,
  PiDeskGoalExtensionStatus,
  PiDeskSubagentsExtensionStatus,
  PiDeskTodoExtensionStatus,
  PiDeskTodoInstallResult,
  PiExtensionSnapshot,
  PiExtensionSummary,
  PiPackageSnapshot,
  PiPackageSummary,
} from "../../bindings/pi-desk/internal/domain";
