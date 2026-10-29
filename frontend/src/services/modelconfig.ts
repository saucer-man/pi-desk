import { ModelConfigService } from "../../bindings/pi-desk/internal/appservice";
import { Browser, Events } from "@wailsio/runtime";
import type { CancellablePromise } from "@wailsio/runtime";
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
  ModelAuthRequest, ModelAuthReply, ModelCatalogSnapshot, DefaultModelRequest,
} from "../../bindings/pi-desk/internal/domain";

export const modelConfigService = {
  catalog(refresh = false): Promise<ModelCatalogSnapshot> { return ModelConfigService.GetModelCatalog(refresh); },
  login(request: ModelAuthRequest) { return ModelConfigService.LoginModelProvider(request); },
  replyLogin(request: ModelAuthReply) { return ModelConfigService.ReplyModelLogin(request); },
  saveAPIKey(request: ModelAuthRequest) { return ModelConfigService.SaveModelAPIKey(request); },
  logout(providerId: string) { return ModelConfigService.LogoutModelProvider(providerId); },
  setDefault(request: DefaultModelRequest) { return ModelConfigService.SetDefaultModel(request); },
  openAuthURL(url: string) {
    if (new URL(url).protocol !== "https:") return Promise.reject(new Error("Invalid authorization URL"));
    return Browser.OpenURL(url);
  },
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
  test(request: TestModelConfigRequest): CancellablePromise<ModelTestResult> {
    return ModelConfigService.TestModel(request);
  },
  quota(request: ModelQuotaRequest): Promise<ModelQuotaResult> {
    return ModelConfigService.GetAccountQuota(request);
  },
};

export function onModelAuthEvent(callback: (event: ModelAuthEvent) => void): () => void {
  return Events.On("models:auth", event => callback(event.data as ModelAuthEvent));
}

export interface ModelAuthEvent {
  operationId: string;
  type: string;
  url?: string;
  verificationUri?: string;
  userCode?: string;
  message?: string;
  instructions?: string;
  promptId?: number;
  promptType?: string;
  placeholder?: string;
  options?: Array<{ id: string; label: string }>;
}

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
  ModelAuthProvider, ModelCatalogSnapshot,
} from "../../bindings/pi-desk/internal/domain";
