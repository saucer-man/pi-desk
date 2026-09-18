<script setup lang="ts">
import { System } from "@wailsio/runtime";
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from "vue";
import AboutDialog from "./components/AboutDialog.vue";
import AppSidebar from "./components/AppSidebar.vue";
import AppTopbar from "./components/AppTopbar.vue";
import BranchDialog from "./components/BranchDialog.vue";
import ConversationPane from "./components/ConversationPane.vue";
import DeleteSessionDialog from "./components/DeleteSessionDialog.vue";
import ExtensionDialog from "./components/ExtensionDialog.vue";
import ExportResultDialog from "./components/ExportResultDialog.vue";
import InspectorPanel from "./components/InspectorPanel.vue";
import NewTaskDialog from "./components/NewTaskDialog.vue";
import OrphanSessionsDialog from "./components/OrphanSessionsDialog.vue";
import RemoteReconnectDialog from "./components/RemoteReconnectDialog.vue";
import ScheduledTasksPage from "./components/ScheduledTasksPage.vue";
import PaneResizer from "./components/PaneResizer.vue";
import WindowControls from "./components/WindowControls.vue";
import { tr } from "./i18n";
import {
  MAX_INSPECTOR_WIDTH,
  MAX_SIDEBAR_WIDTH,
  MIN_INSPECTOR_WIDTH,
  MIN_SIDEBAR_WIDTH,
  useAppStore,
} from "./stores/app";

const appStore = useAppStore();
const SettingsDialog = defineAsyncComponent(() => import("./components/SettingsDialog.vue"));
const isWindows = ref(System.IsWindows());
const windowTitle = computed(() => appStore.activePage === "scheduledTasks"
  ? tr("scheduledTasks.title")
  : appStore.activeExtensionTitle || appStore.activeThread?.title || "Pi Desk");

async function detectWindows() {
  if (isWindows.value) return;
  try {
    isWindows.value = (await System.Environment()).OS === "windows";
  } catch {
    isWindows.value = false;
  }
}

function persistDesktopState() {
  void appStore.persistDesktopState();
}

function syncDocumentTheme(theme: string) {
  document.documentElement.dataset.theme = theme;
}

function syncDocumentFont(font: string) {
  document.documentElement.dataset.fontFamily = font;
}

function syncDocumentFontSize(size: number) {
  document.documentElement.dataset.fontSize = String(size);
}

async function initializeDesktop() {
  await appStore.initialize();
  appStore.startScheduledTaskScheduler();
  void appStore.checkScheduledTasks();
  if (window.innerWidth < 1280) appStore.inspectorOpen = false;
}

onMounted(() => {
  window.addEventListener("beforeunload", persistDesktopState);
  void detectWindows();
  void initializeDesktop();
});

onBeforeUnmount(() => {
  window.removeEventListener("beforeunload", persistDesktopState);
  appStore.stopScheduledTaskScheduler();
  persistDesktopState();
});

watch(windowTitle, (title) => {
  document.title = title === "Pi Desk" ? title : `${title} - Pi Desk`;
}, { immediate: true });

watch(() => appStore.appearance, syncDocumentTheme, { immediate: true });
watch(() => appStore.interfaceFont, syncDocumentFont, { immediate: true });
watch(() => appStore.interfaceFontSize, syncDocumentFontSize, { immediate: true });
</script>

<template>
  <div
    v-if="!appStore.desktopStateReady"
    class="app-shell startup-shell relative grid h-full w-full grid-cols-1 grid-rows-1 place-items-center overflow-hidden bg-[var(--bg-app)] font-body text-[var(--text)] antialiased"
    :data-theme="appStore.appearance"
    :style="{
      '--sidebar-width': `${appStore.sidebarWidth}px`,
      '--inspector-width': `${appStore.inspectorWidth}px`,
    }"
    role="status"
    aria-label="Pi Desk"
  >
    <span class="startup-mark grid size-9 place-items-center rounded-lg border border-[var(--border-strong)] bg-[var(--bg-raised)] text-sm font-bold shadow-sm">Pi</span>
  </div>
  <div
    v-else
    class="app-shell relative grid h-full w-full grid-rows-[var(--topbar-height)_minmax(0,1fr)] overflow-hidden bg-[var(--bg-app)] font-body text-[var(--text)] antialiased max-[760px]:[grid-template-columns:var(--sidebar-collapsed-width)_minmax(0,1fr)]"
    :data-theme="appStore.appearance"
    :style="{
      '--sidebar-width': `${appStore.sidebarWidth}px`,
      '--inspector-width': `${appStore.inspectorWidth}px`,
    }"
    :class="{
      'is-windows': isWindows,
      'is-sidebar-collapsed': appStore.sidebarCollapsed,
      'is-inspector-closed': !appStore.inspectorOpen || appStore.activePage === 'scheduledTasks',
      'is-inspector-open': appStore.inspectorOpen && appStore.activePage === 'task',
      '[grid-template-columns:var(--sidebar-collapsed-width)_minmax(0,1fr)]': appStore.sidebarCollapsed,
      '[grid-template-columns:var(--sidebar-width)_minmax(0,1fr)]': !appStore.sidebarCollapsed,
    }"
  >
    <AppTopbar v-show="!appStore.settingsOpen" />
    <AppSidebar v-show="!appStore.settingsOpen" />
    <PaneResizer
      v-if="!appStore.settingsOpen && !appStore.sidebarCollapsed"
      side="left"
      :value="appStore.sidebarWidth"
      :min="MIN_SIDEBAR_WIDTH"
      :max="MAX_SIDEBAR_WIDTH"
      label="Resize task sidebar"
      @resize="appStore.setSidebarWidth($event)"
      @commit="appStore.setSidebarWidth($event, true)"
    />
    <main v-show="!appStore.settingsOpen" class="workspace-shell relative z-0 col-start-2 row-start-2 grid min-h-0 min-w-0 grid-rows-[minmax(0,1fr)] overflow-hidden bg-[var(--bg-workspace)]">
      <ScheduledTasksPage v-if="appStore.activePage === 'scheduledTasks'" />
      <ConversationPane v-else />
    </main>
    <InspectorPanel v-if="appStore.inspectorOpen && appStore.activePage === 'task'" v-show="!appStore.settingsOpen" />
    <PaneResizer
      v-if="!appStore.settingsOpen && appStore.inspectorOpen && appStore.activePage === 'task'"
      side="right"
      :value="appStore.inspectorWidth"
      :min="MIN_INSPECTOR_WIDTH"
      :max="MAX_INSPECTOR_WIDTH"
      label="Resize inspector"
      @resize="appStore.setInspectorWidth($event)"
      @commit="appStore.setInspectorWidth($event, true)"
    />
    <WindowControls :is-windows="isWindows" />
    <NewTaskDialog v-if="appStore.newTaskOpen" />
    <SettingsDialog v-if="appStore.settingsOpen" />
    <AboutDialog v-if="appStore.aboutOpen" />
    <OrphanSessionsDialog v-if="appStore.orphanSessionsOpen" />
    <RemoteReconnectDialog v-if="appStore.remoteReconnectOpen" />
    <BranchDialog v-if="appStore.branchPanelOpen" />
    <DeleteSessionDialog v-if="appStore.deleteDialogOpen" />
    <ExportResultDialog v-if="appStore.exportDialogOpen" />
    <ExtensionDialog v-if="appStore.activeThreadId && appStore.extensionRequestByThread[appStore.activeThreadId]" />
  </div>
</template>
