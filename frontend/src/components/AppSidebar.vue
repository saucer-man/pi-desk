<script setup lang="ts">
import { ui } from "../ui/classes";
import {
  Copy,
  Download,
  FileSearch,
  Folder,
  FolderOpen,
  GitBranch,
  MoreHorizontal,
  PanelLeftOpen,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Settings,
  Sparkles,
  Play,
  Square,
  SquarePen,
  Trash2,
  Unplug,
  X,
} from "lucide-vue-next";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import RuntimeBadge from "./RuntimeBadge.vue";
import RemoveWorkspaceDialog from "./RemoveWorkspaceDialog.vue";
import { useAppStore } from "../stores/app";
import { tr } from "../i18n";

const appStore = useAppStore();
const searchInput = ref<HTMLInputElement>();
const collapsedWorkspaceIDs = ref<Record<string, boolean>>({});
const taskMenu = ref({ open: false, threadId: "", x: 0, y: 0 });
const workspaceMenu = ref({ open: false, workspaceID: "", x: 0, y: 0 });
const workspaceActionID = ref("");
const workspaceActionError = ref("");
const workspaceRenameOpen = ref(false);
const workspaceRenameID = ref("");
const workspaceRenameValue = ref("");
const workspaceRenameInput = ref<HTMLInputElement>();
const workspaceRemovalID = ref("");
const taskRenameOpen = ref(false);
const taskRenameID = ref("");
const taskRenameValue = ref("");
const taskRenameInput = ref<HTMLInputElement>();
const taskMenuThread = computed(() => appStore.threads.find((thread) => thread.id === taskMenu.value.threadId));
const taskMenuWorkspace = computed(() => {
  const thread = taskMenuThread.value;
  if (!thread) return undefined;
  return appStore.workspaces.find((workspace) => thread.workspaceId
    ? workspace.id === thread.workspaceId
    : comparablePath(workspace.path) === comparablePath(thread.workspacePath));
});
const workspaceMenuItem = computed(() => appStore.workspaces.find((workspace) => workspace.id === workspaceMenu.value.workspaceID));
const workspaceRemovalItem = computed(() => appStore.workspaces.find((workspace) => workspace.id === workspaceRemovalID.value));

function comparablePath(path: string): string {
  const normalized = path.replace(/[\\/]+$/, "").replaceAll("\\", "/");
  return /^[a-z]:\//i.test(normalized) ? normalized.toLocaleLowerCase() : normalized;
}

const workspaceGroups = computed(() => appStore.workspaces.map((workspace) => {
  const threads = appStore.filteredThreads.filter((thread) => workspace.kind === "ssh"
    ? thread.workspaceId === workspace.id
    : comparablePath(thread.workspacePath) === comparablePath(workspace.path));
  return { workspace, threads, threadCount: threads.length };
}).filter((group) => !appStore.searchQuery.trim() || group.threadCount > 0));

watch(() => appStore.searchQuery, (query) => {
  if (query.trim()) void appStore.loadSessionSearchBodies();
});

function isWorkspaceCollapsed(workspaceID: string): boolean {
  return collapsedWorkspaceIDs.value[workspaceID] === true;
}

function toggleWorkspace(workspaceID: string) {
  collapsedWorkspaceIDs.value[workspaceID] = !isWorkspaceCollapsed(workspaceID);
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

async function beginTaskRename() {
  const thread = taskMenuThread.value;
  closeTaskMenu();
  if (!thread) return;
  taskRenameID.value = thread.id;
  taskRenameValue.value = thread.title;
  taskRenameOpen.value = true;
  await nextTick();
  taskRenameInput.value?.focus();
  taskRenameInput.value?.select();
}

function closeTaskRename() {
  taskRenameOpen.value = false;
  taskRenameID.value = "";
}

async function submitTaskRename() {
  const thread = appStore.threads.find((item) => item.id === taskRenameID.value);
  const name = taskRenameValue.value.trim();
  if (!thread || !name) return;
  if (appStore.activeThreadId !== thread.id) appStore.selectThread(thread.id);
  await appStore.renameActiveSession(name);
  closeTaskRename();
}

async function runTaskAction(action: "branch" | "clone" | "export" | "compact") {
  const thread = taskMenuThread.value;
  closeTaskMenu();
  if (!thread) return;
  if (appStore.activeThreadId !== thread.id) appStore.selectThread(thread.id);
  if (action === "branch") await appStore.openBranchPanel();
  else if (action === "clone") await appStore.cloneActiveSession();
  else if (action === "export") await appStore.exportActiveSession();
  else await appStore.compactActiveSession();
}

function closeWorkspaceMenu() {
  workspaceMenu.value.open = false;
}

async function openWorkspaceRename() {
  const workspace = workspaceMenuItem.value;
  closeWorkspaceMenu();
  if (!workspace || workspaceActionID.value) return;
  workspaceRenameID.value = workspace.id;
  workspaceRenameValue.value = workspace.name;
  workspaceRenameOpen.value = true;
  await nextTick();
  workspaceRenameInput.value?.focus();
  workspaceRenameInput.value?.select();
}

function closeWorkspaceRename() {
  if (workspaceActionID.value) return;
  workspaceRenameOpen.value = false;
  workspaceRenameID.value = "";
}

async function submitWorkspaceRename() {
  const name = workspaceRenameValue.value.trim();
  if (!workspaceRenameID.value || !name || workspaceActionID.value) return;
  workspaceActionID.value = workspaceRenameID.value;
  workspaceActionError.value = "";
  try {
    await appStore.renameWorkspace(workspaceRenameID.value, name);
    workspaceRenameOpen.value = false;
    workspaceRenameID.value = "";
  } catch (error) {
    workspaceActionError.value = error instanceof Error ? error.message : String(error);
  } finally {
    workspaceActionID.value = "";
  }
}

function openTaskMenu(event: MouseEvent, threadId: string) {
  const width = 210;
  const height = 300;
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

async function runWorkspaceAction(action: "newTask" | "open" | "disconnect") {
  const workspace = workspaceMenuItem.value;
  closeWorkspaceMenu();
  if (!workspace || workspaceActionID.value) return;
  workspaceActionID.value = workspace.id;
  workspaceActionError.value = "";
  try {
    if (action === "newTask") {
      if (workspace.kind === "ssh") await appStore.createRemoteTaskInWorkspace(workspace.id);
      else await appStore.createThread(workspace.path, workspace.trust);
      if (appStore.activeThreadId) appStore.startThreadInBackground(appStore.activeThreadId);
    } else if (action === "open") await appStore.openWorkspace(workspace.id);
    else await appStore.disconnectRemoteWorkspace(workspace.id);
  } catch (error) {
    workspaceActionError.value = error instanceof Error ? error.message : String(error);
  } finally {
    workspaceActionID.value = "";
  }
}

function openWorkspaceRemoval() {
  const workspace = workspaceMenuItem.value;
  closeWorkspaceMenu();
  if (!workspace || workspaceActionID.value) return;
  workspaceActionError.value = "";
  workspaceRemovalID.value = workspace.id;
}

function closeWorkspaceRemoval() {
  if (workspaceActionID.value) return;
  workspaceRemovalID.value = "";
  workspaceActionError.value = "";
}

async function confirmWorkspaceRemoval(deleteSessions: boolean) {
  const workspace = workspaceRemovalItem.value;
  if (!workspace || workspaceActionID.value) return;
  workspaceActionID.value = workspace.id;
  workspaceActionError.value = "";
  try {
    await appStore.removeWorkspace(workspace.id, deleteSessions);
    workspaceRemovalID.value = "";
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
    closeWorkspaceRename();
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
  <aside class="sidebar col-start-1 row-start-2 flex min-h-0 min-w-0 flex-col overflow-hidden border-r border-[var(--border)] bg-[var(--bg-sidebar)]" :class="ui.root" :aria-label="tr('sidebar.navigation')">
    <button
      v-if="appStore.sidebarCollapsed"
      class="icon-button sidebar-expand-button mx-auto mt-3 inline-grid size-9 shrink-0 place-items-center rounded-lg border border-transparent bg-transparent text-[var(--text-muted)] hover:border-[var(--border)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] focus-visible:outline-2 focus-visible:outline-[var(--text)]" :class="ui.iconButton"
      type="button"
      :title="tr('sidebar.expand')"
      :aria-label="tr('sidebar.expand')"
      @click="appStore.toggleSidebar"
    >
      <PanelLeftOpen :size="17" />
    </button>
    <nav v-else class="primary-nav grid gap-1 border-b border-[var(--border)] p-3" aria-label="Primary">
      <button v-if="!appStore.sidebarCollapsed" class="new-task-button flex h-9 w-full items-center gap-2.5 whitespace-nowrap rounded-lg border border-transparent bg-transparent px-3 text-sm text-[var(--text-secondary)] hover:border-[var(--border)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] disabled:cursor-not-allowed disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-[var(--text)]" type="button" :title="tr('sidebar.newTask')" :aria-label="tr('sidebar.newTask')" @click="appStore.openNewTask">
        <SquarePen :size="17" />
        <span>{{ tr("sidebar.newTask") }}</span>
      </button>
      <div class="primary-nav-row flex min-w-0 items-center">
        <button v-if="!appStore.sidebarCollapsed" class="primary-nav-search flex h-9 w-full min-w-0 items-center gap-2.5 whitespace-nowrap rounded-lg border border-transparent bg-transparent px-3 text-sm text-[var(--text-secondary)] hover:border-[var(--border)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] focus-visible:outline-2 focus-visible:outline-[var(--text)]" type="button" :title="tr('sidebar.openSearch')" :aria-label="tr('sidebar.openSearch')" :aria-pressed="appStore.searchOpen" @click="toggleSearch">
          <Search :size="16" />
          <span>{{ tr("sidebar.search") }}</span>
        </button>
      </div>
      <button v-if="!appStore.sidebarCollapsed" class="flex h-9 w-full items-center gap-2.5 whitespace-nowrap rounded-lg border border-transparent bg-transparent px-3 text-sm text-[var(--text-secondary)] hover:border-[var(--border)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] aria-pressed:border-[var(--border)] aria-pressed:bg-[var(--bg-active)] aria-pressed:text-[var(--text)] focus-visible:outline-2 focus-visible:outline-[var(--text)]" type="button" :title="tr('sidebar.review')" :aria-label="tr('sidebar.review')" :aria-pressed="appStore.inspectorOpen && appStore.inspectorTab === 'changes'" @click="appStore.toggleInspector('changes')">
        <FileSearch :size="16" />
        <span>{{ tr("sidebar.review") }}</span>
      </button>
    </nav>

    <div v-if="!appStore.sidebarCollapsed && appStore.searchOpen" class="sidebar-search mx-3 mt-3 grid h-9 grid-cols-[18px_minmax(0,1fr)_28px] items-center gap-1.5 rounded-lg border border-[var(--border-strong)] bg-[var(--bg-workspace)] px-2 text-[var(--text-muted)] shadow-sm focus-within:border-[var(--text-secondary)] focus-within:outline-2 focus-within:outline-offset-1 focus-within:outline-[var(--text)]">
      <Search :size="14" />
      <input class="h-full min-w-0 border-0 bg-transparent p-0 text-sm text-[var(--text)] outline-none placeholder:text-[var(--text-muted)]" ref="searchInput" v-model="appStore.searchQuery" type="search" :placeholder="tr('sidebar.searchTasks')" :aria-label="tr('sidebar.searchTasks')" />
      <button class="icon-button inline-grid size-7 place-items-center rounded-md border-0 bg-transparent text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] focus-visible:outline-2 focus-visible:outline-offset-0 focus-visible:outline-[var(--text)]" type="button" :title="tr('sidebar.closeSearch')" @click="toggleSearch"><X :size="14" /></button>
    </div>
    <p v-if="!appStore.sidebarCollapsed && appStore.searchOpen" class="sidebar-search-help mx-5 mt-1 text-[calc(11px+var(--font-size-delta))] leading-relaxed text-[var(--text-muted)]">{{ tr("sidebar.searchHelp") }}</p>

    <div v-if="!appStore.sidebarCollapsed" class="sidebar-section task-section min-h-0 flex-1 overflow-y-auto px-3 pt-4">
      <div class="section-heading flex h-8 items-center justify-between px-2 text-xs font-semibold text-[var(--text-secondary)]">
        <span class="tracking-[-0.01em]">{{ tr("sidebar.workspaces") }}</span>
        <div class="section-heading-actions flex items-center gap-0.5">
          <button class="icon-button inline-grid size-7 place-items-center rounded-md border-0 bg-transparent text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] disabled:cursor-not-allowed disabled:opacity-50" :class="ui.iconButton" type="button" :title="tr('sidebar.syncSessions')" :disabled="appStore.sessionSyncLoading" @click="void appStore.syncAndRestoreSessions()">
            <RefreshCw :size="15" :class="{ 'is-spinning': appStore.sessionSyncLoading }" />
          </button>
          <button class="icon-button inline-grid size-7 place-items-center rounded-md border-0 bg-transparent text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)]" :class="ui.iconButton" type="button" :title="tr('sidebar.addWorkspace')" @click="appStore.openNewTask"><Plus :size="15" /></button>
        </div>
      </div>
      <p v-if="appStore.catalogLoading" class="sidebar-empty mx-2 my-1 text-xs leading-relaxed text-[var(--text-secondary)]">{{ tr("sidebar.loading") }}</p>
      <p v-else-if="!appStore.catalogReady && appStore.catalogError" class="sidebar-empty error-text mx-2 my-1 text-xs leading-relaxed text-[var(--text-secondary)]" :title="appStore.catalogError">{{ tr("sidebar.unavailable") }}</p>
      <div v-for="group in workspaceGroups" :key="group.workspace.id" class="workspace-group mt-1">
        <div class="workspace-header flex min-w-0 items-center">
          <button
            type="button"
            class="workspace-row flex h-9 min-w-0 flex-1 items-center gap-2.5 rounded-lg border border-transparent bg-transparent px-2.5 text-left text-sm text-[var(--text-secondary)] hover:border-[var(--border)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)]"
            :aria-expanded="!isWorkspaceCollapsed(group.workspace.id)"
            :title="group.workspace.kind === 'ssh' ? group.workspace.remoteRoot : group.workspace.path"
            @click="toggleWorkspace(group.workspace.id)"
          >
            <Folder :size="16" />
            <span class="workspace-name min-w-0 flex-1 truncate">{{ group.workspace.name }}</span>
            <span v-if="group.workspace.kind === 'ssh'" class="workspace-kind-tag shrink-0 rounded-full border border-[var(--border-strong)] bg-[var(--bg-workspace)] px-1.5 py-0.5 text-[calc(9px+var(--font-size-delta))] font-semibold text-[var(--text-secondary)]">{{ tr("sidebar.remoteDirectory") }}</span>
          </button>
          <button
            class="icon-button workspace-menu-button inline-grid size-8 shrink-0 place-items-center rounded-lg border-0 bg-transparent text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] disabled:cursor-not-allowed disabled:opacity-50" :class="ui.iconButton"
            type="button"
            :aria-label="tr('sidebar.workspaceActions', { workspace: group.workspace.name })"
            :title="tr('sidebar.workspaceActions', { workspace: group.workspace.name })"
            :disabled="Boolean(workspaceActionID)"
            @click.stop="openWorkspaceMenu($event, group.workspace.id)"
          ><MoreHorizontal :size="16" /></button>
        </div>
        <div v-if="!isWorkspaceCollapsed(group.workspace.id)" class="workspace-threads ml-3 border-l border-[var(--border)] py-1 pl-2">
          <button
            v-for="thread in group.threads"
            :key="thread.id"
            class="thread-row flex h-8 w-full min-w-0 items-center gap-2 rounded-lg border border-transparent bg-transparent px-2.5 text-left text-[calc(13px+var(--font-size-delta))] text-[var(--text-secondary)] hover:border-[var(--border)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)]"
            :class="{ 'is-active border-[var(--border)] bg-[var(--bg-active)] text-[var(--text)]': appStore.activeThreadId === thread.id }"
            type="button"
            :title="thread.title"
            @click="appStore.selectThread(thread.id)"
            @contextmenu.prevent="openTaskMenu($event, thread.id)"
          >
            <span class="thread-title min-w-0 flex-1 truncate" :class="{ 'is-started font-semibold text-[var(--text)]': thread.started }">{{ thread.title }}</span>
            <span
              v-if="thread.status === 'running' || thread.status === 'starting'"
              class="thread-status size-3.5 shrink-0 rounded-full border-2 border-[var(--border-strong)] border-t-[var(--text-secondary)] motion-reduce:animate-none"
              :data-state="thread.status"
              :aria-label="thread.status === 'starting' ? tr('sidebar.piStarting') : tr('sidebar.taskRunning')"
            />
            <span v-else-if="thread.unread" class="thread-unread size-2 shrink-0 rounded-full bg-[var(--text)]" :aria-label="tr('sidebar.unread')" />
          </button>
          <p v-if="group.threads.length === 0" class="sidebar-empty mx-2 my-1 text-xs text-[var(--text-secondary)]">{{ tr("sidebar.noTasks") }}</p>
        </div>
      </div>
      <p v-if="workspaceActionError" class="sidebar-empty error-text mx-2 my-1 text-xs text-[var(--red)]" role="alert">{{ workspaceActionError }}</p>
      <p v-if="!appStore.catalogLoading && appStore.workspaces.length === 0" class="sidebar-empty mx-2 my-1 text-xs leading-relaxed text-[var(--text-secondary)]">{{ tr("sidebar.noWorkspaces") }}</p>
      <p v-else-if="appStore.searchQuery.trim() && workspaceGroups.length === 0" class="sidebar-empty mx-2 my-1 text-xs leading-relaxed text-[var(--text-secondary)]">{{ tr("sidebar.noMatches") }}</p>
    </div>

    <div v-if="!appStore.sidebarCollapsed" class="sidebar-footer flex h-14 shrink-0 items-center gap-2 border-t border-[var(--border)] px-4 text-xs text-[var(--text-secondary)]">
      <RuntimeBadge />
      <button class="icon-button inline-grid size-8 place-items-center rounded-lg border border-transparent bg-transparent text-[var(--text-muted)] hover:border-[var(--border)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)]" :class="ui.iconButton" type="button" :title="tr('sidebar.settings')" @click="appStore.openSettings()">
        <Settings :size="17" />
      </button>
    </div>

    <div
      v-if="!appStore.sidebarCollapsed && taskMenu.open && taskMenuThread"
      class="thread-context-menu"
      :class="ui.menuSurface"
      role="menu"
      :style="{ left: `${taskMenu.x}px`, top: `${taskMenu.y}px` }"
      @click.stop="closeTaskMenu"
      @contextmenu.prevent
    >
      <button v-if="taskMenuWorkspace?.kind !== 'ssh'" type="button" role="menuitem" @click="void openTaskWorkspace()"><FolderOpen :size="14" />{{ tr("sidebar.openWorkspace") }}</button>
      <button type="button" role="menuitem" @click="void beginTaskRename()"><Pencil :size="14" />{{ tr("topbar.rename") }}</button>
      <button type="button" role="menuitem" :disabled="!taskMenuThread.sessionFile || Boolean(appStore.activeSessionOperation)" @click="void runTaskAction('branch')"><GitBranch :size="14" />{{ tr("topbar.branches") }}</button>
      <button type="button" role="menuitem" :disabled="!taskMenuThread.sessionFile || Boolean(appStore.activeSessionOperation)" @click="void runTaskAction('clone')"><Copy :size="14" />{{ tr("topbar.clone") }}</button>
      <button type="button" role="menuitem" :disabled="!taskMenuThread.sessionFile || Boolean(appStore.activeSessionOperation)" @click="void runTaskAction('export')"><Download :size="14" />{{ tr("topbar.export") }}</button>
      <button type="button" role="menuitem" :disabled="!taskMenuThread.started" @click="void runTaskAction('compact')"><Sparkles :size="14" />{{ tr("topbar.compact") }}</button>
      <button v-if="taskMenuThread.started" type="button" role="menuitem" @click="void appStore.stopThread(taskMenuThread.id)"><Square :size="14" />{{ tr("sidebar.closePi") }}</button>
      <button v-else type="button" role="menuitem" @click="appStore.startThreadInBackground(taskMenuThread.id)"><Play :size="14" />{{ tr("sidebar.startPi") }}</button>
      <button type="button" role="menuitem" class="danger" @click="appStore.requestDeleteThread(taskMenuThread.id)"><Trash2 :size="14" />{{ tr("sidebar.delete") }}</button>
    </div>

    <form
      v-if="!appStore.sidebarCollapsed && taskRenameOpen"
      class="thread-context-menu task-rename-menu"
      :class="ui.menuSurface"
      role="dialog"
      :aria-label="tr('topbar.rename')"
      :style="{ left: `${taskMenu.x}px`, top: `${taskMenu.y}px` }"
      @click.stop
      @submit.prevent="void submitTaskRename()"
    >
      <label for="task-rename-input">{{ tr("topbar.taskName") }}</label>
      <input :class="ui.input" id="task-rename-input" ref="taskRenameInput" v-model="taskRenameValue" maxlength="200" />
      <div class="workspace-rename-actions">
        <button type="submit" :disabled="!taskRenameValue.trim()">{{ tr("common.confirm") }}</button>
        <button type="button" @click="closeTaskRename">{{ tr("common.cancel") }}</button>
      </div>
    </form>

    <div
      v-if="!appStore.sidebarCollapsed && workspaceMenu.open && workspaceMenuItem"
      class="thread-context-menu workspace-context-menu"
      :class="ui.menuSurface"
      role="menu"
      :style="{ left: `${workspaceMenu.x}px`, top: `${workspaceMenu.y}px` }"
      @click.stop="closeWorkspaceMenu"
      @contextmenu.prevent
    >
      <button type="button" role="menuitem" @click="void runWorkspaceAction('newTask')"><SquarePen :size="15" />{{ tr("sidebar.newTask") }}</button>
      <button v-if="workspaceMenuItem.kind !== 'ssh'" type="button" role="menuitem" @click="void runWorkspaceAction('open')"><FolderOpen :size="15" />{{ tr("sidebar.openWorkspace") }}</button>
      <button v-else-if="appStore.remoteWorkspaceHasConnection(workspaceMenuItem.id)" type="button" role="menuitem" @click="void runWorkspaceAction('disconnect')"><Unplug :size="15" />{{ tr("sidebar.disconnectRemote") }}</button>
      <button type="button" role="menuitem" @click="void openWorkspaceRename()"><Pencil :size="15" />{{ tr("sidebar.renameWorkspace") }}</button>
      <button type="button" role="menuitem" class="danger" @click="openWorkspaceRemoval"><Trash2 :size="15" />{{ tr("sidebar.removeWorkspace") }}</button>
    </div>
    <form
      v-if="!appStore.sidebarCollapsed && workspaceRenameOpen"
      class="thread-context-menu workspace-rename-menu"
      :class="ui.menuSurface"
      role="dialog"
      :style="{ left: `${workspaceMenu.x}px`, top: `${workspaceMenu.y}px` }"
      :aria-label="tr('sidebar.renameWorkspace')"
      @click.stop
      @submit.prevent="void submitWorkspaceRename()"
    >
      <label for="workspace-rename-input">{{ tr("sidebar.workspaceName") }}</label>
      <input :class="ui.input" id="workspace-rename-input" ref="workspaceRenameInput" v-model="workspaceRenameValue" maxlength="200" />
      <div class="workspace-rename-actions">
        <button type="submit" :disabled="!workspaceRenameValue.trim() || Boolean(workspaceActionID)">{{ tr("common.confirm") }}</button>
        <button type="button" :disabled="Boolean(workspaceActionID)" @click="closeWorkspaceRename">{{ tr("common.cancel") }}</button>
      </div>
    </form>
    <RemoveWorkspaceDialog
      v-if="workspaceRemovalItem"
      :workspace-name="workspaceRemovalItem.name"
      :busy="workspaceActionID === workspaceRemovalItem.id"
      :error="workspaceActionError"
      @cancel="closeWorkspaceRemoval"
      @remove="void confirmWorkspaceRemoval(false)"
      @delete="void confirmWorkspaceRemoval(true)"
    />
  </aside>
</template>
