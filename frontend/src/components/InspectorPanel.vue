<script setup lang="ts">
import { ui } from "../ui/classes";
import { ArrowLeft, Binary, ExternalLink, FileCode2, FileDiff, FolderOpen, LoaderCircle, PanelRightClose, RefreshCw } from "lucide-vue-next";
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useAppStore } from "../stores/app";
import { buildRepositoryTree } from "../utils/fileMentions";
import CodePreview from "./CodePreview.vue";
import FileTreeNode from "./FileTreeNode.vue";
import MarkdownBody from "./MarkdownBody.vue";
import { tr } from "../i18n";

const TerminalPane = defineAsyncComponent(() => import("./TerminalPane.vue"));

const appStore = useAppStore();
const state = computed(() => appStore.activeSessionState);
const stats = computed(() => appStore.sessionStatsByThread[appStore.activeThreadId]);
const repository = computed(() => appStore.activeRepository);
const repositoryFiles = computed(() => repository.value?.files ?? []);
const changedFiles = computed(() => repository.value?.git.files ?? []);
const activeDiff = computed(() => appStore.activeRepositoryDiff);
const remoteWorkspace = computed(() => appStore.activeThread ? appStore.remoteWorkspaceForThread(appStore.activeThread) : undefined);
const workspaceLabel = computed(() => remoteWorkspace.value?.remoteRoot || appStore.activeThread?.workspacePath || "");
const filePreview = computed(() => appStore.activeRepositoryFilePreview);
const filePreviewName = computed(() => appStore.activeRepositoryFilePreviewPath.split(/[\\/]/).pop() || appStore.activeRepositoryFilePreviewPath);
const markdownRendered = ref(true);
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
const visibleFiles = computed(() => filePaths.value.slice(0, 500));
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
  let oldLine: number | undefined;
  let newLine: number | undefined;
  return value.replace(/\n$/, "").split("\n").map((line, index) => {
    const kind = diffLineClass(line);
    if (kind === "is-hunk") {
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

const activeDiffStats = computed(() => {
  if (!activeDiff.value) return { additions: 0, deletions: 0 };
  const rows = [
    ...diffRows(activeDiff.value.staged ?? ""),
    ...diffRows(activeDiff.value.working ?? ""),
    ...untrackedRows(activeDiff.value.content ?? ""),
  ];
  return {
    additions: rows.filter((row) => row.kind === "is-addition").length,
    deletions: rows.filter((row) => row.kind === "is-deletion").length,
  };
});

function openTreeFile(path: string) {
  if (changeStatusByPath.value[path] === "D") void appStore.openRepositoryDiff(path);
  else void appStore.openRepositoryFilePreview(path);
}

function refreshPreview() {
  const path = appStore.activeRepositoryFilePreviewPath;
  if (path && !appStore.activeRepositoryFilePreviewLoading) {
    void appStore.openRepositoryFilePreview(path, appStore.activeRepositoryFilePreviewLine);
  }
}

function refreshPreviewWhenVisible() {
  if (document.visibilityState === "visible") refreshPreview();
}

onMounted(() => {
  void appStore.refreshActiveRepository();
  window.addEventListener("focus", refreshPreview);
  document.addEventListener("visibilitychange", refreshPreviewWhenVisible);
});
onBeforeUnmount(() => {
  window.removeEventListener("focus", refreshPreview);
  document.removeEventListener("visibilitychange", refreshPreviewWhenVisible);
});
watch(() => appStore.activeThreadId, () => {
  void appStore.refreshActiveRepository();
});
watch(() => appStore.activeRepositoryFilePreviewPath, () => { markdownRendered.value = true; });
</script>

<template>
  <aside class="inspector absolute bottom-0 right-0 top-[var(--topbar-height)] z-30 grid w-[var(--inspector-width)] grid-rows-[34px_minmax(0,1fr)] overflow-hidden rounded-none border-y-0 border-r-0 border-l border-[var(--border)] bg-[var(--bg-panel)] shadow-none max-[520px]:left-[var(--sidebar-collapsed-width)] max-[520px]:w-[calc(100%_-_var(--sidebar-collapsed-width))]" :class="ui.root" :aria-label="tr('inspector.label')">
    <div class="inspector-header flex items-center justify-between border-b border-[var(--border)]">
      <div class="inspector-tabs flex h-full items-stretch" role="tablist">
        <button class="relative whitespace-nowrap border-0 bg-transparent px-3 text-[calc(13px+var(--font-size-delta))] text-[var(--text-secondary)] after:absolute after:inset-x-3 after:bottom-0 after:h-0.5 after:scale-x-0 after:bg-[var(--text)] after:transition-transform after:duration-150 hover:text-[var(--text)] active:bg-[var(--bg-hover)]" :class="{ 'is-active text-[var(--text)] after:scale-x-100': appStore.inspectorTab === 'changes' }" type="button" role="tab" :aria-selected="appStore.inspectorTab === 'changes'" @click="appStore.setInspectorTab('changes')">{{ tr("inspector.files") }}</button>
        <button class="relative whitespace-nowrap border-0 bg-transparent px-3 text-[calc(13px+var(--font-size-delta))] text-[var(--text-secondary)] after:absolute after:inset-x-3 after:bottom-0 after:h-0.5 after:scale-x-0 after:bg-[var(--text)] after:transition-transform after:duration-150 hover:text-[var(--text)] active:bg-[var(--bg-hover)]" :class="{ 'is-active text-[var(--text)] after:scale-x-100': appStore.inspectorTab === 'context' }" type="button" role="tab" :aria-selected="appStore.inspectorTab === 'context'" @click="appStore.setInspectorTab('context')">{{ tr("inspector.context") }}</button>
        <button class="relative whitespace-nowrap border-0 bg-transparent px-3 text-[calc(13px+var(--font-size-delta))] text-[var(--text-secondary)] after:absolute after:inset-x-3 after:bottom-0 after:h-0.5 after:scale-x-0 after:bg-[var(--text)] after:transition-transform after:duration-150 hover:text-[var(--text)] active:bg-[var(--bg-hover)]" :class="{ 'is-active text-[var(--text)] after:scale-x-100': appStore.inspectorTab === 'terminal' }" type="button" role="tab" :aria-selected="appStore.inspectorTab === 'terminal'" @click="appStore.setInspectorTab('terminal')">{{ tr("inspector.terminal") }}</button>
      </div>
      <button class="icon-button ml-auto hidden size-8 shrink-0 place-items-center rounded-lg border border-transparent bg-transparent text-[var(--text-muted)] hover:border-[var(--border)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] max-[520px]:inline-grid" :class="ui.iconButton" type="button" :title="tr('topbar.closeInspector')" :aria-label="tr('topbar.closeInspector')" @click="appStore.toggleInspector()"><PanelRightClose :size="17" /></button>
    </div>

    <div v-if="appStore.inspectorTab === 'changes' && appStore.activeRepositoryFilePreviewPath" class="inspector-content file-preview-panel">
      <div class="inspector-file-header file-preview-toolbar">
        <button class="icon-button file-preview-toolbar-button" :class="ui.iconButton" type="button" :title="tr('files.closePreview')" @click="appStore.closeRepositoryFilePreview()"><ArrowLeft :size="15" /></button>
        <FileCode2 :size="14" aria-hidden="true" />
        <strong :title="filePreview?.absolutePath || appStore.activeRepositoryFilePreviewPath">{{ filePreviewName }}</strong>
        <span v-if="appStore.activeRepositoryFilePreviewLine" class="file-preview-line">:{{ appStore.activeRepositoryFilePreviewLine }}</span>
        <button class="icon-button file-preview-toolbar-button" :class="ui.iconButton" type="button" :title="tr('files.refreshPreview')" :disabled="appStore.activeRepositoryFilePreviewLoading" @click="refreshPreview"><RefreshCw :size="14" :class="{ 'is-spinning': appStore.activeRepositoryFilePreviewLoading }" /></button>
        <div v-if="!remoteWorkspace" class="inspector-file-actions">
          <button class="icon-button file-preview-toolbar-button" :class="ui.iconButton" type="button" :title="tr('files.open')" @click="void appStore.openPreviewedRepositoryFile()"><ExternalLink :size="14" /></button>
          <button class="icon-button file-preview-toolbar-button" :class="ui.iconButton" type="button" :title="tr('files.reveal')" @click="void appStore.openPreviewedRepositoryFile(true)"><FolderOpen :size="14" /></button>
        </div>
      </div>
      <div v-if="appStore.activeRepositoryFilePreviewLoading" class="repository-state" :class="ui.empty"><LoaderCircle :size="18" class="is-spinning" /></div>
      <div v-else-if="appStore.activeRepositoryFilePreviewError" class="repository-state error-text" :class="ui.empty">{{ appStore.activeRepositoryFilePreviewError }}</div>
      <template v-else-if="filePreview">
        <img v-if="filePreview.mediaType?.startsWith('image/') && filePreview.dataUrl" class="file-media-preview" :src="filePreview.dataUrl" :alt="filePreviewName" />
        <audio v-else-if="filePreview.mediaType?.startsWith('audio/') && filePreview.dataUrl" class="file-audio-preview" :src="filePreview.dataUrl" controls />
        <object v-else-if="filePreview.mediaType === 'application/pdf' && filePreview.dataUrl" class="file-pdf-preview" :data="filePreview.dataUrl" type="application/pdf"><span>{{ tr("files.pdfUnavailable") }}</span></object>
        <template v-else-if="filePreview.mediaType === 'text/markdown'">
          <div class="markdown-preview-toggle" role="group" :aria-label="tr('files.markdownMode')">
            <button type="button" :class="{ 'is-active': markdownRendered }" @click="markdownRendered = true">{{ tr("files.rendered") }}</button>
            <button type="button" :class="{ 'is-active': !markdownRendered }" @click="markdownRendered = false">{{ tr("files.source") }}</button>
          </div>
          <MarkdownBody v-if="markdownRendered" class="file-markdown-preview" :text="filePreview.content ?? ''" />
          <CodePreview v-else flush :path="filePreview.path" :content="filePreview.content ?? ''" :label="tr('files.previewContent')" />
        </template>
        <div v-else-if="filePreview.binary" class="repository-state" :class="ui.empty"><Binary :size="18" /><span>{{ tr("files.binaryPreview") }}</span></div>
        <CodePreview v-else flush :path="filePreview.path" :content="filePreview.content ?? ''" :label="tr('files.previewContent')" />
        <div v-if="filePreview.truncated" class="diff-notice">{{ tr("files.previewTruncated") }}</div>
      </template>
    </div>

    <div v-else-if="appStore.inspectorTab === 'changes'" class="inspector-content repository-panel flex min-h-0 flex-col overflow-hidden p-0 text-sm">
      <div v-if="appStore.activeRepositoryDiffPath" class="diff-toolbar">
        <button class="icon-button" :class="ui.iconButton" type="button" title="Back to changes" @click="appStore.closeRepositoryDiff()"><ArrowLeft :size="15" /></button>
        <div class="diff-toolbar-title">
          <strong :title="appStore.activeRepositoryDiffPath">{{ appStore.activeRepositoryDiffPath }}</strong>
          <span v-if="activeDiff" class="diff-stats" :aria-label="tr('conversation.changeTotals', activeDiffStats)"><span>+{{ activeDiffStats.additions }}</span><span>-{{ activeDiffStats.deletions }}</span></span>
        </div>
        <div v-if="!remoteWorkspace" class="diff-toolbar-actions">
          <button class="icon-button" :class="ui.iconButton" type="button" title="Open file" @click="void appStore.openActiveRepositoryFile()"><ExternalLink :size="14" /></button>
          <button class="icon-button" :class="ui.iconButton" type="button" title="Show in file manager" @click="void appStore.openActiveRepositoryFile(true)"><FolderOpen :size="14" /></button>
        </div>
      </div>
      <div v-if="appStore.activeRepositoryDiffPath" class="repository-diff">
        <div v-if="appStore.activeRepositoryDiffLoading" class="repository-state" :class="ui.empty"><LoaderCircle :size="18" class="is-spinning" /></div>
        <div v-else-if="appStore.activeRepositoryDiffError && !activeDiff" class="repository-state error-text" :class="ui.empty">{{ appStore.activeRepositoryDiffError }}</div>
        <template v-else-if="activeDiff">
          <div v-if="appStore.activeRepositoryDiffError" class="diff-notice error-text">{{ appStore.activeRepositoryDiffError }}</div>
          <div v-if="activeDiff.binary" class="repository-state" :class="ui.empty"><FileDiff :size="18" /><span>Binary file changed</span></div>
          <template v-else>
            <section v-if="activeDiff.staged" class="diff-section">
              <header>Staged changes</header>
              <pre aria-label="Staged diff"><code><span v-for="row in diffRows(activeDiff.staged)" :key="row.key" class="diff-line" :class="row.kind"><span class="diff-line-number diff-line-number--old" aria-hidden="true">{{ row.oldLine }}</span><span class="diff-line-number diff-line-number--new" aria-hidden="true">{{ row.newLine }}</span><span class="diff-line-marker">{{ row.marker }}</span><span class="diff-line-text">{{ row.text }}</span></span></code></pre>
            </section>
            <section v-if="activeDiff.working" class="diff-section">
              <header>{{ activeDiff.session ? tr("inspector.sessionChanges") : "Working tree" }}</header>
              <pre aria-label="Working tree diff"><code><span v-for="row in diffRows(activeDiff.working)" :key="row.key" class="diff-line" :class="row.kind"><span class="diff-line-number diff-line-number--old" aria-hidden="true">{{ row.oldLine }}</span><span class="diff-line-number diff-line-number--new" aria-hidden="true">{{ row.newLine }}</span><span class="diff-line-marker">{{ row.marker }}</span><span class="diff-line-text">{{ row.text }}</span></span></code></pre>
            </section>
            <section v-if="activeDiff.content || (!activeDiff.staged && !activeDiff.working)" class="diff-section">
              <header>Untracked file</header>
              <pre aria-label="Untracked file content"><code><span v-for="row in untrackedRows(activeDiff.content ?? '')" :key="row.key" class="diff-line is-addition"><span class="diff-line-number diff-line-number--old" aria-hidden="true" /><span class="diff-line-number diff-line-number--new" aria-hidden="true">{{ row.newLine }}</span><span class="diff-line-marker">+</span><span class="diff-line-text">{{ row.text }}</span></span></code></pre>
            </section>
          </template>
          <div v-if="activeDiff.truncated" class="diff-notice">Preview truncated at the safety limit.</div>
        </template>
      </div>
      <template v-else>
        <div v-if="appStore.activeRepositoryStale && repository" class="diff-notice error-text">Repository data is stale. Refresh after reconnecting.</div>
        <div v-if="appStore.activeRepositoryLoading && !repository" class="repository-state" :class="ui.empty"><LoaderCircle :size="18" class="is-spinning" /></div>
        <div v-else-if="appStore.activeRepositoryError && !repository" class="repository-state error-text" :class="ui.empty">{{ appStore.activeRepositoryError }}</div>
        <template v-else-if="fileTree.length">
          <div v-if="fileListTruncated" class="diff-notice">{{ tr("inspector.firstFiles", { count: visibleFiles.length }) }}</div>
          <div class="file-tree">
            <FileTreeNode
              v-for="node in fileTree"
              :key="`${node.directory}-${node.path}`"
              :node="node"
              :change-statuses="changeStatusByPath"
              @open="openTreeFile"
              @diff="appStore.openRepositoryDiff"
              @mention="appStore.insertFileMention"
            />
          </div>
        </template>
        <div v-else class="repository-state flex min-h-32 flex-1 items-center justify-center gap-2 text-xs text-[var(--text-secondary)]" :class="ui.empty">
          <span>{{ tr("inspector.noFiles") }}</span>
        </div>
      </template>
    </div>

    <div v-else-if="appStore.inspectorTab === 'context'" class="inspector-content context-panel">
      <dl v-if="appStore.activeThread">
        <div><dt>{{ tr("inspector.workspace") }}</dt><dd :title="workspaceLabel">{{ workspaceLabel }}</dd></div>
        <div><dt>{{ tr("inspector.piProcess") }}</dt><dd>{{ appStore.activeThread.started ? tr("inspector.generation", { generation: appStore.activeThread.generation }) : tr("common.notStarted") }}</dd></div>
        <div><dt>{{ tr("inspector.session") }}</dt><dd :title="appStore.activeThread.title">{{ appStore.activeThread.title }}</dd></div>
        <div><dt>{{ tr("inspector.sessionId") }}</dt><dd :title="state?.sessionId || appStore.activeThread.sessionId">{{ state?.sessionId || appStore.activeThread.sessionId || tr("inspector.createdOnPrompt") }}</dd></div>
        <div><dt>{{ tr("inspector.model") }}</dt><dd>{{ state?.model ? `${state.model.provider}/${state.model.id}` : tr("common.auto") }}</dd></div>
        <div><dt>{{ tr("inspector.reasoning") }}</dt><dd>{{ state?.thinkingLevel || tr("common.auto") }}</dd></div>
        <div><dt>{{ tr("inspector.messages") }}</dt><dd>{{ stats?.totalMessages ?? state?.messageCount ?? 0 }}</dd></div>
        <div><dt>{{ tr("inspector.tokens") }}</dt><dd>{{ stats?.tokens?.total?.toLocaleString() ?? "-" }}</dd></div>
        <div><dt>{{ tr("inspector.cost") }}</dt><dd>{{ stats?.cost ? `$${stats.cost.toFixed(4)}` : "-" }}</dd></div>
        <div><dt>{{ tr("inspector.contextUsage") }}</dt><dd>{{ stats?.contextUsage?.percent != null ? `${stats.contextUsage.percent.toFixed(1)}%` : "-" }}</dd></div>
      </dl>
      <div v-else class="panel-empty" :class="ui.empty"><span>{{ tr("inspector.selectTask") }}</span></div>
    </div>

    <TerminalPane v-else />
  </aside>
</template>
