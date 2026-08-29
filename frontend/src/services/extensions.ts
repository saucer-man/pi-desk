import { PiExtensionService } from "../../bindings/pi-desk/internal/appservice";
import type { PiDeskTodoInstallResult, PiExtensionSnapshot, PiPackageRequest, PiPackageSnapshot, SetPiPackageEnabledRequest } from "../../bindings/pi-desk/internal/domain";

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
  PiDeskTodoExtensionStatus,
  PiDeskTodoInstallResult,
  PiExtensionSnapshot,
  PiExtensionSummary,
  PiPackageSnapshot,
  PiPackageSummary,
} from "../../bindings/pi-desk/internal/domain";
