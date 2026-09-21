<script setup lang="ts">
import { ui } from "../ui/classes";
import { AlertTriangle, Bot, Cable, CheckCircle2, Download, Goal, Globe, Monitor, Puzzle, Trash2 } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { PiPackageScope } from "../../bindings/pi-desk/internal/domain";
import { tr } from "../i18n";
import { piExtensionService, type PiExtensionSnapshot, type PiPackageSnapshot } from "../services/extensions";
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
const packageBusy = ref("");

const packages = computed(() => packageSnapshot.value?.packages ?? []);
const workspacePath = computed(() => appStore.activeThread?.workspacePath ?? "");
const mcpAdapterPackageSource = "npm:pi-mcp-adapter";
const mcpAdapterPackage = computed(() => packages.value.find((pkg) => pkg.source.toLowerCase().includes("pi-mcp-adapter")));

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
    await piExtensionService.removePackage({ source: pkg.source, scope: pkg.scope, workspacePath: workspacePath.value });
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

const extensionCards = computed(() => [
  { id: "todo", name: "Pi Desk Todo", help: tr("settings.todoExtensionHelp"), path: snapshot.value?.todo.path, icon: Puzzle, installed: Boolean(snapshot.value?.todo.installed), armed: removeArmed.value, busy: loading.value || changing.value, install: installTodo, remove: removeTodo },
  { id: "goal", name: "Pi Desk Goal", help: tr("settings.goalExtensionHelp"), path: snapshot.value?.goal.path, icon: Goal, installed: Boolean(snapshot.value?.goal.installed), armed: goalRemoveArmed.value, busy: loading.value || changing.value, install: installGoal, remove: removeGoal },
  { id: "computer-use", name: "Pi Desk Computer Use", help: tr("settings.computerUseExtensionHelp"), path: snapshot.value?.computerUse.path, icon: Monitor, installed: Boolean(snapshot.value?.computerUse.installed), armed: computerUseRemoveArmed.value, busy: loading.value || changing.value, install: installComputerUse, remove: removeComputerUse },
  { id: "subagents", name: "Pi Desk Subagents", help: tr("settings.subagentsExtensionHelp"), path: snapshot.value?.subagents.path, icon: Bot, installed: Boolean(snapshot.value?.subagents.installed), armed: subagentsRemoveArmed.value, busy: loading.value || changing.value, install: installSubagents, remove: removeSubagents },
  { id: "browser", name: "Pi Desk Browser", help: tr("settings.browserExtensionHelp"), path: snapshot.value?.browser.path, icon: Globe, installed: Boolean(snapshot.value?.browser.installed), armed: browserRemoveArmed.value, busy: loading.value || changing.value, install: installBrowser, remove: removeBrowser },
  { id: "mcp-adapter", name: "pi-mcp-adapter", help: tr("settings.mcpAdapterExtensionHelp"), path: mcpAdapterPackage.value?.source || mcpAdapterPackageSource, icon: Cable, installed: Boolean(mcpAdapterPackage.value), armed: false, busy: Boolean(packageBusy.value), install: installMcpAdapter, remove: removeMcpAdapter },
]);

onMounted(() => { void loadExtensions(); });
</script>

<template>
  <div class="settings-content model-config-content extension-config-content" :class="ui.settingsContent">
    <div class="settings-fill-body">
      <section class="extension-recommended" :aria-label="tr('settings.extensionManagement')">
        <div v-for="extension in extensionCards" :key="extension.id" class="extension-feature-row" :data-testid="`${extension.id}-extension-row`">
          <component :is="extension.icon" :size="18" aria-hidden="true" />
          <span>
            <strong>{{ extension.name }}</strong>
            <small>{{ extension.help }}</small>
            <code v-if="extension.path" :title="extension.path">{{ extension.path }}</code>
          </span>
          <div class="extension-feature-actions">
            <button
              :data-testid="`install-${extension.id}-extension`"
              class="text-button primary"
              :class="ui.buttonPrimary"
              type="button"
              :disabled="extension.busy || extension.installed"
              @click="void extension.install()"
            >
              <CheckCircle2 v-if="extension.installed" :size="14" />
              <Download v-else :size="14" />
              {{ extension.installed ? tr("settings.extensionInstalled") : tr("settings.installExtension") }}
            </button>
            <button
              :data-testid="`remove-${extension.id}-extension`"
              class="text-button danger"
              :class="ui.buttonDanger"
              type="button"
              :disabled="extension.busy || !extension.installed"
              @click="void extension.remove()"
            >
              <Trash2 :size="14" />{{ extension.armed ? tr("settings.confirmRemoveExtension") : tr("settings.removeExtension") }}
            </button>
          </div>
        </div>
      </section>
      <p v-if="snapshot?.todo.legacyInstalled" class="extension-warning"><AlertTriangle :size="14" />{{ tr("settings.legacyTodoExtensionWarning", { path: snapshot.todo.legacyPath || "" }) }}</p>
      <p class="setting-status">{{ tr("settings.extensionRestartNeeded") }}</p>
      <p v-if="notice" class="setting-status is-success">{{ notice }}</p>
      <p v-if="loadError" class="form-error">{{ loadError }}</p>
    </div>
  </div>
</template>
