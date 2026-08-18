<script setup lang="ts">
import { CheckCircle2, Copy, FilePlus2, PlugZap, RefreshCw, Save, Trash2, XCircle } from "lucide-vue-next";
import { computed, onMounted, reactive, ref } from "vue";
import { McpConfigScope, type McpServerSummary } from "../../bindings/pi-desk/internal/domain";
import { tr } from "../i18n";
import { mcpConfigService, type McpConfigSnapshot } from "../services/mcpconfig";
import { useAppStore } from "../stores/app";

type TransportChoice = "stdio" | "http" | "socket";

const appStore = useAppStore();
const snapshot = ref<McpConfigSnapshot>();
const loading = ref(true);
const saving = ref(false);
const loadError = ref("");
const formError = ref("");
const notice = ref("");
const selectedKey = ref("");
const deleteArmed = ref(false);
const copied = ref(false);
const savedFingerprint = ref("");
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

const globalServers = computed(() => snapshot.value?.servers ?? []);
const isExisting = computed(() => Boolean(editor.originalName));
const dirty = computed(() => fingerprint() !== savedFingerprint.value);

function fingerprint() {
  return JSON.stringify({ scope: editor.scope, originalName: editor.originalName, name: editor.name, definition: editor.definition });
}

function keyOf(server: Pick<McpServerSummary, "scope" | "name">) {
  return `${server.scope}:${server.name}`;
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
    snapshot.value = await mcpConfigService.list({});
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

async function selectServer(server: McpServerSummary) {
  if (saving.value) return;
  formError.value = "";
  notice.value = "";
  deleteArmed.value = false;
  selectedKey.value = keyOf(server);
  try {
    const loaded = await mcpConfigService.get({
      scope: McpConfigScope.McpConfigScopeGlobal,
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
      scope: McpConfigScope.McpConfigScopeGlobal,
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
      scope: McpConfigScope.McpConfigScopeGlobal,
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

async function copyPath() {
  const path = snapshot.value?.globalPath;
  if (!path) return;
  await navigator.clipboard.writeText(path);
  copied.value = true;
  window.setTimeout(() => { copied.value = false; }, 1200);
}

onMounted(() => { void loadServers(); });
</script>

<template>
  <div class="settings-content model-config-content mcp-config-content">
    <div class="settings-content-header model-config-header">
      <div>
        <h3>{{ tr("settings.mcpManagement") }}</h3>
        <button v-if="snapshot?.globalPath" class="model-config-path" type="button" :title="snapshot.globalPath" @click="void copyPath()">
          <PlugZap :size="12" /><span>{{ snapshot.globalPath }}</span><Copy :size="12" />
        </button>
      </div>
      <div class="settings-actions">
        <button class="icon-button" type="button" :title="tr('common.refresh')" :disabled="loading || saving" @click="void loadServers()"><RefreshCw :size="14" :class="{ 'is-spinning': loading }" /></button>
        <button class="text-button" type="button" :disabled="saving" @click="resetEditor()"><FilePlus2 :size="14" />{{ tr("settings.addMcpServer") }}</button>
      </div>
    </div>
    <p v-if="copied" class="setting-status">{{ tr("settings.copied") }}</p>
    <div v-if="loading" class="settings-empty"><RefreshCw :size="18" class="is-spinning" /><span>{{ tr("settings.loadingMcp") }}</span></div>
    <div v-else-if="loadError" class="settings-empty is-error"><XCircle :size="18" /><span>{{ loadError }}</span></div>
    <div v-else class="prompt-manager-layout">
      <aside class="prompt-config-list" :aria-label="tr('settings.mcpServers')">
        <button class="model-config-add-row" type="button" :class="{ 'is-active': selectedKey === 'new' }" :disabled="saving" @click="resetEditor()"><FilePlus2 :size="14" /><span>{{ tr("settings.addMcpServer") }}</span></button>
        <section class="prompt-config-scope">
          <header><strong>{{ tr("settings.globalMcp") }}</strong><span>{{ globalServers.length }}</span></header>
          <button v-for="server in globalServers" :key="keyOf(server)" type="button" :class="{ 'is-active': selectedKey === keyOf(server) }" :disabled="saving" @click="void selectServer(server)">
            <span><strong>{{ server.name }}</strong><small>{{ server.transport }} · {{ server.disabled ? tr("settings.mcpDisabled") : tr("settings.mcpEnabled") }}</small></span>
            <CheckCircle2 v-if="selectedKey === keyOf(server)" :size="13" />
          </button>
        </section>
      </aside>
      <form class="prompt-editor mcp-editor" @submit.prevent="void saveServer()">
        <div class="model-editor-title">
          <div><strong>{{ isExisting ? editor.name : tr("settings.newMcpServer") }}</strong><small>{{ tr("settings.mcpConfigScope") }}</small></div>
          <span v-if="dirty" class="model-dirty">{{ tr("settings.unsaved") }}</span>
        </div>
        <div class="model-form-grid">
          <label class="model-field">
            <span>{{ tr("settings.mcpName") }}</span>
            <input v-model="editor.name" spellcheck="false" placeholder="filesystem" />
          </label>
          <label class="model-field">
            <span>{{ tr("settings.mcpTransport") }}</span>
            <select :value="editor.transport" @change="selectTransport">
              <option value="stdio">stdio</option><option value="http">HTTP</option><option value="socket">socket</option>
            </select>
          </label>
          <label class="model-field mcp-disabled-field"><span>{{ tr("settings.mcpStatus") }}</span><span class="mcp-checkbox"><input v-model="editor.disabled" type="checkbox" @change="updateDefinitionFromFields" />{{ tr("settings.mcpDisabled") }}</span></label>
          <label v-if="editor.transport === 'stdio'" class="model-field"><span>{{ tr("settings.mcpCommand") }}</span><input v-model="editor.command" spellcheck="false" placeholder="npx" @change="updateDefinitionFromFields" /></label>
          <label v-if="editor.transport === 'stdio'" class="model-field"><span>{{ tr("settings.mcpArgs") }}</span><input v-model="editor.args" spellcheck="false" placeholder='["-y", "package"]' @change="updateDefinitionFromFields" /></label>
          <label v-if="editor.transport === 'http'" class="model-field model-field-wide"><span>URL</span><input v-model="editor.url" spellcheck="false" placeholder="https://example.com/mcp" @change="updateDefinitionFromFields" /></label>
          <label v-if="editor.transport === 'socket'" class="model-field model-field-wide"><span>Socket</span><input v-model="editor.socket" spellcheck="false" placeholder="/path/to/mcp.sock" @change="updateDefinitionFromFields" /></label>
          <label class="model-field model-field-wide">
            <span>{{ tr("settings.mcpAdvancedJson") }}</span>
            <textarea v-model="editor.definition" spellcheck="false" @blur="parseDefinition" />
            <small>{{ tr("settings.mcpAdvancedHelp") }}</small>
          </label>
        </div>
        <p class="prompt-reload-note">{{ appStore.activeThread?.started ? tr("settings.mcpRestartNeeded") : tr("settings.mcpReadyOnStart") }}</p>
        <p v-if="formError" class="form-error">{{ formError }}</p>
        <p v-if="notice" class="setting-status">{{ notice }}</p>
        <footer class="model-editor-footer">
          <button v-if="isExisting" class="text-button danger" type="button" :disabled="saving" @click="void deleteServer()"><Trash2 :size="14" />{{ deleteArmed ? tr("settings.confirmDeleteMcp") : tr("settings.deleteMcp") }}</button>
          <span />
          <button class="text-button primary" type="submit" :disabled="saving || !dirty"><Save :size="14" />{{ saving ? tr("settings.savingMcp") : tr("settings.saveMcp") }}</button>
        </footer>
      </form>
    </div>
  </div>
</template>
