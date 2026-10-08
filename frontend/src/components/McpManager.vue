<script setup lang="ts">
import { ui } from "../ui/classes";
import { ArrowLeft, Cable, ChevronDown, ChevronRight, Folder, Import, Monitor, Plus, RefreshCw, Save, Search, Trash2, XCircle } from "lucide-vue-next";
import { computed, onMounted, reactive, ref, watch } from "vue";
import { McpConfigScope, type McpImportCandidate, type McpServerSummary, type McpServerTestResult } from "../../bindings/pi-desk/internal/domain";
import { tr } from "../i18n";
import { mcpConfigService, type McpConfigSnapshot } from "../services/mcpconfig";
import { useAppStore } from "../stores/app";

type TransportChoice = "stdio" | "http" | "override";
type EditorMode = "form" | "json";

const mcpPresets = [
  { id: "filesystem", definition: '{\n  "command": "npx",\n  "args": ["-y", "@modelcontextprotocol/server-filesystem", "."]\n}\n' },
  { id: "fetch", definition: '{\n  "command": "uvx",\n  "args": ["mcp-server-fetch"]\n}\n' },
  { id: "git", definition: '{\n  "command": "uvx",\n  "args": ["mcp-server-git"]\n}\n' },
  { id: "memory", definition: '{\n  "command": "npx",\n  "args": ["-y", "@modelcontextprotocol/server-memory"]\n}\n' },
  { id: "context7", definition: '{\n  "command": "npx",\n  "args": ["-y", "@upstash/context7-mcp"]\n}\n' },
  { id: "sequential-thinking", definition: '{\n  "command": "npx",\n  "args": ["-y", "@modelcontextprotocol/server-sequential-thinking"]\n}\n' },
] as const;

const appStore = useAppStore();
const snapshot = ref<McpConfigSnapshot>();
const loading = ref(true);
const saving = ref(false);
const testing = ref(false);
const loadError = ref("");
const formError = ref("");
const notice = ref("");
const testError = ref("");
const testResult = ref<McpServerTestResult>();
const selectedKey = ref("");
const deleteArmed = ref(false);
const savedFingerprint = ref("");
const savedJsonDocument = ref("");
const view = ref<"list" | "editor">("list");
const editorMode = ref<EditorMode>("form");
const scopeTarget = ref("global");
const selectedWorkspacePath = ref("");
const searchQuery = ref("");
const jsonDocument = ref("");
const importOpen = ref(false);
const importCandidates = ref<McpImportCandidate[]>([]);
const importLoading = ref(false);
const importError = ref("");
const importBusy = ref(false);
const picked = reactive<Record<string, boolean>>({});
const editor = reactive({
  scope: McpConfigScope.McpConfigScopeGlobal,
  originalName: "",
  name: "",
  transport: "stdio" as TransportChoice,
  command: "",
  args: "[]",
  url: "",
  disabled: false,
  definition: "",
});

const currentWorkspacePath = computed(() => appStore.activeThread?.workspacePath || appStore.workspaces.find((workspace) => workspace.id === appStore.activeThread?.workspaceId)?.path || "");
const workspacePath = computed(() => selectedWorkspacePath.value || currentWorkspacePath.value);
const availableWorkspaces = computed(() => appStore.workspaces.filter((workspace) => workspace.kind !== "ssh" && workspace.path && workspace.trust === "approve"));
const activeScope = computed(() => scopeTarget.value === "global" ? McpConfigScope.McpConfigScopeGlobal : McpConfigScope.McpConfigScopeProject);
const allServers = computed(() => snapshot.value?.servers ?? []);
const globalServers = computed(() => allServers.value.filter((server) => server.scope === McpConfigScope.McpConfigScopeGlobal));
const projectServers = computed(() => allServers.value.filter((server) => server.scope === McpConfigScope.McpConfigScopeProject));
const scopeServers = computed(() => activeScope.value === McpConfigScope.McpConfigScopeGlobal ? globalServers.value : projectServers.value);
const normalizedQuery = computed(() => searchQuery.value.trim().toLocaleLowerCase());
const filteredServers = computed(() => scopeServers.value.filter((server) => !normalizedQuery.value || `${server.name} ${server.transport} ${server.endpoint || ""}`.toLocaleLowerCase().includes(normalizedQuery.value)));
const visibleServerCount = computed(() => scopeServers.value.length);
const isExisting = computed(() => Boolean(editor.originalName));
const dirty = computed(() => fingerprint() !== savedFingerprint.value || (editorMode.value === "json" && jsonDocument.value !== savedJsonDocument.value));
const importTargetScope = computed(() => activeScope.value === McpConfigScope.McpConfigScopeProject && !snapshot.value?.projectEnabled ? McpConfigScope.McpConfigScopeGlobal : activeScope.value);
const importableCandidates = computed(() => importCandidates.value.filter((candidate) => !isConflicting(candidate)));
const pickedCount = computed(() => importableCandidates.value.filter((candidate) => picked[candidateKey(candidate)]).length);
const scopeHint = computed(() => editor.scope === McpConfigScope.McpConfigScopeProject
  ? tr("settings.mcpProjectConfigScope", { path: snapshot.value?.projectPath || `${workspacePath.value}\\.pi\\mcp.json` })
  : tr("settings.mcpGlobalConfigScope", { path: snapshot.value?.globalPath || "~/.pi/agent/mcp.json" }));

function fingerprint() {
  return JSON.stringify({ ...editor });
}

function keyOf(server: Pick<McpServerSummary, "scope" | "name">) {
  return `${server.scope}:${server.name}`;
}

function candidateKey(candidate: McpImportCandidate) {
  return `${candidate.host}:${candidate.path}:${candidate.name}`;
}

function isConflicting(candidate: McpImportCandidate) {
  return allServers.value.some((server) => server.scope === importTargetScope.value && server.name === candidate.name);
}

async function changeListScope(event: Event) {
  scopeTarget.value = (event.target as HTMLSelectElement).value;
  if (scopeTarget.value !== "global") selectedWorkspacePath.value = scopeTarget.value;
  importOpen.value = false;
  searchQuery.value = "";
  await loadServers();
}

async function changeEditorScope(event: Event) {
  const target = (event.target as HTMLSelectElement).value;
  editor.scope = target === "global" ? McpConfigScope.McpConfigScopeGlobal : McpConfigScope.McpConfigScopeProject;
  if (target !== "global" && target !== selectedWorkspacePath.value) {
    selectedWorkspacePath.value = target;
    await loadServers();
  }
}

function defaultDefinition(transport: TransportChoice) {
  if (transport === "http") return '{\n  "url": "https://example.com/mcp"\n}\n';
  return '{\n  "command": "npx",\n  "args": [\n    "-y",\n    "@example/mcp-server"\n  ]\n}\n';
}

function syncJsonDocument() {
  try {
    const definition = JSON.parse(editor.definition || "{}") as Record<string, unknown>;
    jsonDocument.value = `${JSON.stringify({ [editor.name || "my-mcp-server"]: definition }, null, 2)}\n`;
  } catch {
    jsonDocument.value = editor.definition;
  }
}

function rememberSaved() {
  savedFingerprint.value = fingerprint();
  savedJsonDocument.value = jsonDocument.value;
}

function parseJsonDocument() {
  try {
    const parsed = JSON.parse(jsonDocument.value) as Record<string, unknown>;
    const container = parsed?.mcpServers && typeof parsed.mcpServers === "object" && !Array.isArray(parsed.mcpServers)
      ? parsed.mcpServers as Record<string, unknown>
      : parsed;
    const entries = Object.entries(container || {});
    if (entries.length !== 1 || !entries[0][0] || !entries[0][1] || typeof entries[0][1] !== "object" || Array.isArray(entries[0][1])) throw new Error();
    editor.name = entries[0][0];
    editor.definition = `${JSON.stringify(entries[0][1], null, 2)}\n`;
    parseDefinition();
    return !formError.value;
  } catch {
    formError.value = tr("settings.mcpCompleteJsonInvalid");
    return false;
  }
}

function setEditorMode(mode: EditorMode) {
  if (mode === editorMode.value) return;
  if (mode === "json") {
    updateDefinitionFromFields();
    if (formError.value) return;
    syncJsonDocument();
  } else if (!parseJsonDocument()) return;
  editorMode.value = mode;
}

function resetEditor(scope = activeScope.value) {
  selectedKey.value = "new";
  deleteArmed.value = false;
  formError.value = "";
  notice.value = "";
  editor.scope = scope;
  editor.originalName = "";
  editor.name = "";
  editor.transport = "stdio";
  editor.command = "npx";
  editor.args = '["-y", "@example/mcp-server"]';
  editor.url = "";
  editor.disabled = false;
  editor.definition = defaultDefinition("stdio");
  editorMode.value = "form";
  syncJsonDocument();
  rememberSaved();
  view.value = "editor";
}

function closeEditor() {
  view.value = "list";
  formError.value = "";
  testError.value = "";
  testResult.value = undefined;
  deleteArmed.value = false;
}

function parseDefinition() {
  try {
    const parsed = JSON.parse(editor.definition) as Record<string, unknown>;
    if (!parsed || Array.isArray(parsed) || typeof parsed !== "object") throw new Error();
    editor.command = typeof parsed.command === "string" ? parsed.command : "";
    editor.url = typeof parsed.url === "string" ? parsed.url : "";
    editor.args = JSON.stringify(Array.isArray(parsed.args) ? parsed.args : [], null, 2);
    editor.disabled = parsed.enabled === false || parsed.disabled === true;
    editor.transport = editor.command ? "stdio" : editor.url ? "http" : "override";
    formError.value = "";
  } catch {
    formError.value = tr("settings.mcpDefinitionInvalid");
  }
}

function updateDefinitionFromFields() {
  formError.value = "";
  let parsed: Record<string, unknown> = {};
  try {
    parsed = JSON.parse(editor.definition || "{}") as Record<string, unknown>;
    if (!parsed || Array.isArray(parsed) || typeof parsed !== "object") parsed = {};
  } catch {
    // The common fields can repair malformed advanced JSON.
  }
  delete parsed.command;
  delete parsed.args;
  delete parsed.url;
  delete parsed.socket;
  if (editor.transport === "stdio") {
    parsed.command = editor.command.trim();
    try {
      const args = JSON.parse(editor.args || "[]");
      if (!Array.isArray(args) || args.some((value) => typeof value !== "string")) throw new Error();
      if (args.length) parsed.args = args;
    } catch {
      formError.value = tr("settings.mcpArgsInvalid");
      return;
    }
  } else if (editor.transport === "http") parsed.url = editor.url.trim();
  delete parsed.disabled;
  if (editor.disabled) parsed.enabled = false;
  else if (editor.transport === "override") parsed.enabled = true;
  else delete parsed.enabled;
  editor.definition = `${JSON.stringify(parsed, null, 2)}\n`;
}

async function loadServers() {
  loading.value = true;
  loadError.value = "";
  try {
    snapshot.value = await mcpConfigService.list({ workspacePath: workspacePath.value });
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    loading.value = false;
  }
}

function applyPreset(preset: (typeof mcpPresets)[number]) {
  if (isExisting.value || saving.value) return;
  editor.name = preset.id;
  editor.definition = preset.definition;
  parseDefinition();
}

async function toggleImport() {
  importOpen.value = !importOpen.value;
  if (!importOpen.value || importCandidates.value.length || importLoading.value) return;
  importLoading.value = true;
  importError.value = "";
  try {
    importCandidates.value = (await mcpConfigService.importCandidates()) ?? [];
    for (const candidate of importableCandidates.value) picked[candidateKey(candidate)] = true;
  } catch (cause) {
    importError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    importLoading.value = false;
  }
}

async function importSelected() {
  if (importBusy.value || !pickedCount.value) return;
  importBusy.value = true;
  formError.value = "";
  notice.value = "";
  let imported = 0;
  let failed = 0;
  for (const candidate of importableCandidates.value) {
    if (!picked[candidateKey(candidate)]) continue;
    try {
      await mcpConfigService.upsert({ scope: importTargetScope.value, workspacePath: workspacePath.value, name: candidate.name, definition: candidate.definition });
      imported += 1;
    } catch {
      failed += 1;
    }
  }
  importBusy.value = false;
  if (imported) {
    await loadServers();
    notice.value = tr("settings.mcpImported", { count: imported });
  }
  if (failed) formError.value = tr("settings.mcpImportFailed", { count: failed });
}

async function selectServer(server: McpServerSummary) {
  if (saving.value) return;
  formError.value = "";
  notice.value = "";
  deleteArmed.value = false;
  selectedKey.value = keyOf(server);
  try {
    const loaded = await mcpConfigService.get({
      scope: server.scope,
      workspacePath: workspacePath.value,
      name: server.name,
    });
    editor.scope = loaded.scope;
    editor.originalName = loaded.name;
    editor.name = loaded.name;
    editor.definition = loaded.definition;
    parseDefinition();
    editorMode.value = "form";
    syncJsonDocument();
    rememberSaved();
    view.value = "editor";
  } catch (cause) {
    formError.value = cause instanceof Error ? cause.message : String(cause);
  }
}

function validForm(includeName = true) {
  formError.value = "";
  if (includeName && !editor.name.trim()) formError.value = tr("settings.mcpNameRequired");
  else if (includeName && !/^[A-Za-z0-9_-]+$/.test(editor.name.trim())) formError.value = tr("settings.mcpNameInvalid");
  else {
    try {
      const definition = JSON.parse(editor.definition) as Record<string, unknown>;
      if (!definition || Array.isArray(definition) || typeof definition !== "object") throw new Error();
      if (editor.transport !== "override" && ![definition.command, definition.url].some((value) => typeof value === "string" && value.trim())) formError.value = tr("settings.mcpTransportRequired");
    } catch {
      formError.value = tr("settings.mcpDefinitionInvalid");
    }
  }
  return !formError.value;
}

async function testServer() {
  if (editorMode.value === "json" && !parseJsonDocument()) return;
  if (editorMode.value === "form") updateDefinitionFromFields();
  if (!validForm(false) || testing.value) return;
  testing.value = true;
  testError.value = "";
  testResult.value = undefined;
  try {
    testResult.value = await mcpConfigService.test({ scope: editor.scope, workspacePath: workspacePath.value, name: editor.name || undefined, definition: editor.definition });
  } catch (cause) {
    testError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    testing.value = false;
  }
}

async function saveServer() {
  if (editorMode.value === "json" && !parseJsonDocument()) return;
  if (editorMode.value === "form") updateDefinitionFromFields();
  if (!validForm() || saving.value) return;
  saving.value = true;
  notice.value = "";
  try {
    const saved = await mcpConfigService.upsert({
      scope: editor.scope,
      workspacePath: workspacePath.value,
      originalName: editor.originalName || undefined,
      name: editor.name.trim(),
      definition: editor.definition,
    });
    selectedKey.value = keyOf(saved);
    editor.originalName = saved.name;
    editor.name = saved.name;
    editor.definition = saved.definition;
    parseDefinition();
    syncJsonDocument();
    rememberSaved();
    await loadServers();
    notice.value = tr("settings.mcpSaved");
    view.value = "list";
  } catch (cause) {
    formError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    saving.value = false;
  }
}

async function deleteServer() {
  if (!isExisting.value || saving.value) return;
  if (!deleteArmed.value) {
    deleteArmed.value = true;
    window.setTimeout(() => { deleteArmed.value = false; }, 5000);
    return;
  }
  saving.value = true;
  try {
    await mcpConfigService.delete({
      scope: editor.scope,
      workspacePath: workspacePath.value,
      name: editor.originalName,
    });
    await loadServers();
    notice.value = tr("settings.mcpDeleted");
    view.value = "list";
  } catch (cause) {
    formError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    saving.value = false;
    deleteArmed.value = false;
  }
}

async function toggleServer(server: McpServerSummary) {
  if (saving.value) return;
  saving.value = true;
  formError.value = "";
  try {
    const loaded = await mcpConfigService.get({ scope: server.scope, workspacePath: workspacePath.value, name: server.name });
    const definition = JSON.parse(loaded.definition) as Record<string, unknown>;
    delete definition.disabled;
    if (server.disabled) delete definition.enabled;
    else definition.enabled = false;
    await mcpConfigService.upsert({ scope: server.scope, workspacePath: workspacePath.value, originalName: server.name, name: server.name, definition: `${JSON.stringify(definition, null, 2)}\n` });
    await loadServers();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    saving.value = false;
  }
}

watch(() => editor.definition, () => {
  if (testing.value) return;
  testError.value = "";
  testResult.value = undefined;
});

onMounted(() => {
  selectedWorkspacePath.value = currentWorkspacePath.value;
  void loadServers();
});
</script>

<template>
  <div class="settings-content model-config-content mcp-config-content" :class="ui.settingsContent">
    <div v-if="loading" class="settings-empty" :class="ui.empty"><RefreshCw :size="18" class="is-spinning" /><span>{{ tr("settings.loadingMcp") }}</span></div>
    <div v-else-if="loadError" class="settings-empty is-error" :class="ui.empty"><XCircle :size="18" /><span>{{ loadError }}</span></div>
    <main v-else class="mcp-page">
      <section v-if="view === 'list'" class="mcp-list-page">
        <header class="mcp-page-title"><h1>{{ tr("settings.mcpServers") }}</h1></header>

        <div class="mcp-list-toolbar">
          <div class="mcp-scope-summary">
            <label class="mcp-scope-picker">
              <Monitor v-if="scopeTarget === 'global'" :size="15" aria-hidden="true" />
              <Folder v-else :size="15" aria-hidden="true" />
              <select data-testid="mcp-scope-target" :value="scopeTarget" :aria-label="tr('settings.mcpScope')" @change="void changeListScope($event)">
                <option value="global">{{ tr("settings.mcpUserScope") }}</option>
                <option v-for="workspace in availableWorkspaces" :key="workspace.id" :value="workspace.path">{{ workspace.name }}</option>
              </select>
              <ChevronDown :size="14" aria-hidden="true" />
            </label>
            <i aria-hidden="true"></i>
            <span>MCP {{ visibleServerCount }}</span>
          </div>
          <label class="mcp-search-box">
            <Search :size="16" aria-hidden="true" />
            <input v-model="searchQuery" type="search" :placeholder="tr('settings.mcpSearch')" />
          </label>
        </div>

        <div class="mcp-list-actions">
          <div><strong>{{ tr("settings.mcpInstalled") }}</strong><span>{{ scopeServers.length }}</span></div>
          <div>
            <button class="icon-button" :class="ui.iconButton" type="button" :title="tr('settings.mcpImport')" :aria-expanded="importOpen" @click="void toggleImport()"><Import :size="16" /></button>
            <button class="icon-button" :class="ui.iconButton" type="button" :title="tr('common.refresh')" :disabled="loading" @click="void loadServers()"><RefreshCw :size="16" /></button>
            <button data-testid="new-mcp-server" class="text-button primary" :class="ui.buttonPrimary" type="button" @click="resetEditor()"><Plus :size="16" />{{ tr("settings.newMcpServer") }}</button>
          </div>
        </div>

        <section v-if="importOpen" class="mcp-import-panel" aria-live="polite">
          <p v-if="importLoading" class="settings-inline-note"><RefreshCw :size="12" class="is-spinning" /><span>{{ tr("settings.loadingMcp") }}</span></p>
          <p v-else-if="importError" class="settings-inline-note is-error">{{ importError }}</p>
          <p v-else-if="!importCandidates.length" class="settings-inline-note">{{ tr("settings.mcpImportEmpty") }}</p>
          <template v-else>
            <p>{{ tr("settings.mcpImportHint") }}</p>
            <label v-for="candidate in importCandidates" :key="candidateKey(candidate)" class="mcp-import-row">
              <input v-model="picked[candidateKey(candidate)]" type="checkbox" :disabled="isConflicting(candidate)" />
              <span><strong>{{ candidate.name }}</strong><small>{{ candidate.host }}{{ isConflicting(candidate) ? ` 路 ${tr("settings.mcpImportConflict")}` : "" }}</small></span>
            </label>
            <button class="text-button primary" :class="ui.buttonPrimary" type="button" :disabled="importBusy || !pickedCount" @click="void importSelected()">{{ importBusy ? tr("settings.mcpImporting") : tr("settings.mcpImportAction", { count: pickedCount }) }}</button>
          </template>
        </section>

        <div v-if="!visibleServerCount && !searchQuery" class="mcp-empty-state">
          <Cable :size="22" aria-hidden="true" />
          <strong>{{ tr("settings.mcpNoServers") }}</strong>
          <p>{{ tr("settings.mcpNoServersHelp") }}</p>
          <div>
            <button class="text-button primary" :class="ui.buttonPrimary" type="button" @click="resetEditor()"><Plus :size="16" />{{ tr("settings.newMcpServer") }}</button>
            <button class="text-button" :class="ui.button" type="button" @click="void toggleImport()"><Import :size="15" />{{ tr("settings.mcpImport") }}</button>
          </div>
        </div>
        <div v-else-if="!filteredServers.length" class="settings-empty compact" :class="ui.empty"><Search :size="17" /><span>{{ tr("settings.noResources") }}</span></div>

        <section v-if="filteredServers.length" class="mcp-server-group" aria-labelledby="mcp-installed-heading">
          <header><h2 id="mcp-installed-heading">{{ tr("settings.mcpInstalled") }}</h2><span>{{ scopeServers.length }}</span></header>
          <div class="mcp-server-list">
            <article v-for="server in filteredServers" :key="keyOf(server)" class="mcp-server-row">
              <button class="mcp-server-main" type="button" :disabled="saving" @click="void selectServer(server)">
                <span class="mcp-server-icon"><Cable :size="17" aria-hidden="true" /><i :class="{ 'is-offline': server.disabled }" aria-hidden="true"></i></span>
                <span><strong>{{ server.name }}</strong><small>{{ server.transport }}{{ server.endpoint ? ` 路 ${server.endpoint}` : "" }}</small></span>
              </button>
              <button class="mcp-switch" type="button" role="switch" :aria-checked="!server.disabled" :aria-label="server.disabled ? tr('settings.enableMcp') : tr('settings.disableMcp')" :disabled="saving" @click="void toggleServer(server)"><span></span></button>
            </article>
          </div>
        </section>

        <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>
        <p v-if="notice" class="setting-status" aria-live="polite">{{ notice }}</p>
      </section>

      <template v-else>
        <nav class="mcp-editor-breadcrumb" :aria-label="tr('settings.mcpServers')">
          <button type="button" @click="closeEditor"><ArrowLeft :size="15" />{{ tr("settings.mcpServers") }}</button>
          <ChevronRight :size="15" aria-hidden="true" />
          <span>{{ isExisting ? editor.name : tr("settings.newMcpServer") }}</span>
        </nav>

        <section class="mcp-editor-page">
          <header class="mcp-editor-heading">
            <div><h1>{{ isExisting ? editor.name : tr("settings.newMcpServer") }}</h1><p>{{ tr("settings.mcpEditorHelp") }}</p></div>
            <div class="mcp-editor-tabs" role="tablist" :aria-label="tr('settings.mcpEditorMode')">
              <button type="button" role="tab" :aria-selected="editorMode === 'form'" :class="{ 'is-active': editorMode === 'form' }" @click="setEditorMode('form')">{{ tr("settings.mcpFormView") }}</button>
              <button type="button" role="tab" :aria-selected="editorMode === 'json'" :class="{ 'is-active': editorMode === 'json' }" @click="setEditorMode('json')">JSON</button>
            </div>
          </header>

          <form class="mcp-editor-card" data-testid="mcp-editor" @submit.prevent="void saveServer()">
            <div class="mcp-editor-scope">
              <span>{{ tr("settings.mcpScope") }}</span>
              <label class="mcp-scope-picker">
                <Monitor v-if="editor.scope === McpConfigScope.McpConfigScopeGlobal" :size="15" aria-hidden="true" />
                <Folder v-else :size="15" aria-hidden="true" />
                <select :value="editor.scope === McpConfigScope.McpConfigScopeGlobal ? 'global' : workspacePath" :disabled="isExisting" @change="void changeEditorScope($event)">
                  <option value="global">{{ tr("settings.mcpUserScope") }}</option>
                  <option v-for="workspace in availableWorkspaces" :key="workspace.id" :value="workspace.path">{{ workspace.name }}</option>
                </select>
                <ChevronDown :size="14" aria-hidden="true" />
              </label>
              <small>{{ scopeHint }}</small>
            </div>

            <fieldset v-if="editorMode === 'form'" class="mcp-form-fields">
              <div v-if="!isExisting" class="mcp-preset-field">
                <span>{{ tr("settings.mcpPresets") }}</span>
                <div><button v-for="preset in mcpPresets" :key="preset.id" :class="ui.button" class="text-button" type="button" :disabled="saving" @click="applyPreset(preset)">{{ preset.id }}</button></div>
              </div>
              <label class="model-field" :class="ui.field"><span>{{ tr("settings.mcpName") }}</span><input :class="ui.input" v-model="editor.name" spellcheck="false" placeholder="my-mcp-server" /></label>
              <label class="mcp-editor-switch"><span><strong>{{ tr("settings.mcpStatus") }}</strong><small>{{ editor.disabled ? tr("settings.mcpDisabled") : tr("settings.mcpEnabled") }}</small></span><input v-model="editor.disabled" type="checkbox" @change="updateDefinitionFromFields" /></label>
              <label v-if="editor.transport === 'stdio'" class="model-field mcp-field-wide" :class="ui.field"><span>{{ tr("settings.mcpCommand") }}</span><input :class="ui.input" v-model="editor.command" spellcheck="false" placeholder="npx" /></label>
              <label v-if="editor.transport === 'stdio'" class="model-field mcp-field-wide" :class="ui.field"><span>{{ tr("settings.mcpArgs") }}</span><input :class="ui.input" v-model="editor.args" spellcheck="false" placeholder='["-y", "package"]' /></label>
              <label v-if="editor.transport === 'http'" class="model-field mcp-field-wide" :class="ui.field"><span>URL</span><input :class="ui.input" v-model="editor.url" spellcheck="false" placeholder="https://example.com/mcp" /></label>
              <details class="mcp-advanced-field mcp-field-wide">
                <summary>{{ tr("settings.mcpAdvancedJson") }} <small>{{ tr("settings.optional") }}</small></summary>
                <label class="model-field" :class="ui.field"><textarea :class="ui.textarea" v-model="editor.definition" :aria-label="tr('settings.mcpAdvancedJson')" spellcheck="false" @blur="parseDefinition" /><small>{{ tr("settings.mcpAdvancedHelp") }}</small></label>
              </details>
            </fieldset>

            <label v-else class="mcp-complete-json">
              <span>{{ tr("settings.mcpCompleteConfig") }}</span>
              <textarea :class="ui.textarea" v-model="jsonDocument" spellcheck="false" @blur="parseJsonDocument" />
              <small>{{ tr("settings.mcpCompleteJsonHelp") }}</small>
            </label>

          <section v-if="testResult || testError" class="grid gap-2 rounded-lg border border-[var(--border)] bg-[var(--bg-raised)] p-3 text-sm" aria-live="polite" data-testid="mcp-test-result">
            <div v-if="testError" class="flex min-w-0 items-start gap-2 text-[var(--red)]" role="alert">
              <XCircle :size="16" class="mt-0.5 shrink-0" aria-hidden="true" />
              <div class="min-w-0"><strong>{{ tr("settings.mcpTestFailed") }}</strong><p class="mt-1 whitespace-pre-wrap break-words text-xs leading-relaxed">{{ testError }}</p></div>
            </div>
            <template v-else-if="testResult">
              <strong>{{ tr("settings.mcpTestPassed") }} 路 {{ testResult.durationMillis }} ms</strong>
              <pre class="whitespace-pre-wrap break-words">{{ testResult.output }}</pre>
            </template>
          </section>
          <p class="prompt-reload-note">{{ appStore.activeThread?.started ? tr("settings.mcpRestartNeeded") : tr("settings.mcpReadyOnStart") }}</p>
          <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>
          <p v-if="notice" class="setting-status" aria-live="polite">{{ notice }}</p>
          <footer class="mcp-editor-footer">
            <button v-if="isExisting" class="text-button danger" :class="ui.buttonDanger" type="button" :disabled="saving" @click="void deleteServer()"><Trash2 :size="14" />{{ deleteArmed ? tr("settings.confirmDeleteMcp") : tr("settings.deleteMcp") }}</button>
            <div>
              <button class="text-button" :class="ui.button" type="button" :disabled="saving" @click="closeEditor">{{ tr("common.cancel") }}</button>
              <button class="text-button" :class="ui.button" type="button" :disabled="saving || testing" @click="void testServer()"><RefreshCw v-if="testing" :size="14" class="is-spinning" /><Cable v-else :size="14" />{{ testing ? tr("settings.mcpTesting") : tr("settings.mcpTest") }}</button>
              <button class="text-button primary" :class="ui.buttonPrimary" type="submit" :disabled="saving || testing || !dirty"><Save :size="14" />{{ saving ? tr("settings.savingMcp") : tr("settings.saveMcp") }}</button>
            </div>
          </footer>
        </form>
        </section>
      </template>
    </main>
  </div>
</template>
