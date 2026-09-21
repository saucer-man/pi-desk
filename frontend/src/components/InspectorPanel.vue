<script setup lang="ts">
import { ui } from "../ui/classes";
import { Binary, ChevronRight, FileCode2, FileDiff, LoaderCircle, PanelRightClose, FolderOpen, Globe, Terminal, Plus, X, Maximize2, Minimize2, Search, ChevronDown, ExternalLink } from "lucide-vue-next";
import { computed, defineAsyncComponent, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { type PanelTab, useAppStore } from "../stores/app";
import { buildRepositoryTree } from "../utils/fileMentions";
import { highlightCodeLines, type CodeSegment } from "../utils/codeHighlight";
import CodePreview from "./CodePreview.vue";
import FileTreeNode from "./FileTreeNode.vue";
import MarkdownBody from "./MarkdownBody.vue";
import { tr } from "../i18n";

const TerminalPane = defineAsyncComponent(() => import("./TerminalPane.vue"));
const BrowserPane = defineAsyncComponent(() => import("./BrowserPane.vue"));

const appStore = useAppStore();
const repository = computed(() => appStore.activeRepository);
const repositoryFiles = computed(() => repository.value?.files ?? []);
const changedFiles = computed(() => repository.value?.git.files ?? []);
const activeDiff = computed(() => appStore.activeRepositoryDiff);
const remoteWorkspace = computed(() => appStore.activeThread ? appStore.remoteWorkspaceForThread(appStore.activeThread) : undefined);
const workspaceLabel = computed(() => remoteWorkspace.value?.remoteRoot || appStore.activeThread?.workspacePath || "");
const filePreview = computed(() => appStore.activeRepositoryFilePreview);
const filePreviewName = computed(() => appStore.activeRepositoryFilePreviewPath.split(/[\\/]/).pop() || appStore.activeRepositoryFilePreviewPath);
const workspaceName = computed(() => workspaceLabel.value.split(/[\\/]/).filter(Boolean).pop() || tr("inspector.files"));
function breadcrumbs(path: string) {
  const parts = path.replaceAll("\\", "/").split("/").filter(Boolean);
  if (parts[0]?.toLocaleLowerCase() === workspaceName.value.toLocaleLowerCase()) parts.shift();
  return [workspaceName.value, ...parts];
}
const currentTab = computed(() => appStore.activePanelTab);
const markdownRendered = computed({ get: () => currentTab.value?.markdownRendered !== false, set: (value) => { if (currentTab.value) currentTab.value.markdownRendered = value; appStore.scheduleDesktopStateSave(); } });
const activeSpreadsheetSheet = computed({ get: () => currentTab.value?.sheet ?? 0, set: (value) => { if (currentTab.value) currentTab.value.sheet = value; appStore.scheduleDesktopStateSave(); } });
const filter = computed({ get: () => currentTab.value?.filter ?? "", set: (value) => { if (currentTab.value) currentTab.value.filter = value; appStore.scheduleDesktopStateSave(); } });
const expandedDirectories = computed(() => currentTab.value ? (currentTab.value.expanded ??= {}) : {});
const panelElement = ref<HTMLElement>();
const addMenu = ref<HTMLElement>();
const openMenu = ref<HTMLElement>();
let tabDrag: { id: string; x: number; y: number } | undefined;
let draggedClick = false;
function startTabDrag(event: PointerEvent, id: string) {
  if (event.button !== 0) return;
  draggedClick = false;
  tabDrag = { id, x: event.clientX, y: event.clientY };
  (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
}
function finishTabDrag(event: PointerEvent) {
  const start = tabDrag;
  tabDrag = undefined;
  if (!start || Math.hypot(event.clientX - start.x, event.clientY - start.y) < 5) return;
  draggedClick = true;
  const target = document.elementFromPoint(event.clientX, event.clientY)?.closest<HTMLElement>("[data-panel-tab]");
  if (target?.dataset.panelTab) appStore.reorderPanelTab(start.id, target.dataset.panelTab);
}
function clickTab(id: string) {
  if (!draggedClick) appStore.selectPanelTab(id);
  draggedClick = false;
}
const scrollSelectors = [".file-preview-content", ".repository-diff", ".spreadsheet-scroll", ".file-markdown-preview", ".panel-file-tree", ".xterm-viewport"];
function saveScroll(event: Event) {
  const tab = currentTab.value, target = event.target;
  if (!tab || !(target instanceof HTMLElement)) return;
  const selector = scrollSelectors.find((selector) => target.matches(selector));
  if (selector) {
    (tab.scroll ??= {})[selector] = [target.scrollLeft, target.scrollTop];
    appStore.scheduleDesktopStateSave();
  }
}
async function restoreScroll() {
  await nextTick();
  for (const [selector, [left, top]] of Object.entries(currentTab.value?.scroll ?? {})) {
    const element = panelElement.value?.querySelector(selector);
    if (element) { element.scrollLeft = left; element.scrollTop = top; }
  }
}
function tabIcon(tab: PanelTab) {
  return { files: FolderOpen, file: FileCode2, diff: FileDiff, browser: Globe, terminal: Terminal }[tab.kind];
}
function addTab(kind: "changes" | "browser" | "terminal") {
  addMenu.value?.hidePopover();
  appStore.setInspectorTab(kind);
}
function toggleTree() {
  if (!currentTab.value) return;
  currentTab.value.treeOpen = !currentTab.value.treeOpen;
  appStore.scheduleDesktopStateSave();
  void nextTick(() => panelElement.value?.querySelector(".file-tree-row.is-selected")?.scrollIntoView?.({ block: "nearest" }));
}
function toggleExpanded() {
  const panel = appStore.activePanel;
  if (panel) panel.expanded = !panel.expanded;
  appStore.scheduleDesktopStateSave();
}
function onTabKey(event: KeyboardEvent, tab: PanelTab) {
  const tabs = appStore.activePanel?.tabs ?? [], index = tabs.indexOf(tab);
  if (event.key === "Delete") { event.preventDefault(); void appStore.closePanelTab(tab.id); return; }
  const next = event.key === "ArrowRight" ? (index + 1) % tabs.length : event.key === "ArrowLeft" ? (index + tabs.length - 1) % tabs.length : event.key === "Home" ? 0 : event.key === "End" ? tabs.length - 1 : -1;
  if (next < 0) return;
  event.preventDefault();
  if (event.altKey) appStore.reorderPanelTab(tab.id, tabs[next].id);
  else appStore.selectPanelTab(tabs[next].id);
  void nextTick(() => panelElement.value?.querySelector<HTMLButtonElement>('[role="tab"][aria-selected="true"]')?.focus());
}
const spreadsheetMediaType = "application/x-pi-desk-spreadsheet";
interface SpreadsheetSheet { name: string; columns: number; rows: string[][] }
interface SpreadsheetPreview { sheets: SpreadsheetSheet[] }
const spreadsheet = computed<SpreadsheetPreview | undefined>(() => {
  if (filePreview.value?.mediaType !== spreadsheetMediaType) return undefined;
  try {
    const value = JSON.parse(filePreview.value.content ?? "") as SpreadsheetPreview;
    return Array.isArray(value.sheets) ? value : undefined;
  } catch {
    return undefined;
  }
});
const activeSheet = computed(() => spreadsheet.value?.sheets[activeSpreadsheetSheet.value] ?? spreadsheet.value?.sheets[0]);
const spreadsheetColumns = computed(() => Array.from({ length: activeSheet.value?.columns ?? 0 }, (_, index) => index));

const sessionChanges = computed(() => appStore.activeSessionChanges);
const sessionChangesError = computed(() => appStore.activeSessionChangesError);
const rollbackArmed = ref<Record<string, boolean>>({});
let rollbackArmTimer = 0;
const rollbackActions = computed<Record<string, string>>(() => Object.fromEntries(sessionChanges.value.map((change) => [
  change.path,
  rollbackArmed.value[change.path] ? rollbackConfirmText(change.plan) : tr("inspector.rollback"),
])));

function rollbackConfirmText(plan: string): string {
  if (plan === "git-restore") return tr("inspector.rollbackConfirmGit");
  if (plan === "delete") return tr("inspector.rollbackConfirmDelete");
  return tr("inspector.rollbackConfirmEdits");
}

function confirmRollback(path: string) {
  if (!rollbackArmed.value[path]) {
    rollbackArmed.value = { ...rollbackArmed.value, [path]: true };
    window.clearTimeout(rollbackArmTimer);
    rollbackArmTimer = window.setTimeout(() => {
      rollbackArmed.value = {};
    }, 5000);
    return;
  }
  window.clearTimeout(rollbackArmTimer);
  rollbackArmed.value = {};
  void appStore.rollbackSessionFile(path);
}
const normalizedChangedFiles = computed(() => changedFiles.value.map((file) => ({
  ...file,
  path: file.path.replaceAll("\\", "/"),
})));
const changeStatusByPath = computed<Record<string, string>>(() => Object.fromEntries(
  normalizedChangedFiles.value.map((file) => [file.path, reviewChangeLabel(file)]),
));
const filePaths = computed(() => [...new Set([
  ...repositoryFiles.value.map((file) => file.path.replaceAll("\\", "/")),
  ...normalizedChangedFiles.value.map((file) => file.path),
])]);
const visibleFiles = computed(() => filePaths.value.filter((path) => path.toLocaleLowerCase().includes(filter.value.toLocaleLowerCase())).slice(0, 500));
const fileListTruncated = computed(() => filePaths.value.length > visibleFiles.value.length || Boolean(repository.value?.truncated));
const fileTree = computed(() => buildRepositoryTree(visibleFiles.value));

function changeLabel(indexStatus: string, worktreeStatus: string): string {
  if (indexStatus === "?" || worktreeStatus === "?") return "U";
  if (indexStatus === "R" || worktreeStatus === "R") return "R";
  if (indexStatus === "A") return "A";
  if (indexStatus === "D" || worktreeStatus === "D") return "D";
  return "M";
}

function reviewChangeLabel(file: { indexStatus?: string; worktreeStatus?: string }): string {
  return changeLabel(file.indexStatus ?? "", file.worktreeStatus ?? "");
}

function diffLineClass(line: string): string {
  if (line.startsWith("@@")) return "is-hunk";
  if (line.startsWith("+") && !line.startsWith("+++ ")) return "is-addition";
  if (line.startsWith("-") && !line.startsWith("--- ")) return "is-deletion";
  if (line.startsWith("diff ") || line.startsWith("index ") || line.startsWith("--- ") || line.startsWith("+++ ") || line.startsWith("\\")) return "is-meta";
  return "";
}

interface DiffRow {
  key: string;
  text: string;
  kind: string;
  marker: string;
  oldLine?: number;
  newLine?: number;
}

function diffRows(value: string): DiffRow[] {
  if (!value) return [];
  // Pi tool results include a display gutter; Git patches carry line numbers in @@ headers.
  const numbered = activeDiff.value?.session && !/^@@ /m.test(value)
    && /^[+-] *\d+ /m.test(value);
  let oldLine: number | undefined;
  let newLine: number | undefined;
  let hunkCount = 0;
  return value.replace(/\n$/, "").split("\n").map((line, index) => {
    if (numbered) {
      const match = line.match(/^([ +\-]) *(\d+) (.*)$/);
      if (match) {
        const marker = match[1];
        return {
          key: `${index}-${line}`, text: match[3], marker,
          kind: marker === "+" ? "is-addition" : marker === "-" ? "is-deletion" : "",
          ...(marker === "-" ? { oldLine: Number(match[2]) } : { newLine: Number(match[2]) }),
        };
      }
      if (/^\s*\.{3}\s*$/.test(line)) {
        return { key: `${index}-${line}`, text: "", marker: "", kind: index === 0 ? "is-hunk is-first-hunk" : "is-hunk" };
      }
    }
    let kind = diffLineClass(line);
    if (kind === "is-hunk") {
      kind += hunkCount++ === 0 ? " is-first-hunk" : "";
      const hunk = line.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
      if (hunk) {
        oldLine = Number(hunk[1]);
        newLine = Number(hunk[2]);
      }
    }
    const row: DiffRow = { key: `${index}-${line}`, text: line, kind, marker: "" };
    if (kind === "is-deletion") {
      row.oldLine = oldLine;
      row.marker = "−";
      row.text = line.slice(1);
      if (oldLine !== undefined) oldLine++;
    } else if (kind === "is-addition") {
      row.newLine = newLine;
      row.marker = "+";
      row.text = line.slice(1);
      if (newLine !== undefined) newLine++;
    } else if (!kind && line.startsWith(" ")) {
      row.oldLine = oldLine;
      row.newLine = newLine;
      row.text = line.slice(1);
      if (oldLine !== undefined) oldLine++;
      if (newLine !== undefined) newLine++;
    }
    return row;
  });
}

function untrackedRows(value: string): DiffRow[] {
  if (!value) return [];
  return value.replace(/\n$/, "").split("\n").map((text, index) => ({
    key: `${index}-${text}`, text, kind: "is-addition", marker: "+", newLine: index + 1,
  }));
}

type DiffSection = "staged" | "working" | "content";
const diffHighlights = ref<Record<DiffSection, CodeSegment[][]>>({ staged: [], working: [], content: [] });
let diffHighlightGeneration = 0;

function visibleDiffRows(value: string, section: DiffSection) {
  const rows = section === "content" ? untrackedRows(value) : diffRows(value);
  const lines = diffHighlights.value[section];
  return rows.map((row, index) => ({
    ...row,
    lineNumber: row.newLine ?? row.oldLine,
    segments: lines[index] ?? [{ text: row.text, classes: "" }],
  }));
}

watch(() => [
  appStore.activeRepositoryDiffPath,
  activeDiff.value?.staged ?? "",
  activeDiff.value?.working ?? "",
  activeDiff.value?.content ?? "",
] as const, async ([path, staged, working, content]) => {
  const currentGeneration = ++diffHighlightGeneration;
  diffHighlights.value = { staged: [], working: [], content: [] };
  const values: Record<DiffSection, string> = { staged, working, content };
  const highlighted = await Promise.all((Object.keys(values) as DiffSection[]).map(async (section) => {
    const rows = section === "content" ? untrackedRows(values[section]) : diffRows(values[section]);
    const plain = rows.map((row) => row.text).join("\n");
    return [section, await highlightCodeLines(path, plain) ?? []] as const;
  }));
  if (currentGeneration === diffHighlightGeneration) diffHighlights.value = Object.fromEntries(highlighted) as Record<DiffSection, CodeSegment[][]>;
}, { immediate: true });

function openTreeFile(path: string) {
  if (changeStatusByPath.value[path] === "D") void appStore.openRepositoryDiff(path);
  else void appStore.openRepositoryFilePreview(path, undefined, false);
}

function refreshPreview() {
  const path = appStore.activeRepositoryFilePreviewPath;
  if (path && !appStore.activeRepositoryFilePreviewLoading) {
    if (currentTab.value) void appStore.loadPanelFile(appStore.activeThreadId, currentTab.value.id);
  }
}

function refreshPreviewWhenVisible() {
  if (document.visibilityState === "visible") refreshPreview();
}

function spreadsheetColumnName(index: number): string {
  let name = "";
  for (let value = index + 1; value > 0; value = Math.floor((value - 1) / 26)) {
    name = String.fromCharCode(65 + ((value - 1) % 26)) + name;
  }
  return name;
}

onMounted(() => {
  if (!currentTab.value) appStore.setInspectorTab("changes");
  void appStore.refreshActiveRepository();
  if (currentTab.value && ((currentTab.value.kind === "file" && !currentTab.value.preview) || (currentTab.value.kind === "diff" && !currentTab.value.diff))) void appStore.loadPanelFile(appStore.activeThreadId, currentTab.value.id);
  window.addEventListener("focus", refreshPreview);
  document.addEventListener("visibilitychange", refreshPreviewWhenVisible);
});
onBeforeUnmount(() => {
  window.clearTimeout(rollbackArmTimer);
  window.removeEventListener("focus", refreshPreview);
  document.removeEventListener("visibilitychange", refreshPreviewWhenVisible);
});
watch(() => appStore.activeThreadId, () => {
  void appStore.refreshActiveRepository();
});
watch(() => [currentTab.value?.id, currentTab.value?.loading, markdownRendered.value, activeSpreadsheetSheet.value], restoreScroll, { flush: "post", immediate: true });
watch(() => currentTab.value?.id, async () => {
  await nextTick();
  panelElement.value?.querySelector('.panel-tab.is-active')?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
}, { flush: "post", immediate: true });
</script>

<template>
  <aside ref="panelElement" class="inspector panel-workbench" :class="ui.root" :aria-label="tr('inspector.label')" @scroll.capture="saveScroll">
    <div class="panel-tabbar">
      <div class="panel-tabs" role="tablist" aria-label="工作区标签">
        <div v-for="tab in appStore.activePanel?.tabs" :key="tab.id" :data-panel-tab="tab.id" class="panel-tab" :class="{ 'is-active': currentTab?.id === tab.id, 'is-preview': tab.kind === 'file' && !tab.pinned }">
          <button type="button" role="tab" @pointerdown="startTabDrag($event, tab.id)" @pointerup="finishTabDrag" @pointercancel="tabDrag = undefined" :aria-selected="currentTab?.id === tab.id" :tabindex="currentTab?.id === tab.id ? 0 : -1" :title="tab.path || tab.url || tab.title" @click="clickTab(tab.id)" @dblclick="appStore.pinPanelTab(tab.id)" @keydown="onTabKey($event, tab)">
            <component :is="tabIcon(tab)" :size="17" /><span>{{ tab.title }}</span><span v-if="tab.kind === 'diff'" class="panel-tab-kind">审查</span>
          </button>
          <button class="panel-tab-close" type="button" :aria-label="'关闭 ' + tab.title" @click="void appStore.closePanelTab(tab.id)"><X :size="14" /></button>
        </div>
      </div>
      <button id="panel-add-button" class="panel-icon-button" type="button" popovertarget="panel-add-menu" aria-label="新建标签" title="新建标签"><Plus :size="20" /></button>
      <button class="panel-icon-button panel-expand" type="button" :aria-label="appStore.activePanel?.expanded ? '还原面板' : '展开面板'" @click="toggleExpanded"><Minimize2 v-if="appStore.activePanel?.expanded" :size="17" /><Maximize2 v-else :size="17" /></button>
      <button class="panel-icon-button" type="button" :aria-label="tr('topbar.closeInspector')" @click="appStore.toggleInspector()"><PanelRightClose :size="18" /></button>
    </div>
    <div id="panel-add-menu" ref="addMenu" popover class="panel-menu" aria-label="新建标签">
      <button type="button" @click="addTab('terminal')"><Terminal :size="18" />终端<kbd>Ctrl+`</kbd></button>
      <button type="button" @click="addTab('browser')"><Globe :size="18" />浏览器<kbd>Ctrl+T</kbd></button>
      <button type="button" @click="addTab('changes')"><FolderOpen :size="18" />文件<kbd>Ctrl+P</kbd></button>
    </div>
    <div v-if="currentTab?.kind === 'file' || currentTab?.kind === 'diff'" class="panel-pathbar">
      <nav class="code-breadcrumb" :title="currentTab.path" aria-label="File path">
        <template v-for="(part, index) in breadcrumbs(currentTab.path || '')" :key="index">
          <ChevronRight v-if="index" :size="15" aria-hidden="true" />
          <span :class="{ 'is-current': index === breadcrumbs(currentTab.path || '').length - 1 }">{{ part }}</span>
        </template>
      </nav>
      <button class="panel-icon-button" :class="{ 'is-active': currentTab.treeOpen }" type="button" aria-label="文件目录" :aria-expanded="!!currentTab.treeOpen" title="文件目录" @click="toggleTree"><FolderOpen :size="19" /></button>
      <div v-if="!remoteWorkspace" class="panel-open-split">
        <button type="button" title="使用系统默认应用打开文件" @click="void appStore.openActiveRepositoryFile()"><ExternalLink :size="18" />打开</button>
        <button id="panel-open-button" type="button" popovertarget="panel-open-menu" aria-label="打开选项"><ChevronDown :size="14" /></button>
      </div>
    </div>
    <div id="panel-open-menu" ref="openMenu" popover class="panel-menu"><button type="button" @click="openMenu?.hidePopover(); void appStore.openActiveRepositoryFile(true)"><FolderOpen :size="17" />在文件资源管理器中显示</button></div>
    <p v-if="currentTab?.error && (currentTab.kind === 'browser' || currentTab.kind === 'terminal')" class="diff-notice error-text" role="alert">{{ currentTab.error }}</p>
    <div class="panel-body" :class="{ 'has-tree': (currentTab?.kind === 'file' || currentTab?.kind === 'diff') && currentTab.treeOpen }">
    <div v-if="currentTab?.kind === 'file'" :key="currentTab.id" class="inspector-content file-preview-panel">
      <div v-if="appStore.activeRepositoryFilePreviewLoading && !filePreview" class="repository-state" :class="ui.empty"><LoaderCircle :size="18" class="is-spinning" /></div>
      <div v-else-if="appStore.activeRepositoryFilePreviewError" class="repository-state error-text" :class="ui.empty">{{ appStore.activeRepositoryFilePreviewError }}</div>
      <template v-else-if="filePreview">
        <img v-if="filePreview.mediaType?.startsWith('image/') && filePreview.dataUrl" class="file-media-preview" :src="filePreview.dataUrl" :alt="filePreviewName" />
        <audio v-else-if="filePreview.mediaType?.startsWith('audio/') && filePreview.dataUrl" class="file-audio-preview" :src="filePreview.dataUrl" controls />
        <object v-else-if="filePreview.mediaType === 'application/pdf' && filePreview.dataUrl" class="file-pdf-preview" :data="filePreview.dataUrl" type="application/pdf"><span>{{ tr("files.pdfUnavailable") }}</span></object>
        <div v-else-if="spreadsheet" class="file-spreadsheet-preview">
          <div class="spreadsheet-scroll" role="region" :aria-label="tr('files.spreadsheetPreview')" tabindex="0">
            <table v-if="activeSheet?.rows.length" class="spreadsheet-grid">
              <thead><tr><th class="spreadsheet-corner" aria-hidden="true"></th><th v-for="column in spreadsheetColumns" :key="column" scope="col">{{ spreadsheetColumnName(column) }}</th></tr></thead>
              <tbody><tr v-for="(row, rowIndex) in activeSheet.rows" :key="rowIndex"><th scope="row">{{ rowIndex + 1 }}</th><td v-for="column in spreadsheetColumns" :key="column" :title="row[column] || undefined">{{ row[column] ?? '' }}</td></tr></tbody>
            </table>
            <div v-else class="spreadsheet-empty">{{ tr("files.emptySpreadsheet") }}</div>
          </div>
          <div class="spreadsheet-tabs" role="tablist" :aria-label="tr('files.workbookSheets')">
            <button v-for="(sheet, index) in spreadsheet.sheets" :key="`${index}-${sheet.name}`" type="button" role="tab" :aria-selected="activeSpreadsheetSheet === index" :class="{ 'is-active': activeSpreadsheetSheet === index }" @click="activeSpreadsheetSheet = index">{{ sheet.name }}</button>
          </div>
        </div>
        <template v-else-if="filePreview.mediaType === 'text/markdown'">
          <div class="markdown-preview-toggle" role="group" :aria-label="tr('files.markdownMode')">
            <button type="button" :class="{ 'is-active': markdownRendered }" @click="markdownRendered = true">{{ tr("files.rendered") }}</button>
            <button type="button" :class="{ 'is-active': !markdownRendered }" @click="markdownRendered = false">{{ tr("files.source") }}</button>
          </div>
          <div v-if="markdownRendered" class="file-markdown-preview"><MarkdownBody :text="filePreview.content ?? ''" /></div>
          <CodePreview v-else flush :path="filePreview.path" :content="filePreview.content ?? ''" :label="tr('files.previewContent')" />
        </template>
        <div v-else-if="filePreview.binary" class="repository-state" :class="ui.empty"><Binary :size="18" /><span>{{ tr("files.binaryPreview") }}</span></div>
        <CodePreview v-else flush :path="filePreview.path" :content="filePreview.content ?? ''" :label="tr('files.previewContent')" />
        <div v-if="filePreview.truncated" class="diff-notice">{{ tr("files.previewTruncated") }}</div>
      </template>
    </div>

    <div v-else-if="currentTab?.kind === 'diff'" :key="currentTab.id" class="inspector-content repository-panel flex min-h-0 flex-col overflow-hidden p-0 text-sm">
      <div class="repository-diff">
        <div v-if="appStore.activeRepositoryDiffLoading" class="repository-state" :class="ui.empty"><LoaderCircle :size="18" class="is-spinning" /></div>
        <div v-else-if="appStore.activeRepositoryDiffError && !activeDiff" class="repository-state error-text" :class="ui.empty">{{ appStore.activeRepositoryDiffError }}</div>
        <template v-else-if="activeDiff">
          <div v-if="appStore.activeRepositoryDiffError" class="diff-notice error-text">{{ appStore.activeRepositoryDiffError }}</div>
          <div v-if="activeDiff.binary" class="repository-state" :class="ui.empty"><FileDiff :size="18" /><span>Binary file changed</span></div>
          <template v-else>
            <section v-if="activeDiff.staged" class="diff-section">
              <header>Staged changes</header>
              <pre aria-label="Staged diff"><code><span v-for="row in visibleDiffRows(activeDiff.staged, 'staged')" :key="row.key" class="diff-line" :class="row.kind"><span class="diff-line-number" aria-hidden="true">{{ row.lineNumber }}</span><span class="diff-line-text"><span v-for="(segment, index) in row.segments" :key="index" :class="segment.classes">{{ segment.text }}</span></span></span></code></pre>
            </section>
            <section v-if="activeDiff.working" class="diff-section">
              <header>{{ activeDiff.session ? tr("inspector.sessionChanges") : "Working tree" }}</header>
              <pre aria-label="Working tree diff"><code><span v-for="row in visibleDiffRows(activeDiff.working, 'working')" :key="row.key" class="diff-line" :class="row.kind"><span class="diff-line-number" aria-hidden="true">{{ row.lineNumber }}</span><span class="diff-line-text"><span v-for="(segment, index) in row.segments" :key="index" :class="segment.classes">{{ segment.text }}</span></span></span></code></pre>
            </section>
            <section v-if="activeDiff.content || (!activeDiff.staged && !activeDiff.working)" class="diff-section">
              <header>Untracked file</header>
              <pre aria-label="Untracked file content"><code><span v-for="row in visibleDiffRows(activeDiff.content ?? '', 'content')" :key="row.key" class="diff-line is-addition"><span class="diff-line-number" aria-hidden="true">{{ row.lineNumber }}</span><span class="diff-line-text"><span v-for="(segment, index) in row.segments" :key="index" :class="segment.classes">{{ segment.text }}</span></span></span></code></pre>
            </section>
          </template>
          <div v-if="activeDiff.truncated" class="diff-notice">Preview truncated at the safety limit.</div>
        </template>
      </div>
    </div>

    <TerminalPane v-else-if="currentTab?.kind === 'terminal'" />
    <BrowserPane v-else-if="currentTab?.kind === 'browser'" :key="currentTab.id" :tab="currentTab" />
    <div v-if="currentTab?.kind === 'files' || ((currentTab?.kind === 'file' || currentTab?.kind === 'diff') && currentTab.treeOpen)" key="directory" class="panel-directory">
      <label class="panel-file-filter"><Search :size="16" /><input v-model="filter" type="search" placeholder="筛选文件..." aria-label="筛选文件" /></label>
      <div class="panel-file-tree">
        <div v-if="appStore.activeRepositoryLoading && !repository" class="repository-state"><LoaderCircle :size="18" class="is-spinning" /></div>
        <p v-else-if="appStore.activeRepositoryError" class="error-text" role="alert">{{ appStore.activeRepositoryError }}</p>
        <template v-else>
          <p v-if="sessionChangesError" class="error-text">{{ sessionChangesError }}</p>
          <FileTreeNode v-for="node in fileTree" :key="node.path" :node="node" :change-statuses="changeStatusByPath" :rollback-actions="rollbackActions" :rollback-armed="rollbackArmed" :selected-path="currentTab?.path" :expanded="expandedDirectories" :filtering="!!filter" @open="openTreeFile" @pin="appStore.openRepositoryFilePreview($event, undefined, true)" @diff="appStore.openRepositoryDiff" @mention="appStore.insertFileMention" @rollback="confirmRollback" @expand="appStore.scheduleDesktopStateSave()" />
          <p v-if="!fileTree.length" class="panel-tree-empty">{{ filter ? '没有匹配的文件' : tr('inspector.noFiles') }}</p>
          <p v-if="fileListTruncated" class="diff-notice">{{ tr('inspector.firstFiles', { count: visibleFiles.length }) }}</p>
        </template>
      </div>
    </div>
    </div>
  </aside>
</template>
