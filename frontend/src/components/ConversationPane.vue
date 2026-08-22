<script setup lang="ts">
import { useVirtualizer } from "@tanstack/vue-virtual";
import { CircleDot, History, LoaderCircle } from "lucide-vue-next";
import { computed, nextTick, ref, watch, type ComponentPublicInstance } from "vue";
import ComposerBar from "./ComposerBar.vue";
import ConversationMessage from "./ConversationMessage.vue";
import { useAppStore } from "../stores/app";
import { CONVERSATION_VIRTUALIZATION_THRESHOLD, estimateMessageSize, shouldVirtualizeMessages } from "../utils/conversationVirtualization";
import { isNearBottom } from "../utils/scroll";
import { groupConversationTurns } from "../utils/conversationGrouping";
import { tr } from "../i18n";

const appStore = useAppStore();
const timeline = ref<HTMLElement>();
const stickToBottom = ref(true);
const messages = computed(() => groupConversationTurns(appStore.activeMessages));
const shouldVirtualize = computed(() => shouldVirtualizeMessages(messages.value)
  || (appStore.activeThread?.messageCount ?? 0) > CONVERSATION_VIRTUALIZATION_THRESHOLD);
const lastMessage = computed(() => messages.value.at(-1));
const streamSignal = computed(() => {
  const message = lastMessage.value;
  if (!message) return [appStore.activeThreadId, "", 0, 0, 0, 0, "", appStore.activeWaitingForOutput] as const;
  const toolOutput = message.tools.reduce((total, tool) => total + tool.output.length, 0);
  return [appStore.activeThreadId, message.id, message.text.length, message.thinking.length, message.tools.length, toolOutput, message.runNotice?.status ?? "", appStore.activeWaitingForOutput] as const;
});

const virtualizer = useVirtualizer(computed(() => ({
  count: shouldVirtualize.value ? messages.value.length : 0,
  getScrollElement: () => timeline.value ?? null,
  getItemKey: (index: number) => messages.value[index]?.id ?? index,
  estimateSize: (index: number) => estimateMessageSize(messages.value[index]),
  overscan: 6,
})));
const virtualRows = computed(() => virtualizer.value.getVirtualItems());
const virtualTotalSize = computed(() => virtualizer.value.getTotalSize());

function measureVirtualRow(element: Element | ComponentPublicInstance | null) {
  if (element instanceof Element) virtualizer.value.measureElement(element);
}

function scrollToBottom() {
  if (shouldVirtualize.value && messages.value.length > 0) {
    virtualizer.value.scrollToIndex(messages.value.length - 1, { align: "end" });
    return;
  }
  const element = timeline.value;
  if (element) element.scrollTop = element.scrollHeight;
}

function onTimelineScroll() {
  const element = timeline.value;
  if (!element) return;
  stickToBottom.value = isNearBottom(element.scrollTop, element.clientHeight, element.scrollHeight);
}

watch(() => appStore.activeThreadId, async () => {
  stickToBottom.value = true;
  await nextTick();
  virtualizer.value.measure();
  scrollToBottom();
});

watch(streamSignal, async (_signal, previous) => {
  const messageChanged = previous?.[1] !== lastMessage.value?.id;
  if (messageChanged && lastMessage.value?.role === "user") stickToBottom.value = true;
  if (!stickToBottom.value) return;
  await nextTick();
  scrollToBottom();
});

watch(virtualTotalSize, async () => {
  if (!stickToBottom.value || !shouldVirtualize.value) return;
  await nextTick();
  scrollToBottom();
});
</script>

<template>
  <section class="conversation-pane" :aria-label="tr('conversation.label')">
    <div ref="timeline" class="timeline" role="log" aria-live="polite" @scroll="onTimelineScroll">
      <div v-if="appStore.activeSessionOperation === tr('topbar.compacting')" class="conversation-operation-banner" role="status" aria-live="polite">
        <LoaderCircle :size="14" class="is-spinning" aria-hidden="true" />
        <span>{{ tr("topbar.compacting") }}</span>
      </div>
      <div v-if="!appStore.activeThread" class="welcome-empty">
        <div class="empty-logo">Pi</div>
        <h1>{{ tr("conversation.prompt") }}</h1>
        <button class="text-button primary" type="button" @click="appStore.openNewTask">{{ tr("conversation.startTask") }}</button>
      </div>

      <div v-else-if="messages.length === 0" class="empty-thread">
        <LoaderCircle v-if="appStore.transcriptStateByThread[appStore.activeThread.id] === 'loading'" :size="22" class="is-spinning" />
        <History v-else-if="appStore.activeThread.sessionFile" :size="22" />
        <CircleDot v-else :size="22" />
        <strong>{{ appStore.activeThread.sessionFile ? tr("conversation.previous") : tr("conversation.startIn", { workspace: appStore.activeThread.workspace }) }}</strong>
        <span>{{ appStore.activeThread.messageCount ? tr("conversation.savedMessages", { count: appStore.activeThread.messageCount }) : appStore.activeThread.trust === "approve" ? tr("conversation.resourcesEnabled") : tr("conversation.resourcesDisabled") }}</span>
        <button
          v-if="appStore.activeThread.sessionFile && appStore.transcriptStateByThread[appStore.activeThread.id] !== 'loading'"
          class="text-button"
          type="button"
          @click="appStore.loadThreadTranscript(appStore.activeThread.id)"
        >
          {{ tr("conversation.open") }}
        </button>
      </div>

      <div
        v-else-if="shouldVirtualize"
        class="virtual-message-list"
        data-virtualized="true"
        :style="{ height: `${virtualTotalSize}px` }"
      >
        <div
          v-for="row in virtualRows"
          :key="String(row.key)"
          :ref="measureVirtualRow"
          class="virtual-message-row"
          :data-index="row.index"
          :style="{ transform: `translateY(${row.start}px)` }"
        >
          <ConversationMessage :message="messages[row.index]" />
        </div>
      </div>

      <ConversationMessage v-else v-for="message in messages" :key="message.id" :message="message" />
      <div
        v-if="appStore.activeWaitingForOutput"
        class="waiting-for-output"
        role="status"
        aria-live="polite"
        :aria-label="tr('conversation.waitingForOutput')"
      >
        <LoaderCircle :size="14" class="is-spinning" aria-hidden="true" />
      </div>
    </div>
    <ComposerBar v-if="appStore.activeThread" />
  </section>
</template>
