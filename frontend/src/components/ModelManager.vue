<script setup lang="ts">
import { ui } from "../ui/classes";
import {
  CheckCircle2,
  CircleDollarSign,
  Download,
  FlaskConical,
  Plus,
  RefreshCw,
  Save,
  Sparkles,
  Trash2,
  XCircle,
} from "lucide-vue-next";
import { computed, onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { tr } from "../i18n";
import { modelConfigService, type DiscoveredModel, type ManagedModel, type ManagedModelProvider, type ModelConfigSnapshot, type ModelQuotaResult, type ModelTestResult } from "../services/modelconfig";
import { useAppStore } from "../stores/app";
import ModelQuotaDialog from "./ModelQuotaDialog.vue";
import ModelTestDialog from "./ModelTestDialog.vue";

const apiOptions = ["openai-completions", "openai-responses", "anthropic-messages", "google-generative-ai"] as const;
const thinkingLevels = ["off", "minimal", "low", "medium", "high", "xhigh", "max"] as const;
const newProviderValue = "__new_provider__";
const defaultProviderUserAgent = "codex_cli_rs/0.146.0 (Windows 11.0.26100; x86_64) Terminal";
let headerEntrySequence = 0;

type ProviderHeaderEntry = {
  id: number;
  name: string;
  value: string;
};
const appStore = useAppStore();
const snapshot = ref<ModelConfigSnapshot>();
const loading = ref(true);
const saving = ref(false);
const testing = ref(false);
const quotaLoading = ref(false);
const discovering = ref(false);
const loadError = ref("");
const formError = ref("");
const discoveryError = ref("");
const discoveryEndpoint = ref("");
const discoveredModels = ref<DiscoveredModel[]>([]);
const notice = ref("");
const testDialogOpen = ref(false);
const testPrompt = ref(tr("settings.modelTestDefaultPrompt"));
const testResult = ref<ModelTestResult>();
const quotaDialogOpen = ref(false);
const quotaResult = ref<ModelQuotaResult>();
const selectedKey = ref("");
const deleteArmed = ref(false);
const savedFingerprint = ref("");
const providerMenu = ref({ open: false, providerId: "", x: 0, y: 0, confirming: false });

type ModelDefaults = {
  contextWindow?: number;
  maxTokens?: number;
  reasoning?: boolean;
  imageInput?: boolean;
  source: "provider" | "catalog";
};

const knownModelDefaults: Array<{ pattern: RegExp; defaults: Omit<ModelDefaults, "source"> }> = [
  {
    pattern: /^gpt-5\.6-sol$/i,
    defaults: { contextWindow: 272000, maxTokens: 128000, reasoning: true, imageInput: true },
  },
];

const editor = reactive({
  originalProviderId: "",
  originalModelId: "",
  providerChoice: newProviderValue,
  providerId: "",
  baseUrl: "",
  api: "",
  apiKey: "",
  headers: [] as ProviderHeaderEntry[],
  providerCompatJson: "",
  modelId: "",
  modelName: "",
  contextWindow: 128000,
  maxTokens: 16384,
  reasoning: false,
  imageInput: false,
  thinkingLevelMapJson: "",
  modelCompatJson: "",
});

const providers = computed(() => snapshot.value?.providers ?? []);
const hasSelection = computed(() => selectedKey.value === "new" || Boolean(editor.originalModelId || editor.modelId || editor.providerId));
const isExisting = computed(() => Boolean(editor.originalProviderId && editor.originalModelId));
const fingerprint = computed(() => JSON.stringify({
  originalProviderId: editor.originalProviderId,
  originalModelId: editor.originalModelId,
  providerId: editor.providerId,
  baseUrl: editor.baseUrl,
  api: editor.api,
  apiKey: editor.apiKey,
  headers: editor.headers.map(({ name, value }) => ({ name, value })),
  providerCompatJson: editor.providerCompatJson,
  modelId: editor.modelId,
  modelName: editor.modelName,
  contextWindow: editor.contextWindow,
  maxTokens: editor.maxTokens,
  reasoning: editor.reasoning,
  imageInput: editor.imageInput,
  thinkingLevelMapJson: editor.thinkingLevelMapJson,
  modelCompatJson: editor.modelCompatJson,
}));
const dirty = computed(() => fingerprint.value !== savedFingerprint.value);
const hasRequiredProbeFields = computed(() => Boolean(editor.baseUrl.trim() && editor.api && editor.modelId.trim()));
const hasRequiredQuotaFields = computed(() => Boolean(editor.baseUrl.trim() && editor.api));
const availableDiscoveredModels = computed(() => discoveredModels.value.filter((model) => !findModel(editor.providerId.trim(), model.id)));
const modelDefaults = computed(() => defaultsForModel(editor.modelId));
const usesBuiltInOpenAIProvider = computed(() => editor.providerId.trim().toLocaleLowerCase() === "openai");
const testModelName = computed(() => editor.modelName.trim() || editor.modelId.trim());

function defaultsForModel(modelId: string, discovered?: DiscoveredModel): ModelDefaults | undefined {
  const contextWindow = discovered?.contextWindow ?? 0;
  const maxTokens = discovered?.maxTokens ?? 0;
  if (discovered && (contextWindow > 0 || maxTokens > 0 || discovered.reasoning || discovered.imageInput)) {
    return {
      contextWindow: contextWindow || undefined,
      maxTokens: maxTokens || undefined,
      reasoning: discovered.reasoning || undefined,
      imageInput: discovered.imageInput || undefined,
      source: "provider",
    };
  }
  const known = knownModelDefaults.find((candidate) => candidate.pattern.test(modelId.trim()));
  return known ? { ...known.defaults, source: "catalog" } : undefined;
}

function providerById(id: string): ManagedModelProvider | undefined {
  return providers.value.find((provider) => provider.id === id);
}

function findModel(providerId: string, modelId: string): { provider: ManagedModelProvider; model: ManagedModel } | undefined {
  const provider = providerById(providerId);
  const model = provider?.models?.find((candidate) => candidate.id === modelId);
  return provider && model ? { provider, model } : undefined;
}

function newHeaderEntry(name = "", value = ""): ProviderHeaderEntry {
  headerEntrySequence += 1;
  return { id: headerEntrySequence, name, value };
}

function headerEntries(headers?: Record<string, string | undefined> | null): ProviderHeaderEntry[] {
  return Object.entries(headers ?? {})
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([name, value]) => newHeaderEntry(name, value ?? ""));
}

function setProviderFields(provider?: ManagedModelProvider, useDefaultHeaders = false) {
  editor.baseUrl = provider?.baseUrl ?? "";
  editor.api = provider?.api ?? "";
  editor.apiKey = provider?.apiKey ?? "";
  editor.headers = provider ? headerEntries(provider.headers) : useDefaultHeaders ? [newHeaderEntry("User-Agent", defaultProviderUserAgent)] : [];
  editor.providerCompatJson = provider?.compatJson ?? "";
}

function resetDiscovery() {
  discoveryError.value = "";
  discoveryEndpoint.value = "";
  discoveredModels.value = [];
}

function selectModel(provider: ManagedModelProvider, model: ManagedModel) {
  selectedKey.value = `${provider.id}/${model.id}`;
  deleteArmed.value = false;
  notice.value = "";
  formError.value = "";
  testDialogOpen.value = false;
  testResult.value = undefined;
  quotaDialogOpen.value = false;
  quotaResult.value = undefined;
  resetDiscovery();
  editor.originalProviderId = provider.id;
  editor.originalModelId = model.id;
  editor.providerChoice = provider.id;
  editor.providerId = provider.id;
  setProviderFields(provider);
  editor.modelId = model.id;
  editor.modelName = model.name ?? "";
  editor.contextWindow = model.contextWindow;
  editor.maxTokens = model.maxTokens;
  editor.reasoning = model.reasoning;
  editor.imageInput = model.imageInput;
  editor.thinkingLevelMapJson = model.thinkingLevelMapJson ?? "";
  editor.modelCompatJson = model.compatJson ?? "";
  savedFingerprint.value = fingerprint.value;
}

function startNewModel() {
  selectedKey.value = "new";
  deleteArmed.value = false;
  notice.value = "";
  formError.value = "";
  testDialogOpen.value = false;
  testResult.value = undefined;
  quotaDialogOpen.value = false;
  quotaResult.value = undefined;
  resetDiscovery();
  editor.originalProviderId = "";
  editor.originalModelId = "";
  const provider = providers.value[0];
  editor.providerChoice = provider?.id ?? newProviderValue;
  editor.providerId = provider?.id ?? "";
  setProviderFields(provider, !provider);
  editor.modelId = "";
  editor.modelName = "";
  editor.contextWindow = 128000;
  editor.maxTokens = 16384;
  editor.reasoning = false;
  editor.imageInput = false;
  editor.thinkingLevelMapJson = "";
  editor.modelCompatJson = "";
  savedFingerprint.value = fingerprint.value;
}

function chooseProvider() {
  resetDiscovery();
  if (editor.providerChoice === newProviderValue) {
    editor.providerId = "";
    setProviderFields(undefined, true);
    return;
  }
  editor.providerId = editor.providerChoice;
  setProviderFields(providerById(editor.providerChoice));
}

async function loadConfig(preferredKey = selectedKey.value) {
  loading.value = true;
  loadError.value = "";
  try {
    snapshot.value = await modelConfigService.get();
    const [providerId, ...modelParts] = preferredKey.split("/");
    const selected = providerId && modelParts.length ? findModel(providerId, modelParts.join("/")) : undefined;
    const firstProvider = providers.value[0];
    const firstModel = firstProvider?.models?.[0];
    if (selected) selectModel(selected.provider, selected.model);
    else if (firstProvider && firstModel) selectModel(firstProvider, firstModel);
    else startNewModel();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    loading.value = false;
  }
}

function validateObjectJSON(label: string, value: string): string | undefined {
  if (!value.trim()) return undefined;
  try {
    const parsed: unknown = JSON.parse(value);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return tr("settings.modelJSONError", { field: label });
  } catch {
    return tr("settings.modelJSONError", { field: label });
  }
  return undefined;
}

function validateThinkingLevelMapJSON(value: string): string | undefined {
  if (!value.trim()) return undefined;
  let parsed: unknown;
  try {
    parsed = JSON.parse(value);
  } catch {
    return tr("settings.thinkingLevelMapError");
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return tr("settings.thinkingLevelMapError");
  for (const [level, mapped] of Object.entries(parsed)) {
    if (!(thinkingLevels as readonly string[]).includes(level) || (mapped !== null && (typeof mapped !== "string" || !mapped.trim()))) {
      return tr("settings.thinkingLevelMapError");
    }
  }
  return undefined;
}

function validateProviderHeaders(): string | undefined {
  const seen = new Set<string>();
  for (const header of editor.headers) {
    const name = header.name.trim();
    if (!name) {
      if (header.value) return tr("settings.providerHeaderNameRequired");
      continue;
    }
    if (!/^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/.test(name)) return tr("settings.providerHeaderInvalidName", { name });
    if (/\r|\n/.test(header.value)) return tr("settings.providerHeaderInvalidValue", { name });
    const normalized = name.toLocaleLowerCase();
    if (seen.has(normalized)) return tr("settings.providerHeaderDuplicate", { name });
    seen.add(normalized);
  }
  return undefined;
}

function headersForRequest(): Record<string, string> {
  return Object.fromEntries(editor.headers
    .filter((header) => header.name.trim())
    .map((header) => [header.name.trim(), header.value]));
}

function addProviderHeader() {
  editor.headers.push(newHeaderEntry());
}

function removeProviderHeader(id: number) {
  editor.headers = editor.headers.filter((header) => header.id !== id);
}

function validateForm(): boolean {
  formError.value = "";
  if (!editor.providerId.trim() || !editor.modelId.trim()) formError.value = tr("settings.modelIDsRequired");
  else if (/\s/.test(editor.providerId) || /\s/.test(editor.modelId)) formError.value = tr("settings.modelIDsNoSpaces");
  else if (editor.contextWindow < 1 || editor.maxTokens < 1 || editor.maxTokens > editor.contextWindow) formError.value = tr("settings.modelTokenError");
  else formError.value = validateProviderHeaders()
    ?? validateThinkingLevelMapJSON(editor.thinkingLevelMapJson)
    ?? validateObjectJSON(tr("settings.providerCompatibility"), editor.providerCompatJson)
    ?? validateObjectJSON(tr("settings.modelCompatibility"), editor.modelCompatJson)
    ?? "";
  return !formError.value;
}

async function refreshModelChoices() {
  await appStore.refreshConfiguredModels();
  if (appStore.activeThread?.started) await appStore.refreshModels(appStore.activeThread.id);
}

async function saveModel(showNotice = true): Promise<boolean> {
  if (!validateForm() || saving.value) return false;
  saving.value = true;
  notice.value = "";
  testResult.value = undefined;
  try {
    snapshot.value = await modelConfigService.upsert({
      originalProviderId: editor.originalProviderId,
      originalModelId: editor.originalModelId,
      providerId: editor.providerId,
      baseUrl: editor.baseUrl,
      api: editor.api,
      apiKey: editor.apiKey,
      headers: headersForRequest(),
      providerCompatJson: editor.providerCompatJson,
      modelId: editor.modelId,
      modelName: editor.modelName,
      contextWindow: editor.contextWindow,
      maxTokens: editor.maxTokens,
      reasoning: editor.reasoning,
      imageInput: editor.imageInput,
      thinkingLevelMapJson: editor.thinkingLevelMapJson,
      modelCompatJson: editor.modelCompatJson,
    });
    const selected = findModel(editor.providerId, editor.modelId);
    if (selected) selectModel(selected.provider, selected.model);
    if (showNotice) notice.value = tr("settings.modelSaved");
    await refreshModelChoices();
    return true;
  } catch (cause) {
    formError.value = cause instanceof Error ? cause.message : String(cause);
    return false;
  } finally {
    saving.value = false;
  }
}

function openModelTest() {
  formError.value = "";
  if (!editor.baseUrl.trim() || !editor.api || !editor.modelId.trim()) {
    formError.value = tr("settings.modelProbeFieldsRequired");
    return;
  }
  testResult.value = undefined;
  testDialogOpen.value = true;
}

function closeModelTest() {
  if (!testing.value) testDialogOpen.value = false;
}

async function testModel() {
  if (testing.value || !testPrompt.value.trim()) return;
  testing.value = true;
  notice.value = "";
  testResult.value = undefined;
  try {
    testResult.value = await modelConfigService.test({
      baseUrl: editor.baseUrl,
      api: editor.api,
      apiKey: editor.apiKey,
      headers: headersForRequest(),
      modelId: editor.modelId,
      prompt: testPrompt.value,
    });
  } catch (cause) {
    testResult.value = { ok: false, latencyMs: 0, error: cause instanceof Error ? cause.message : String(cause) };
  } finally {
    testing.value = false;
  }
}

function closeAccountQuota() {
  if (!quotaLoading.value) quotaDialogOpen.value = false;
}

async function queryAccountQuota() {
  if (quotaLoading.value) return;
  formError.value = "";
  if (!hasRequiredQuotaFields.value) {
    formError.value = tr("settings.modelDiscoveryFieldsRequired");
    return;
  }
  quotaDialogOpen.value = true;
  quotaLoading.value = true;
  quotaResult.value = undefined;
  try {
    quotaResult.value = await modelConfigService.quota({
      baseUrl: editor.baseUrl,
      api: editor.api,
      apiKey: editor.apiKey,
      headers: headersForRequest(),
    });
  } catch (cause) {
    quotaResult.value = { ok: false, latencyMs: 0, error: cause instanceof Error ? cause.message : String(cause) };
  } finally {
    quotaLoading.value = false;
  }
}

async function discoverModels() {
  if (discovering.value) return;
  discoveryError.value = "";
  discoveryEndpoint.value = "";
  discoveredModels.value = [];
  if (!editor.baseUrl.trim() || !editor.api) {
    discoveryError.value = tr("settings.modelDiscoveryFieldsRequired");
    return;
  }
  discovering.value = true;
  try {
    const result = await modelConfigService.discover({
      baseUrl: editor.baseUrl,
      api: editor.api,
      apiKey: editor.apiKey,
      headers: headersForRequest(),
    });
    discoveredModels.value = result.models ?? [];
    discoveryEndpoint.value = result.endpoint;
  } catch (cause) {
    discoveryError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    discovering.value = false;
  }
}

function chooseDiscoveredModel(event: Event) {
  const id = (event.target as HTMLSelectElement).value;
  if (!id) return;
  const model = discoveredModels.value.find((candidate) => candidate.id === id);
  editor.modelId = id;
  if (model?.name) editor.modelName = model.name;
  applyModelDefaults(model);
}

function applyModelDefaults(discovered?: DiscoveredModel) {
  const defaults = defaultsForModel(editor.modelId, discovered);
  if (!defaults) {
    notice.value = tr("settings.modelDefaultsUnavailable");
    return;
  }
  if (defaults.contextWindow) editor.contextWindow = defaults.contextWindow;
  if (defaults.maxTokens) editor.maxTokens = defaults.maxTokens;
  if (defaults.reasoning !== undefined) editor.reasoning = defaults.reasoning;
  if (defaults.imageInput !== undefined) editor.imageInput = defaults.imageInput;
  notice.value = tr(defaults.source === "provider" ? "settings.modelDefaultsFromProvider" : "settings.modelDefaultsApplied");
}

function discoveredModelConfig(model: DiscoveredModel): ManagedModel {
  const defaults = defaultsForModel(model.id, model);
  return {
    id: model.id,
    name: model.name ?? "",
    contextWindow: defaults?.contextWindow ?? editor.contextWindow,
    maxTokens: defaults?.maxTokens ?? editor.maxTokens,
    reasoning: defaults?.reasoning ?? editor.reasoning,
    imageInput: defaults?.imageInput ?? editor.imageInput,
    thinkingLevelMapJson: selectedKey.value === "new" ? editor.thinkingLevelMapJson : "",
    compatJson: selectedKey.value === "new" ? editor.modelCompatJson.trim() : "",
  };
}

async function addDiscoveredModels() {
  if (saving.value || availableDiscoveredModels.value.length === 0) return;
  if (!validateForm()) return;
  saving.value = true;
  formError.value = "";
  notice.value = "";
  try {
    const models = availableDiscoveredModels.value.map(discoveredModelConfig);
    snapshot.value = await modelConfigService.addModels({
      originalProviderId: editor.originalProviderId,
      providerId: editor.providerId,
      baseUrl: editor.baseUrl,
      api: editor.api,
      apiKey: editor.apiKey,
      headers: headersForRequest(),
      providerCompatJson: editor.providerCompatJson,
      models,
    });
    const selected = findModel(editor.providerId, models[models.length - 1].id);
    if (selected) selectModel(selected.provider, selected.model);
    notice.value = tr("settings.modelsAdded", { count: models.length });
    resetDiscovery();
    await refreshModelChoices();
  } catch (cause) {
    formError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    saving.value = false;
  }
}

async function deleteModel() {
  if (!isExisting.value || saving.value) return;
  if (!deleteArmed.value) {
    deleteArmed.value = true;
    window.setTimeout(() => { deleteArmed.value = false; }, 5000);
    return;
  }
  saving.value = true;
  formError.value = "";
  try {
    snapshot.value = await modelConfigService.delete({ providerId: editor.originalProviderId, modelId: editor.originalModelId });
    const firstProvider = providers.value[0];
    const firstModel = firstProvider?.models?.[0];
    if (firstProvider && firstModel) selectModel(firstProvider, firstModel);
    else startNewModel();
    notice.value = tr("settings.modelDeleted");
    await refreshModelChoices();
  } catch (cause) {
    formError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    saving.value = false;
    deleteArmed.value = false;
  }
}

function closeProviderMenu() {
  providerMenu.value.open = false;
  providerMenu.value.confirming = false;
}

function openProviderMenu(event: MouseEvent, provider: ManagedModelProvider) {
  const width = 220;
  const height = 116;
  providerMenu.value = {
    open: true,
    providerId: provider.id,
    x: Math.max(8, Math.min(event.clientX, window.innerWidth - width - 8)),
    y: Math.max(8, Math.min(event.clientY, window.innerHeight - height - 8)),
    confirming: false,
  };
}

function showProviderDeleteConfirmation() {
  providerMenu.value.confirming = true;
}

async function deleteProvider(provider: ManagedModelProvider) {
  if (saving.value || !providerMenu.value.confirming || providerMenu.value.providerId !== provider.id) return;
  saving.value = true;
  formError.value = "";
  try {
    snapshot.value = await modelConfigService.deleteProvider(provider.id);
    const firstProvider = providers.value[0];
    const firstModel = firstProvider?.models?.[0];
    if (firstProvider && firstModel) selectModel(firstProvider, firstModel);
    else startNewModel();
    notice.value = tr("settings.deleteProvider");
    closeProviderMenu();
    await refreshModelChoices();
  } catch (cause) {
    formError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    saving.value = false;
  }
}

function onProviderMenuPointerDown(event: PointerEvent) {
  const target = event.target;
  if (!(target instanceof Element) || !target.closest(".provider-context-menu")) closeProviderMenu();
}

function onProviderMenuKeydown(event: KeyboardEvent) {
  if (event.key === "Escape") closeProviderMenu();
}

onMounted(() => {
  void loadConfig();
  document.addEventListener("pointerdown", onProviderMenuPointerDown);
  document.addEventListener("keydown", onProviderMenuKeydown);
});
onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", onProviderMenuPointerDown);
  document.removeEventListener("keydown", onProviderMenuKeydown);
});
</script>

<template>
  <div class="settings-content model-config-content" :class="ui.settingsContent">
    <div v-if="loading" class="settings-empty" :class="ui.empty"><RefreshCw :size="18" class="is-spinning" /><span>{{ tr("settings.loadingModelsConfig") }}</span></div>
    <div v-else-if="loadError" class="settings-empty is-error" :class="ui.empty"><XCircle :size="18" /><span>{{ loadError }}</span></div>
    <div v-else class="model-manager-layout" :class="ui.managerLayout">
      <aside class="model-config-list" :class="ui.managerList" :aria-label="tr('settings.configuredModels')">
        <button class="model-config-add-row" type="button" :class="[ui.listItem, { 'is-active': selectedKey === 'new' }]" @click="startNewModel">
          <Plus :size="14" /><span>{{ tr("settings.addModel") }}</span>
        </button>
        <section v-for="provider in providers" :key="provider.id" class="model-config-provider">
          <header @contextmenu.prevent="openProviderMenu($event, provider)"><strong>{{ provider.id }}</strong><span>{{ provider.models?.length ?? 0 }}</span></header>
          <button
            v-for="model in provider.models ?? []"
            :key="`${provider.id}/${model.id}`"
            type="button"
            :class="{ 'is-active': selectedKey === `${provider.id}/${model.id}` }"
            @click="selectModel(provider, model)"
          >
            <span><strong>{{ model.name || model.id }}</strong><small>{{ model.id }}</small></span>
            <CheckCircle2 v-if="selectedKey === `${provider.id}/${model.id}`" :size="13" />
          </button>
        </section>
        <div v-if="providers.length === 0" class="model-config-list-empty">{{ tr("settings.noManagedModels") }}</div>
      </aside>
      <div
        v-if="providerMenu.open"
        class="provider-context-menu"
        :class="ui.menuSurface"
        :style="{ left: `${providerMenu.x}px`, top: `${providerMenu.y}px` }"
        role="menu"
        @click.stop
      >
        <button v-if="!providerMenu.confirming" type="button" role="menuitem" @click="showProviderDeleteConfirmation"><Trash2 :size="14" />{{ tr("settings.deleteProvider") }}</button>
        <template v-else>
          <p>{{ tr("settings.confirmDeleteProvider") }}</p>
          <div class="provider-context-actions">
            <button type="button" class="is-danger" role="menuitem" :disabled="saving" @click="void deleteProvider(providerById(providerMenu.providerId)!)"><Trash2 :size="14" />{{ tr("settings.deleteProvider") }}</button>
            <button type="button" role="menuitem" :disabled="saving" @click="closeProviderMenu">{{ tr("common.cancel") }}</button>
          </div>
        </template>
      </div>

      <form v-if="hasSelection" class="model-editor" :class="ui.managerEditor" @submit.prevent="void saveModel()">
        <div class="model-editor-title">
          <div><strong>{{ isExisting ? (editor.modelName || editor.modelId) : tr("settings.newModel") }}</strong><small>{{ tr("settings.modelConfigScope") }}</small></div>
          <span v-if="dirty" class="model-dirty">{{ tr("settings.unsaved") }}</span>
        </div>

        <div class="model-form-grid" :class="ui.formGrid">
          <label v-if="!isExisting" class="model-field" :class="ui.field">
            <span>{{ tr("settings.provider") }}</span>
            <select :class="ui.select" v-model="editor.providerChoice" @change="chooseProvider">
              <option v-for="provider in providers" :key="provider.id" :value="provider.id">{{ provider.id }}</option>
              <option :value="newProviderValue">{{ tr("settings.newProvider") }}</option>
            </select>
          </label>
          <label class="model-field" :class="ui.field">
            <span>{{ tr("settings.providerName") }}</span>
            <input :class="ui.input" v-model="editor.providerId" data-testid="provider-id" spellcheck="false" placeholder="openai-custom" />
            <small v-if="usesBuiltInOpenAIProvider" class="is-warning" role="status">{{ tr("settings.openAIProviderMergeWarning") }}</small>
            <small v-if="isExisting">{{ tr("settings.providerRenameHelp") }}</small>
          </label>
          <label class="model-field model-field-wide" :class="ui.field">
            <span>{{ tr("settings.baseURL") }}</span>
            <input :class="ui.input" v-model="editor.baseUrl" type="url" spellcheck="false" placeholder="https://api.example.com/v1" />
            <small>{{ tr("settings.baseURLHelp") }}</small>
          </label>
          <label class="model-field" :class="ui.field">
            <span>{{ tr("settings.apiType") }}</span>
            <select :class="ui.select" v-model="editor.api">
              <option value="">{{ tr("settings.inheritProvider") }}</option>
              <option v-for="api in apiOptions" :key="api" :value="api">{{ api }}</option>
            </select>
          </label>
          <label class="model-field model-field-wide" :class="ui.field">
            <span>{{ tr("settings.apiKey") }}</span>
            <input :class="ui.input" v-model="editor.apiKey" type="text" spellcheck="false" autocomplete="off" placeholder="sk-... / $API_KEY" />
            <small>{{ tr("settings.apiKeyHelp") }}</small>
          </label>
        </div>

        <div class="model-form-separator"><span>{{ tr("settings.providerRequestHeaders") }}</span></div>
        <section class="provider-header-editor">
          <header>
            <div>
              <strong>{{ tr("settings.providerRequestHeaders") }}</strong>
              <small>{{ tr("settings.providerRequestHeadersHelp") }}</small>
            </div>
            <button class="model-header-add" type="button" @click="addProviderHeader">
              <Plus :size="13" />{{ tr("settings.addProviderHeader") }}
            </button>
          </header>
          <div v-if="editor.headers.length" class="provider-header-list">
            <div v-for="header in editor.headers" :key="header.id" class="provider-header-row" data-testid="provider-header-row">
              <input :class="ui.input"
                v-model="header.name"
                data-testid="provider-header-name"
                spellcheck="false"
                :aria-label="tr('settings.providerHeaderName')"
                :placeholder="tr('settings.providerHeaderNamePlaceholder')"
              />
              <input
                v-model="header.value"
                data-testid="provider-header-value"
                spellcheck="false"
                autocomplete="off"
                :aria-label="tr('settings.providerHeaderValue')"
                :placeholder="tr('settings.providerHeaderValuePlaceholder')"
              />
              <button class="icon-button" :class="ui.iconButton" type="button" :title="tr('settings.removeProviderHeader')" @click="removeProviderHeader(header.id)">
                <Trash2 :size="14" />
              </button>
            </div>
          </div>
          <small v-else class="provider-header-empty">{{ tr("settings.noProviderHeaders") }}</small>
        </section>

        <div class="model-form-separator"><span>{{ tr("settings.modelDetails") }}</span></div>
        <div class="model-form-grid" :class="ui.formGrid">
          <label class="model-field" :class="ui.field">
            <span class="model-field-heading">
              <span>{{ tr("settings.modelID") }}</span>
              <span class="model-field-heading-actions">
                <button
                  type="button"
                  :aria-label="tr('settings.applyModelDefaults')"
                  :title="tr('settings.applyModelDefaults')"
                  :disabled="!editor.modelId.trim()"
                  @click="applyModelDefaults()"
                >
                  <Sparkles :size="12" />{{ tr("settings.applyModelDefaults") }}
                </button>
                <button
                  type="button"
                  :aria-label="tr('settings.fetchModels')"
                  :title="tr('settings.fetchModels')"
                  :disabled="discovering || !editor.baseUrl.trim() || !editor.api"
                  @click="void discoverModels()"
                >
                  <Download :size="12" />{{ discovering ? tr("settings.fetchingModels") : tr("settings.fetchModels") }}
                </button>
              </span>
            </span>
            <input :class="ui.input" v-model="editor.modelId" spellcheck="false" placeholder="gpt-5" />
            <select :class="ui.select" v-if="discoveredModels.length" :value="editor.modelId" @change="chooseDiscoveredModel">
              <option value="">{{ tr("settings.chooseFetchedModel", { count: discoveredModels.length }) }}</option>
              <option v-for="model in discoveredModels" :key="model.id" :value="model.id">{{ model.name ? `${model.name} (${model.id})` : model.id }}</option>
            </select>
            <div v-if="discoveredModels.length" class="model-discovery-actions">
              <button type="button" :disabled="saving || availableDiscoveredModels.length === 0" @click="void addDiscoveredModels()">
                <Plus :size="12" />{{ tr("settings.addFetchedModels", { count: availableDiscoveredModels.length }) }}
              </button>
              <small>{{ tr("settings.modelsAvailableToAdd", { count: availableDiscoveredModels.length }) }}</small>
            </div>
            <small v-if="discoveryEndpoint">{{ tr("settings.modelsFetched", { count: discoveredModels.length }) }} · {{ discoveryEndpoint }}</small>
            <small v-if="modelDefaults">{{ modelDefaults.source === "provider" ? tr("settings.modelDefaultsFromProvider") : tr("settings.modelDefaultsAvailable") }}</small>
            <small v-if="discoveryError" class="is-error">{{ discoveryError }}</small>
          </label>
          <label class="model-field" :class="ui.field">
            <span>{{ tr("settings.displayName") }}</span>
            <input :class="ui.input" v-model="editor.modelName" placeholder="GPT 5" />
          </label>
          <label class="model-field" :class="ui.field">
            <span>{{ tr("settings.contextWindow") }}</span>
            <input :class="ui.input" v-model.number="editor.contextWindow" type="number" min="1" max="10000000" step="1" />
          </label>
          <label class="model-field" :class="ui.field">
            <span>{{ tr("settings.maxTokens") }}</span>
            <input :class="ui.input" v-model.number="editor.maxTokens" type="number" min="1" max="10000000" step="1" />
          </label>
          <label class="model-check-row">
            <span><strong>{{ tr("settings.supportsReasoning") }}</strong><small>{{ tr("settings.supportsReasoningHelp") }}</small></span>
            <input v-model="editor.reasoning" type="checkbox" />
          </label>
          <label class="model-check-row">
            <span><strong>{{ tr("settings.supportsImages") }}</strong><small>{{ tr("settings.supportsImagesHelp") }}</small></span>
            <input v-model="editor.imageInput" type="checkbox" />
          </label>
          <label class="model-field model-field-wide" :class="ui.field">
            <span>{{ tr("settings.thinkingLevelMap") }}</span>
            <textarea :class="ui.textarea"
              v-model="editor.thinkingLevelMapJson"
              data-testid="thinking-level-map"
              spellcheck="false"
              placeholder='{"xhigh":"xhigh","max":"max"}'
            />
            <small>{{ tr("settings.thinkingLevelMapHelp") }}</small>
          </label>
        </div>

        <details class="model-advanced">
          <summary>{{ tr("settings.advancedCompatibility") }}</summary>
          <p>{{ tr("settings.advancedCompatibilityHelp") }}</p>
          <div class="model-compat-help">
            <span>{{ tr("settings.compatibilityHelp") }}</span>
            <pre>&#123;
{{ tr("settings.compatibilityExample") }}
&#125;</pre>
          </div>
          <label class="model-field" :class="ui.field">
            <span>{{ tr("settings.providerCompatibility") }}</span>
            <textarea :class="ui.textarea" v-model="editor.providerCompatJson" spellcheck="false" placeholder="{}" />
          </label>
          <label class="model-field" :class="ui.field">
            <span>{{ tr("settings.modelCompatibility") }}</span>
            <textarea :class="ui.textarea" v-model="editor.modelCompatJson" spellcheck="false" placeholder="{}" />
          </label>
        </details>

        <p v-if="formError" class="form-error">{{ formError }}</p>
        <p v-if="notice" class="model-result is-success"><CheckCircle2 :size="14" />{{ notice }}</p>

        <footer class="model-editor-actions">
          <button v-if="isExisting" class="text-button danger-button" :class="ui.buttonDanger" type="button" :disabled="saving || testing || quotaLoading" @click="void deleteModel()">
            <Trash2 :size="14" />{{ deleteArmed ? tr("settings.confirmDeleteModel") : tr("settings.deleteModel") }}
          </button>
          <span />
          <button class="text-button" :class="ui.button" type="button" :disabled="saving || testing || quotaLoading || !hasRequiredQuotaFields" @click="void queryAccountQuota()">
            <CircleDollarSign :size="14" />{{ quotaLoading ? tr("settings.loadingAccountQuota") : tr("settings.accountQuota") }}
          </button>
          <button class="text-button" :class="ui.button" type="button" :disabled="saving || testing || quotaLoading || !hasRequiredProbeFields" :title="tr('settings.modelTestCost')" @click="openModelTest">
            <FlaskConical :size="14" />{{ testing ? tr("settings.testingModel") : tr("settings.testModel") }}
          </button>
          <button class="text-button primary-button" :class="ui.buttonPrimary" type="submit" :disabled="saving || testing || quotaLoading || !dirty">
            <Save :size="14" />{{ saving ? tr("settings.savingModel") : tr("settings.saveModel") }}
          </button>
        </footer>
      </form>
    </div>
    <ModelQuotaDialog
      v-if="quotaDialogOpen"
      :provider-name="editor.providerId"
      :loading="quotaLoading"
      :result="quotaResult"
      @close="closeAccountQuota"
      @retry="void queryAccountQuota()"
    />
    <ModelTestDialog
      v-if="testDialogOpen"
      v-model:prompt="testPrompt"
      :model-name="testModelName"
      :testing="testing"
      :result="testResult"
      @close="closeModelTest"
      @submit="void testModel()"
    />
  </div>
</template>
