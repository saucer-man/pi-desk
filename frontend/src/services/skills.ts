import { ManagedSkillService } from "../../bindings/pi-desk/internal/appservice";
import type {
  CreateManagedSkillRequest,
  ListManagedSkillsRequest,
  ManagedSkill,
  ManagedSkillRequest,
  ManagedSkillSnapshot,
  UpdateManagedSkillRequest,
} from "../../bindings/pi-desk/internal/domain";

export const managedSkillService = {
  list(request: ListManagedSkillsRequest): Promise<ManagedSkillSnapshot> {
    return ManagedSkillService.ListManagedSkills(request);
  },
  get(request: ManagedSkillRequest): Promise<ManagedSkill> {
    return ManagedSkillService.GetManagedSkill(request);
  },
  create(request: CreateManagedSkillRequest): Promise<ManagedSkill> {
    return ManagedSkillService.CreateManagedSkill(request);
  },
  update(request: UpdateManagedSkillRequest): Promise<ManagedSkill> {
    return ManagedSkillService.UpdateManagedSkill(request);
  },
  delete(request: ManagedSkillRequest): Promise<void> {
    return ManagedSkillService.DeleteManagedSkill(request);
  },
};

export type {
  CreateManagedSkillRequest,
  ManagedSkill,
  ManagedSkillRequest,
  ManagedSkillSnapshot,
  ManagedSkillSummary,
  UpdateManagedSkillRequest,
} from "../../bindings/pi-desk/internal/domain";
