import { PiExtensionService } from "../../bindings/pi-desk/internal/appservice";
import type { PiDeskTodoInstallResult, PiExtensionSnapshot } from "../../bindings/pi-desk/internal/domain";

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
};

export type {
  PiDeskTodoExtensionStatus,
  PiDeskTodoInstallResult,
  PiExtensionSnapshot,
  PiExtensionSummary,
} from "../../bindings/pi-desk/internal/domain";
