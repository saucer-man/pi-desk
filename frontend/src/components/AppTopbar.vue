<script setup lang="ts">
import { ui } from "../ui/classes";
import { ArrowLeft, ArrowRight, CalendarClock, Check, ChevronDown, ChevronRight, FolderGit2, PanelLeftClose, PanelRightOpen } from "lucide-vue-next";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { type AppPage, useAppStore } from "../stores/app";
import { tr } from "../i18n";

const appStore = useAppStore();
const workspaceApplicationMenuOpen = ref(false);
const workspaceApplicationButton = ref<HTMLButtonElement>();
const activeWorkspaceApplication = computed(() => appStore.activeWorkspaceApplication);
const workspaceApplicationDisabled = computed(() => !activeWorkspaceApplication.value || appStore.activeThread?.trust !== "approve");
const workspaceApplicationTitle = computed(() => workspaceApplicationDisabled.value
  ? tr("topbar.trustToOpen")
  : tr("topbar.openWithApplication", { application: activeWorkspaceApplication.value?.name ?? "" }));
type NavigationTarget = { page: AppPage; threadId: string };
const navigationHistory = ref<NavigationTarget[]>([]);
const navigationIndex = ref(-1);
let restoringNavigation = false;
const currentNavigationTarget = computed<NavigationTarget>(() => ({ page: appStore.activePage, threadId: appStore.activeThreadId }));

function sameNavigationTarget(left: NavigationTarget | undefined, right: NavigationTarget): boolean {
  return left?.page === right.page && left.threadId === right.threadId;
}

function findNavigationIndex(direction: -1 | 1): number {
  for (let index = navigationIndex.value + direction; index >= 0 && index < navigationHistory.value.length; index += direction) {
    const target = navigationHistory.value[index];
    if (target.page === "scheduledTasks" || appStore.threads.some((thread) => thread.id === target.threadId)) return index;
  }
  return -1;
}

const canNavigateBack = computed(() => findNavigationIndex(-1) >= 0);
const canNavigateForward = computed(() => findNavigationIndex(1) >= 0);

function navigateHistory(direction: -1 | 1) {
  const index = findNavigationIndex(direction);
  if (index < 0) return;
  const target = navigationHistory.value[index];
  navigationIndex.value = index;
  restoringNavigation = true;
  if (target.page === "scheduledTasks") appStore.openScheduledTasks();
  else appStore.selectThread(target.threadId);
  queueMicrotask(() => { restoringNavigation = false; });
}

watch(currentNavigationTarget, (target) => {
  if (target.page === "task" && !target.threadId) return;
  if (restoringNavigation) {
    restoringNavigation = false;
    return;
  }
  if (sameNavigationTarget(navigationHistory.value[navigationIndex.value], target)) return;
  navigationHistory.value = [...navigationHistory.value.slice(0, navigationIndex.value + 1), target];
  navigationIndex.value = navigationHistory.value.length - 1;
}, { immediate: true });

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
  <header
    class="topbar relative z-40 col-span-full row-start-1 grid h-[var(--topbar-height)] min-w-0 border-b border-[var(--border)] bg-[var(--bg-workspace)] max-[760px]:[grid-template-columns:var(--sidebar-collapsed-width)_minmax(0,1fr)]"
    :class="[ui.root, appStore.sidebarCollapsed ? '[grid-template-columns:var(--sidebar-collapsed-width)_minmax(0,1fr)]' : '[grid-template-columns:var(--sidebar-width)_minmax(0,1fr)]']"
  >
    <div class="topbar-brand flex min-w-0 items-center gap-2 border-r border-[var(--border)] px-3" aria-label="Pi Desk">
      <span class="topbar-brand-mark grid size-6 shrink-0 place-items-center rounded-md bg-[var(--text)] text-xs font-bold tracking-tight text-[var(--bg-workspace)]" aria-hidden="true">Pi</span>
      <div v-if="!appStore.sidebarCollapsed" class="topbar-history flex items-center gap-0.5">
        <button class="icon-button topbar-history-button" type="button" :title="tr('sidebar.back')" :aria-label="tr('sidebar.back')" :disabled="!canNavigateBack" @click="navigateHistory(-1)"><ArrowLeft :size="18" :stroke-width="1.8" /></button>
        <button class="icon-button topbar-history-button" type="button" :title="tr('sidebar.forward')" :aria-label="tr('sidebar.forward')" :disabled="!canNavigateForward" @click="navigateHistory(1)"><ArrowRight :size="18" :stroke-width="1.8" /></button>
      </div>
      <button
        v-if="!appStore.sidebarCollapsed"
        class="icon-button topbar-sidebar-toggle ml-auto inline-grid size-7 shrink-0 place-items-center rounded-md border-0 bg-transparent text-[var(--text-muted)] transition-colors duration-150 ease-out hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus)]"
        type="button"
        :title="tr('sidebar.collapse')"
        :aria-label="tr('sidebar.collapse')"
        @click="appStore.toggleSidebar"
      >
        <PanelLeftClose :size="16" />
      </button>
    </div>

    <div class="topbar-task flex min-w-0 items-center justify-between gap-3">
      <div class="topbar-title-group flex min-w-0 items-center gap-2.5">
        <CalendarClock v-if="appStore.activePage === 'scheduledTasks'" :size="17" class="text-[var(--text-muted)]" />
        <strong class="min-w-0 max-w-[min(42vw,540px)] truncate font-display text-[calc(15px+var(--font-size-delta))] font-semibold tracking-[-0.01em] text-[var(--text)]" :title="appStore.activePage === 'scheduledTasks' ? tr('scheduledTasks.title') : appStore.activeThread?.title || 'Pi Desk'">{{ appStore.activePage === "scheduledTasks" ? tr("scheduledTasks.title") : appStore.activeThread?.title || "Pi Desk" }}</strong>
        <span v-if="appStore.activePage === 'task' && appStore.activeExtensionTitle" class="extension-window-title min-w-0 truncate text-xs text-[var(--text-secondary)]" :title="appStore.activeExtensionTitle">{{ appStore.activeExtensionTitle }}</span>
        <span v-if="appStore.activePage === 'task' && appStore.activeThread" class="workspace-chip inline-flex min-w-0 max-w-56 items-center gap-1.5 rounded-md border border-[var(--border)] bg-[var(--bg-app)] px-2 py-1 text-xs text-[var(--text-secondary)]" :title="appStore.activeThread.workspacePath">
          <FolderGit2 :size="14" />
          <span>{{ appStore.activeThread.workspace }}</span>
        </span>
      </div>

      <div class="topbar-actions flex shrink-0 items-center gap-1">
        <div v-if="appStore.activePage === 'task' && appStore.activeThread && !appStore.workspaceApplicationsLoading && activeWorkspaceApplication" class="menu-anchor topbar-menu-anchor workspace-application-anchor relative">
          <div class="workspace-application-split inline-flex h-8 items-stretch overflow-hidden rounded-lg border border-[var(--border)] bg-[var(--bg-panel)] shadow-sm">
            <button
              class="icon-button workspace-application-primary inline-grid size-[30px] place-items-center rounded-none border-0 bg-transparent text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] active:bg-[var(--bg-active)] disabled:cursor-not-allowed disabled:opacity-50"
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
              class="workspace-application-toggle inline-grid w-5 place-items-center border-0 border-l border-[var(--border)] bg-transparent text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] disabled:cursor-not-allowed disabled:opacity-50"
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
          <div v-if="workspaceApplicationMenuOpen" class="command-menu workspace-application-menu absolute right-0 top-[calc(100%+8px)] z-50 grid min-w-56 gap-1 rounded-xl border border-[var(--border-strong)] bg-[var(--bg-menu)] p-1.5 shadow-xl" :class="ui.menuSurface" role="menu" @keydown.esc.stop.prevent="closeWorkspaceApplicationMenu(true)">
            <button
              v-for="application in appStore.workspaceApplications"
              :key="application.id"
              type="button"
              role="menuitemradio"
              :aria-checked="application.id === activeWorkspaceApplication?.id"
              class="flex min-h-9 w-full items-center gap-2 rounded-lg border-0 bg-transparent px-2.5 text-left text-xs text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)]"
              :class="{ 'is-selected bg-[var(--bg-hover)] text-[var(--text)]': application.id === activeWorkspaceApplication?.id }"
              @click="void openWorkspaceWith(application.id)"
            >
              <img class="workspace-application-icon workspace-application-icon-menu" :src="application.iconDataUrl" alt="" width="20" height="20" draggable="false" />
              <span>{{ application.name }}</span>
              <Check v-if="application.id === activeWorkspaceApplication?.id" class="workspace-application-check" :size="15" />
            </button>
          </div>
          <p v-else-if="appStore.workspaceApplicationError" class="workspace-application-error absolute right-0 top-[calc(100%+8px)] z-50 m-0 w-64 rounded-lg border border-[color-mix(in_srgb,var(--red)_45%,var(--border))] bg-[var(--bg-menu)] px-3 py-2 text-xs leading-relaxed text-[var(--red)] shadow-xl" role="alert">{{ appStore.workspaceApplicationError }}</p>
        </div>
        <button
          v-if="appStore.activePage === 'task'"
          class="icon-button inspector-toggle inline-grid size-7 shrink-0 place-items-center rounded-md border border-transparent bg-transparent text-[var(--text-muted)] transition-colors duration-150 ease-out hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus)] max-[520px]:hidden"
          type="button"
          :title="appStore.inspectorOpen ? tr('topbar.closeInspector') : tr('topbar.openInspector')"
          :aria-label="appStore.inspectorOpen ? tr('topbar.closeInspector') : tr('topbar.openInspector')"
          @click="appStore.toggleInspector()"
        >
          <ChevronRight v-if="appStore.inspectorOpen" :size="18" />
          <PanelRightOpen v-else :size="18" />
        </button>
      </div>

    </div>
  </header>
</template>
