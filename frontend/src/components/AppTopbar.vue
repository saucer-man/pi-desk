<script setup lang="ts">
import { ui } from "../ui/classes";
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
  <header
    class="topbar relative z-40 col-span-full row-start-1 grid h-[52px] min-w-0 border-b border-[var(--border)] bg-[var(--bg-workspace)] shadow-[0_1px_0_color-mix(in_srgb,var(--text)_3%,transparent)] max-[760px]:[grid-template-columns:56px_minmax(0,1fr)]"
    :class="[ui.root, appStore.sidebarCollapsed ? '[grid-template-columns:56px_minmax(0,1fr)]' : '[grid-template-columns:var(--sidebar-width)_minmax(0,1fr)]']"
  >
    <div class="topbar-brand flex min-w-0 items-center gap-2.5 border-r border-[var(--border)] px-4" aria-label="Pi Desk">
      <span class="topbar-brand-mark grid size-7 shrink-0 place-items-center rounded-lg bg-[var(--text)] text-xs font-extrabold tracking-tight text-[var(--bg-workspace)] shadow-sm" aria-hidden="true">Pi</span>
      <strong class="min-w-0 truncate font-display text-sm font-bold tracking-[-0.01em]">Pi Desk</strong>
      <button
        v-if="!appStore.sidebarCollapsed"
        class="icon-button topbar-sidebar-toggle ml-auto inline-grid size-8 shrink-0 place-items-center rounded-lg border-0 bg-transparent text-[var(--text-muted)] transition-colors duration-150 ease-out hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--text)]"
        type="button"
        :title="tr('sidebar.collapse')"
        :aria-label="tr('sidebar.collapse')"
        @click="appStore.toggleSidebar"
      >
        <PanelLeftClose :size="16" />
      </button>
    </div>

    <div class="topbar-task flex min-w-0 items-center justify-between gap-4">
      <div class="topbar-title-group flex min-w-0 items-center gap-2.5">
        <strong class="min-w-0 max-w-[min(42vw,540px)] truncate font-display text-[calc(15px+var(--font-size-delta))] font-semibold tracking-[-0.01em] text-[var(--text)]" :title="appStore.activeThread?.title || 'Pi Desk'">{{ appStore.activeThread?.title || "Pi Desk" }}</strong>
        <span v-if="appStore.activeExtensionTitle" class="extension-window-title min-w-0 truncate text-xs text-[var(--text-secondary)]" :title="appStore.activeExtensionTitle">{{ appStore.activeExtensionTitle }}</span>
        <span v-if="appStore.activeThread" class="workspace-chip inline-flex min-w-0 max-w-56 items-center gap-1.5 rounded-lg border border-[var(--border)] bg-[var(--bg-app)] px-2.5 py-1.5 text-xs text-[var(--text-secondary)] shadow-sm" :title="appStore.activeThread.workspacePath">
          <FolderGit2 :size="14" />
          <span>{{ appStore.activeThread.workspace }}</span>
        </span>
        <span v-if="activeBranch" class="branch-chip inline-flex min-w-0 max-w-56 items-center gap-1.5 rounded-lg border border-[var(--border)] bg-[var(--bg-app)] px-2.5 py-1.5 font-mono text-[calc(11px+var(--font-size-delta))] text-[var(--text-secondary)] shadow-sm" :title="activeBranch">
          <GitBranch :size="14" />
          <span>{{ activeBranch }}</span>
        </span>
      </div>

      <div class="topbar-actions flex shrink-0 items-center gap-1">
        <div v-if="appStore.activeThread && !appStore.workspaceApplicationsLoading && activeWorkspaceApplication" class="menu-anchor topbar-menu-anchor workspace-application-anchor relative">
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
          class="icon-button inspector-toggle inline-grid size-8 shrink-0 place-items-center rounded-lg border border-transparent bg-transparent text-[var(--text-muted)] transition-colors duration-150 ease-out hover:border-[var(--border)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--text)] max-[520px]:hidden"
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
