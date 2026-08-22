<script setup lang="ts">
import { System } from "@wailsio/runtime";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
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
import PaneResizer from "./components/PaneResizer.vue";
import SettingsDialog from "./components/SettingsDialog.vue";
import WindowControls from "./components/WindowControls.vue";
import {
  MAX_INSPECTOR_WIDTH,
  MAX_SIDEBAR_WIDTH,
  MIN_INSPECTOR_WIDTH,
  MIN_SIDEBAR_WIDTH,
  useAppStore,
} from "./stores/app";

const appStore = useAppStore();
const isWindows = ref(System.IsWindows());
const windowTitle = computed(() => appStore.activeExtensionTitle || appStore.activeThread?.title || "Pi Desk");

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

async function initializeDesktop() {
  await appStore.initialize();
  if (window.innerWidth < 1280) appStore.inspectorOpen = false;
}

onMounted(() => {
  window.addEventListener("beforeunload", persistDesktopState);
  void detectWindows();
  void initializeDesktop();
});

onBeforeUnmount(() => {
  window.removeEventListener("beforeunload", persistDesktopState);
  persistDesktopState();
});

watch(windowTitle, (title) => {
  document.title = title === "Pi Desk" ? title : `${title} - Pi Desk`;
}, { immediate: true });

watch(() => appStore.appearance, syncDocumentTheme, { immediate: true });
</script>

<template>
  <div
    v-if="!appStore.desktopStateReady"
    class="app-shell startup-shell"
    :data-theme="appStore.appearance"
    :style="{
      '--sidebar-width': `${appStore.sidebarWidth}px`,
      '--inspector-width': `${appStore.inspectorWidth}px`,
    }"
    role="status"
    aria-label="Pi Desk"
  >
    <span class="startup-mark">Pi</span>
  </div>
  <div
    v-else
    class="app-shell"
    :data-theme="appStore.appearance"
    :style="{
      '--sidebar-width': `${appStore.sidebarWidth}px`,
      '--inspector-width': `${appStore.inspectorWidth}px`,
    }"
    :class="{
      'is-windows': isWindows,
      'is-sidebar-collapsed': appStore.sidebarCollapsed,
      'is-inspector-closed': !appStore.inspectorOpen,
      'is-inspector-open': appStore.inspectorOpen,
    }"
  >
    <AppTopbar />
    <AppSidebar />
    <PaneResizer
      v-if="!appStore.sidebarCollapsed"
      side="left"
      :value="appStore.sidebarWidth"
      :min="MIN_SIDEBAR_WIDTH"
      :max="MAX_SIDEBAR_WIDTH"
      label="Resize task sidebar"
      @resize="appStore.setSidebarWidth($event)"
      @commit="appStore.setSidebarWidth($event, true)"
    />
    <main class="workspace-shell">
      <ConversationPane />
    </main>
    <InspectorPanel v-if="appStore.inspectorOpen" />
    <PaneResizer
      v-if="appStore.inspectorOpen"
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
