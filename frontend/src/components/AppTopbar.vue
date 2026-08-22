<script setup lang="ts">
import { Check, ChevronDown, ChevronRight, FolderGit2, GitBranch, PanelLeftClose, PanelRightOpen } from "lucide-vue-next";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useAppStore } from "../stores/app";
import { tr } from "../i18n";

const appStore = useAppStore();
const workspaceApplicationMenuOpen = ref(false);
const workspaceApplicationButton = ref<HTMLButtonElement>();
const activeBranch = computed(() => appStore.activeRepository?.git.branch || "");
const activeWorkspaceApplication = computed(() => appStore.activeWorkspaceApplication);
const workspaceApplicationDisabled = computed(() => !activeWorkspaceApplication.value || appStore.activeThread?.trust !== "approve");
const workspaceApplicationTitle = computed(() => workspaceApplicationDisabled.value
  ? tr("topbar.trustToOpen")
  : tr("topbar.openWithApplication", { application: activeWorkspaceApplication.value?.name ?? "" }));

watch(() => appStore.activeThreadId, () => {
  workspaceApplicationMenuOpen.value = false;
  appStore.workspaceApplicationError = "";
});

function closeWorkspaceApplicationMenu(restoreFocus = false) {
  workspaceApplicationMenuOpen.value = false;
  if (restoreFocus) workspaceApplicationButton.value?.focus();
}

async function openWorkspaceWith(applicationId = "") {
  closeWorkspaceApplicationMenu();
  await appStore.openActiveWorkspaceWith(applicationId);
}

function onDocumentPointerDown(event: PointerEvent) {
  const target = event.target;
  if (target instanceof Element && target.closest(".topbar-menu-anchor")) return;
  workspaceApplicationMenuOpen.value = false;
  appStore.workspaceApplicationError = "";
}

function onDocumentKeydown(event: KeyboardEvent) {
  if (event.key !== "Escape") return;
  if (workspaceApplicationMenuOpen.value) {
    event.preventDefault();
    closeWorkspaceApplicationMenu(true);
  } else if (appStore.workspaceApplicationError) {
    event.preventDefault();
    appStore.workspaceApplicationError = "";
  }
}

onMounted(() => {
  document.addEventListener("pointerdown", onDocumentPointerDown);
  document.addEventListener("keydown", onDocumentKeydown);
});

onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", onDocumentPointerDown);
  document.removeEventListener("keydown", onDocumentKeydown);
});
</script>

<template>
  <header class="topbar">
    <div class="topbar-brand" aria-label="Pi Desk">
      <span class="topbar-brand-mark" aria-hidden="true">Pi</span>
      <strong>Pi Desk</strong>
      <button
        v-if="!appStore.sidebarCollapsed"
        class="icon-button topbar-sidebar-toggle"
        type="button"
        :title="tr('sidebar.collapse')"
        :aria-label="tr('sidebar.collapse')"
        @click="appStore.toggleSidebar"
      >
        <PanelLeftClose :size="16" />
      </button>
    </div>

    <div class="topbar-task">
      <div class="topbar-title-group">
        <strong :title="appStore.activeThread?.title || 'Pi Desk'">{{ appStore.activeThread?.title || "Pi Desk" }}</strong>
        <span v-if="appStore.activeExtensionTitle" class="extension-window-title" :title="appStore.activeExtensionTitle">{{ appStore.activeExtensionTitle }}</span>
        <span v-if="appStore.activeThread" class="workspace-chip" :title="appStore.activeThread.workspacePath">
          <FolderGit2 :size="14" />
          <span>{{ appStore.activeThread.workspace }}</span>
        </span>
        <span v-if="activeBranch" class="branch-chip" :title="activeBranch">
          <GitBranch :size="14" />
          <span>{{ activeBranch }}</span>
        </span>
      </div>

      <div class="topbar-actions">
        <div v-if="appStore.activeThread && !appStore.workspaceApplicationsLoading && activeWorkspaceApplication" class="menu-anchor topbar-menu-anchor workspace-application-anchor">
          <div class="workspace-application-split">
            <button
              class="icon-button workspace-application-primary"
              type="button"
              :title="workspaceApplicationTitle"
              :aria-label="workspaceApplicationTitle"
              :disabled="workspaceApplicationDisabled"
              @click="void openWorkspaceWith()"
            >
              <img class="workspace-application-icon workspace-application-icon-primary" :src="activeWorkspaceApplication.iconDataUrl" alt="" width="18" height="18" draggable="false" />
            </button>
            <button
              ref="workspaceApplicationButton"
              class="workspace-application-toggle"
              type="button"
              :title="tr('topbar.chooseApplication')"
              :aria-label="tr('topbar.chooseApplication')"
              aria-haspopup="menu"
              :aria-expanded="workspaceApplicationMenuOpen"
              :disabled="!appStore.workspaceApplications.length || appStore.activeThread.trust !== 'approve'"
              @click="workspaceApplicationMenuOpen = !workspaceApplicationMenuOpen"
            >
              <ChevronDown :size="12" />
            </button>
          </div>
          <div v-if="workspaceApplicationMenuOpen" class="command-menu workspace-application-menu" role="menu" @keydown.esc.stop.prevent="closeWorkspaceApplicationMenu(true)">
            <button
              v-for="application in appStore.workspaceApplications"
              :key="application.id"
              type="button"
              role="menuitemradio"
              :aria-checked="application.id === activeWorkspaceApplication?.id"
              :class="{ 'is-selected': application.id === activeWorkspaceApplication?.id }"
              @click="void openWorkspaceWith(application.id)"
            >
              <img class="workspace-application-icon workspace-application-icon-menu" :src="application.iconDataUrl" alt="" width="20" height="20" draggable="false" />
              <span>{{ application.name }}</span>
              <Check v-if="application.id === activeWorkspaceApplication?.id" class="workspace-application-check" :size="15" />
            </button>
          </div>
          <p v-else-if="appStore.workspaceApplicationError" class="workspace-application-error" role="alert">{{ appStore.workspaceApplicationError }}</p>
        </div>
        <button
          class="icon-button inspector-toggle"
          type="button"
          :title="appStore.inspectorOpen ? tr('topbar.closeInspector') : tr('topbar.openInspector')"
          @click="appStore.toggleInspector()"
        >
          <ChevronRight v-if="appStore.inspectorOpen" :size="18" />
          <PanelRightOpen v-else :size="18" />
        </button>
      </div>

    </div>
  </header>
</template>
