import { ModelConfigService } from "../../bindings/pi-desk/internal/appservice";
import type {
  AddModelsConfigRequest,
  DeleteModelConfigRequest,
  DiscoverModelsRequest,
  ModelDiscoveryResult,
  ModelConfigSnapshot,
  ModelQuotaRequest,
  ModelQuotaResult,
  ModelTestResult,
  SelectableModel,
  TestModelConfigRequest,
  UpsertModelConfigRequest,
} from "../../bindings/pi-desk/internal/domain";

export const modelConfigService = {
  async selectable(): Promise<SelectableModel[]> {
    return (await ModelConfigService.GetConfiguredModels()) ?? [];
  },
  get(): Promise<ModelConfigSnapshot> {
    return ModelConfigService.GetModelsConfig();
  },
  upsert(request: UpsertModelConfigRequest): Promise<ModelConfigSnapshot> {
    return ModelConfigService.UpsertModel(request);
  },
  addModels(request: AddModelsConfigRequest): Promise<ModelConfigSnapshot> {
    return ModelConfigService.AddModels(request);
  },
  delete(request: DeleteModelConfigRequest): Promise<ModelConfigSnapshot> {
    return ModelConfigService.DeleteModel(request);
  },
  deleteProvider(providerId: string): Promise<ModelConfigSnapshot> {
    return ModelConfigService.DeleteProvider({ providerId });
  },
  discover(request: DiscoverModelsRequest): Promise<ModelDiscoveryResult> {
    return ModelConfigService.DiscoverModels(request);
  },
  test(request: TestModelConfigRequest): Promise<ModelTestResult> {
    return ModelConfigService.TestModel(request);
  },
  quota(request: ModelQuotaRequest): Promise<ModelQuotaResult> {
    return ModelConfigService.GetAccountQuota(request);
  },
};

export type {
  AddModelsConfigRequest,
  DiscoveredModel,
  ManagedModel,
  ManagedModelProvider,
  ModelConfigSnapshot,
  ModelDiscoveryResult,
  ModelQuotaResult,
  ModelTestResult,
  SelectableModel,
  UpsertModelConfigRequest,
} from "../../bindings/pi-desk/internal/domain";
