<script setup lang="ts">
import {
  FileSearch,
  Folder,
  FolderOpen,
  MoreHorizontal,
  PanelLeftClose,
  PanelLeftOpen,
  Plus,
  Search,
  Settings,
  Play,
  Square,
  SquarePen,
  Trash2,
  X,
} from "lucide-vue-next";
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import RuntimeBadge from "./RuntimeBadge.vue";
import { useAppStore } from "../stores/app";
import { tr } from "../i18n";

const appStore = useAppStore();
const searchInput = ref<HTMLInputElement>();
const collapsedWorkspaceIDs = ref<Record<string, boolean>>({});
const taskMenu = ref({ open: false, threadId: "", x: 0, y: 0 });
const workspaceMenu = ref({ open: false, workspaceID: "", x: 0, y: 0 });
const workspaceActionID = ref("");
const workspaceActionError = ref("");
const taskMenuThread = computed(() => appStore.threads.find((thread) => thread.id === taskMenu.value.threadId));
const taskMenuWorkspace = computed(() => {
  const thread = taskMenuThread.value;
  if (!thread) return undefined;
  return appStore.workspaces.find((workspace) => comparablePath(workspace.path) === comparablePath(thread.workspacePath));
});
const workspaceMenuItem = computed(() => appStore.workspaces.find((workspace) => workspace.id === workspaceMenu.value.workspaceID));

function comparablePath(path: string): string {
  const normalized = path.replace(/[\\/]+$/, "").replaceAll("\\", "/");
  return /^[a-z]:\//i.test(normalized) ? normalized.toLocaleLowerCase() : normalized;
}

const workspaceGroups = computed(() => appStore.workspaces.map((workspace) => {
  const threads = appStore.filteredThreads.filter((thread) => comparablePath(thread.workspacePath) === comparablePath(workspace.path));
  return { workspace, threads, threadCount: threads.length };
}).filter((group) => !appStore.searchQuery.trim() || group.threadCount > 0));

function isWorkspaceCollapsed(workspaceID: string): boolean {
  return collapsedWorkspaceIDs.value[workspaceID] === true;
}

function toggleWorkspace(workspaceID: string) {
  collapsedWorkspaceIDs.value[workspaceID] = !isWorkspaceCollapsed(workspaceID);
}

async function createThreadInWorkspace(workspaceID: string, path: string, trust: "approve" | "deny") {
  if (workspaceActionID.value) return;
  workspaceActionID.value = workspaceID;
  workspaceActionError.value = "";
  try {
    await appStore.createThread(path, trust);
    if (appStore.activeThreadId) appStore.startThreadInBackground(appStore.activeThreadId);
  } catch (error) {
    workspaceActionError.value = error instanceof Error ? error.message : String(error);
  } finally {
    workspaceActionID.value = "";
  }
}

async function toggleSearch() {
  if (!appStore.searchOpen && appStore.sidebarCollapsed) appStore.toggleSidebar();
  appStore.toggleSearch();
  if (appStore.searchOpen) {
    await nextTick();
    searchInput.value?.focus();
  }
}

function closeTaskMenu() {
  taskMenu.value.open = false;
}

function closeWorkspaceMenu() {
  workspaceMenu.value.open = false;
}

function openTaskMenu(event: MouseEvent, threadId: string) {
  const width = 210;
  const height = 130;
  taskMenu.value = {
    open: true,
    threadId,
    x: Math.max(8, Math.min(event.clientX, window.innerWidth - width - 8)),
    y: Math.max(8, Math.min(event.clientY, window.innerHeight - height - 8)),
  };
}

async function openTaskWorkspace() {
  const workspace = taskMenuWorkspace.value;
  closeTaskMenu();
  if (!workspace) return;
  try {
    await appStore.openWorkspace(workspace.id);
  } catch (error) {
    workspaceActionError.value = error instanceof Error ? error.message : String(error);
  }
}

function openWorkspaceMenu(event: MouseEvent, workspaceID: string) {
  const width = 220;
  const height = 124;
  taskMenu.value.open = false;
  workspaceMenu.value = {
    open: true,
    workspaceID,
    x: Math.max(8, Math.min(event.clientX, window.innerWidth - width - 8)),
    y: Math.max(8, Math.min(event.clientY + 8, window.innerHeight - height - 8)),
  };
}

async function runWorkspaceAction(action: "open" | "remove") {
  const workspace = workspaceMenuItem.value;
  closeWorkspaceMenu();
  if (!workspace || workspaceActionID.value) return;
  workspaceActionID.value = workspace.id;
  workspaceActionError.value = "";
  try {
    if (action === "open") await appStore.openWorkspace(workspace.id);
    else await appStore.removeWorkspace(workspace.id);
  } catch (error) {
    workspaceActionError.value = error instanceof Error ? error.message : String(error);
  } finally {
    workspaceActionID.value = "";
  }
}

function onDocumentKeydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    closeTaskMenu();
    closeWorkspaceMenu();
  }
}

function onDocumentClick() {
  closeTaskMenu();
  closeWorkspaceMenu();
}

onMounted(() => {
  document.addEventListener("click", onDocumentClick);
  document.addEventListener("keydown", onDocumentKeydown);
});

onBeforeUnmount(() => {
  document.removeEventListener("click", onDocumentClick);
  document.removeEventListener("keydown", onDocumentKeydown);
});
</script>

<template>
  <aside class="sidebar" :aria-label="tr('sidebar.navigation')">
    <nav class="primary-nav" aria-label="Primary">
      <button v-if="!appStore.sidebarCollapsed" class="new-task-button" type="button" :title="tr('sidebar.newTask')" :aria-label="tr('sidebar.newTask')" @click="appStore.openNewTask">
        <SquarePen :size="17" />
        <span>{{ tr("sidebar.newTask") }}</span>
      </button>
      <div class="primary-nav-row">
        <button v-if="!appStore.sidebarCollapsed" class="primary-nav-search" type="button" :title="tr('sidebar.openSearch')" :aria-label="tr('sidebar.openSearch')" :aria-pressed="appStore.searchOpen" @click="toggleSearch">
          <Search :size="16" />
          <span>{{ tr("sidebar.search") }}</span>
        </button>
        <button
          class="sidebar-toggle"
          type="button"
          :title="appStore.sidebarCollapsed ? tr('sidebar.expand') : tr('sidebar.collapse')"
          :aria-label="appStore.sidebarCollapsed ? tr('sidebar.expand') : tr('sidebar.collapse')"
          @click="appStore.toggleSidebar"
        >
          <PanelLeftOpen v-if="appStore.sidebarCollapsed" :size="16" />
          <PanelLeftClose v-else :size="16" />
        </button>
      </div>
      <button v-if="!appStore.sidebarCollapsed" type="button" :title="tr('sidebar.review')" :aria-label="tr('sidebar.review')" :aria-pressed="appStore.inspectorOpen && appStore.inspectorTab === 'changes'" @click="appStore.toggleInspector('changes')">
        <FileSearch :size="16" />
        <span>{{ tr("sidebar.review") }}</span>
      </button>
    </nav>

    <div v-if="!appStore.sidebarCollapsed && appStore.searchOpen" class="sidebar-search">
      <Search :size="14" />
      <input ref="searchInput" v-model="appStore.searchQuery" type="search" :placeholder="tr('sidebar.searchTasks')" :aria-label="tr('sidebar.searchTasks')" />
      <button class="icon-button" type="button" :title="tr('sidebar.closeSearch')" @click="toggleSearch"><X :size="14" /></button>
    </div>
    <p v-if="!appStore.sidebarCollapsed && appStore.searchOpen" class="sidebar-search-help">{{ tr("sidebar.searchHelp") }}</p>

    <div v-if="!appStore.sidebarCollapsed" class="sidebar-section task-section">
      <div class="section-heading">
        <span>{{ tr("sidebar.workspaces") }}</span>
        <button class="icon-button" type="button" :title="tr('sidebar.addWorkspace')" @click="appStore.openNewTask">
          <Plus :size="15" />
        </button>
      </div>
      <p v-if="appStore.catalogLoading" class="sidebar-empty">{{ tr("sidebar.loading") }}</p>
      <p v-else-if="!appStore.catalogReady && appStore.catalogError" class="sidebar-empty error-text" :title="appStore.catalogError">{{ tr("sidebar.unavailable") }}</p>
      <div v-for="group in workspaceGroups" :key="group.workspace.id" class="workspace-group">
        <div class="workspace-header">
          <button
            type="button"
            class="workspace-row"
            :aria-expanded="!isWorkspaceCollapsed(group.workspace.id)"
            :title="group.workspace.path"
            @click="toggleWorkspace(group.workspace.id)"
          >
            <Folder :size="16" />
            <span>{{ group.workspace.name }}</span>
          </button>
          <button
            class="icon-button workspace-menu-button"
            type="button"
            :aria-label="tr('sidebar.workspaceActions', { workspace: group.workspace.name })"
            :title="tr('sidebar.workspaceActions', { workspace: group.workspace.name })"
            :disabled="Boolean(workspaceActionID)"
            @click.stop="openWorkspaceMenu($event, group.workspace.id)"
          ><MoreHorizontal :size="16" /></button>
        </div>
        <div v-if="!isWorkspaceCollapsed(group.workspace.id)" class="workspace-threads">
          <button
            v-for="thread in group.threads"
            :key="thread.id"
            class="thread-row"
            :class="{ 'is-active': appStore.activeThreadId === thread.id }"
            type="button"
            :title="thread.title"
            @click="appStore.selectThread(thread.id)"
            @contextmenu.prevent="openTaskMenu($event, thread.id)"
          >
            <span class="thread-title" :class="{ 'is-started': thread.started }">{{ thread.title }}</span>
            <span
              v-if="thread.status === 'running' || thread.status === 'starting'"
              class="thread-status"
              :data-state="thread.status"
              :aria-label="thread.status === 'starting' ? tr('sidebar.piStarting') : tr('sidebar.taskRunning')"
            />
            <span v-else-if="thread.unread" class="thread-unread" :aria-label="tr('sidebar.unread')" />
          </button>
          <p v-if="group.threads.length === 0" class="sidebar-empty">{{ tr("sidebar.noTasks") }}</p>
        </div>
      </div>
      <p v-if="workspaceActionError" class="sidebar-empty error-text" role="alert">{{ workspaceActionError }}</p>
      <p v-if="!appStore.catalogLoading && appStore.workspaces.length === 0" class="sidebar-empty">{{ tr("sidebar.noWorkspaces") }}</p>
      <p v-else-if="appStore.searchQuery.trim() && workspaceGroups.length === 0" class="sidebar-empty">{{ tr("sidebar.noMatches") }}</p>
    </div>

    <div v-if="!appStore.sidebarCollapsed" class="sidebar-footer">
      <RuntimeBadge />
      <button class="icon-button" type="button" :title="tr('sidebar.settings')" @click="appStore.openSettings()">
        <Settings :size="17" />
      </button>
    </div>

    <div
      v-if="!appStore.sidebarCollapsed && taskMenu.open && taskMenuThread"
      class="thread-context-menu"
      role="menu"
      :style="{ left: `${taskMenu.x}px`, top: `${taskMenu.y}px` }"
      @click.stop="closeTaskMenu"
      @contextmenu.prevent
    >
      <button type="button" role="menuitem" @click="void openTaskWorkspace()"><FolderOpen :size="14" />{{ tr("sidebar.openWorkspace") }}</button>
      <button v-if="taskMenuThread.started" type="button" role="menuitem" @click="void appStore.stopThread(taskMenuThread.id)"><Square :size="14" />{{ tr("sidebar.closePi") }}</button>
      <button v-else type="button" role="menuitem" @click="appStore.startThreadInBackground(taskMenuThread.id)"><Play :size="14" />{{ tr("sidebar.startPi") }}</button>
      <button type="button" role="menuitem" class="danger" @click="appStore.requestDeleteThread(taskMenuThread.id)"><Trash2 :size="14" />{{ tr("sidebar.delete") }}</button>
    </div>

    <div
      v-if="!appStore.sidebarCollapsed && workspaceMenu.open && workspaceMenuItem"
      class="thread-context-menu workspace-context-menu"
      role="menu"
      :style="{ left: `${workspaceMenu.x}px`, top: `${workspaceMenu.y}px` }"
      @click.stop="closeWorkspaceMenu"
      @contextmenu.prevent
    >
      <button
        type="button"
        role="menuitem"
        @click="void createThreadInWorkspace(workspaceMenuItem.id, workspaceMenuItem.path, workspaceMenuItem.trust)"
      ><SquarePen :size="15" />{{ tr("sidebar.newTask") }}</button>
      <button type="button" role="menuitem" @click="void runWorkspaceAction('open')"><FolderOpen :size="15" />{{ tr("sidebar.openWorkspace") }}</button>
      <button type="button" role="menuitem" class="danger" @click="void runWorkspaceAction('remove')"><Trash2 :size="15" />{{ tr("sidebar.removeWorkspace") }}</button>
    </div>
  </aside>
</template>
