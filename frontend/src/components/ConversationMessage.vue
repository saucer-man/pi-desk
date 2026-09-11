<script setup lang="ts">
/* Hallmark · pre-emit critique: P5 H5 E5 S5 R5 V4 */
import { ui } from "../ui/classes";
import { ArrowUp, BrainCircuit, Check, CheckCircle2, ChevronDown, ChevronRight, ChevronUp, Copy, FileDiff, GitFork, LoaderCircle, Pencil, RefreshCw, Save, Sparkles, Trash2, TriangleAlert, X } from "lucide-vue-next";
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import type { ExecutionStep, TimelineMessage, ToolDiff } from "../stores/app";
import { useAppStore } from "../stores/app";
import type { PreparedImage } from "../utils/imageAttachments";
import { resolveWorkspaceFileLink, type WorkspaceFileLink } from "../utils/fileLinks";
import { mergeToolDiffs } from "../utils/toolDiff";
import { parseSkillInvocation, replaceSkillInvocationUserMessage, skillInvocationCommandText } from "../utils/skillInvocation";
import { splitTaggedThinking } from "../utils/taggedThinking";
import ImagePreviewDialog from "./ImagePreviewDialog.vue";
import MarkdownBody from "./MarkdownBody.vue";
import ToolCallPanel from "./ToolCallPanel.vue";
import { tr } from "../i18n";

const props = defineProps<{
  message: TimelineMessage;
  searchQuery?: string;
  searchActive?: boolean;
}>();
const appStore = useAppStore();
const editing = ref(false);
const editText = ref("");
const editError = ref("");
const editSubmitting = ref(false);
const confirmingDelete = ref(false);
const copied = ref(false);
const previewImage = ref<PreparedImage>();
const executionOpen = ref(props.message.streaming);
const changedFilesExpanded = ref(false);
const editBox = ref<HTMLTextAreaElement>();
const skillInvocation = computed(() => props.message.role === "user" ? parseSkillInvocation(props.message.text) : undefined);

const taggedThinking = computed(() => (
  props.message.role === "assistant"
    ? splitTaggedThinking(props.message.text)
    : { text: props.message.text, thinking: "", open: false }
));
const visibleMessageText = computed(() => skillInvocation.value?.userMessage ?? taggedThinking.value.text);
const actionable = computed(() => (
  (props.message.role === "user" || props.message.role === "assistant")
  && Boolean(visibleMessageText.value.trim() || props.message.images?.length)
));
const sessionBusy = computed(() => (
  props.message.streaming
  || appStore.activeThread?.status === "running"
  || appStore.activeThread?.status === "starting"
  || Boolean(appStore.activeSessionOperation)
));
const showActions = computed(() => actionable.value && !props.message.streaming);
const persistedActionsDisabled = computed(() => (
  !props.message.entryId
  || sessionBusy.value
));
const latestUserMessage = computed(() => (
  props.message.role === "user"
  && appStore.activeMessages.findLast((message) => message.role === "user")?.id === props.message.id
));
const showMessageMeta = computed(() => (
  Boolean(props.message.delivery)
  || (props.message.role === "user" && Boolean(props.message.timestamp))
  || showActions.value
));
const runNotice = computed(() => {
  if (props.message.role !== "assistant") return undefined;
  const notice = props.message.runNotice ?? (props.message.error
    ? { status: "failed" as const, error: props.message.error }
    : undefined);
  return notice?.status === "recovered" ? undefined : notice;
});
const retryNow = ref(Date.now());
let retryTimer: number | undefined;
watch(() => [runNotice.value?.status, runNotice.value?.retryAt], () => {
  if (retryTimer !== undefined) window.clearInterval(retryTimer);
  retryTimer = undefined;
  retryNow.value = Date.now();
  if (runNotice.value?.status === "retrying" && runNotice.value.retryAt !== undefined) {
    retryTimer = window.setInterval(() => { retryNow.value = Date.now(); }, 250);
  }
}, { immediate: true });
onBeforeUnmount(() => {
  if (retryTimer !== undefined) window.clearInterval(retryTimer);
});
const runNoticeLabel = computed(() => {
  const notice = runNotice.value;
  if (!notice) return "";
  if (notice.status === "retrying") {
    const attempt = notice.attempt ?? 0;
    const maxAttempts = notice.maxAttempts ?? 0;
    if (attempt > 0 && maxAttempts > 0) {
      const delayMs = notice.retryAt === undefined
        ? Math.max(0, notice.delayMs ?? 0)
        : Math.max(0, notice.retryAt - retryNow.value);
      if (delayMs === 0) {
        return tr("conversation.requestRetryInProgress", { attempt, maxAttempts });
      }
      const delay = notice.retryAt === undefined
        ? delayMs < 1000
          ? `${Math.round(delayMs)}ms`
          : `${(delayMs / 1000).toFixed(delayMs % 1000 === 0 ? 0 : 1)}s`
        : `${Math.ceil(delayMs / 1000)}s`;
      return tr("conversation.requestRetrying", {
        delay,
        attempt,
        maxAttempts,
      });
    }
    return tr("conversation.requestRetryingUnknown");
  }
  if (notice.status === "retried") return tr("conversation.requestRetried");
  return tr(notice.status === "recovered" ? "conversation.requestRecovered" : "conversation.requestFailed");
});
const executionSteps = computed(() => [
  ...(taggedThinking.value.thinking ? [{
    id: `${props.message.id}-tagged-thinking`, kind: "thinking" as const, text: taggedThinking.value.thinking,
    active: props.message.streaming && taggedThinking.value.open,
  }] : []),
  ...(props.message.executionSteps ?? [
  ...(props.message.thinking ? [{
    id: `${props.message.id}-thinking`, kind: "thinking" as const, text: props.message.thinking,
    active: props.message.streaming && props.message.activeExecution === "thinking",
  }] : []),
  ...(props.message.tools.length ? [{ id: `${props.message.id}-tools`, kind: "tools" as const, tools: props.message.tools }] : []),
  ]),
]);
type ChangedFileSummary = WorkspaceFileLink & { additions: number; deletions: number; diff: string };

function diffStats(diff: string): { additions: number; deletions: number } {
  let additions = 0;
  let deletions = 0;
  for (const line of diff.split("\n")) {
    if (line.startsWith("+") && !line.startsWith("+++ ")) additions++;
    else if (line.startsWith("-") && !line.startsWith("--- ")) deletions++;
  }
  return { additions, deletions };
}

const changedFiles = computed(() => {
  if (props.message.role !== "assistant" || props.message.streaming) return [];
  const thread = appStore.activeThread;
  const root = thread ? appStore.remoteWorkspaceForThread(thread)?.remoteRoot || thread.workspacePath : "";
  const windows = /^[a-z]:[\\/]/i.test(root);
  const files = new Map<string, WorkspaceFileLink & { diffs: ToolDiff[] }>();
  for (const step of executionSteps.value) {
    for (const tool of step.tools ?? []) {
      if (tool.resultReceived !== true || tool.status !== "complete" || !tool.diff?.path) continue;
      const file = resolveWorkspaceFileLink(tool.diff.path, root);
      if (!file) continue;
      const key = windows ? file.absolutePath.toLowerCase() : file.absolutePath;
      const existing = files.get(key);
      if (existing) {
        existing.diffs.push(tool.diff);
      } else {
        files.set(key, { ...file, diffs: [tool.diff] });
      }
    }
  }
  return [...files.values()].flatMap(({ diffs, ...file }) => {
    const diff = mergeToolDiffs(diffs);
    return diff ? [{ ...file, ...diffStats(diff), diff } satisfies ChangedFileSummary] : [];
  });
});
const shownChangedFiles = computed(() => changedFilesExpanded.value ? changedFiles.value : changedFiles.value.slice(0, 3));
const changedTotals = computed(() => changedFiles.value.reduce((total, file) => ({
  additions: total.additions + file.additions,
  deletions: total.deletions + file.deletions,
}), { additions: 0, deletions: 0 }));
const toolCount = computed(() => executionSteps.value.reduce((count, step) => count + (step.tools?.length ?? 0), 0));
const thinkingCount = computed(() => executionSteps.value.filter((step) => step.kind === "thinking").length);
const executionSummary = computed(() => {
  const parts: string[] = [];
  if (toolCount.value) parts.push(tr("conversation.toolCount", { count: toolCount.value }));
  if (thinkingCount.value) parts.push(tr("conversation.thinkingCount", { count: thinkingCount.value }));
  return tr("conversation.execution", { detail: parts.join(" · ") });
});
const compactionTokensBeforeLabel = computed(() => {
  const value = props.message.compaction?.tokensBefore;
  if (value === undefined) return "";
  return tr("conversation.compactedTokens", { count: value.toLocaleString() });
});
const compactionTokensAfterLabel = computed(() => {
  const value = props.message.compaction?.estimatedTokensAfter;
  if (value === undefined) return "";
  return tr("conversation.compactedTokensAfter", { count: value.toLocaleString() });
});
const durationLabel = computed(() => {
  const duration = props.message.durationMs;
  if (duration === undefined || duration < 0) return "";
  const seconds = Math.floor(duration / 1000);
  if (seconds < 1) return `${duration}ms`;
  if (seconds < 60) return `${(duration / 1000).toFixed(seconds < 10 ? 1 : 0)}s`;
  return `${Math.floor(seconds / 60)}m${seconds % 60}s`;
});

watch(() => props.message.streaming, (streaming, wasStreaming) => {
  if (streaming) executionOpen.value = true;
  else if (wasStreaming) executionOpen.value = false;
});
watch(() => props.message.id, () => { changedFilesExpanded.value = false; });

function syncExecutionOpen(event: Event) {
  const details = event.currentTarget as HTMLDetailsElement;
  if (props.message.streaming && !details.open) {
    details.open = true;
    executionOpen.value = true;
    return;
  }
  executionOpen.value = details.open;
}

function openChangedFile(file: ChangedFileSummary) {
  void appStore.openRepositoryDiff(file.relativePath, file.diff);
}

async function copyMessage() {
  if (!props.message.text) return;
  try {
    await navigator.clipboard.writeText(skillInvocationCommandText(props.message.text));
    copied.value = true;
    window.setTimeout(() => { copied.value = false; }, 1400);
  } catch {
    copied.value = false;
  }
}

async function beginEdit() {
  if (persistedActionsDisabled.value) return;
  editText.value = visibleMessageText.value;
  editError.value = "";
  editing.value = true;
  confirmingDelete.value = false;
  await nextTick();
  editBox.value?.focus();
}

async function submitEdit() {
  if (!editText.value.trim() || editSubmitting.value || persistedActionsDisabled.value) return;
  const thread = appStore.activeThread;
  const text = replaceSkillInvocationUserMessage(props.message.text, editText.value);
  editSubmitting.value = true;
  editError.value = "";
  if (thread) thread.error = undefined;
  try {
    const saved = latestUserMessage.value
      ? await appStore.resendEditedMessage(props.message.id, text)
      : await appStore.editMessage(props.message.id, text);
    if (saved) editing.value = false;
    else editError.value = thread?.error || tr("conversation.editFailed");
  } catch (error) {
    editError.value = error instanceof Error ? error.message : String(error);
  } finally {
    editSubmitting.value = false;
  }
}

async function deleteMessage() {
  if (await appStore.deleteMessage(props.message.id)) confirmingDelete.value = false;
}

function stepThinking(step: ExecutionStep): string {
  return step.text ?? "";
}
</script>

<template>
  <article
    class="message-row"
    :class="[ui.root, {
      'message-row--compact': executionSteps.length > 0,
      'message-row--editing': editing,
      'message-row--compaction': Boolean(message.compaction),
      'message-row--search-active': searchActive,
    }]"
    :data-role="message.role"
    :data-message-id="message.id"
  >
    <details v-if="message.compaction" class="compaction-divider">
      <summary>
        <span class="compaction-line" aria-hidden="true" />
        <span class="compaction-label">
          <ChevronRight class="disclosure-icon" :size="13" aria-hidden="true" />
          <strong>{{ tr("conversation.contextCompacted") }}</strong>
          <span v-if="compactionTokensBeforeLabel">· {{ compactionTokensBeforeLabel }}</span>
          <span v-if="compactionTokensAfterLabel">· {{ compactionTokensAfterLabel }}</span>
          <time v-if="message.timestamp">· {{ message.timestamp }}</time>
        </span>
        <span class="compaction-line" aria-hidden="true" />
      </summary>
      <div class="compaction-summary">
        <MarkdownBody :text="message.compaction.summary" :streaming="false" :search-query="searchQuery" :search-active="searchActive" />
      </div>
    </details>
    <div v-else class="message-content">
      <div v-if="message.timestamp && message.role === 'assistant'" class="message-header">
        <span class="message-sender">Pi</span>
        <time>{{ message.timestamp }}</time>
        <span v-if="durationLabel" class="message-duration">{{ durationLabel }}</span>
      </div>
      <details v-if="executionSteps.length" class="execution-process" :open="executionOpen" @toggle="syncExecutionOpen">
        <summary>
          <ChevronRight class="disclosure-icon" :size="13" aria-hidden="true" />
          <span>{{ executionSummary }}</span>
          <LoaderCircle v-if="message.streaming" :size="12" class="is-spinning" aria-hidden="true" />
        </summary>
        <div class="execution-process-details">
          <template v-for="step in executionSteps" :key="step.id">
            <details v-if="step.kind === 'thinking'" class="thinking-block" :open="step.active">
              <summary>
                <ChevronRight class="disclosure-icon" :size="13" aria-hidden="true" />
                <BrainCircuit class="thinking-icon" :size="15" aria-hidden="true" />
                <span class="thinking-summary">{{ tr("conversation.reasoning") }}</span>
              </summary>
              <pre>{{ stepThinking(step) }}</pre>
            </details>
            <template v-else-if="step.kind === 'tools'">
              <ToolCallPanel v-for="tool in step.tools" :key="tool.id" :tool="tool" />
            </template>
            <MarkdownBody v-else-if="step.text" :text="step.text" :streaming="false" :search-query="searchQuery" :search-active="searchActive" />
          </template>
        </div>
      </details>
      <div v-if="message.images?.length" class="message-images">
        <button
          v-for="image in message.images"
          :key="image.id"
          class="message-image-open"
          type="button"
          :title="tr('composer.viewImage')"
          @click="previewImage = image"
        >
          <img :src="image.previewUrl" :alt="image.name" />
        </button>
      </div>
      <div v-if="skillInvocation" class="message-skill-invocation">
        <Sparkles :size="13" aria-hidden="true" />
        <code>/skill:{{ skillInvocation.name }}</code>
      </div>
      <div v-if="editing" class="message-edit">
        <textarea :class="ui.textarea" ref="editBox" v-model="editText" rows="3" @keydown.ctrl.enter.prevent="void submitEdit()" @keydown.meta.enter.prevent="void submitEdit()" @keydown.escape.prevent="editing = false" />
        <div class="message-edit-actions">
          <button class="message-edit-button" type="button" :title="tr('common.cancel')" @click="editing = false"><X :size="14" /></button>
          <button class="message-edit-button message-edit-button--primary" type="button" :title="tr(latestUserMessage ? 'conversation.sendEdit' : 'conversation.save')" :disabled="!editText.trim() || persistedActionsDisabled || editSubmitting" :aria-busy="editSubmitting" @click="void submitEdit()">
            <ArrowUp v-if="latestUserMessage" :size="14" />
            <Save v-else :size="14" />
          </button>
        </div>
        <p v-if="editError" class="error-text" role="alert">{{ tr("conversation.editFailed") }} {{ editError === tr("conversation.editFailed") ? '' : editError }}</p>
      </div>
      <p v-else-if="message.text && message.role === 'system'" :class="{ 'error-text': message.error }">{{ message.text }}</p>
      <MarkdownBody v-else-if="visibleMessageText" :text="visibleMessageText" :streaming="message.streaming" :search-query="searchQuery" :search-active="searchActive" />
      <section v-if="changedFiles.length" class="mt-4 overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--bg-panel)]" :aria-label="tr('conversation.filesChanged')">
        <header class="flex min-h-14 items-center gap-3 border-b border-[var(--border)] px-3 py-2">
          <span class="grid size-9 shrink-0 place-items-center rounded-lg bg-[var(--bg-app)] text-[var(--text-secondary)]" aria-hidden="true">
            <FileDiff :size="17" />
          </span>
          <div class="min-w-0 flex-1">
            <strong class="block truncate text-sm font-semibold text-[var(--text)]">{{ tr("conversation.filesChangedCount", { count: changedFiles.length }) }}</strong>
            <span class="mt-1 flex items-center gap-1 font-mono text-xs" :aria-label="tr('conversation.changeTotals', changedTotals)">
              <span class="text-[var(--diff-add-text)]">+{{ changedTotals.additions }}</span>
              <span class="text-[var(--diff-delete-text)]">-{{ changedTotals.deletions }}</span>
            </span>
          </div>
        </header>
        <ul class="m-0 list-none p-0">
          <li v-for="file in shownChangedFiles" :key="file.absolutePath" class="border-b border-[var(--border)] last:border-b-0">
            <button
              class="flex min-h-10 w-full min-w-0 items-center gap-3 border-0 bg-transparent px-3 py-2 text-left text-sm text-[var(--text-secondary)] enabled:hover:bg-[var(--bg-hover)] enabled:hover:text-[var(--text)] enabled:active:bg-[var(--bg-active)] focus-visible:relative focus-visible:z-10 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[var(--focus)] disabled:cursor-not-allowed disabled:opacity-50 pointer-coarse:min-h-11 aria-current:bg-[var(--bg-selected)]"
              type="button"
              :title="file.relativePath"
              :aria-label="tr('conversation.openChangedFile', { path: file.relativePath })"
              :aria-current="appStore.activeRepositoryDiffPath === file.relativePath ? 'true' : undefined"
              :aria-busy="appStore.activeRepositoryDiffPath === file.relativePath && appStore.activeRepositoryDiffLoading"
              :disabled="appStore.activeThread?.trust !== 'approve'"
              @click="openChangedFile(file)"
            >
              <span class="min-w-0 flex-1 truncate"><span class="text-[var(--text-activity)]">{{ file.relativePath.slice(0, -file.name.length) }}</span>{{ file.name }}</span>
              <LoaderCircle v-if="appStore.activeRepositoryDiffPath === file.relativePath && appStore.activeRepositoryDiffLoading" class="is-spinning shrink-0" :size="13" aria-hidden="true" />
              <span v-else class="flex shrink-0 items-center gap-1 font-mono text-xs" :aria-label="tr('conversation.changeTotals', { additions: file.additions, deletions: file.deletions })">
                <span class="text-[var(--diff-add-text)]">+{{ file.additions }}</span>
                <span class="text-[var(--diff-delete-text)]">-{{ file.deletions }}</span>
              </span>
            </button>
          </li>
        </ul>
        <button
          v-if="changedFiles.length > 3"
          class="flex min-h-10 w-full items-center gap-2 border-0 border-t border-[var(--border)] bg-transparent px-3 py-2 text-left text-xs font-medium text-[var(--text-secondary)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[var(--focus)] pointer-coarse:min-h-11"
          type="button"
          :aria-expanded="changedFilesExpanded"
          @click="changedFilesExpanded = !changedFilesExpanded"
        >
          {{ changedFilesExpanded ? tr("conversation.fewerChangedFiles") : tr("conversation.moreChangedFiles", { count: changedFiles.length - shownChangedFiles.length }) }}
          <ChevronUp v-if="changedFilesExpanded" :size="14" aria-hidden="true" />
          <ChevronDown v-else :size="14" aria-hidden="true" />
        </button>
      </section>
      <div
        v-if="runNotice"
        class="message-run-notice"
        :data-status="runNotice.status"
        :role="runNotice.status === 'failed' ? 'alert' : 'status'"
        :aria-live="runNotice.status === 'failed' ? 'assertive' : 'polite'"
      >
        <RefreshCw v-if="runNotice.status === 'retrying'" :size="14" class="is-spinning" aria-hidden="true" />
        <RefreshCw v-else-if="runNotice.status === 'retried'" :size="14" aria-hidden="true" />
        <CheckCircle2 v-else-if="runNotice.status === 'recovered'" :size="14" aria-hidden="true" />
        <TriangleAlert v-else :size="14" aria-hidden="true" />
        <div class="message-run-notice-copy">
          <strong>{{ runNoticeLabel }}</strong>
          <span v-if="runNotice.error" :title="runNotice.error">{{ runNotice.error }}</span>
        </div>
      </div>
      <div v-if="confirmingDelete" class="message-delete-confirm" role="alert">
        <span>{{ tr('conversation.deleteConfirm') }}</span>
        <button type="button" @click="confirmingDelete = false">{{ tr('common.cancel') }}</button>
        <button class="is-danger" type="button" :disabled="Boolean(appStore.activeSessionOperation)" @click="void deleteMessage()">{{ tr('conversation.delete') }}</button>
      </div>
      <div v-if="showMessageMeta" class="message-meta">
        <span v-if="message.delivery" class="delivery-label">{{ message.delivery === "steer" ? "Steer" : "Follow up" }}</span>
        <time v-if="message.role === 'user' && message.timestamp" class="message-meta-time">{{ message.timestamp }}</time>
        <div v-if="showActions" class="message-actions" role="toolbar" :aria-label="tr('conversation.actions')">
          <button class="message-action message-action--copy" type="button" :title="tr('conversation.copy')" @click="void copyMessage()">
            <Check v-if="copied" :size="13" />
            <Copy v-else :size="13" />
          </button>
          <button class="message-action" type="button" :title="tr('conversation.edit')" :disabled="persistedActionsDisabled" @click="void beginEdit()">
            <Pencil :size="13" />
          </button>
          <button class="message-action" type="button" :title="tr('conversation.delete')" :disabled="persistedActionsDisabled" @click="confirmingDelete = !confirmingDelete; editing = false">
            <Trash2 :size="13" />
          </button>
          <button class="message-action" type="button" :title="tr('conversation.fork')" :disabled="persistedActionsDisabled" @click="void appStore.forkFromMessage(message.id)">
            <GitFork :size="13" />
          </button>
        </div>
      </div>
    </div>
    <ImagePreviewDialog v-if="previewImage" :image="previewImage" @close="previewImage = undefined" />
  </article>
</template>
