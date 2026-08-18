<script setup lang="ts">
import { Bug, Check, ChevronDown, ChevronRight, CircleHelp, Copy, Download, Ellipsis, FolderGit2, GitBranch, Info, PanelRightOpen, X } from "lucide-vue-next";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { toggleDebugMode } from "../services/desktop";
import { useAppStore } from "../stores/app";
import { tr } from "../i18n";

const appStore = useAppStore();
const menuOpen = ref(false);
const helpOpen = ref(false);
const workspaceApplicationMenuOpen = ref(false);
const renameOpen = ref(false);
const renameValue = ref("");
const menuButton = ref<HTMLButtonElement>();
const helpButton = ref<HTMLButtonElement>();
const workspaceApplicationButton = ref<HTMLButtonElement>();
const debugEnabled = ref(false);
const debugBusy = ref(false);
const debugLabel = computed(() => tr(debugEnabled.value ? "appMenu.closeDebugMode" : "appMenu.openDebugMode"));
const activeBranch = computed(() => appStore.activeRepository?.git.branch || "");
const activeWorkspaceApplication = computed(() => appStore.activeWorkspaceApplication);
const workspaceApplicationDisabled = computed(() => !activeWorkspaceApplication.value || appStore.activeThread?.trust !== "approve");
const workspaceApplicationTitle = computed(() => workspaceApplicationDisabled.value
  ? tr("topbar.trustToOpen")
  : tr("topbar.openWithApplication", { application: activeWorkspaceApplication.value?.name ?? "" }));

watch(() => appStore.activeThreadId, () => {
  menuOpen.value = false;
  helpOpen.value = false;
  workspaceApplicationMenuOpen.value = false;
  appStore.workspaceApplicationError = "";
  renameOpen.value = false;
});

function beginRename() {
  renameValue.value = appStore.activeThread?.title ?? "";
  renameOpen.value = true;
  menuOpen.value = false;
}

async function submitRename() {
  await appStore.renameActiveSession(renameValue.value);
  renameOpen.value = false;
}

function closeMenu() {
  if (!menuOpen.value) return;
  menuOpen.value = false;
  menuButton.value?.focus();
}

function closeHelp(restoreFocus = false) {
  helpOpen.value = false;
  if (restoreFocus) helpButton.value?.focus();
}

function closeWorkspaceApplicationMenu(restoreFocus = false) {
  workspaceApplicationMenuOpen.value = false;
  if (restoreFocus) workspaceApplicationButton.value?.focus();
}

async function openWorkspaceWith(applicationId = "") {
  closeWorkspaceApplicationMenu();
  await appStore.openActiveWorkspaceWith(applicationId);
}

function openAbout() {
  closeHelp();
  appStore.openAbout();
}

async function toggleDebug() {
  if (debugBusy.value) return;
  debugBusy.value = true;
  try {
    debugEnabled.value = await toggleDebugMode();
    closeHelp();
  } finally {
    debugBusy.value = false;
  }
}

function onDocumentPointerDown(event: PointerEvent) {
  const target = event.target;
  if (target instanceof Element && target.closest(".topbar-menu-anchor")) return;
  menuOpen.value = false;
  helpOpen.value = false;
  workspaceApplicationMenuOpen.value = false;
  appStore.workspaceApplicationError = "";
}

function onDocumentKeydown(event: KeyboardEvent) {
  if (event.key !== "Escape") return;
  if (helpOpen.value) {
    event.preventDefault();
    closeHelp(true);
  } else if (workspaceApplicationMenuOpen.value) {
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
              @click="workspaceApplicationMenuOpen = !workspaceApplicationMenuOpen; menuOpen = false; helpOpen = false"
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
          v-if="appStore.activeThread"
          class="icon-button session-branch-button"
          type="button"
          :title="tr('topbar.branches')"
          :disabled="!appStore.activeThread.sessionFile || Boolean(appStore.activeSessionOperation)"
          @click="void appStore.openBranchPanel()"
        >
          <GitBranch :size="17" />
        </button>
        <div v-if="appStore.activeThread" class="menu-anchor topbar-menu-anchor">
          <button ref="menuButton" class="icon-button" type="button" :title="tr('topbar.actions')" aria-haspopup="menu" :aria-expanded="menuOpen" @click="menuOpen = !menuOpen; helpOpen = false; workspaceApplicationMenuOpen = false">
            <Ellipsis :size="18" />
          </button>
          <div v-if="menuOpen" class="command-menu topbar-command-menu" role="menu" @keydown.esc.stop.prevent="closeMenu">
            <button type="button" role="menuitem" @click="beginRename">{{ tr("topbar.rename") }}</button>
            <button type="button" role="menuitem" :disabled="!appStore.activeThread.sessionFile || Boolean(appStore.activeSessionOperation)" @click="menuOpen = false; void appStore.cloneActiveSession()"><Copy :size="15" /> {{ tr("topbar.clone") }}</button>
            <button type="button" role="menuitem" :disabled="!appStore.activeThread.sessionFile || Boolean(appStore.activeSessionOperation)" @click="menuOpen = false; void appStore.exportActiveSession()"><Download :size="15" /> {{ tr("topbar.export") }}</button>
            <button type="button" role="menuitem" :disabled="!appStore.activeThread.started" @click="menuOpen = false; void appStore.compactActiveSession()">{{ tr("topbar.compact") }}</button>
            <button type="button" role="menuitem" :disabled="!appStore.activeThread.started" @click="menuOpen = false; void appStore.stopActiveSession()">{{ tr("topbar.closePi") }}</button>
          </div>
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
        <div class="menu-anchor topbar-menu-anchor help-menu-anchor">
          <button ref="helpButton" class="icon-button" type="button" :title="tr('appMenu.help')" aria-haspopup="menu" :aria-expanded="helpOpen" @click="helpOpen = !helpOpen; menuOpen = false; workspaceApplicationMenuOpen = false">
            <CircleHelp :size="17" />
          </button>
          <div v-if="helpOpen" class="app-menu-popover help-menu" role="menu" @keydown.esc.stop.prevent="closeHelp(true)">
            <button type="button" role="menuitem" :disabled="debugBusy" @click="void toggleDebug()"><Bug :size="15" /><span>{{ debugLabel }}</span></button>
            <button type="button" role="menuitem" @click="openAbout"><Info :size="15" /><span>{{ tr("appMenu.about") }}</span></button>
          </div>
        </div>
      </div>

      <form v-if="renameOpen" class="inline-dialog" role="dialog" :aria-label="tr('topbar.rename')" @submit.prevent="submitRename" @keydown.esc.stop.prevent="renameOpen = false">
        <label for="task-name">{{ tr("topbar.taskName") }}</label>
        <input id="task-name" v-model="renameValue" maxlength="200" autofocus />
        <button class="text-button" type="submit" :disabled="!renameValue.trim()">{{ tr("topbar.rename") }}</button>
        <button class="icon-button" type="button" :title="tr('topbar.cancelRename')" @click="renameOpen = false"><X :size="15" /></button>
      </form>
    </div>
  </header>
</template>
