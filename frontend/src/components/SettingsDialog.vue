<script setup lang="ts">
import { ui } from "../ui/classes";
import { BarChart3, BookOpen, Boxes, Copy, Database, Download, ExternalLink, FileText, Info, PlugZap, Puzzle, RefreshCw, RotateCw, Search, Settings2, X } from "lucide-vue-next";
import { computed, ref } from "vue";
import { PiMaintenanceAction, type PiMaintenanceResult } from "../../bindings/pi-desk/internal/domain";
import { useModalFocus } from "../composables/useModalFocus";
import { maintainPi } from "../services/desktop";
import { useAppStore, type QueueMode, type SettingsSection, type SlashCommand } from "../stores/app";
import { tr } from "../i18n";
import ModelManager from "./ModelManager.vue";
import ExtensionManager from "./ExtensionManager.vue";
import PromptTemplateManager from "./PromptTemplateManager.vue";
import SkillManager from "./SkillManager.vue";
import McpManager from "./McpManager.vue";
import SessionStatistics from "./SessionStatistics.vue";

const appStore = useAppStore();
const copied = ref(false);
const section = computed<SettingsSection>({
  get: () => appStore.settingsSection,
  set: (value) => { appStore.settingsSection = value; },
});
const resourceSource = ref<"all" | SlashCommand["source"]>("all");
const resourceQuery = ref("");
const runtimeLoading = ref(false);
const runtimeError = ref("");
const maintenanceAction = ref<PiMaintenanceAction | null>(null);
const maintenanceLoading = ref(false);
const maintenanceError = ref("");
const maintenanceResult = ref<PiMaintenanceResult | null>(null);
const dialog = ref<HTMLElement | null>(null);
useModalFocus(dialog, () => appStore.closeSettings());
const filteredResources = computed(() => {
  const query = resourceQuery.value.trim().toLocaleLowerCase();
  return appStore.activeCommands.filter((command) => {
    if (resourceSource.value !== "all" && command.source !== resourceSource.value) return false;
    return !query || `${command.name} ${command.description ?? ""} ${command.path ?? ""}`.toLocaleLowerCase().includes(query);
  });
});
const resourceCounts = computed(() => ({
  all: appStore.activeCommands.length,
  skill: appStore.activeCommands.filter((command) => command.source === "skill").length,
  extension: appStore.activeCommands.filter((command) => command.source === "extension").length,
  prompt: appStore.activeCommands.filter((command) => command.source === "prompt").length,
}));
const runtimeReady = computed(() => appStore.bootstrap?.runtime.state === "ready");
const runtimeMissing = computed(() => appStore.bootstrap?.runtime.state === "missing");

async function copyRuntimePath() {
  const path = appStore.bootstrap?.runtime.command;
  if (!path) return;
  await navigator.clipboard.writeText(path);
  copied.value = true;
  window.setTimeout(() => { copied.value = false; }, 1200);
}

async function refreshRuntimeResources() {
  const thread = appStore.activeThread;
  if (!thread?.started || runtimeLoading.value) return;
  runtimeLoading.value = true;
  runtimeError.value = "";
  try {
    await Promise.all([
      appStore.refreshModels(thread.id),
      appStore.refreshThinkingLevels(thread.id),
      appStore.refreshCommands(thread.id),
    ]);
  } catch (cause) {
    runtimeError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    runtimeLoading.value = false;
  }
}

async function checkForUpdates() {
  await appStore.checkForUpdates();
}

function maintenanceActionLabel(action: PiMaintenanceAction) {
  switch (action) {
    case PiMaintenanceAction.PiInstall: return tr("settings.installPi");
    case PiMaintenanceAction.PiUpdateSelf: return tr("settings.updatePi");
    default: return "";
  }
}

function requestPiMaintenance(action: PiMaintenanceAction) {
  if (maintenanceLoading.value) return;
  maintenanceAction.value = action;
  maintenanceError.value = "";
  maintenanceResult.value = null;
}

function maintenanceErrorMessage(cause: unknown): string {
  const message = cause instanceof Error ? cause.message : String(cause);
  return /Pi sessions|session is starting|could not be closed/i.test(message)
    ? tr("settings.maintenanceStopFailed")
    : message;
}

async function confirmPiMaintenance() {
  const action = maintenanceAction.value;
  if (!action || maintenanceLoading.value) return;
  if (appStore.piMaintenanceBusy) {
    maintenanceError.value = tr("settings.maintenanceBusy");
    return;
  }
  maintenanceLoading.value = true;
  maintenanceError.value = "";
  try {
    if (!await appStore.stopAllSessions()) throw new Error(tr("settings.maintenanceStopFailed"));
    const result = await maintainPi(action);
    maintenanceResult.value = result;
    if (appStore.bootstrap) appStore.bootstrap.runtime = result.runtime;
    maintenanceAction.value = null;
  } catch (cause) {
    maintenanceError.value = maintenanceErrorMessage(cause);
  } finally {
    maintenanceLoading.value = false;
  }
}

function updateMessage() {
  const result = appStore.updateCheckResult;
  if (!result) return tr("settings.notChecked");
  if (result.status === "available") return tr("settings.available", { version: result.latestVersion || "" });
  return result.message;
}

async function updateRuntimeBehavior(operation: () => Promise<void>) {
  runtimeLoading.value = true;
  runtimeError.value = "";
  try {
    await operation();
  } catch (cause) {
    runtimeError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    runtimeLoading.value = false;
  }
}

function queueMode(event: Event): QueueMode {
  return (event.target as HTMLSelectElement).value as QueueMode;
}

function sourceIcon(source: SlashCommand["source"]) {
  if (source === "skill") return BookOpen;
  if (source === "extension") return Puzzle;
  return FileText;
}

function settingsSectionLabel(value: SettingsSection) {
  switch (value) {
    case "general": return tr("settings.general");
    case "modelManagement": return tr("settings.modelManagement");
    case "promptManagement": return tr("settings.promptManagement");
    case "skillManagement": return tr("settings.skillManagement");
    case "extensionManagement": return tr("settings.extensionManagement");
    case "mcpManagement": return tr("settings.mcpManagement");
    case "statistics": return tr("settings.statistics");
    case "resources": return tr("settings.runtimeResources");
  }
}
</script>

<template>
  <div class="dialog-backdrop" :class="ui.dialogBackdrop" @mousedown.self="appStore.closeSettings()">
    <section
      ref="dialog"
      class="dialog-window settings-dialog"
      :class="[ui.dialog, ui.dialogWide, ui.settingsControls]"
      role="dialog"
      aria-modal="true"
      aria-labelledby="settings-title"
      tabindex="-1"
    >
      <header :class="ui.dialogHeader">
        <div class="settings-title-path">
          <h2 id="settings-title">{{ tr("settings.title") }}</h2>
          <span aria-hidden="true">/</span>
          <strong>{{ settingsSectionLabel(section) }}</strong>
        </div>
        <button class="icon-button" :class="ui.iconButton" type="button" :title="tr('settings.close')" @click="appStore.closeSettings()"><X :size="17" /></button>
      </header>
      <div class="dialog-body settings-layout">
        <nav class="settings-nav" :aria-label="tr('settings.sections')">
          <button type="button" :class="{ 'is-active': section === 'general' }" @click="section = 'general'"><Settings2 :size="15" /><span>{{ tr("settings.general") }}</span></button>
          <button type="button" :class="{ 'is-active': section === 'modelManagement' }" @click="section = 'modelManagement'"><Database :size="15" /><span>{{ tr("settings.modelManagement") }}</span></button>
          <button type="button" :class="{ 'is-active': section === 'promptManagement' }" @click="section = 'promptManagement'"><FileText :size="15" /><span>{{ tr("settings.promptManagement") }}</span></button>
          <button type="button" :class="{ 'is-active': section === 'skillManagement' }" @click="section = 'skillManagement'"><BookOpen :size="15" /><span>{{ tr("settings.skillManagement") }}</span></button>
          <button type="button" :class="{ 'is-active': section === 'extensionManagement' }" @click="section = 'extensionManagement'"><Puzzle :size="15" /><span>{{ tr("settings.extensionManagement") }}</span></button>
          <button type="button" :class="{ 'is-active': section === 'mcpManagement' }" @click="section = 'mcpManagement'"><PlugZap :size="15" /><span>{{ tr("settings.mcpManagement") }}</span></button>
          <button type="button" :class="{ 'is-active': section === 'statistics' }" @click="section = 'statistics'"><BarChart3 :size="15" /><span>{{ tr("settings.statistics") }}</span></button>
          <button type="button" :class="{ 'is-active': section === 'resources' }" @click="section = 'resources'"><Boxes :size="15" /><span>{{ tr("settings.runtimeResources") }}</span></button>
          <button class="settings-nav-about" type="button" @click="appStore.closeSettings(); appStore.openAbout()"><Info :size="15" /><span>{{ tr("appMenu.about") }}</span></button>
        </nav>

        <div v-if="section === 'general'" class="settings-content settings-sections" :class="ui.settingsSections">
          <section>
            <h3>{{ tr("settings.appearance") }}</h3>
            <label class="setting-row setting-row-select" :class="ui.row">
              <span><strong>{{ tr("settings.theme") }}</strong><small>{{ tr("settings.themeHelp") }}</small></span>
              <select class="appearance-select !w-32 !basis-32" :class="ui.select" v-model="appStore.appearance" :aria-label="tr('settings.theme')" @change="appStore.appearanceChanged()">
                <option value="dark">{{ tr("settings.dark") }}</option>
                <option value="light">{{ tr("settings.light") }}</option>
                <option value="system">{{ tr("settings.system") }}</option>
              </select>
            </label>
            <label class="setting-row setting-row-select" :class="ui.row">
              <span><strong>{{ tr("settings.language") }}</strong><small>{{ tr("settings.languageHelp") }}</small></span>
              <select class="appearance-select !w-32 !basis-32" :class="ui.select" v-model="appStore.language" :aria-label="tr('settings.language')" @change="appStore.languageChanged()">
                <option value="zh-CN">{{ tr("settings.chinese") }}</option>
                <option value="en">{{ tr("settings.english") }}</option>
              </select>
            </label>
          </section>
          <section class="runtime-settings">
            <h3>{{ tr("settings.runtime") }}</h3>
            <dl>
              <div><dt>Pi</dt><dd>{{ appStore.bootstrap?.runtime.version || tr("common.unavailable") }}</dd></div>
              <div><dt>Wails</dt><dd>{{ appStore.bootstrap?.wailsVersion || "-" }}</dd></div>
              <div><dt>Pi Desk</dt><dd>{{ appStore.bootstrap?.appVersion || "-" }}</dd></div>
              <div class="runtime-path"><dt>{{ tr("settings.command") }}</dt><dd :title="appStore.bootstrap?.runtime.command">{{ appStore.bootstrap?.runtime.command || tr("common.notFound") }}</dd></div>
            </dl>
            <div class="settings-actions">
              <button class="text-button" :class="ui.button" type="button" :disabled="!appStore.bootstrap?.runtime.command" @click="copyRuntimePath"><Copy :size="14" />{{ copied ? tr("settings.copied") : tr("settings.copyPath") }}</button>
              <button class="text-button" :class="ui.button" type="button" :disabled="appStore.runtimeCheckLoading" @click="appStore.checkRuntime"><RotateCw :size="14" :class="{ 'is-spinning': appStore.runtimeCheckLoading }" />{{ tr("settings.recheck") }}</button>
              <button v-if="runtimeReady" data-testid="update-pi" class="text-button" :class="ui.button" type="button" :disabled="maintenanceLoading" @click="requestPiMaintenance(PiMaintenanceAction.PiUpdateSelf)"><RefreshCw :size="14" />{{ tr("settings.updatePi") }}</button>
              <button v-else-if="runtimeMissing" data-testid="install-pi" class="text-button primary" :class="ui.buttonPrimary" type="button" :disabled="maintenanceLoading" @click="requestPiMaintenance(PiMaintenanceAction.PiInstall)"><Download :size="14" />{{ tr("settings.installPi") }}</button>
            </div>
            <div v-if="maintenanceAction" class="maintenance-confirm" role="alert">
              <p><strong>{{ tr("settings.maintenanceConfirmTitle", { action: maintenanceActionLabel(maintenanceAction) }) }}</strong><span>{{ tr("settings.maintenanceConfirmHelp") }}</span></p>
              <div class="settings-actions">
                <button class="text-button" :class="ui.button" type="button" :disabled="maintenanceLoading" @click="maintenanceAction = null">{{ tr("common.cancel") }}</button>
                <button data-testid="confirm-pi-maintenance" class="text-button primary" :class="ui.buttonPrimary" type="button" :disabled="maintenanceLoading" @click="void confirmPiMaintenance()"><RefreshCw v-if="maintenanceLoading" :size="14" class="is-spinning" />{{ maintenanceLoading ? tr("settings.maintainingPi") : tr("common.confirm") }}</button>
              </div>
            </div>
            <p v-if="maintenanceError" class="form-error">{{ maintenanceError }}</p>
            <div v-if="maintenanceResult" class="maintenance-result">
              <strong>{{ tr("settings.maintenanceComplete", { action: maintenanceActionLabel(maintenanceResult.action) }) }}</strong>
              <code v-if="maintenanceResult.command">{{ maintenanceResult.command }}</code>
              <pre v-if="maintenanceResult.output">{{ maintenanceResult.output }}</pre>
            </div>
          </section>
          <section>
            <h3>{{ tr("settings.network") }}</h3>
            <label class="setting-row" :class="ui.row">
              <span><strong>{{ tr("settings.offline") }}</strong><small>{{ tr("settings.offlineHelp") }}</small></span>
              <input v-model="appStore.offlineMode" type="checkbox" @change="appStore.preferencesChanged()" />
            </label>
            <label class="setting-row" :class="ui.row">
              <span><strong>{{ tr("settings.proxy") }}</strong><small>{{ tr("settings.proxyHelp") }}</small></span>
              <input v-model="appStore.proxyEnabled" type="checkbox" @change="appStore.preferencesChanged()" />
            </label>
            <label v-if="appStore.proxyEnabled" for="proxy-url">{{ tr("settings.proxyUrl") }}</label>
            <input :class="ui.input" v-if="appStore.proxyEnabled" id="proxy-url" v-model="appStore.proxyURL" class="settings-input" spellcheck="false" @change="appStore.preferencesChanged()" />
          </section>
          <section>
            <h3>{{ tr("settings.tasks") }}</h3>
            <label class="setting-row setting-row-select" :class="ui.row">
              <span><strong>{{ tr("settings.steeringQueue") }}</strong><small>{{ tr("settings.steeringQueueHelp") }}</small></span>
              <select :class="ui.select"
                aria-label="Steering queue processing"
                :value="appStore.activeSessionState?.steeringMode || 'one-at-a-time'"
                :disabled="!appStore.activeThread?.started || runtimeLoading"
                @change="void updateRuntimeBehavior(() => appStore.setSteeringMode(queueMode($event)))"
              >
                <option value="one-at-a-time">{{ tr("settings.onePerTurn") }}</option>
                <option value="all">{{ tr("settings.allQueued") }}</option>
              </select>
            </label>
            <label class="setting-row" :class="ui.row">
              <span><strong>{{ tr("settings.autoCompaction") }}</strong><small>{{ tr("settings.autoCompactionHelp") }}</small></span>
              <input
                type="checkbox"
                :checked="appStore.activeSessionState?.autoCompactionEnabled ?? true"
                :disabled="!appStore.activeThread?.started || runtimeLoading"
                @change="void updateRuntimeBehavior(() => appStore.setAutoCompaction(($event.target as HTMLInputElement).checked))"
              />
            </label>
            <label class="setting-row" :class="ui.row">
              <span><strong>{{ tr("settings.autoRetry") }}</strong><small>{{ tr("settings.autoRetryHelp") }}</small></span>
              <input
                type="checkbox"
                :checked="appStore.activeAutoRetryEnabled"
                :disabled="!appStore.activeThread?.started || runtimeLoading"
                @change="void updateRuntimeBehavior(() => appStore.setAutoRetry(($event.target as HTMLInputElement).checked))"
              />
            </label>
            <p v-if="!appStore.activeThread?.started" class="setting-status">{{ tr("settings.startForRuntime") }}</p>
          </section>
          <section>
            <h3>{{ tr("settings.desktop") }}</h3>
            <label class="setting-row" :class="ui.row">
              <span><strong>{{ tr("settings.notifications") }}</strong><small>{{ tr("settings.notificationsHelp") }}</small></span>
              <input v-model="appStore.notificationsEnabled" type="checkbox" @change="appStore.preferencesChanged()" />
            </label>
            <label class="setting-row" :class="ui.row">
              <span><strong>{{ tr("settings.updates") }}</strong><small>{{ tr("settings.updatesHelp") }}</small></span>
              <input v-model="appStore.updateChecksEnabled" type="checkbox" @change="appStore.preferencesChanged()" />
            </label>
            <div class="settings-actions update-actions">
              <button class="text-button" :class="ui.button" type="button" :disabled="appStore.updateCheckLoading" @click="void checkForUpdates()"><RefreshCw :size="14" :class="{ 'is-spinning': appStore.updateCheckLoading }" />{{ tr("settings.checkNow") }}</button>
              <a v-if="appStore.updateCheckResult?.url" class="text-button" :class="ui.button" :href="appStore.updateCheckResult.url" target="_blank" rel="noreferrer"><ExternalLink :size="14" />{{ tr("settings.release") }}</a>
            </div>
            <p class="setting-status" :class="{ 'is-warning': appStore.updateCheckResult?.status === 'available', 'is-error': appStore.updateCheckResult?.status === 'error' }">{{ updateMessage() }}</p>
          </section>
          <p v-if="appStore.settingsError" class="form-error">{{ appStore.settingsError }}</p>
        </div>

        <ModelManager v-else-if="section === 'modelManagement'" />

        <PromptTemplateManager v-else-if="section === 'promptManagement'" />

        <SkillManager v-else-if="section === 'skillManagement'" />

        <ExtensionManager v-else-if="section === 'extensionManagement'" />

        <McpManager v-else-if="section === 'mcpManagement'" />

        <SessionStatistics v-else-if="section === 'statistics'" />

        <div v-else class="settings-content runtime-settings-content">
          <div class="settings-content-header" :class="ui.settingsHeader">
            <div><h3>{{ tr("settings.runtimeResources") }}</h3><span>{{ tr("settings.runtimeResourcesHelp") }}</span></div>
            <button class="icon-button" :class="ui.iconButton" type="button" title="Refresh resources" :disabled="!appStore.activeThread?.started || runtimeLoading" @click="void refreshRuntimeResources()"><RefreshCw :size="14" :class="{ 'is-spinning': runtimeLoading }" /></button>
          </div>
          <div class="resource-filters" role="tablist" aria-label="Resource type">
            <button v-for="source in (['all', 'skill', 'extension', 'prompt'] as const)" :key="source" :class="ui.tab" type="button" role="tab" :aria-selected="resourceSource === source" @click="resourceSource = source">
              {{ source === "all" ? tr("settings.all") : source === "skill" ? tr("settings.skills") : source === "extension" ? tr("settings.extensions") : tr("settings.prompts") }}
              <span>{{ resourceCounts[source] }}</span>
            </button>
          </div>
          <label class="resource-search"><Search :size="14" /><input :class="ui.input" v-model="resourceQuery" type="search" :placeholder="tr('settings.filterResources')" :aria-label="tr('settings.filterResources')" /></label>
          <div v-if="!appStore.activeThread?.started" class="settings-empty" :class="ui.empty"><Boxes :size="18" /><span>{{ tr("settings.piNotRunning") }}</span></div>
          <div v-else-if="filteredResources.length" class="resource-list">
            <div v-for="resource in filteredResources" :key="`${resource.source}-${resource.name}-${resource.path}`" class="resource-row" :class="ui.listItem">
              <component :is="sourceIcon(resource.source)" :size="15" />
              <span><strong>/{{ resource.name }}</strong><small>{{ resource.description || resource.source }}</small><code v-if="resource.path" :title="resource.path">{{ resource.path }}</code></span>
              <em>{{ resource.location || resource.source }}</em>
            </div>
          </div>
          <div v-else class="settings-empty" :class="ui.empty"><Boxes :size="18" /><span>{{ tr("settings.noResources") }}</span></div>
          <p v-if="runtimeError" class="form-error">{{ runtimeError }}</p>
        </div>
      </div>
    </section>
  </div>
</template>
