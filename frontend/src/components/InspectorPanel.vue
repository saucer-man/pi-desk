<script setup lang="ts">
import { ui } from "../ui/classes";
import { ArrowLeft, Binary, Check, ChevronDown, ExternalLink, FileCode2, FileDiff, FolderGit2, FolderOpen, GitBranch, LoaderCircle, PanelRightClose, RefreshCw, Search } from "lucide-vue-next";
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useAppStore } from "../stores/app";
import { buildRepositoryTree } from "../utils/fileMentions";
import { rankFuzzy } from "../utils/fuzzySearch";
import CodePreview from "./CodePreview.vue";
import FileTreeNode from "./FileTreeNode.vue";
import MarkdownBody from "./MarkdownBody.vue";
import { tr } from "../i18n";

const TerminalPane = defineAsyncComponent(() => import("./TerminalPane.vue"));

const appStore = useAppStore();
const state = computed(() => appStore.activeSessionState);
const stats = computed(() => appStore.sessionStatsByThread[appStore.activeThreadId]);
const fileQuery = ref("");
const fileScope = ref<"all" | "changed">("all");
const branchQuery = ref("");
const branchesOpen = ref(false);
const repository = computed(() => appStore.activeRepository);
const repositoryFiles = computed(() => repository.value?.files ?? []);
const changedFiles = computed(() => repository.value?.git.files ?? []);
const activeDiff = computed(() => appStore.activeRepositoryDiff);
const remoteWorkspace = computed(() => appStore.activeThread ? appStore.remoteWorkspaceForThread(appStore.activeThread) : undefined);
const workspaceLabel = computed(() => remoteWorkspace.value?.remoteRoot || appStore.activeThread?.workspacePath || "");
const filePreview = computed(() => appStore.activeRepositoryFilePreview);
const filePreviewName = computed(() => appStore.activeRepositoryFilePreviewPath.split(/[\\/]/).pop() || appStore.activeRepositoryFilePreviewPath);
const markdownRendered = ref(true);
const filteredBranches = computed(() => {
  const query = branchQuery.value.trim().toLocaleLowerCase();
  const branches = appStore.activeRepositoryBranches?.branches ?? [];
  return (query ? branches.filter((branch) => `${branch.name} ${branch.upstream}`.toLocaleLowerCase().includes(query)) : branches).slice(0, 500);
});
const localBranches = computed(() => filteredBranches.value.filter((branch) => !branch.remote));
const remoteBranches = computed(() => filteredBranches.value.filter((branch) => branch.remote));
const normalizedChangedFiles = computed(() => changedFiles.value.map((file) => ({
  ...file,
  path: file.path.replaceAll("\\", "/"),
})));
const changeStatusByPath = computed<Record<string, string>>(() => Object.fromEntries(
  normalizedChangedFiles.value.map((file) => [file.path, reviewChangeLabel(file)]),
));
const scopedFilePaths = computed(() => fileScope.value === "changed"
  ? normalizedChangedFiles.value.map((file) => file.path)
  : [...new Set([
    ...repositoryFiles.value.map((file) => file.path.replaceAll("\\", "/")),
    ...normalizedChangedFiles.value.map((file) => file.path),
  ])]);
const matchingFilePaths = computed(() => {
  return rankFuzzy(scopedFilePaths.value, fileQuery.value, (path) => [path.split("/").pop() ?? path, path]);
});
const filteredFiles = computed(() => matchingFilePaths.value.slice(0, 500));
const fileListTruncated = computed(() => matchingFilePaths.value.length > filteredFiles.value.length
  || (fileScope.value === "all" && Boolean(repository.value?.truncated)));
const fileTree = computed(() => buildRepositoryTree(filteredFiles.value));

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
  if (line.startsWith("+") && !line.startsWith("+++")) return "is-addition";
  if (line.startsWith("-") && !line.startsWith("---")) return "is-deletion";
  if (line.startsWith("diff ") || line.startsWith("index ") || line.startsWith("---") || line.startsWith("+++")) return "is-meta";
  return "";
}

function diffLines(value: string): string[] {
  return value.replace(/\n$/, "").split("\n");
}

function toggleBranches() {
  branchesOpen.value = !branchesOpen.value;
  if (branchesOpen.value) void appStore.refreshActiveRepositoryBranches();
}

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
  branchesOpen.value = false;
  void appStore.refreshActiveRepository();
});
watch(() => appStore.activeRepositoryFilePreviewPath, () => { markdownRendered.value = true; });
</script>

<template>
  <aside class="inspector absolute bottom-4 right-4 top-[68px] z-30 grid w-[var(--inspector-width)] grid-rows-[52px_minmax(0,1fr)] overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--bg-panel)] shadow-lg max-[1279px]:bottom-0 max-[1279px]:right-0 max-[1279px]:top-[52px] max-[1279px]:rounded-none max-[1279px]:rounded-l-xl max-[520px]:left-14 max-[520px]:w-[calc(100%_-_56px)] max-[520px]:rounded-none" :class="ui.root" :aria-label="tr('inspector.label')">
    <div class="inspector-header flex min-h-[52px] items-center justify-between border-b border-[var(--border)] px-3">
      <div v-if="appStore.activeRepositoryFilePreviewPath" class="inspector-file-header flex min-w-0 flex-1 items-center gap-2">
        <button class="icon-button" :class="ui.iconButton" type="button" :title="tr('files.closePreview')" @click="appStore.closeRepositoryFilePreview()"><ArrowLeft :size="15" /></button>
        <FileCode2 :size="14" aria-hidden="true" />
        <strong :title="filePreview?.absolutePath || appStore.activeRepositoryFilePreviewPath">{{ filePreviewName }}</strong>
        <span v-if="appStore.activeRepositoryFilePreviewLine" class="file-preview-line">:{{ appStore.activeRepositoryFilePreviewLine }}</span>
        <button class="icon-button" :class="ui.iconButton" type="button" :title="tr('files.refreshPreview')" :disabled="appStore.activeRepositoryFilePreviewLoading" @click="refreshPreview"><RefreshCw :size="14" :class="{ 'is-spinning': appStore.activeRepositoryFilePreviewLoading }" /></button>
        <div v-if="!remoteWorkspace" class="inspector-file-actions">
          <button class="icon-button" :class="ui.iconButton" type="button" :title="tr('files.open')" @click="void appStore.openPreviewedRepositoryFile()"><ExternalLink :size="14" /></button>
          <button class="icon-button" :class="ui.iconButton" type="button" :title="tr('files.reveal')" @click="void appStore.openPreviewedRepositoryFile(true)"><FolderOpen :size="14" /></button>
        </div>
      </div>
      <div v-else class="inspector-tabs flex h-full items-stretch" role="tablist">
        <button class="relative whitespace-nowrap border-0 bg-transparent px-3 text-[calc(13px+var(--font-size-delta))] text-[var(--text-secondary)] after:absolute after:inset-x-3 after:bottom-0 after:h-0.5 after:scale-x-0 after:bg-[var(--text)] after:transition-transform after:duration-150 hover:text-[var(--text)] active:bg-[var(--bg-hover)]" :class="{ 'is-active text-[var(--text)] after:scale-x-100': appStore.inspectorTab === 'changes' }" type="button" role="tab" :aria-selected="appStore.inspectorTab === 'changes'" @click="appStore.setInspectorTab('changes')">{{ tr("inspector.files") }}</button>
        <button class="relative whitespace-nowrap border-0 bg-transparent px-3 text-[calc(13px+var(--font-size-delta))] text-[var(--text-secondary)] after:absolute after:inset-x-3 after:bottom-0 after:h-0.5 after:scale-x-0 after:bg-[var(--text)] after:transition-transform after:duration-150 hover:text-[var(--text)] active:bg-[var(--bg-hover)]" :class="{ 'is-active text-[var(--text)] after:scale-x-100': appStore.inspectorTab === 'context' }" type="button" role="tab" :aria-selected="appStore.inspectorTab === 'context'" @click="appStore.setInspectorTab('context')">{{ tr("inspector.context") }}</button>
        <button class="relative whitespace-nowrap border-0 bg-transparent px-3 text-[calc(13px+var(--font-size-delta))] text-[var(--text-secondary)] after:absolute after:inset-x-3 after:bottom-0 after:h-0.5 after:scale-x-0 after:bg-[var(--text)] after:transition-transform after:duration-150 hover:text-[var(--text)] active:bg-[var(--bg-hover)]" :class="{ 'is-active text-[var(--text)] after:scale-x-100': appStore.inspectorTab === 'terminal' }" type="button" role="tab" :aria-selected="appStore.inspectorTab === 'terminal'" @click="appStore.setInspectorTab('terminal')">{{ tr("inspector.terminal") }}</button>
      </div>
      <button class="icon-button ml-auto hidden size-8 shrink-0 place-items-center rounded-lg border border-transparent bg-transparent text-[var(--text-muted)] hover:border-[var(--border)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] max-[520px]:inline-grid" :class="ui.iconButton" type="button" :title="tr('topbar.closeInspector')" :aria-label="tr('topbar.closeInspector')" @click="appStore.toggleInspector()"><PanelRightClose :size="17" /></button>
    </div>

    <div v-if="appStore.activeRepositoryFilePreviewPath" class="inspector-content file-preview-panel">
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
      <div v-if="!appStore.activeRepositoryDiffPath" class="repository-toolbar flex min-h-11 shrink-0 items-center justify-between gap-2 border-b border-[var(--border)] px-3">
        <button class="branch-summary flex min-w-0 items-center gap-1.5 rounded-lg border border-transparent bg-transparent px-1.5 py-1.5 text-xs text-[var(--text-secondary)] hover:border-[var(--border)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] disabled:cursor-default disabled:opacity-60" type="button" title="Show branches" :disabled="!repository?.git.isRepository" @click="toggleBranches">
          <GitBranch :size="14" />
          <span>{{ repository?.git.isRepository ? repository.git.branch || "Detached HEAD" : "Not a Git repository" }}</span>
          <small v-if="repository?.git.ahead">+{{ repository.git.ahead }}</small>
          <small v-if="repository?.git.behind">-{{ repository.git.behind }}</small>
          <ChevronDown :size="13" :class="{ 'is-expanded': branchesOpen }" />
        </button>
        <div class="repository-toolbar-actions flex items-center gap-1">
          <span v-if="changedFiles.length" class="changed-count" title="Changed files">{{ changedFiles.length }}</span>
          <button class="icon-button inline-grid size-8 place-items-center rounded-lg border border-transparent bg-transparent text-[var(--text-muted)] hover:border-[var(--border)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] disabled:cursor-not-allowed disabled:opacity-50" :class="ui.iconButton" type="button" title="Refresh repository" :disabled="appStore.activeRepositoryLoading" @click="void appStore.refreshActiveRepository()">
            <RefreshCw :size="14" :class="{ 'is-spinning': appStore.activeRepositoryLoading }" />
          </button>
        </div>
      </div>
      <div v-if="branchesOpen && !appStore.activeRepositoryDiffPath" class="branch-browser">
        <label class="file-search">
          <Search :size="13" />
          <input :class="ui.input" v-model="branchQuery" type="search" placeholder="Filter branches" aria-label="Filter branches" />
        </label>
        <div v-if="appStore.activeRepositoryBranchesLoading && !appStore.activeRepositoryBranches" class="branch-browser-state"><LoaderCircle :size="16" class="is-spinning" /></div>
        <div v-else-if="appStore.activeRepositoryBranchesError" class="branch-browser-state error-text">{{ appStore.activeRepositoryBranchesError }}</div>
        <template v-else-if="filteredBranches.length">
          <section v-if="localBranches.length" class="branch-group">
            <header>Local</header>
            <div v-for="branch in localBranches" :key="branch.fullName" class="branch-row">
              <Check v-if="branch.current" :size="13" class="current-branch" />
              <GitBranch v-else :size="13" />
              <span :title="branch.fullName">{{ branch.name }}</span>
              <small :title="branch.upstream">{{ branch.upstream }}</small>
              <span class="branch-worktree"><FolderGit2 v-if="branch.worktreePath" :size="13" :title="`Checked out at ${branch.worktreePath}`" /></span>
            </div>
          </section>
          <section v-if="remoteBranches.length" class="branch-group">
            <header>Remote</header>
            <div v-for="branch in remoteBranches" :key="branch.fullName" class="branch-row">
              <GitBranch :size="13" />
              <span :title="branch.fullName">{{ branch.name }}</span>
              <small>{{ branch.commit }}</small>
            </div>
          </section>
        </template>
        <div v-else class="branch-browser-state">No matching branches</div>
      </div>
      <div v-if="appStore.activeRepositoryDiffPath" class="diff-toolbar">
        <button class="icon-button" :class="ui.iconButton" type="button" title="Back to changes" @click="appStore.closeRepositoryDiff()"><ArrowLeft :size="15" /></button>
        <strong :title="appStore.activeRepositoryDiffPath">{{ appStore.activeRepositoryDiffPath }}</strong>
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
              <pre aria-label="Staged diff"><code><span v-for="(line, index) in diffLines(activeDiff.staged)" :key="index" class="diff-line" :class="diffLineClass(line)">{{ `${line}\n` }}</span></code></pre>
            </section>
            <section v-if="activeDiff.working" class="diff-section">
              <header>Working tree</header>
              <pre aria-label="Working tree diff"><code><span v-for="(line, index) in diffLines(activeDiff.working)" :key="index" class="diff-line" :class="diffLineClass(line)">{{ `${line}\n` }}</span></code></pre>
            </section>
            <section v-if="activeDiff.content || (!activeDiff.staged && !activeDiff.working)" class="diff-section">
              <header>Untracked file</header>
              <pre aria-label="Untracked file content"><code>{{ activeDiff.content }}</code></pre>
            </section>
          </template>
          <div v-if="activeDiff.truncated" class="diff-notice">Preview truncated at the safety limit.</div>
        </template>
      </div>
      <template v-else>
        <div class="repository-file-controls grid shrink-0 grid-cols-[auto_minmax(0,1fr)] items-center gap-2 border-b border-[var(--border)] p-2.5">
          <div class="repository-file-filters inline-flex h-8 items-center rounded-lg border border-[var(--border)] bg-[var(--bg-app)] p-0.5" role="group" :aria-label="tr('inspector.fileScope')">
            <button class="h-7 whitespace-nowrap rounded-md border-0 bg-transparent px-2.5 text-[calc(11px+var(--font-size-delta))] text-[var(--text-secondary)] hover:text-[var(--text)] active:bg-[var(--bg-active)] aria-pressed:bg-[var(--bg-raised)] aria-pressed:text-[var(--text)] aria-pressed:shadow-sm" :class="{ 'is-active': fileScope === 'all' }" type="button" :aria-pressed="fileScope === 'all'" @click="fileScope = 'all'">{{ tr("inspector.allFiles") }}</button>
            <button class="h-7 whitespace-nowrap rounded-md border-0 bg-transparent px-2.5 text-[calc(11px+var(--font-size-delta))] text-[var(--text-secondary)] hover:text-[var(--text)] active:bg-[var(--bg-active)] aria-pressed:bg-[var(--bg-raised)] aria-pressed:text-[var(--text)] aria-pressed:shadow-sm" :class="{ 'is-active': fileScope === 'changed' }" type="button" :aria-pressed="fileScope === 'changed'" @click="fileScope = 'changed'">{{ tr("inspector.changes") }}<span v-if="changedFiles.length">{{ changedFiles.length }}</span></button>
          </div>
          <label class="file-search flex h-8 min-w-0 items-center gap-2 rounded-lg border border-[var(--border)] bg-[var(--bg-app)] px-2.5 text-[var(--text-secondary)] focus-within:border-[var(--text)] focus-within:ring-1 focus-within:ring-[var(--text)]/20">
            <Search :size="13" />
            <input :class="ui.input" class="min-w-0 flex-1 border-0 bg-transparent text-xs text-[var(--text)] outline-none placeholder:text-[var(--text-muted)]" v-model="fileQuery" type="search" :placeholder="tr('inspector.filterFiles')" :aria-label="tr('inspector.filterFiles')" />
          </label>
        </div>
        <div v-if="appStore.activeRepositoryStale && repository" class="diff-notice error-text">Repository data is stale. Refresh after reconnecting.</div>
        <div v-if="appStore.activeRepositoryLoading && !repository" class="repository-state" :class="ui.empty"><LoaderCircle :size="18" class="is-spinning" /></div>
        <div v-else-if="appStore.activeRepositoryError && !repository" class="repository-state error-text" :class="ui.empty">{{ appStore.activeRepositoryError }}</div>
        <template v-else-if="fileTree.length">
          <div v-if="fileListTruncated" class="diff-notice">{{ tr("inspector.firstFiles", { count: filteredFiles.length }) }}</div>
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
          <FileDiff v-if="fileScope === 'changed'" :size="18" />
          <span>{{ fileScope === "changed" ? tr("inspector.noChanges") : tr("inspector.noFiles") }}</span>
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
