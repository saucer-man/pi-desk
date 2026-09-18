<script setup lang="ts">
import { ui } from "../ui/classes";
import { AlertTriangle, Bot, Cable, CheckCircle2, Download, Goal, Globe, Monitor, Package, Puzzle, RefreshCw, Trash2, XCircle } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { PiExtensionOrigin, PiPackageScope } from "../../bindings/pi-desk/internal/domain";
import { tr } from "../i18n";
import { piExtensionService, type PiExtensionSnapshot, type PiPackageSnapshot, type PiPackageSummary } from "../services/extensions";
import { useAppStore } from "../stores/app";

const appStore = useAppStore();
const snapshot = ref<PiExtensionSnapshot>();
const packageSnapshot = ref<PiPackageSnapshot>();
const loading = ref(true);
const changing = ref(false);
const loadError = ref("");
const notice = ref("");
const removeArmed = ref(false);
const goalRemoveArmed = ref(false);
const computerUseRemoveArmed = ref(false);
const subagentsRemoveArmed = ref(false);
const browserRemoveArmed = ref(false);
const packageSource = ref("");
const packageScope = ref(PiPackageScope.PiPackageScopeGlobal);
const packageBusy = ref("");

const extensions = computed(() => (snapshot.value?.extensions ?? []).filter((extension) => extension.origin !== PiExtensionOrigin.PiExtensionOriginPackage));
const packages = computed(() => packageSnapshot.value?.packages ?? []);
const workspacePath = computed(() => appStore.activeThread?.workspacePath ?? "");
const mcpAdapterPackageSource = "npm:pi-mcp-adapter";
const mcpAdapterPackage = computed(() => packages.value.find((pkg) => pkg.source.toLowerCase().includes("pi-mcp-adapter")));

function originLabel(origin: string) {
  if (origin === PiExtensionOrigin.PiExtensionOriginGlobal) return tr("settings.extensionOriginGlobal");
  if (origin === PiExtensionOrigin.PiExtensionOriginSettings) return tr("settings.extensionOriginSettings");
  return tr("settings.extensionOriginPackage");
}

async function loadExtensions() {
  loading.value = true;
  loadError.value = "";
  try {
    [snapshot.value, packageSnapshot.value] = await Promise.all([
      piExtensionService.list(),
      piExtensionService.listPackages(workspacePath.value),
    ]);
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    loading.value = false;
  }
}

function packageRequest(pkg?: PiPackageSummary) {
  return {
    source: pkg?.source ?? packageSource.value.trim(),
    scope: pkg?.scope ?? packageScope.value,
    workspacePath: workspacePath.value,
  };
}

async function installPackage() {
  const request = packageRequest();
  if (!request.source || packageBusy.value) return;
  packageBusy.value = `install:${request.scope}:${request.source}`;
  loadError.value = "";
  notice.value = "";
  try {
    await piExtensionService.installPackage(request);
    packageSource.value = "";
    notice.value = tr("settings.packageInstalled");
    await loadExtensions();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    packageBusy.value = "";
  }
}

async function updatePackage(pkg: PiPackageSummary) {
  if (packageBusy.value) return;
  packageBusy.value = `update:${pkg.scope}:${pkg.source}`;
  loadError.value = "";
  try {
    await piExtensionService.updatePackage(packageRequest(pkg));
    notice.value = tr("settings.packageUpdated");
    await loadExtensions();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    packageBusy.value = "";
  }
}

async function removePackage(pkg: PiPackageSummary) {
  if (packageBusy.value || !window.confirm(tr("settings.confirmRemovePackage", { source: pkg.source }))) return;
  packageBusy.value = `remove:${pkg.scope}:${pkg.source}`;
  loadError.value = "";
  try {
    await piExtensionService.removePackage(packageRequest(pkg));
    notice.value = tr("settings.packageRemoved");
    await loadExtensions();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    packageBusy.value = "";
  }
}

async function setPackageEnabled(pkg: PiPackageSummary) {
  if (packageBusy.value) return;
  packageBusy.value = `toggle:${pkg.scope}:${pkg.source}`;
  loadError.value = "";
  try {
    await piExtensionService.setPackageEnabled({ ...packageRequest(pkg), enabled: !pkg.enabled });
    notice.value = pkg.enabled ? tr("settings.packageDisabled") : tr("settings.packageEnabled");
    await loadExtensions();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    packageBusy.value = "";
  }
}

async function installMcpAdapter() {
  if (packageBusy.value) return;
  packageBusy.value = `install:${PiPackageScope.PiPackageScopeGlobal}:${mcpAdapterPackageSource}`;
  loadError.value = "";
  notice.value = "";
  try {
    await piExtensionService.installPackage({ source: mcpAdapterPackageSource, scope: PiPackageScope.PiPackageScopeGlobal, workspacePath: workspacePath.value });
    notice.value = tr("settings.packageInstalled");
    await loadExtensions();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    packageBusy.value = "";
  }
}

async function removeMcpAdapter() {
  const pkg = mcpAdapterPackage.value;
  if (!pkg || packageBusy.value || !window.confirm(tr("settings.confirmRemovePackage", { source: pkg.source }))) return;
  packageBusy.value = `remove:${pkg.scope}:${pkg.source}`;
  loadError.value = "";
  try {
    await piExtensionService.removePackage(packageRequest(pkg));
    notice.value = tr("settings.packageRemoved");
    await loadExtensions();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    packageBusy.value = "";
  }
}

async function installTodo() {
  if (changing.value) return;
  changing.value = true;
  loadError.value = "";
  notice.value = "";
  try {
    const result = await piExtensionService.installTodo();
    notice.value = result.replacedLegacy
      ? tr("settings.todoExtensionMigrated")
      : tr("settings.todoExtensionInstalled");
    await loadExtensions();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    changing.value = false;
  }
}

async function removeTodo() {
  if (changing.value) return;
  if (!removeArmed.value) {
    removeArmed.value = true;
    window.setTimeout(() => { removeArmed.value = false; }, 5000);
    return;
  }
  changing.value = true;
  loadError.value = "";
  notice.value = "";
  try {
    await piExtensionService.removeTodo();
    notice.value = tr("settings.todoExtensionRemoved");
    removeArmed.value = false;
    await loadExtensions();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    changing.value = false;
  }
}

async function installGoal() {
  if (changing.value) return;
  changing.value = true;
  loadError.value = "";
  notice.value = "";
  try {
    await piExtensionService.installGoal();
    notice.value = tr("settings.goalExtensionInstalled");
    await loadExtensions();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    changing.value = false;
  }
}

async function removeGoal() {
  if (changing.value) return;
  if (!goalRemoveArmed.value) {
    goalRemoveArmed.value = true;
    window.setTimeout(() => { goalRemoveArmed.value = false; }, 5000);
    return;
  }
  changing.value = true;
  loadError.value = "";
  notice.value = "";
  try {
    await piExtensionService.removeGoal();
    notice.value = tr("settings.goalExtensionRemoved");
    goalRemoveArmed.value = false;
    await loadExtensions();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    changing.value = false;
  }
}

async function installComputerUse() {
  if (changing.value) return;
  changing.value = true;
  loadError.value = "";
  notice.value = "";
  try {
    await piExtensionService.installComputerUse();
    notice.value = tr("settings.computerUseExtensionInstalled");
    await loadExtensions();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    changing.value = false;
  }
}

async function removeComputerUse() {
  if (changing.value) return;
  if (!computerUseRemoveArmed.value) {
    computerUseRemoveArmed.value = true;
    window.setTimeout(() => { computerUseRemoveArmed.value = false; }, 5000);
    return;
  }
  changing.value = true;
  loadError.value = "";
  notice.value = "";
  try {
    await piExtensionService.removeComputerUse();
    notice.value = tr("settings.computerUseExtensionRemoved");
    computerUseRemoveArmed.value = false;
    await loadExtensions();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    changing.value = false;
  }
}

async function installSubagents() {
  if (changing.value) return;
  changing.value = true;
  loadError.value = "";
  notice.value = "";
  try {
    await piExtensionService.installSubagents();
    notice.value = tr("settings.subagentsExtensionInstalled");
    await loadExtensions();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    changing.value = false;
  }
}

async function removeSubagents() {
  if (changing.value) return;
  if (!subagentsRemoveArmed.value) {
    subagentsRemoveArmed.value = true;
    window.setTimeout(() => { subagentsRemoveArmed.value = false; }, 5000);
    return;
  }
  changing.value = true;
  loadError.value = "";
  notice.value = "";
  try {
    await piExtensionService.removeSubagents();
    notice.value = tr("settings.subagentsExtensionRemoved");
    subagentsRemoveArmed.value = false;
    await loadExtensions();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    changing.value = false;
  }
}

async function installBrowser() {
  if (changing.value) return;
  changing.value = true;
  loadError.value = "";
  notice.value = "";
  try {
    await piExtensionService.installBrowser();
    notice.value = tr("settings.browserExtensionInstalled");
    await loadExtensions();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    changing.value = false;
  }
}

async function removeBrowser() {
  if (changing.value) return;
  if (!browserRemoveArmed.value) {
    browserRemoveArmed.value = true;
    window.setTimeout(() => { browserRemoveArmed.value = false; }, 5000);
    return;
  }
  changing.value = true;
  loadError.value = "";
  notice.value = "";
  try {
    await piExtensionService.removeBrowser();
    notice.value = tr("settings.browserExtensionRemoved");
    browserRemoveArmed.value = false;
    await loadExtensions();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    changing.value = false;
  }
}

onMounted(() => { void loadExtensions(); });
</script>

<template>
  <div class="settings-content model-config-content extension-config-content" :class="ui.settingsContent">
    <div class="settings-fill-body">
      <section class="extension-recommended" aria-labelledby="todo-extension-title">
      <div class="extension-feature-row">
        <Puzzle :size="18" />
        <span>
          <strong id="todo-extension-title">Pi Desk Todo</strong>
          <small>{{ tr("settings.todoExtensionHelp") }}</small>
          <code :title="snapshot?.todo.path">{{ snapshot?.todo.path }}</code>
        </span>
        <em v-if="snapshot?.todo.installed && !snapshot.todo.updateAvailable" class="is-installed"><CheckCircle2 :size="12" />{{ tr("settings.extensionInstalled") }}</em>
        <em v-else-if="snapshot?.todo.updateAvailable" class="is-update"><AlertTriangle :size="12" />{{ tr("settings.extensionUpdateAvailable") }}</em>
        <em v-else>{{ tr("settings.extensionNotInstalled") }}</em>
        <div class="extension-feature-actions">
          <button
            v-if="!snapshot?.todo.installed || snapshot.todo.updateAvailable"
            data-testid="install-todo-extension"
            class="text-button primary" :class="ui.buttonPrimary"
            type="button"
            :disabled="loading || changing"
            @click="void installTodo()"
          >
            <Download :size="14" />{{ snapshot?.todo.updateAvailable ? tr("settings.updateExtension") : tr("settings.installExtension") }}
          </button>
          <button
            v-else
            data-testid="remove-todo-extension"
            class="text-button danger" :class="ui.buttonDanger"
            type="button"
            :disabled="changing"
            @click="void removeTodo()"
          >
            <Trash2 :size="14" />{{ removeArmed ? tr("settings.confirmRemoveExtension") : tr("settings.removeExtension") }}
          </button>
        </div>
      </div>
      <p v-if="snapshot?.todo.legacyInstalled" class="extension-warning"><AlertTriangle :size="14" />{{ tr("settings.legacyTodoExtensionWarning", { path: snapshot.todo.legacyPath || "" }) }}</p>
      <div class="extension-feature-row" data-testid="goal-extension-row">
        <Goal :size="18" />
        <span>
          <strong>Pi Desk Goal</strong>
          <small>{{ tr("settings.goalExtensionHelp") }}</small>
          <code :title="snapshot?.goal.path">{{ snapshot?.goal.path }}</code>
        </span>
        <em v-if="snapshot?.goal.installed && !snapshot.goal.updateAvailable" class="is-installed"><CheckCircle2 :size="12" />{{ tr("settings.extensionInstalled") }}</em>
        <em v-else-if="snapshot?.goal.updateAvailable" class="is-update"><AlertTriangle :size="12" />{{ tr("settings.extensionUpdateAvailable") }}</em>
        <em v-else>{{ tr("settings.extensionNotInstalled") }}</em>
        <div class="extension-feature-actions">
          <button
            v-if="!snapshot?.goal.installed || snapshot.goal.updateAvailable"
            data-testid="install-goal-extension"
            class="text-button primary" :class="ui.buttonPrimary"
            type="button"
            :disabled="loading || changing"
            @click="void installGoal()"
          >
            <Download :size="14" />{{ snapshot?.goal.updateAvailable ? tr("settings.updateExtension") : tr("settings.installExtension") }}
          </button>
          <button
            v-else
            data-testid="remove-goal-extension"
            class="text-button danger" :class="ui.buttonDanger"
            type="button"
            :disabled="changing"
            @click="void removeGoal()"
          >
            <Trash2 :size="14" />{{ goalRemoveArmed ? tr("settings.confirmRemoveExtension") : tr("settings.removeExtension") }}
          </button>
        </div>
      </div>
      <div class="extension-feature-row" data-testid="computer-use-extension-row">
        <Monitor :size="18" />
        <span>
          <strong>Pi Desk Computer Use</strong>
          <small>{{ tr("settings.computerUseExtensionHelp") }}</small>
          <code :title="snapshot?.computerUse.path">{{ snapshot?.computerUse.path }}</code>
        </span>
        <em v-if="snapshot?.computerUse.installed && !snapshot.computerUse.updateAvailable" class="is-installed"><CheckCircle2 :size="12" />{{ tr("settings.extensionInstalled") }}</em>
        <em v-else-if="snapshot?.computerUse.updateAvailable" class="is-update"><AlertTriangle :size="12" />{{ tr("settings.extensionUpdateAvailable") }}</em>
        <em v-else>{{ tr("settings.extensionNotInstalled") }}</em>
        <div class="extension-feature-actions">
          <button
            v-if="!snapshot?.computerUse.installed || snapshot.computerUse.updateAvailable"
            data-testid="install-computer-use-extension"
            class="text-button primary" :class="ui.buttonPrimary"
            type="button"
            :disabled="loading || changing"
            @click="void installComputerUse()"
          >
            <Download :size="14" />{{ snapshot?.computerUse.updateAvailable ? tr("settings.updateExtension") : tr("settings.installExtension") }}
          </button>
          <button
            v-else
            data-testid="remove-computer-use-extension"
            class="text-button danger" :class="ui.buttonDanger"
            type="button"
            :disabled="changing"
            @click="void removeComputerUse()"
          >
            <Trash2 :size="14" />{{ computerUseRemoveArmed ? tr("settings.confirmRemoveExtension") : tr("settings.removeExtension") }}
          </button>
        </div>
      </div>
      <div class="extension-feature-row" data-testid="subagents-extension-row">
        <Bot :size="18" />
        <span>
          <strong>Pi Desk Subagents</strong>
          <small>{{ tr("settings.subagentsExtensionHelp") }}</small>
          <code :title="snapshot?.subagents.path">{{ snapshot?.subagents.path }}</code>
        </span>
        <em v-if="snapshot?.subagents.installed && !snapshot.subagents.updateAvailable" class="is-installed"><CheckCircle2 :size="12" />{{ tr("settings.extensionInstalled") }}</em>
        <em v-else-if="snapshot?.subagents.updateAvailable" class="is-update"><AlertTriangle :size="12" />{{ tr("settings.extensionUpdateAvailable") }}</em>
        <em v-else>{{ tr("settings.extensionNotInstalled") }}</em>
        <div class="extension-feature-actions">
          <button
            v-if="!snapshot?.subagents.installed || snapshot.subagents.updateAvailable"
            data-testid="install-subagents-extension"
            class="text-button primary" :class="ui.buttonPrimary"
            type="button"
            :disabled="loading || changing"
            @click="void installSubagents()"
          >
            <Download :size="14" />{{ snapshot?.subagents.updateAvailable ? tr("settings.updateExtension") : tr("settings.installExtension") }}
          </button>
          <button
            v-else
            data-testid="remove-subagents-extension"
            class="text-button danger" :class="ui.buttonDanger"
            type="button"
            :disabled="changing"
            @click="void removeSubagents()"
          >
            <Trash2 :size="14" />{{ subagentsRemoveArmed ? tr("settings.confirmRemoveExtension") : tr("settings.removeExtension") }}
          </button>
        </div>
      </div>
      <div class="extension-feature-row" data-testid="browser-extension-row">
        <Globe :size="18" />
        <span>
          <strong>Pi Desk Browser</strong>
          <small>{{ tr("settings.browserExtensionHelp") }}</small>
          <code :title="snapshot?.browser.path">{{ snapshot?.browser.path }}</code>
        </span>
        <em v-if="snapshot?.browser.installed && !snapshot.browser.updateAvailable" class="is-installed"><CheckCircle2 :size="12" />{{ tr("settings.extensionInstalled") }}</em>
        <em v-else-if="snapshot?.browser.updateAvailable" class="is-update"><AlertTriangle :size="12" />{{ tr("settings.extensionUpdateAvailable") }}</em>
        <em v-else>{{ tr("settings.extensionNotInstalled") }}</em>
        <div class="extension-feature-actions">
          <button
            v-if="!snapshot?.browser.installed || snapshot.browser.updateAvailable"
            data-testid="install-browser-extension"
            class="text-button primary" :class="ui.buttonPrimary"
            type="button"
            :disabled="loading || changing"
            @click="void installBrowser()"
          >
            <Download :size="14" />{{ snapshot?.browser.updateAvailable ? tr("settings.updateExtension") : tr("settings.installExtension") }}
          </button>
          <button
            v-else
            data-testid="remove-browser-extension"
            class="text-button danger" :class="ui.buttonDanger"
            type="button"
            :disabled="changing"
            @click="void removeBrowser()"
          >
            <Trash2 :size="14" />{{ browserRemoveArmed ? tr("settings.confirmRemoveExtension") : tr("settings.removeExtension") }}
          </button>
        </div>
      </div>
      <div class="extension-feature-row" data-testid="mcp-adapter-extension-row">
        <Cable :size="18" />
        <span>
          <strong>pi-mcp-adapter</strong>
          <small>{{ tr("settings.mcpAdapterExtensionHelp") }}</small>
          <code :title="mcpAdapterPackage?.source || mcpAdapterPackageSource">{{ mcpAdapterPackage?.source || mcpAdapterPackageSource }}</code>
        </span>
        <em v-if="mcpAdapterPackage?.enabled" class="is-installed"><CheckCircle2 :size="12" />{{ tr("settings.extensionInstalled") }}</em>
        <em v-else-if="mcpAdapterPackage" class="is-update">{{ tr("settings.mcpDisabled") }}</em>
        <em v-else>{{ tr("settings.extensionNotInstalled") }}</em>
        <div class="extension-feature-actions">
          <button
            v-if="!mcpAdapterPackage"
            data-testid="install-mcp-adapter"
            class="text-button primary" :class="ui.buttonPrimary"
            type="button"
            :disabled="Boolean(packageBusy)"
            @click="void installMcpAdapter()"
          >
            <Download :size="14" />{{ packageBusy ? tr("settings.installingPackage" ) : tr("settings.installExtension") }}
          </button>
          <button
            v-else
            data-testid="remove-mcp-adapter"
            class="text-button danger" :class="ui.buttonDanger"
            type="button"
            :disabled="Boolean(packageBusy)"
            @click="void removeMcpAdapter()"
          >
            <Trash2 :size="14" />{{ tr("settings.removeExtension") }}
          </button>
        </div>
      </div>
      <p class="setting-status">{{ tr("settings.extensionRestartNeeded") }}</p>
      </section>

      <p v-if="notice" class="setting-status is-success">{{ notice }}</p>
      <p v-if="loadError" class="form-error">{{ loadError }}</p>

      <section class="installed-extensions" aria-labelledby="installed-extensions-title">
      <header>
        <strong id="installed-extensions-title">{{ tr("settings.installedExtensions") }}</strong>
        <span>{{ packages.length + extensions.length }}</span>
      </header>
      <div class="extension-package-install">
        <input :class="ui.input" v-model="packageSource" type="text" spellcheck="false" :placeholder="tr('settings.packageSource')" @keydown.enter.prevent="void installPackage()" />
        <select :class="ui.select" v-model="packageScope" :aria-label="tr('settings.packageScope')">
          <option :value="PiPackageScope.PiPackageScopeGlobal">{{ tr("settings.packageScopeGlobal") }}</option>
          <option :value="PiPackageScope.PiPackageScopeProject" :disabled="!packageSnapshot?.projectEnabled">{{ tr("settings.packageScopeProject") }}</option>
        </select>
        <button class="text-button primary" :class="ui.buttonPrimary" type="button" :disabled="!packageSource.trim() || Boolean(packageBusy)" @click="void installPackage()"><Download :size="14" />{{ tr("settings.installPackage") }}</button>
      </div>
      <p v-if="packageSnapshot?.projectNotice" class="setting-status">{{ packageSnapshot.projectNotice }}</p>
      <div v-if="loading" class="settings-empty compact" :class="ui.empty"><RefreshCw :size="17" class="is-spinning" /><span>{{ tr("settings.loadingExtensions") }}</span></div>
      <div v-else-if="packages.length || extensions.length" class="extension-list">
        <div v-for="pkg in packages" :key="`${pkg.scope}-${pkg.source}`" class="resource-row package-row" :class="ui.listItem">
          <Package :size="15" />
          <span><strong>{{ pkg.source }}</strong><small>{{ pkg.scope === PiPackageScope.PiPackageScopeProject ? tr("settings.packageScopeProject") : tr("settings.packageScopeGlobal") }}</small></span>
          <div class="extension-feature-actions">
            <button class="text-button" :class="ui.button" type="button" :disabled="Boolean(packageBusy)" @click="void setPackageEnabled(pkg)">{{ pkg.enabled ? tr("settings.disablePackage") : tr("settings.enablePackage") }}</button>
            <button class="text-button" :class="ui.button" type="button" :disabled="Boolean(packageBusy)" @click="void updatePackage(pkg)">{{ tr("settings.updateExtension") }}</button>
            <button class="icon-button danger" :class="ui.iconButton" type="button" :title="tr('settings.removePackage')" :disabled="Boolean(packageBusy)" @click="void removePackage(pkg)"><Trash2 :size="14" /></button>
          </div>
        </div>
        <div v-for="extension in extensions" :key="`${extension.origin}-${extension.path || extension.source}`" class="resource-row" :class="ui.listItem">
          <Package v-if="extension.origin === PiExtensionOrigin.PiExtensionOriginPackage" :size="15" />
          <Puzzle v-else :size="15" />
          <span>
            <strong>{{ extension.name }}</strong>
            <small>{{ extension.source }}</small>
            <code v-if="extension.path" :title="extension.path">{{ extension.path }}</code>
          </span>
          <em>{{ originLabel(extension.origin) }}</em>
        </div>
      </div>
      <div v-else class="settings-empty compact" :class="ui.empty"><XCircle :size="17" /><span>{{ tr("settings.noInstalledExtensions") }}</span></div>
      </section>

      <p class="extension-storage-note">{{ tr("settings.extensionStorageHelp", { directory: snapshot?.globalDirectory || "~/.pi/agent/extensions", settings: snapshot?.settingsPath || "~/.pi/agent/settings.json" }) }}</p>
    </div>
  </div>
</template>
