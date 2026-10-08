<script setup lang="ts">
import { ui } from "../ui/classes";
import { AlertTriangle, Bot, CheckCircle2, Download, Goal, Globe, Monitor, Puzzle, Trash2 } from "lucide-vue-next";
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { tr } from "../i18n";
import { piExtensionService, type PiExtensionSnapshot } from "../services/extensions";

const snapshot = ref<PiExtensionSnapshot>();
const loading = ref(true);
const changing = ref(false);
const loadError = ref("");
const notice = ref("");
const removeArmed = ref("");
let removeTimer: ReturnType<typeof setTimeout> | undefined;

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

async function changeExtension(extension: typeof extensionCards.value[number], removing = false) {
  if (changing.value) return;
  if (removing && removeArmed.value !== extension.id) {
    removeArmed.value = extension.id;
    clearTimeout(removeTimer);
    removeTimer = setTimeout(() => { removeArmed.value = ""; }, 5000);
    return;
  }
  changing.value = true; loadError.value = ""; notice.value = "";
  try {
    const result = await (removing ? extension.remove() : extension.install());
    notice.value = tr(extension.id === "todo" && !removing && (result as { replacedLegacy?: boolean } | undefined)?.replacedLegacy
      ? "settings.todoExtensionMigrated" : `settings.${extension.noticePrefix}Extension${removing ? "Removed" : "Installed"}`);
    removeArmed.value = ""; clearTimeout(removeTimer);
    await loadExtensions();
  } catch (cause) { loadError.value = cause instanceof Error ? cause.message : String(cause); }
  finally { changing.value = false; }
}
onBeforeUnmount(() => clearTimeout(removeTimer));

const extensionCards = computed(() => [
  { id: "todo", name: "Pi Desk Todo", help: tr("settings.todoExtensionHelp"), path: snapshot.value?.todo.path, icon: Puzzle, installed: Boolean(snapshot.value?.todo.installed), updateAvailable: Boolean(snapshot.value?.todo.updateAvailable), noticePrefix: "todo", armed: removeArmed.value === "todo", busy: loading.value || changing.value, install: piExtensionService.installTodo, remove: piExtensionService.removeTodo },
  { id: "goal", name: "Pi Desk Goal", help: tr("settings.goalExtensionHelp"), path: snapshot.value?.goal.path, icon: Goal, installed: Boolean(snapshot.value?.goal.installed), updateAvailable: Boolean(snapshot.value?.goal.updateAvailable), noticePrefix: "goal", armed: removeArmed.value === "goal", busy: loading.value || changing.value, install: piExtensionService.installGoal, remove: piExtensionService.removeGoal },
  { id: "computer-use", name: "Pi Desk Computer Use", help: tr("settings.computerUseExtensionHelp"), path: snapshot.value?.computerUse.path, icon: Monitor, installed: Boolean(snapshot.value?.computerUse.installed), updateAvailable: Boolean(snapshot.value?.computerUse.updateAvailable), noticePrefix: "computerUse", armed: removeArmed.value === "computer-use", busy: loading.value || changing.value, install: piExtensionService.installComputerUse, remove: piExtensionService.removeComputerUse },
  { id: "subagents", name: "Pi Desk Subagents", help: tr("settings.subagentsExtensionHelp"), path: snapshot.value?.subagents.path, icon: Bot, installed: Boolean(snapshot.value?.subagents.installed), updateAvailable: Boolean(snapshot.value?.subagents.updateAvailable), noticePrefix: "subagents", armed: removeArmed.value === "subagents", busy: loading.value || changing.value, install: piExtensionService.installSubagents, remove: piExtensionService.removeSubagents },
  { id: "browser", name: "Pi Desk Browser", help: tr("settings.browserExtensionHelp"), path: snapshot.value?.browser.path, icon: Globe, installed: Boolean(snapshot.value?.browser.installed), updateAvailable: Boolean(snapshot.value?.browser.updateAvailable), noticePrefix: "browser", armed: removeArmed.value === "browser", busy: loading.value || changing.value, install: piExtensionService.installBrowser, remove: piExtensionService.removeBrowser },
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
              :disabled="extension.busy || (extension.installed && !extension.updateAvailable)"
              @click="void changeExtension(extension)"
            >
              <CheckCircle2 v-if="extension.installed" :size="14" />
              <Download v-else :size="14" />
              {{ extension.updateAvailable ? tr("settings.updateExtension") : extension.installed ? tr("settings.extensionInstalled") : tr("settings.installExtension") }}
            </button>
            <button
              :data-testid="`remove-${extension.id}-extension`"
              class="text-button danger"
              :class="ui.buttonDanger"
              type="button"
              :disabled="extension.busy || !extension.installed"
              @click="void changeExtension(extension, true)"
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
