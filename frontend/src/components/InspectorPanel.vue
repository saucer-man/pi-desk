<script setup lang="ts">
import { ArrowLeft, AtSign, Binary, Check, ChevronDown, ExternalLink, FileCode2, FileDiff, FolderGit2, FolderOpen, GitBranch, LoaderCircle, RefreshCw, Search } from "lucide-vue-next";
import { computed, defineAsyncComponent, onMounted, ref, watch } from "vue";
import { useAppStore } from "../stores/app";
import { buildRepositoryTree } from "../utils/fileMentions";
import FileTreeNode from "./FileTreeNode.vue";
import { tr } from "../i18n";

const TerminalPane = defineAsyncComponent(() => import("./TerminalPane.vue"));

const appStore = useAppStore();
const state = computed(() => appStore.activeSessionState);
const stats = computed(() => appStore.sessionStatsByThread[appStore.activeThreadId]);
const fileQuery = ref("");
const branchQuery = ref("");
const branchesOpen = ref(false);
const repository = computed(() => appStore.activeRepository);
const repositoryFiles = computed(() => repository.value?.files ?? []);
const changedFiles = computed(() => repository.value?.git.files ?? []);
const activeDiff = computed(() => appStore.activeRepositoryDiff);
const filePreview = computed(() => appStore.activeRepositoryFilePreview);
const filePreviewName = computed(() => appStore.activeRepositoryFilePreviewPath.split(/[\\/]/).pop() || appStore.activeRepositoryFilePreviewPath);
const filteredBranches = computed(() => {
  const query = branchQuery.value.trim().toLocaleLowerCase();
  const branches = appStore.activeRepositoryBranches?.branches ?? [];
  return (query ? branches.filter((branch) => `${branch.name} ${branch.upstream}`.toLocaleLowerCase().includes(query)) : branches).slice(0, 500);
});
const localBranches = computed(() => filteredBranches.value.filter((branch) => !branch.remote));
const remoteBranches = computed(() => filteredBranches.value.filter((branch) => branch.remote));
const filteredFiles = computed(() => {
  const query = fileQuery.value.trim().toLocaleLowerCase();
  const files = repositoryFiles.value;
  return (query ? files.filter((file) => file.path.toLocaleLowerCase().includes(query)) : files).slice(0, 500);
});
const fileTree = computed(() => buildRepositoryTree(filteredFiles.value.map((file) => file.path)));

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

function changedFilePath(path: string, originalPath?: string): string {
  return originalPath ? `${originalPath} -> ${path}` : path;
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

function formatFileSize(value: number): string {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(value < 10 * 1024 ? 1 : 0)} KB`;
  return `${(value / (1024 * 1024)).toFixed(value < 10 * 1024 * 1024 ? 1 : 0)} MB`;
}

function toggleBranches() {
  branchesOpen.value = !branchesOpen.value;
  if (branchesOpen.value) void appStore.refreshActiveRepositoryBranches();
}

onMounted(() => {
  void appStore.refreshActiveRepository();
});
watch(() => appStore.activeThreadId, () => {
  branchesOpen.value = false;
  void appStore.refreshActiveRepository();
});
</script>

<template>
  <aside class="inspector" :aria-label="tr('inspector.label')">
    <div class="inspector-header">
      <div v-if="appStore.activeRepositoryFilePreviewPath" class="inspector-file-header">
        <button class="icon-button" type="button" :title="tr('files.closePreview')" @click="appStore.closeRepositoryFilePreview()"><ArrowLeft :size="15" /></button>
        <FileCode2 :size="14" aria-hidden="true" />
        <strong :title="filePreview?.absolutePath || appStore.activeRepositoryFilePreviewPath">{{ filePreviewName }}</strong>
        <span v-if="appStore.activeRepositoryFilePreviewLine" class="file-preview-line">:{{ appStore.activeRepositoryFilePreviewLine }}</span>
        <div class="inspector-file-actions">
          <button class="icon-button" type="button" :title="tr('files.open')" @click="void appStore.openPreviewedRepositoryFile()"><ExternalLink :size="14" /></button>
          <button class="icon-button" type="button" :title="tr('files.reveal')" @click="void appStore.openPreviewedRepositoryFile(true)"><FolderOpen :size="14" /></button>
        </div>
      </div>
      <div v-else class="inspector-tabs" role="tablist">
        <button :class="{ 'is-active': appStore.inspectorTab === 'changes' }" type="button" role="tab" @click="appStore.setInspectorTab('changes')">{{ tr("inspector.changes") }}</button>
        <button :class="{ 'is-active': appStore.inspectorTab === 'context' }" type="button" role="tab" @click="appStore.setInspectorTab('context')">{{ tr("inspector.context") }}</button>
        <button :class="{ 'is-active': appStore.inspectorTab === 'terminal' }" type="button" role="tab" @click="appStore.setInspectorTab('terminal')">{{ tr("inspector.terminal") }}</button>
      </div>
    </div>

    <div v-if="appStore.activeRepositoryFilePreviewPath" class="inspector-content file-preview-panel">
      <div v-if="appStore.activeRepositoryFilePreviewLoading" class="repository-state"><LoaderCircle :size="18" class="is-spinning" /></div>
      <div v-else-if="appStore.activeRepositoryFilePreviewError" class="repository-state error-text">{{ appStore.activeRepositoryFilePreviewError }}</div>
      <template v-else-if="filePreview">
        <div class="file-preview-meta">
          <span :title="filePreview.absolutePath">{{ filePreview.absolutePath }}</span>
          <small>{{ formatFileSize(filePreview.size) }}</small>
        </div>
        <div v-if="filePreview.binary" class="repository-state"><Binary :size="18" /><span>{{ tr("files.binaryPreview") }}</span></div>
        <pre v-else class="file-preview-content" :aria-label="tr('files.previewContent')"><code>{{ filePreview.content }}</code></pre>
        <div v-if="filePreview.truncated" class="diff-notice">{{ tr("files.previewTruncated") }}</div>
      </template>
    </div>

    <div v-else-if="appStore.inspectorTab === 'changes'" class="inspector-content repository-panel">
      <div v-if="!appStore.activeRepositoryDiffPath" class="repository-toolbar">
        <button class="branch-summary" type="button" title="Show branches" :disabled="!repository?.git.isRepository" @click="toggleBranches">
          <GitBranch :size="14" />
          <span>{{ repository?.git.isRepository ? repository.git.branch || "Detached HEAD" : "Not a Git repository" }}</span>
          <small v-if="repository?.git.ahead">+{{ repository.git.ahead }}</small>
          <small v-if="repository?.git.behind">-{{ repository.git.behind }}</small>
          <ChevronDown :size="13" :class="{ 'is-expanded': branchesOpen }" />
        </button>
        <div class="repository-toolbar-actions">
          <span v-if="changedFiles.length" class="changed-count" title="Changed files">{{ changedFiles.length }}</span>
          <button class="icon-button" type="button" title="Refresh repository" :disabled="appStore.activeRepositoryLoading" @click="void appStore.refreshActiveRepository()">
            <RefreshCw :size="14" :class="{ 'is-spinning': appStore.activeRepositoryLoading }" />
          </button>
        </div>
      </div>
      <div v-if="branchesOpen && !appStore.activeRepositoryDiffPath" class="branch-browser">
        <label class="file-search">
          <Search :size="13" />
          <input v-model="branchQuery" type="search" placeholder="Filter branches" aria-label="Filter branches" />
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
        <button class="icon-button" type="button" title="Back to changes" @click="appStore.closeRepositoryDiff()"><ArrowLeft :size="15" /></button>
        <strong :title="appStore.activeRepositoryDiffPath">{{ appStore.activeRepositoryDiffPath }}</strong>
        <div class="diff-toolbar-actions">
          <button class="icon-button" type="button" title="Open file" @click="void appStore.openActiveRepositoryFile()"><ExternalLink :size="14" /></button>
          <button class="icon-button" type="button" title="Show in file manager" @click="void appStore.openActiveRepositoryFile(true)"><FolderOpen :size="14" /></button>
        </div>
      </div>
      <div v-if="appStore.activeRepositoryDiffPath" class="repository-diff">
        <div v-if="appStore.activeRepositoryDiffLoading" class="repository-state"><LoaderCircle :size="18" class="is-spinning" /></div>
        <div v-else-if="appStore.activeRepositoryDiffError && !activeDiff" class="repository-state error-text">{{ appStore.activeRepositoryDiffError }}</div>
        <template v-else-if="activeDiff">
          <div v-if="appStore.activeRepositoryDiffError" class="diff-notice error-text">{{ appStore.activeRepositoryDiffError }}</div>
          <div v-if="activeDiff.binary" class="repository-state"><FileDiff :size="18" /><span>Binary file changed</span></div>
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
        <div v-if="appStore.activeRepositoryLoading && !repository" class="repository-state"><LoaderCircle :size="18" class="is-spinning" /></div>
        <div v-else-if="appStore.activeRepositoryError" class="repository-state error-text">{{ appStore.activeRepositoryError }}</div>
        <div v-else-if="!changedFiles.length" class="repository-state">
          <FileDiff :size="18" /><span>No working tree changes</span>
        </div>
        <div v-else class="changed-file-list">
          <div v-for="file in changedFiles" :key="`${file.path}-${file.indexStatus}-${file.worktreeStatus}`" class="changed-file-row">
            <span class="change-status" :data-status="reviewChangeLabel(file)">{{ reviewChangeLabel(file) }}</span>
            <button class="changed-file-open" type="button" :title="`View diff for ${file.path}`" @click="void appStore.openRepositoryDiff(file.path)">{{ changedFilePath(file.path, file.originalPath) }}</button>
            <button type="button" title="Mention file" @click="appStore.insertFileMention(file.path)"><AtSign :size="13" /></button>
          </div>
        </div>
      </template>
    </div>

    <div v-else-if="appStore.inspectorTab === 'context'" class="inspector-content context-panel">
      <dl v-if="appStore.activeThread">
        <div><dt>{{ tr("inspector.workspace") }}</dt><dd :title="appStore.activeThread.workspacePath">{{ appStore.activeThread.workspacePath }}</dd></div>
        <div><dt>{{ tr("inspector.piProcess") }}</dt><dd>{{ appStore.activeThread.started ? tr("inspector.generation", { generation: appStore.activeThread.generation }) : tr("common.notStarted") }}</dd></div>
        <div><dt>{{ tr("inspector.session") }}</dt><dd>{{ state?.sessionId || tr("inspector.createdOnPrompt") }}</dd></div>
        <div><dt>{{ tr("inspector.model") }}</dt><dd>{{ state?.model ? `${state.model.provider}/${state.model.id}` : tr("common.auto") }}</dd></div>
        <div><dt>{{ tr("inspector.reasoning") }}</dt><dd>{{ state?.thinkingLevel || tr("common.auto") }}</dd></div>
        <div><dt>{{ tr("inspector.messages") }}</dt><dd>{{ stats?.totalMessages ?? state?.messageCount ?? 0 }}</dd></div>
        <div><dt>{{ tr("inspector.tokens") }}</dt><dd>{{ stats?.tokens?.total?.toLocaleString() ?? "-" }}</dd></div>
        <div><dt>{{ tr("inspector.cost") }}</dt><dd>{{ stats?.cost ? `$${stats.cost.toFixed(4)}` : "-" }}</dd></div>
        <div><dt>{{ tr("inspector.contextUsage") }}</dt><dd>{{ stats?.contextUsage?.percent != null ? `${stats.contextUsage.percent.toFixed(1)}%` : "-" }}</dd></div>
      </dl>
      <div v-else class="panel-empty"><span>{{ tr("inspector.selectTask") }}</span></div>
      <section v-if="appStore.activeThread" class="workspace-files">
        <div class="section-heading"><strong>{{ tr("inspector.workspaceFiles") }}</strong><span v-if="repository?.truncated || filteredFiles.length < repositoryFiles.length">{{ tr("inspector.firstFiles", { count: filteredFiles.length }) }}</span></div>
        <label class="file-search">
          <Search :size="13" />
          <input v-model="fileQuery" type="search" :placeholder="tr('inspector.filterFiles')" :aria-label="tr('inspector.filterFiles')" />
        </label>
        <div v-if="appStore.activeRepositoryLoading && !repository" class="repository-state"><LoaderCircle :size="18" class="is-spinning" /></div>
        <div v-else-if="appStore.activeRepositoryError" class="repository-state error-text">{{ appStore.activeRepositoryError }}</div>
        <div v-else-if="fileTree.length" class="file-tree">
          <FileTreeNode v-for="node in fileTree" :key="`${node.directory}-${node.path}`" :node="node" @mention="appStore.insertFileMention" />
        </div>
        <div v-else class="repository-state">{{ tr("inspector.noFiles") }}</div>
      </section>
    </div>

    <TerminalPane v-else />
  </aside>
</template>
