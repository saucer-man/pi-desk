<script setup lang="ts">
import { ui } from "../ui/classes";
import { CheckCircle2, Download, FilePlus2, Import, RefreshCw, Save, Trash2, XCircle } from "lucide-vue-next";
import { computed, onMounted, reactive, ref } from "vue";
import { McpConfigScope, PiPackageScope, type McpEngineStatus, type McpImportCandidate, type McpServerSummary } from "../../bindings/pi-desk/internal/domain";
import { tr } from "../i18n";
import { mcpConfigService, type McpConfigSnapshot } from "../services/mcpconfig";
import { piExtensionService } from "../services/extensions";
import { useAppStore } from "../stores/app";

type TransportChoice = "stdio" | "http" | "socket";

const mcpEngineSource = "npm:@nicobailon/pi-mcp-adapter";
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
const loadError = ref("");
const formError = ref("");
const notice = ref("");
const selectedKey = ref("");
const deleteArmed = ref(false);
const savedFingerprint = ref("");
const engine = ref<McpEngineStatus>();
const engineError = ref("");
const engineBusy = ref(false);
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
  socket: "",
  disabled: false,
  definition: "",
});

const workspacePath = computed(() => appStore.activeThread?.workspacePath || appStore.workspaces.find((workspace) => workspace.id === appStore.activeThread?.workspaceId)?.path || "");
const allServers = computed(() => snapshot.value?.servers ?? []);
const globalServers = computed(() => allServers.value.filter((server) => server.scope === McpConfigScope.McpConfigScopeGlobal));
const projectServers = computed(() => allServers.value.filter((server) => server.scope === McpConfigScope.McpConfigScopeProject));
const isExisting = computed(() => Boolean(editor.originalName));
const dirty = computed(() => fingerprint() !== savedFingerprint.value);
const importTargetScope = computed(() => editor.scope === McpConfigScope.McpConfigScopeProject && !snapshot.value?.projectEnabled ? McpConfigScope.McpConfigScopeGlobal : editor.scope);
const importableCandidates = computed(() => importCandidates.value.filter((candidate) => !isConflicting(candidate)));
const pickedCount = computed(() => importableCandidates.value.filter((candidate) => picked[candidateKey(candidate)]).length);

function fingerprint() {
  return JSON.stringify({ scope: editor.scope, originalName: editor.originalName, name: editor.name, definition: editor.definition });
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

function defaultDefinition(transport: TransportChoice) {
  if (transport === "http") return '{\n  "url": "https://example.com/mcp",\n  "auth": false\n}\n';
  if (transport === "socket") return '{\n  "socket": "/path/to/mcp.sock"\n}\n';
  return '{\n  "command": "npx",\n  "args": [\n    "-y",\n    "@example/mcp-server"\n  ]\n}\n';
}

function resetEditor(scope = McpConfigScope.McpConfigScopeGlobal) {
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
  editor.socket = "";
  editor.disabled = false;
  editor.definition = defaultDefinition("stdio");
  savedFingerprint.value = fingerprint();
}

function parseDefinition() {
  try {
    const parsed = JSON.parse(editor.definition) as Record<string, unknown>;
    if (!parsed || Array.isArray(parsed) || typeof parsed !== "object") throw new Error();
    editor.command = typeof parsed.command === "string" ? parsed.command : "";
    editor.url = typeof parsed.url === "string" ? parsed.url : "";
    editor.socket = typeof parsed.socket === "string" ? parsed.socket : "";
    editor.args = JSON.stringify(Array.isArray(parsed.args) ? parsed.args : [], null, 2);
    editor.disabled = parsed.disabled === true;
    editor.transport = editor.command ? "stdio" : editor.url ? "http" : editor.socket ? "socket" : "stdio";
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
  else parsed.socket = editor.socket.trim();
  if (editor.disabled) parsed.disabled = true;
  else delete parsed.disabled;
  editor.definition = `${JSON.stringify(parsed, null, 2)}\n`;
}

async function loadServers(preferredKey = selectedKey.value) {
  loading.value = true;
  loadError.value = "";
  try {
    snapshot.value = await mcpConfigService.list({ workspacePath: workspacePath.value });
    const selected = (snapshot.value.servers ?? []).find((server) => keyOf(server) === preferredKey);
    if (selected) await selectServer(selected);
    else if (globalServers.value[0]) await selectServer(globalServers.value[0]);
    else resetEditor();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    loading.value = false;
  }
}

async function loadEngine() {
  engineError.value = "";
  try {
    engine.value = await mcpConfigService.engineStatus({ workspacePath: workspacePath.value });
  } catch (cause) {
    engineError.value = cause instanceof Error ? cause.message : String(cause);
  }
}

async function runEngineCommand(action: "install" | "update") {
  if (engineBusy.value) return;
  engineBusy.value = true;
  engineError.value = "";
  notice.value = "";
  try {
    const request = { scope: PiPackageScope.PiPackageScopeGlobal, source: engine.value?.source || mcpEngineSource, workspacePath: "" };
    if (action === "install") await piExtensionService.installPackage(request);
    else await piExtensionService.updatePackage(request);
    await loadEngine();
    notice.value = tr(action === "install" ? "settings.mcpEngineInstalledNotice" : "settings.mcpEngineUpdatedNotice");
  } catch (cause) {
    engineError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    engineBusy.value = false;
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
    await loadServers(selectedKey.value);
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
    savedFingerprint.value = fingerprint();
  } catch (cause) {
    formError.value = cause instanceof Error ? cause.message : String(cause);
  }
}

function selectTransport(event: Event) {
  editor.transport = (event.target as HTMLSelectElement).value as TransportChoice;
  updateDefinitionFromFields();
}

function validForm() {
  formError.value = "";
  if (!editor.name.trim()) formError.value = tr("settings.mcpNameRequired");
  else if (!/^[\p{L}\p{N}_.-]+$/u.test(editor.name.trim())) formError.value = tr("settings.mcpNameInvalid");
  else {
    try {
      const definition = JSON.parse(editor.definition) as Record<string, unknown>;
      if (!definition || Array.isArray(definition) || typeof definition !== "object") throw new Error();
      if (![definition.command, definition.url, definition.socket].some((value) => typeof value === "string" && value.trim())) formError.value = tr("settings.mcpTransportRequired");
    } catch {
      formError.value = tr("settings.mcpDefinitionInvalid");
    }
  }
  return !formError.value;
}

async function saveServer() {
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
    savedFingerprint.value = fingerprint();
    await loadServers(selectedKey.value);
    notice.value = tr("settings.mcpSaved");
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
    resetEditor();
    await loadServers("new");
    notice.value = tr("settings.mcpDeleted");
  } catch (cause) {
    formError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    saving.value = false;
    deleteArmed.value = false;
  }
}

onMounted(() => {
  void loadServers();
  void loadEngine();
});
</script>

<template>
  <div class="settings-content model-config-content mcp-config-content" :class="ui.settingsContent">
    <div v-if="loading" class="settings-empty" :class="ui.empty"><RefreshCw :size="18" class="is-spinning" /><span>{{ tr("settings.loadingMcp") }}</span></div>
    <div v-else-if="loadError" class="settings-empty is-error" :class="ui.empty"><XCircle :size="18" /><span>{{ loadError }}</span></div>
    <template v-else>
      <section class="mcp-engine-card mb-2.5 flex min-w-0 flex-wrap items-center justify-between gap-2 rounded-lg border border-[var(--border)] bg-[var(--bg-raised)] px-3 py-2" data-testid="mcp-engine">
        <div class="grid min-w-0 gap-0.5">
          <strong class="text-sm">{{ tr("settings.mcpEngine") }}</strong>
          <small v-if="engineError" class="truncate text-xs text-[var(--red)]">{{ engineError }}</small>
          <template v-else-if="engine?.installed">
            <small class="truncate text-xs text-[var(--text-muted)]">{{ tr("settings.mcpEngineInstalled") }}<template v-if="engine.source"> · {{ engine.source }}</template></small>
            <small v-if="engine.shadowedPaths?.length" class="truncate text-xs text-[var(--amber)]" :title="engine.shadowedPaths.join(' · ')">{{ tr("settings.mcpEngineShadowed") }} {{ engine.shadowedPaths.join(" · ") }}</small>
          </template>
          <small v-else class="text-xs text-[var(--amber)]">{{ tr("settings.mcpEngineMissing") }}</small>
        </div>
        <button v-if="engine && !engine.installed" class="text-button primary" :class="ui.buttonPrimary" type="button" :disabled="engineBusy" @click="void runEngineCommand('install')">
          <Download :size="14" />{{ engineBusy ? tr("settings.mcpEngineInstalling") : tr("settings.mcpEngineInstall") }}
        </button>
        <button v-else-if="engine?.installed" class="text-button" :class="ui.button" type="button" :disabled="engineBusy" :title="tr('settings.mcpEngineUpdate')" @click="void runEngineCommand('update')">
          <RefreshCw :size="14" :class="{ 'is-spinning': engineBusy }" />{{ engineBusy ? tr("settings.mcpEngineUpdating") : tr("settings.mcpEngineUpdate") }}
        </button>
      </section>
      <div class="prompt-manager-layout" :class="ui.managerLayout">
        <aside class="prompt-config-list" :class="ui.managerList" :aria-label="tr('settings.mcpServers')">
          <button class="model-config-add-row" type="button" :class="[ui.listItem, { 'is-active': selectedKey === 'new' }]" :disabled="saving" @click="resetEditor()"><FilePlus2 :size="14" /><span>{{ tr("settings.addMcpServer") }}</span></button>
          <button class="model-config-add-row" type="button" :class="[ui.listItem, { 'is-active': importOpen }]" :disabled="saving" @click="void toggleImport()"><Import :size="14" /><span>{{ tr("settings.mcpImport") }}</span></button>
          <section v-if="importOpen" class="mcp-import-panel grid min-w-0 gap-1 border-b border-[var(--border)] px-1.5 pb-2 pt-1">
            <p v-if="importLoading" class="settings-inline-note"><RefreshCw :size="12" class="is-spinning" /><span>{{ tr("settings.loadingMcp") }}</span></p>
            <p v-else-if="importError" class="settings-inline-note is-error">{{ importError }}</p>
            <p v-else-if="!importCandidates.length" class="settings-inline-note">{{ tr("settings.mcpImportEmpty") }}</p>
            <template v-else>
              <p class="text-xs leading-relaxed text-[var(--text-muted)]">{{ tr("settings.mcpImportHint") }}</p>
              <label v-for="candidate in importCandidates" :key="candidateKey(candidate)" class="mcp-import-row flex min-w-0 items-center gap-2 py-0.5 text-sm text-[var(--text-secondary)]">
                <input v-model="picked[candidateKey(candidate)]" type="checkbox" class="size-3.5 shrink-0" :disabled="isConflicting(candidate)" />
                <span class="flex min-w-0 flex-col"><strong class="truncate font-medium">{{ candidate.name }}</strong><small class="truncate text-xs text-[var(--text-muted)]">{{ candidate.host }}{{ isConflicting(candidate) ? ` · ${tr("settings.mcpImportConflict")}` : "" }}</small></span>
              </label>
              <button class="text-button primary" :class="ui.buttonPrimary" type="button" :disabled="importBusy || !pickedCount" @click="void importSelected()">
                {{ importBusy ? tr("settings.mcpImporting") : tr("settings.mcpImportAction", { count: pickedCount }) }}
              </button>
            </template>
          </section>
          <section class="prompt-config-scope" :class="ui.group">
            <header><strong>{{ tr("settings.globalMcp") }}</strong><span>{{ globalServers.length }}</span></header>
            <button v-for="server in globalServers" :key="keyOf(server)" type="button" :class="{ 'is-active': selectedKey === keyOf(server) }" :disabled="saving" @click="void selectServer(server)">
              <span><strong>{{ server.name }}</strong><small>{{ server.transport }}{{ server.endpoint ? ` · ${server.endpoint}` : "" }}{{ server.disabled ? ` · ${tr("settings.mcpDisabled")}` : "" }}</small></span>
              <CheckCircle2 v-if="selectedKey === keyOf(server)" :size="13" />
            </button>
          </section>
          <section class="prompt-config-scope" :class="ui.group">
            <header><strong>{{ tr("settings.projectMcp") }}</strong><span>{{ projectServers.length }}</span></header>
            <p v-if="!snapshot?.projectEnabled" class="settings-inline-note">{{ snapshot?.projectNotice || tr("settings.projectMcpUnavailable") }}</p>
            <button v-for="server in projectServers" :key="keyOf(server)" type="button" :class="{ 'is-active': selectedKey === keyOf(server) }" :disabled="saving" @click="void selectServer(server)">
              <span><strong>{{ server.name }}</strong><small>{{ server.transport }}{{ server.endpoint ? ` · ${server.endpoint}` : "" }}{{ server.disabled ? ` · ${tr("settings.mcpDisabled")}` : "" }}</small></span>
              <CheckCircle2 v-if="selectedKey === keyOf(server)" :size="13" />
            </button>
          </section>
        </aside>
        <form class="prompt-editor mcp-editor" :class="ui.managerEditor" @submit.prevent="void saveServer()">
          <div class="model-editor-title">
            <div><strong>{{ isExisting ? editor.name : tr("settings.newMcpServer") }}</strong><small>{{ tr("settings.mcpConfigScope") }}</small></div>
            <span v-if="dirty" class="model-dirty">{{ tr("settings.unsaved") }}</span>
          </div>
          <div v-if="!isExisting" class="model-field model-field-wide" :class="ui.field">
            <span>{{ tr("settings.mcpPresets") }}</span>
            <div class="flex min-w-0 flex-wrap gap-1.5">
              <button v-for="preset in mcpPresets" :key="preset.id" :class="ui.button" class="text-button" type="button" :disabled="saving" @click="applyPreset(preset)">{{ preset.id }}</button>
            </div>
          </div>
          <label class="model-field" :class="ui.field"><span>{{ tr("settings.mcpScope") }}</span><select :class="ui.select" v-model="editor.scope" :disabled="isExisting"><option :value="McpConfigScope.McpConfigScopeGlobal">{{ tr("settings.globalMcp") }}</option><option :value="McpConfigScope.McpConfigScopeProject" :disabled="!snapshot?.projectEnabled">{{ tr("settings.projectMcp") }}</option></select></label>
          <div class="model-form-grid" :class="ui.formGrid">
            <label class="model-field" :class="ui.field">
              <span>{{ tr("settings.mcpName") }}</span>
              <input :class="ui.input" v-model="editor.name" spellcheck="false" placeholder="filesystem" />
            </label>
            <label class="model-field" :class="ui.field">
              <span>{{ tr("settings.mcpTransport") }}</span>
              <select :class="ui.select" :value="editor.transport" @change="selectTransport">
                <option value="stdio">stdio</option><option value="http">HTTP</option><option value="socket">socket</option>
              </select>
            </label>
            <label class="model-field mcp-disabled-field" :class="ui.field"><span>{{ tr("settings.mcpStatus") }}</span><span class="mcp-checkbox"><input v-model="editor.disabled" type="checkbox" @change="updateDefinitionFromFields" />{{ tr("settings.mcpDisabled") }}</span></label>
            <label v-if="editor.transport === 'stdio'" class="model-field" :class="ui.field"><span>{{ tr("settings.mcpCommand") }}</span><input :class="ui.input" v-model="editor.command" spellcheck="false" placeholder="npx" @change="updateDefinitionFromFields" /></label>
            <label v-if="editor.transport === 'stdio'" class="model-field" :class="ui.field"><span>{{ tr("settings.mcpArgs") }}</span><input :class="ui.input" v-model="editor.args" spellcheck="false" placeholder='["-y", "package"]' @change="updateDefinitionFromFields" /></label>
            <label v-if="editor.transport === 'http'" class="model-field model-field-wide" :class="ui.field"><span>URL</span><input :class="ui.input" v-model="editor.url" spellcheck="false" placeholder="https://example.com/mcp" @change="updateDefinitionFromFields" /></label>
            <label v-if="editor.transport === 'socket'" class="model-field model-field-wide" :class="ui.field"><span>Socket</span><input :class="ui.input" v-model="editor.socket" spellcheck="false" placeholder="/path/to/mcp.sock" @change="updateDefinitionFromFields" /></label>
            <label class="model-field model-field-wide" :class="ui.field">
              <span>{{ tr("settings.mcpAdvancedJson") }}</span>
              <textarea :class="ui.textarea" v-model="editor.definition" spellcheck="false" @blur="parseDefinition" />
              <small>{{ tr("settings.mcpAdvancedHelp") }}</small>
            </label>
          </div>
          <p class="prompt-reload-note">{{ appStore.activeThread?.started ? tr("settings.mcpRestartNeeded") : tr("settings.mcpReadyOnStart") }}</p>
          <p v-if="formError" class="form-error">{{ formError }}</p>
          <p v-if="notice" class="setting-status">{{ notice }}</p>
          <footer class="model-editor-footer">
            <button v-if="isExisting" class="text-button danger" :class="ui.buttonDanger" type="button" :disabled="saving" @click="void deleteServer()"><Trash2 :size="14" />{{ deleteArmed ? tr("settings.confirmDeleteMcp") : tr("settings.deleteMcp") }}</button>
            <span />
            <button class="text-button primary" :class="ui.buttonPrimary" type="submit" :disabled="saving || !dirty"><Save :size="14" />{{ saving ? tr("settings.savingMcp") : tr("settings.saveMcp") }}</button>
          </footer>
        </form>
      </div>
    </template>
  </div>
</template>
