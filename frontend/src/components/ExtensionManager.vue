<script setup lang="ts">
import { AlertTriangle, CheckCircle2, Download, Package, Puzzle, RefreshCw, Trash2, XCircle } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import { PiExtensionOrigin } from "../../bindings/pi-desk/internal/domain";
import { tr } from "../i18n";
import { piExtensionService, type PiExtensionSnapshot } from "../services/extensions";

const snapshot = ref<PiExtensionSnapshot>();
const loading = ref(true);
const changing = ref(false);
const loadError = ref("");
const notice = ref("");
const removeArmed = ref(false);

const extensions = computed(() => snapshot.value?.extensions ?? []);

function originLabel(origin: string) {
  if (origin === PiExtensionOrigin.PiExtensionOriginGlobal) return tr("settings.extensionOriginGlobal");
  if (origin === PiExtensionOrigin.PiExtensionOriginSettings) return tr("settings.extensionOriginSettings");
  return tr("settings.extensionOriginPackage");
}

async function loadExtensions() {
  loading.value = true;
  loadError.value = "";
  try {
    snapshot.value = await piExtensionService.list();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    loading.value = false;
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

onMounted(() => { void loadExtensions(); });
</script>

<template>
  <div class="settings-content extension-config-content">
    <div class="settings-content-header">
      <div>
        <h3>{{ tr("settings.extensionManagement") }}</h3>
        <span>{{ tr("settings.extensionManagementHelp") }}</span>
      </div>
      <button class="icon-button" type="button" :title="tr('common.refresh')" :disabled="loading || changing" @click="void loadExtensions()">
        <RefreshCw :size="14" :class="{ 'is-spinning': loading }" />
      </button>
    </div>

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
            class="text-button primary"
            type="button"
            :disabled="loading || changing"
            @click="void installTodo()"
          >
            <Download :size="14" />{{ snapshot?.todo.updateAvailable ? tr("settings.updateExtension") : tr("settings.installExtension") }}
          </button>
          <button
            v-else
            data-testid="remove-todo-extension"
            class="text-button danger"
            type="button"
            :disabled="changing"
            @click="void removeTodo()"
          >
            <Trash2 :size="14" />{{ removeArmed ? tr("settings.confirmRemoveExtension") : tr("settings.removeExtension") }}
          </button>
        </div>
      </div>
      <p v-if="snapshot?.todo.legacyInstalled" class="extension-warning"><AlertTriangle :size="14" />{{ tr("settings.legacyTodoExtensionWarning", { path: snapshot.todo.legacyPath || "" }) }}</p>
      <p class="setting-status">{{ tr("settings.extensionRestartNeeded") }}</p>
    </section>

    <p v-if="notice" class="setting-status is-success">{{ notice }}</p>
    <p v-if="loadError" class="form-error">{{ loadError }}</p>

    <section class="installed-extensions" aria-labelledby="installed-extensions-title">
      <header>
        <strong id="installed-extensions-title">{{ tr("settings.installedExtensions") }}</strong>
        <span>{{ extensions.length }}</span>
      </header>
      <div v-if="loading" class="settings-empty compact"><RefreshCw :size="17" class="is-spinning" /><span>{{ tr("settings.loadingExtensions") }}</span></div>
      <div v-else-if="extensions.length" class="extension-list">
        <div v-for="extension in extensions" :key="`${extension.origin}-${extension.path || extension.source}`" class="resource-row">
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
      <div v-else class="settings-empty compact"><XCircle :size="17" /><span>{{ tr("settings.noInstalledExtensions") }}</span></div>
    </section>

    <p class="extension-storage-note">{{ tr("settings.extensionStorageHelp", { directory: snapshot?.globalDirectory || "~/.pi/agent/extensions", settings: snapshot?.settingsPath || "~/.pi/agent/settings.json" }) }}</p>
  </div>
</template>
