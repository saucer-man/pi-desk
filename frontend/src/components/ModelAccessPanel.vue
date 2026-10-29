<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { CheckCircle2, ExternalLink, KeyRound, LogIn, LogOut, RefreshCw, Search, Star, X } from "lucide-vue-next";
import { modelConfigService, onModelAuthEvent, type ModelAuthEvent, type ModelCatalogSnapshot } from "../services/modelconfig";
import { tr } from "../i18n";
import { ui } from "../ui/classes";

const emit = defineEmits<{ changed: [] }>();
const catalog = ref<ModelCatalogSnapshot>();
const providerId = ref("openai");
const query = ref("");
const loading = ref(false);
const busy = ref(false);
const error = ref("");
const notice = ref("");
const apiKey = ref("");
const method = ref("oauth");
const confirmation = ref<"logout" | "key" | "login" | "">("");
const defaultValue = ref("");
const loginOpen = ref(false);
const loginURL = ref("");
const loginMessage = ref("");
const loginPrompt = ref<ModelAuthEvent>();
const promptValue = ref("");
const loginError = ref("");
const loginDialog = ref<HTMLDialogElement>();
let loginRequest: ReturnType<typeof modelConfigService.login> | undefined;
let operationId = "";
let generation = 0;
let alive = true;
let disposeEvents: (() => void) | undefined;

const providers = computed(() => catalog.value?.providers ?? []);
const provider = computed(() => providers.value.find(p => p.id === providerId.value));
const visibleProviders = computed(() => providers.value.filter(p => p.configured || p.id === providerId.value || p.id === "openai"));
const models = computed(() => catalog.value?.models ?? []);
const filteredModels = computed(() => models.value.filter(m => m.provider === providerId.value && `${m.name} ${m.id}`.toLowerCase().includes(query.value.toLowerCase())));
const modelKey = (p: string, id: string) => JSON.stringify([p, id]);
const defaultKey = computed(() => catalog.value?.defaultProvider && catalog.value.defaultModel ? modelKey(catalog.value.defaultProvider, catalog.value.defaultModel) : "");
const defaultModel = computed(() => models.value.find(m => modelKey(m.provider, m.id) === defaultKey.value));
const defaultUnavailable = computed(() => Boolean(defaultKey.value && !defaultModel.value));
const sourceText = computed(() => {
  const source = provider.value?.authSource;
  return source === "stored" ? "auth.json" : source?.startsWith("models_json") ? "models.json" : source === "environment" ? tr("settings.authEnvironment") : source || "—";
});
const messageOf = (cause: unknown) => cause instanceof Error ? cause.message : String(cause);

function applyCatalog(value: ModelCatalogSnapshot) {
  const initial = !catalog.value;
  catalog.value = value;
  if (initial) method.value = provider.value?.authType || (provider.value?.oauth ? "oauth" : "api_key");
  defaultValue.value = value.defaultProvider && value.defaultModel ? modelKey(value.defaultProvider, value.defaultModel) : "";
}

async function load(refresh = false) {
  const token = ++generation;
  loading.value = true;
  error.value = "";
  try {
    const value = await modelConfigService.catalog(refresh);
    if (alive && token === generation) applyCatalog(value);
  } catch (cause) {
    if (alive && token === generation) error.value = messageOf(cause);
  } finally {
    if (alive && token === generation) loading.value = false;
  }
}

async function mutate(operation: () => Promise<ModelCatalogSnapshot>) {
  if (busy.value) return;
  generation++;
  loading.value = false;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const value = await operation();
    if (!alive) return;
    applyCatalog(value);
    confirmation.value = "";
    apiKey.value = "";
    notice.value = tr("settings.modelAccessSaved");
    emit("changed");
  } catch (cause) {
    if (alive) error.value = messageOf(cause);
  } finally {
    if (alive) busy.value = false;
  }
}

async function saveDefault(value = defaultValue.value) {
  const [providerId, modelId] = value ? JSON.parse(value) as [string, string] : ["", ""];
  await mutate(() => modelConfigService.setDefault({ providerId, modelId }));
}

async function openLoginURL() {
  try { await modelConfigService.openAuthURL(loginURL.value); }
  catch (cause) { loginError.value = messageOf(cause); }
}

function handleAuth(event: ModelAuthEvent) {
  if (!alive || !operationId || event.operationId !== operationId) return;
  if (event.type === "auth_url" || event.type === "device_code") {
    loginURL.value = event.url || event.verificationUri || "";
    loginMessage.value = event.instructions || event.userCode || tr("settings.authWaitingBrowser");
    if (loginURL.value) void openLoginURL();
  } else if (event.type === "prompt") {
    loginPrompt.value = event;
    promptValue.value = event.options?.[0]?.id ?? "";
  } else if (event.type === "prompt_cancelled") {
    if (loginPrompt.value?.promptId === event.promptId) { loginPrompt.value = undefined; promptValue.value = ""; }
  } else if (event.message) loginMessage.value = event.message;
}

async function login() {
  generation++;
  loading.value = false;
  confirmation.value = "";
  error.value = "";
  loginError.value = "";
  loginURL.value = "";
  loginPrompt.value = undefined;
  promptValue.value = "";
  loginMessage.value = tr("settings.authPreparing");
  loginOpen.value = true;
  busy.value = true;
  operationId = crypto.randomUUID();
  await nextTick();
  if (!alive || !loginOpen.value) return;
  loginDialog.value?.showModal();
  const request = modelConfigService.login({ operationId, providerId: providerId.value });
  loginRequest = request;
  try {
    const result = await request;
    if (!alive || loginRequest !== request) return;
    applyCatalog(result);
    loginDialog.value?.close();
    loginOpen.value = false;
    notice.value = tr("settings.authCompleted");
    emit("changed");
  } catch (cause) {
    if (alive && loginRequest === request) loginError.value = messageOf(cause);
  } finally {
    if (loginRequest === request) { loginRequest = undefined; operationId = ""; busy.value = false; }
  }
}

function cancelLogin() {
  const request = loginRequest;
  loginRequest = undefined;
  operationId = "";
  loginDialog.value?.close();
  loginOpen.value = false;
  loginURL.value = "";
  loginPrompt.value = undefined;
  promptValue.value = "";
  busy.value = false;
  if (request) void request.cancel();
  if (alive) void load();
}

async function replyLogin() {
  const prompt = loginPrompt.value;
  if (!prompt?.promptId || !operationId || !promptValue.value.trim()) return;
  const value = promptValue.value;
  promptValue.value = "";
  try {
    await modelConfigService.replyLogin({ operationId, promptId: prompt.promptId, value });
    if (loginPrompt.value === prompt) loginPrompt.value = undefined;
  } catch (cause) { loginError.value = messageOf(cause); }
}

function requestLogin() {
  if (provider.value?.stored && provider.value.authType !== "oauth") confirmation.value = "login";
  else void login();
}

function saveKey() {
  if (!apiKey.value.trim()) return;
  if (provider.value?.stored) confirmation.value = "key";
  else void confirmChange("key");
}

async function confirmChange(action = confirmation.value) {
  if (action === "login") { await login(); return; }
  const id = providerId.value;
  if (action === "logout") await mutate(() => modelConfigService.logout(id));
  else if (action === "key") await mutate(() => modelConfigService.saveAPIKey({ providerId: id, apiKey: apiKey.value }));
}

watch(providerId, () => { method.value = provider.value?.authType || (provider.value?.oauth ? "oauth" : "api_key"); apiKey.value = ""; confirmation.value = ""; query.value = ""; error.value = ""; notice.value = ""; });
onMounted(() => { disposeEvents = onModelAuthEvent(handleAuth); void load(); });
onBeforeUnmount(() => { alive = false; generation++; disposeEvents?.(); if (loginRequest) void loginRequest.cancel(); });
</script>

<template>
  <div class="model-access-panel">
    <section class="model-default-bar">
      <Star :size="18" aria-hidden="true" />
      <div class="model-access-grow"><strong>{{ tr("settings.defaultModelTitle") }}</strong><small>{{ tr("settings.defaultModelHelp") }}</small><p v-if="defaultUnavailable" class="is-warning" role="status">{{ tr("settings.defaultModelUnavailable", { model: `${catalog?.defaultProvider}/${catalog?.defaultModel}` }) }}</p></div>
      <label class="model-default-choice"><span class="sr-only">{{ tr("settings.defaultModelTitle") }}</span><select v-model="defaultValue" :class="ui.select" :disabled="loading || busy || !catalog" data-testid="default-model"><option value="">{{ tr("settings.piAutoModel") }}</option><option v-if="defaultUnavailable" :value="defaultKey" disabled>{{ catalog?.defaultProvider }}/{{ catalog?.defaultModel }}</option><option v-for="m in models" :key="modelKey(m.provider, m.id)" :value="modelKey(m.provider, m.id)">{{ m.provider }} / {{ m.name || m.id }}</option></select></label>
      <button type="button" :class="ui.button" :disabled="busy || loading || !catalog || defaultValue === defaultKey" @click="void saveDefault()">{{ tr("common.confirm") }}</button>
    </section>
    <div class="model-access-toolbar"><small>{{ tr("settings.modelAuthShared") }}</small><button type="button" :class="ui.button" :disabled="loading || busy" @click="void load(true)"><RefreshCw :size="14" :class="{ 'is-spinning': loading }" />{{ tr("common.refresh") }}</button></div>
    <p v-if="error" class="form-error" role="alert">{{ error }}</p><p v-if="notice" class="model-result is-success" role="status">{{ notice }}</p><p v-if="catalog?.warning" class="is-warning" role="status">{{ catalog.warning }}</p>
    <div v-if="!catalog && loading" :class="ui.empty"><RefreshCw class="is-spinning" :size="18" />{{ tr("settings.authLoading") }}</div>
    <div v-if="catalog" class="model-access-layout">
      <aside class="model-access-providers" :aria-label="tr('settings.provider')">
        <button v-for="p in visibleProviders" :key="p.id" type="button" :class="[ui.listItem, { 'is-active': providerId === p.id }]" :aria-current="providerId === p.id ? 'true' : undefined" :disabled="busy" @click="providerId = p.id"><KeyRound :size="16" aria-hidden="true" /><span><strong>{{ p.name || p.id }}</strong><small>{{ p.configured ? (p.authType === 'oauth' ? 'OAuth' : 'API Key') : tr('settings.authNotConnected') }}</small></span><CheckCircle2 v-if="p.configured" :size="13" /></button>
        <label class="model-access-add"><span>{{ tr("settings.authOtherProvider") }}</span><select v-model="providerId" :class="ui.select" :disabled="busy"><option v-for="p in providers" :key="p.id" :value="p.id">{{ p.name || p.id }}</option></select></label>
      </aside>
      <section v-if="provider" class="model-access-detail">
        <header class="model-access-heading"><div><h3>{{ provider.name || provider.id }}</h3><small>{{ provider.id }}</small></div><span :class="{ 'is-success': provider.configured }">{{ tr(provider.configured ? "settings.authConnected" : "settings.authNotConnected") }}</span></header>
        <section class="model-auth-box">
          <header><strong>{{ tr("settings.authMethod") }}</strong><div class="model-auth-methods" role="group" :aria-label="tr('settings.authMethod')"><button v-if="provider.oauth" type="button" :class="ui.button" :aria-pressed="method === 'oauth'" :disabled="busy" @click="method = 'oauth'">{{ provider.id === 'openai' ? tr('settings.chatGPTSubscription') : 'OAuth' }}</button><button v-if="provider.apiKey" type="button" :class="ui.button" :aria-pressed="method === 'api_key'" :disabled="busy" @click="method = 'api_key'">API Key</button></div></header>
          <div class="model-auth-content">
            <p v-if="provider.configured" class="model-auth-source">{{ tr("settings.authSource") }}: <code>{{ sourceText }}</code><span v-if="provider.stored"> · {{ provider.authType === 'oauth' ? 'OAuth' : 'API Key' }}</span></p>
            <template v-if="method === 'oauth'"><p>{{ tr(provider.id === 'openai' ? "settings.chatGPTAuthHelp" : "settings.oauthAuthHelp") }}</p><div class="model-auth-actions"><button type="button" :class="ui.button" :disabled="busy" @click="requestLogin"><LogIn :size="14" />{{ tr(provider.stored && provider.authType === 'oauth' ? "settings.authRelogin" : "settings.authLogin") }}</button><button v-if="provider.stored" type="button" :class="ui.buttonDanger" :disabled="busy" @click="confirmation = 'logout'"><LogOut :size="14" />{{ tr("settings.authLogout") }}</button></div><p v-if="provider.id === 'openai'" class="model-auth-usage">{{ tr("settings.chatGPTUsageHelp") }} <button :class="ui.button" type="button" @click="void modelConfigService.openAuthURL('https://chatgpt.com/settings/usage').catch(cause => error = messageOf(cause))">{{ tr("settings.authManageUsage") }}<ExternalLink :size="12" /></button></p></template>
            <form v-else @submit.prevent="saveKey"><label class="model-field"><span>API Key</span><input v-model="apiKey" type="password" :class="ui.input" autocomplete="off" :disabled="busy" :placeholder="tr(provider.stored ? 'settings.authReplaceKey' : 'settings.authEnterKey')" required data-testid="auth-api-key" /></label><small>{{ tr("settings.authKeyHelp") }}</small><div class="model-auth-actions"><button type="submit" :class="ui.button" :disabled="busy || !apiKey.trim()">{{ tr("settings.authSaveKey") }}</button><button v-if="provider.stored" type="button" :class="ui.buttonDanger" :disabled="busy" @click="confirmation = 'logout'">{{ tr("settings.authLogout") }}</button></div></form>
            <div v-if="confirmation" class="model-auth-confirm" role="alert"><p>{{ tr(confirmation === 'logout' ? "settings.authLogoutWarning" : "settings.authReplaceWarning") }}</p><div class="model-auth-actions"><button type="button" :class="ui.buttonDanger" :disabled="busy" @click="void confirmChange()">{{ tr("common.confirm") }}</button><button type="button" :class="ui.button" :disabled="busy" @click="confirmation = ''">{{ tr("common.cancel") }}</button></div></div>
          </div>
        </section>
        <p v-if="provider.warning" class="is-warning" role="status">{{ tr("settings.authCatalogWarning") }} {{ provider.warning }}</p>
        <div class="model-access-heading model-catalog-heading"><strong>{{ tr("settings.authModelCatalog") }} · {{ filteredModels.length }}</strong><label class="model-access-search"><Search :size="14" /><input v-model="query" :class="ui.input" type="search" :aria-label="tr('settings.authSearchModels')" :placeholder="tr('settings.authSearchModels')" /></label></div>
        <div v-for="m in filteredModels" :key="modelKey(m.provider, m.id)" class="model-access-model"><div class="model-access-grow"><strong>{{ m.name || m.id }} <Star v-if="modelKey(m.provider, m.id) === defaultKey" :size="12" :aria-label="tr('settings.defaultModelTitle')" /></strong><small>{{ m.id }} · {{ tr(m.source === 'account' ? 'settings.authAccountCatalog' : 'settings.authPiCatalog') }}</small></div><button type="button" :class="ui.button" :disabled="busy || modelKey(m.provider, m.id) === defaultKey" @click="void saveDefault(modelKey(m.provider, m.id))">{{ tr(modelKey(m.provider, m.id) === defaultKey ? "settings.authIsDefault" : "settings.authSetDefault") }}</button></div>
        <p v-if="filteredModels.length === 0" :class="ui.empty">{{ tr(provider.configured ? "settings.authNoModels" : "settings.authConnectFirst") }}</p>
      </section>
    </div>
    <details v-if="catalog" class="model-access-sources"><summary>{{ tr("settings.authConfigFiles") }}</summary><p><code>{{ catalog.agentDir }}</code></p><p>auth.json · {{ tr("settings.authFilePurpose") }}</p><p>models.json · {{ tr("settings.modelsFilePurpose") }}</p><p>settings.json · defaultProvider / defaultModel</p></details>
    <Teleport to="body"><dialog v-if="loginOpen" ref="loginDialog" class="model-auth-dialog" aria-labelledby="model-auth-title" @cancel.prevent="cancelLogin"><header :class="ui.dialogHeader"><h2 id="model-auth-title">{{ tr("settings.authLogin") }} · {{ provider?.name }}</h2><button type="button" :class="ui.iconButton" :aria-label="tr('common.close')" @click="cancelLogin"><X :size="16" /></button></header><div :class="ui.dialogBody"><p role="status"><RefreshCw v-if="busy" class="is-spinning" :size="15" /> {{ loginMessage }}</p><button v-if="loginURL" type="button" :class="ui.button" @click="void openLoginURL()"><ExternalLink :size="14" />{{ tr("settings.authOpenBrowser") }}</button><form v-if="loginPrompt" class="model-auth-prompt" @submit.prevent="void replyLogin()"><label class="model-field"><span>{{ loginPrompt.message }}</span><select v-if="loginPrompt.promptType === 'select'" v-model="promptValue" :class="ui.select"><option v-for="option in loginPrompt.options" :key="option.id" :value="option.id">{{ option.label }}</option></select><input v-else v-model="promptValue" :class="ui.input" :type="loginPrompt.promptType === 'secret' ? 'password' : 'text'" :placeholder="loginPrompt.placeholder" autocomplete="off" /></label><button type="submit" :class="ui.button" :disabled="!promptValue.trim()">{{ tr("common.confirm") }}</button></form><p v-if="loginError" class="form-error" role="alert">{{ loginError }}</p></div><footer :class="ui.dialogFooter"><button type="button" :class="ui.button" @click="cancelLogin">{{ tr(loginRequest ? "settings.authCancelLogin" : "common.close") }}</button></footer></dialog></Teleport>
  </div>
</template>

<style scoped>
.model-access-panel { container-type: inline-size; flex: 1; min-width: 0; min-height: 0; overflow-y: auto; }
.model-default-bar { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; padding: 16px; border-bottom: 1px solid var(--border); background: var(--bg-app); }
.model-access-grow { flex: 1; min-width: 0; }
.model-access-grow strong, .model-access-grow small { display: block; overflow-wrap: anywhere; }
.model-access-grow strong { font-size: var(--font-size-control); }
.model-access-grow small { color: var(--text-muted); margin-top: 4px; font-size: var(--font-size-label); }
.model-default-choice { flex: 1; min-width: 160px; max-width: 340px; }
.model-default-choice select { width: 100%; min-width: 0; }
.model-access-toolbar, .model-access-heading, .model-auth-box > header { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; }
.model-access-toolbar { padding: 10px 16px; }
.model-access-toolbar small, .model-access-heading small, .model-auth-content > p, .model-auth-content small { color: var(--text-muted); font-size: var(--font-size-label); }
.model-access-panel > .form-error, .model-access-panel > .model-result, .model-access-panel > .is-warning { margin: 8px 16px; }
.model-access-layout { display: grid; grid-template-columns: 200px minmax(0, 1fr); border-top: 1px solid var(--border); min-height: 400px; }
.model-access-providers { padding: 10px 8px; background: var(--bg-app); border-right: 1px solid var(--border); }
.model-access-providers > button { width: 100%; margin-bottom: 4px; }
.model-access-providers > button.is-active { background: var(--bg-hover); color: var(--text); }
.model-access-providers > button span { display: grid; flex: 1; min-width: 0; gap: 3px; }
.model-access-providers strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500; font-size: var(--font-size-control); }
.model-access-providers small { color: var(--text-muted); font-size: var(--font-size-label); }
.model-access-add { display: grid; gap: 7px; margin: 18px 6px 0; font-size: var(--font-size-label); color: var(--text-muted); }
.model-access-add select { min-width: 0; width: 100%; }
.model-access-detail { min-width: 0; padding: 20px; }
.model-access-heading { margin-bottom: 18px; }
.model-access-heading h3 { margin: 0; font-size: var(--font-size-title); }
.model-access-heading > span { color: var(--text-muted); font-size: var(--font-size-label); }
.model-access-heading > span.is-success { color: var(--green); }
.model-auth-box { border: 1px solid var(--border); border-radius: 8px; overflow: hidden; }
.model-auth-box > header { padding: 12px 14px; border-bottom: 1px solid var(--border); font-size: var(--font-size-control); }
.model-auth-methods { display: flex; gap: 4px; flex-wrap: wrap; }
.model-auth-methods button[aria-pressed=true] { border-color: var(--border-strong); background: var(--bg-hover); color: var(--text); }
.model-auth-content { padding: 14px; }
.model-auth-content p { margin: 0 0 10px; }
.model-auth-content .model-field { margin: 0 0 7px; }
.model-auth-source code, .model-access-sources code { font: inherit; font-family: var(--font-mono); overflow-wrap: anywhere; }
.model-auth-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 12px; }
.model-auth-usage { display: flex; align-items: center; gap: 5px; flex-wrap: wrap; margin-top: 12px !important; margin-bottom: 0 !important; }
.model-auth-confirm { margin-top: 14px; padding: 12px; border-top: 1px solid var(--border); color: var(--amber); font-size: var(--font-size-label); }
.model-catalog-heading { margin: 24px 0 10px; font-size: var(--font-size-control); }
.model-access-search { display: flex; align-items: center; gap: 7px; color: var(--text-muted); }
.model-access-search input { min-width: 0; width: 190px; }
.model-access-model { display: flex; align-items: center; gap: 12px; padding: 13px 0; border-top: 1px solid var(--border); }
.model-access-model strong > svg { display: inline-block; vertical-align: -1px; margin-left: 4px; }
.model-access-model button { white-space: nowrap; flex-shrink: 0; }
.model-access-sources { padding: 14px 16px; border-top: 1px solid var(--border); color: var(--text-muted); font-size: var(--font-size-label); }
.model-access-sources summary { cursor: pointer; }
.model-access-sources p { margin: 8px 0 0; }
.is-warning { color: var(--amber); font-size: var(--font-size-label); overflow-wrap: anywhere; }
.model-auth-dialog { width: min(520px, calc(100vw - 32px)); max-height: calc(100dvh - 32px); margin: auto; padding: 0; color: var(--text); background: var(--bg-panel); border: 1px solid var(--border-strong); border-radius: 12px; box-shadow: var(--shadow-dialog); }
.model-auth-dialog::backdrop { background: var(--overlay); }
.model-auth-dialog > div { max-height: calc(100dvh - 180px); }
.model-auth-dialog p { overflow-wrap: anywhere; }
.model-auth-dialog [role=status] { display: flex; align-items: center; gap: 8px; margin-bottom: 16px; }
.model-auth-prompt { display: grid; gap: 12px; margin-top: 20px; }
.model-auth-prompt .model-field { display: grid; gap: 8px; }
.model-auth-prompt input, .model-auth-prompt select { width: 100%; min-width: 0; }
.model-auth-prompt > button { justify-self: end; }
@container (max-width: 640px) {
  .model-access-layout { grid-template-columns: minmax(0, 1fr); }
  .model-access-providers { display: flex; flex-wrap: wrap; gap: 6px; border-right: 0; border-bottom: 1px solid var(--border); }
  .model-access-providers > button { flex: 1 1 130px; width: auto; margin: 0; }
  .model-access-add { width: 100%; margin-top: 8px; grid-template-columns: auto minmax(0, 1fr); align-items: center; }
  .model-access-detail { padding: 16px; }
  .model-default-choice { max-width: none; }
  .model-default-bar > .model-access-grow { flex-basis: calc(100% - 44px); }
  .model-default-bar > svg { align-self: flex-start; margin-top: 2px; }
  .model-default-choice { margin-left: 30px; min-width: 0; }
  .model-access-search { width: 100%; }
  .model-access-search input { flex: 1; width: auto; }
}
</style>
