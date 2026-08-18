<script setup lang="ts">
import { BrainCircuit, Check, CheckCircle2, ChevronRight, Copy, GitFork, LoaderCircle, Pencil, RefreshCw, Save, Sparkles, Trash2, TriangleAlert, X } from "lucide-vue-next";
import { computed, nextTick, ref, watch } from "vue";
import type { ExecutionStep, TimelineMessage } from "../stores/app";
import { useAppStore } from "../stores/app";
import type { PreparedImage } from "../utils/imageAttachments";
import { parseSkillInvocation, replaceSkillInvocationUserMessage, skillInvocationCommandText } from "../utils/skillInvocation";
import ImagePreviewDialog from "./ImagePreviewDialog.vue";
import MarkdownBody from "./MarkdownBody.vue";
import ToolCallPanel from "./ToolCallPanel.vue";
import { tr } from "../i18n";

const props = defineProps<{ message: TimelineMessage }>();
const appStore = useAppStore();
const editing = ref(false);
const editText = ref("");
const confirmingDelete = ref(false);
const copied = ref(false);
const previewImage = ref<PreparedImage>();
const executionOpen = ref(props.message.streaming);
const editBox = ref<HTMLTextAreaElement>();
const skillInvocation = computed(() => props.message.role === "user" ? parseSkillInvocation(props.message.text) : undefined);
const visibleMessageText = computed(() => skillInvocation.value?.userMessage ?? props.message.text);
const actionable = computed(() => props.message.role === "user" || props.message.role === "assistant");
const sessionBusy = computed(() => (
  props.message.streaming
  || appStore.activeThread?.status === "running"
  || appStore.activeThread?.status === "starting"
  || Boolean(appStore.activeSessionOperation)
));
const showActions = computed(() => actionable.value && !sessionBusy.value);
const persistedActionsDisabled = computed(() => (
  !props.message.entryId
  || sessionBusy.value
));
const showMessageMeta = computed(() => (
  Boolean(props.message.delivery)
  || (props.message.role === "user" && Boolean(props.message.timestamp))
  || showActions.value
));
const runNotice = computed(() => {
  if (props.message.role !== "assistant") return undefined;
  return props.message.runNotice ?? (props.message.error
    ? { status: "failed" as const, error: props.message.error }
    : undefined);
});
const runNoticeLabel = computed(() => {
  const notice = runNotice.value;
  if (!notice) return "";
  if (notice.status === "retrying") {
    const attempt = notice.attempt ?? 0;
    const maxAttempts = notice.maxAttempts ?? 0;
    if (attempt > 0 && maxAttempts > 0) {
      const delayMs = Math.max(0, notice.delayMs ?? 0);
      const delay = delayMs < 1000
        ? `${Math.round(delayMs)}ms`
        : `${(delayMs / 1000).toFixed(delayMs % 1000 === 0 ? 0 : 1)}s`;
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
const executionSteps = computed(() => props.message.executionSteps ?? [
  ...(props.message.thinking ? [{ id: `${props.message.id}-thinking`, kind: "thinking" as const, text: props.message.thinking }] : []),
  ...(props.message.tools.length ? [{ id: `${props.message.id}-tools`, kind: "tools" as const, tools: props.message.tools }] : []),
]);
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

function syncExecutionOpen(event: Event) {
  const details = event.currentTarget as HTMLDetailsElement;
  if (props.message.streaming && !details.open) {
    details.open = true;
    executionOpen.value = true;
    return;
  }
  executionOpen.value = details.open;
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
  editing.value = true;
  confirmingDelete.value = false;
  await nextTick();
  editBox.value?.focus();
}

async function saveEdit() {
  if (!editText.value.trim()) return;
  const text = replaceSkillInvocationUserMessage(props.message.text, editText.value);
  if (await appStore.editMessage(props.message.id, text)) editing.value = false;
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
    :class="{
      'message-row--compact': Boolean(message.thinking || message.tools.length),
      'message-row--editing': editing,
      'message-row--compaction': Boolean(message.compaction),
    }"
    :data-role="message.role"
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
        <MarkdownBody :text="message.compaction.summary" :streaming="false" />
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
            <details v-if="step.kind === 'thinking'" class="thinking-block">
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
            <MarkdownBody v-else-if="step.text" :text="step.text" :streaming="false" />
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
        <textarea ref="editBox" v-model="editText" rows="3" @keydown.ctrl.enter.prevent="void saveEdit()" @keydown.escape.prevent="editing = false" />
        <div class="message-edit-actions">
          <button class="message-edit-button" type="button" :title="tr('common.cancel')" @click="editing = false"><X :size="14" /></button>
          <button class="message-edit-button message-edit-button--primary" type="button" :title="tr('conversation.save')" :disabled="!editText.trim() || Boolean(appStore.activeSessionOperation)" @click="void saveEdit()"><Save :size="14" /></button>
        </div>
      </div>
      <p v-else-if="message.text && message.role === 'system'" :class="{ 'error-text': message.error }">{{ message.text }}</p>
      <MarkdownBody v-else-if="visibleMessageText" :text="visibleMessageText" :streaming="message.streaming" />
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
