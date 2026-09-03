<script setup lang="ts">
import { ui } from "../ui/classes";
import { useVirtualizer } from "@tanstack/vue-virtual";
import { ChevronDown, ChevronUp, CircleDot, History, LoaderCircle, Search, X } from "lucide-vue-next";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch, type ComponentPublicInstance } from "vue";
import ComposerBar from "./ComposerBar.vue";
import ConversationMessage from "./ConversationMessage.vue";
import { useAppStore } from "../stores/app";
import { CONVERSATION_VIRTUALIZATION_THRESHOLD, estimateMessageSize, shouldVirtualizeMessages } from "../utils/conversationVirtualization";
import { isNearBottom } from "../utils/scroll";
import { groupConversationTurns } from "../utils/conversationGrouping";
import { tr } from "../i18n";

const appStore = useAppStore();
const timeline = ref<HTMLElement>();
const searchInput = ref<HTMLInputElement>();
const searchOpen = ref(false);
const searchQuery = ref("");
const activeSearchMatch = ref(0);
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

type SearchMatch = { messageId: string; messageIndex: number };
type ConversationNavigationItem = SearchMatch & { title: string; answer: string };

function previewText(text: string, maxLength = 120): string {
  const normalized = text
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/!\[[^\]]*\]\([^)]*\)/g, "图片")
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/[\\`*_>#~]/g, "")
    .replace(/\s+/g, " ")
    .trim();
  return normalized.length > maxLength ? `${normalized.slice(0, maxLength - 1)}…` : normalized;
}

const navigationItems = computed<ConversationNavigationItem[]>(() => messages.value.flatMap((message, messageIndex) => {
  if (message.role !== "user") return [];
  const nextMessage = messages.value[messageIndex + 1];
  const answer = nextMessage?.role === "assistant" ? nextMessage.text : "";
  return [{
    messageId: message.id,
    messageIndex,
    title: previewText(message.text) || tr("conversation.navigationUntitled"),
    answer: previewText(answer) || tr("conversation.navigationNoAnswer"),
  }];
}));
const activeNavigationId = ref("");
const hoveredNavigationId = ref("");
const hoveredNavigationTop = ref(0);
const hoverClearTimer = ref<number>();
const hoveredNavigationItem = computed(() => navigationItems.value.find((item) => item.messageId === hoveredNavigationId.value));

function findMatches(text: string, query: string): number {
  const needle = query.trim().toLocaleLowerCase();
  if (!needle) return 0;
  const haystack = text.toLocaleLowerCase();
  let count = 0;
  let from = 0;
  while (true) {
    const index = haystack.indexOf(needle, from);
    if (index < 0) return count;
    count += 1;
    from = index + needle.length;
  }
}

const searchMatches = computed<SearchMatch[]>(() => {
  const query = searchQuery.value;
  if (!query.trim()) return [];
  return messages.value.flatMap((message, messageIndex) => Array.from(
    { length: findMatches(message.text, query) },
    () => ({ messageId: message.id, messageIndex }),
  ));
});
const currentSearchMatch = computed(() => searchMatches.value[activeSearchMatch.value]);
const searchResultLabel = computed(() => searchMatches.value.length
  ? `${activeSearchMatch.value + 1} / ${searchMatches.value.length}`
  : tr("conversation.searchResults", { count: 0 }));
const activeSearchMessageId = computed(() => currentSearchMatch.value?.messageId ?? "");

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
  updateActiveNavigation();
}

function updateActiveNavigation() {
  const element = timeline.value;
  if (!element || !navigationItems.value.length) return;

  if (shouldVirtualize.value) {
    const activeIndex = virtualRows.value.reduce((current, row) => (
      row.start <= element.scrollTop + 48 ? Math.max(current, row.index) : current
    ), 0);
    const activeItem = navigationItems.value.findLast((item) => item.messageIndex <= activeIndex);
    activeNavigationId.value = activeItem?.messageId ?? navigationItems.value[0].messageId;
    return;
  }

  const viewportTop = element.getBoundingClientRect().top + 48;
  const rows = Array.from(element.querySelectorAll<HTMLElement>('.message-row[data-role="user"]'));
  let activeId = "";
  for (const row of rows) {
    if (row.getBoundingClientRect().top <= viewportTop) activeId = row.dataset.messageId ?? activeId;
  }
  activeNavigationId.value = activeId || rows[0]?.dataset.messageId || activeNavigationId.value;
}

async function focusSearch() {
  await nextTick();
  searchInput.value?.focus();
  searchInput.value?.select();
}

async function openSearch() {
  if (!appStore.activeThread) return;
  searchOpen.value = true;
  await focusSearch();
}

function closeSearch() {
  searchOpen.value = false;
  searchQuery.value = "";
  activeSearchMatch.value = 0;
}

async function scrollToMessage(messageIndex: number, messageId: string, block: ScrollLogicalPosition) {
  if (shouldVirtualize.value) virtualizer.value.scrollToIndex(messageIndex, { align: block === "start" ? "start" : "center" });
  await nextTick();
  const row = Array.from(timeline.value?.querySelectorAll<HTMLElement>("[data-message-id]") ?? [])
    .find((element) => element.dataset.messageId === messageId);
  if (row && typeof row.scrollIntoView === "function") row.scrollIntoView({ behavior: "smooth", block });
  updateActiveNavigation();
}

async function scrollToSearchMatch() {
  const match = currentSearchMatch.value;
  if (match) await scrollToMessage(match.messageIndex, match.messageId, "center");
}

async function scrollToNavigation(item: ConversationNavigationItem) {
  activeNavigationId.value = item.messageId;
  await scrollToMessage(item.messageIndex, item.messageId, "start");
}

function setHoveredNavigation(messageId: string, event: Event) {
  if (hoverClearTimer.value !== undefined) window.clearTimeout(hoverClearTimer.value);
  hoveredNavigationId.value = messageId;
  const target = event.currentTarget;
  if (!(target instanceof HTMLElement)) return;
  const outline = target.closest<HTMLElement>(".conversation-outline");
  if (!outline) return;
  const targetRect = target.getBoundingClientRect();
  hoveredNavigationTop.value = targetRect.top - outline.getBoundingClientRect().top + targetRect.height / 2;
}

function keepHoveredNavigation() {
  if (hoverClearTimer.value !== undefined) window.clearTimeout(hoverClearTimer.value);
}

function clearHoveredNavigation() {
  if (hoverClearTimer.value !== undefined) window.clearTimeout(hoverClearTimer.value);
  hoverClearTimer.value = window.setTimeout(() => {
    hoveredNavigationId.value = "";
    hoverClearTimer.value = undefined;
  }, 120);
}

function hideHoveredNavigation() {
  if (hoverClearTimer.value !== undefined) window.clearTimeout(hoverClearTimer.value);
  hoverClearTimer.value = undefined;
  hoveredNavigationId.value = "";
}

function moveSearchMatch(direction: 1 | -1) {
  const total = searchMatches.value.length;
  if (!total) return;
  activeSearchMatch.value = (activeSearchMatch.value + direction + total) % total;
  void scrollToSearchMatch();
}

function onDocumentKeydown(event: KeyboardEvent) {
  if ((event.ctrlKey || event.metaKey) && event.key.toLocaleLowerCase() === "f") {
    if (!appStore.activeThread) return;
    event.preventDefault();
    void openSearch();
    return;
  }
  if (!searchOpen.value) return;
  if (event.key === "Escape") {
    event.preventDefault();
    closeSearch();
  }
}

watch(() => appStore.activeThreadId, async () => {
  activeNavigationId.value = navigationItems.value[0]?.messageId ?? "";
  stickToBottom.value = true;
  await nextTick();
  virtualizer.value.measure();
  scrollToBottom();
  updateActiveNavigation();
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
  updateActiveNavigation();
});

watch(navigationItems, (items) => {
  if (!items.some((item) => item.messageId === activeNavigationId.value)) activeNavigationId.value = items[0]?.messageId ?? "";
}, { flush: "post" });

watch(searchQuery, () => {
  activeSearchMatch.value = 0;
  if (searchOpen.value) void nextTick().then(scrollToSearchMatch);
});

watch(searchMatches, (matches) => {
  activeSearchMatch.value = matches.length ? Math.min(activeSearchMatch.value, matches.length - 1) : 0;
  if (searchOpen.value) void nextTick().then(scrollToSearchMatch);
}, { flush: "post" });

onMounted(async () => {
  document.addEventListener("keydown", onDocumentKeydown, true);
  await nextTick();
  updateActiveNavigation();
});
onBeforeUnmount(() => {
  document.removeEventListener("keydown", onDocumentKeydown, true);
  hideHoveredNavigation();
});
</script>

<template>
  <section class="conversation-pane relative grid h-full min-h-0 min-w-0 grid-rows-[minmax(0,1fr)_auto] overflow-hidden bg-[var(--bg-workspace)]" :class="ui.root" :aria-label="tr('conversation.label')">
    <div v-if="searchOpen" class="conversation-search absolute right-4 top-3 z-20 w-[min(360px,calc(100%_-_32px))] overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--bg-raised)] shadow-lg" role="search" :aria-label="tr('conversation.search')">
      <div class="conversation-search-main flex min-h-10 items-center gap-2 px-2 text-[var(--text-muted)]">
        <Search :size="17" aria-hidden="true" />
        <input :class="ui.input"
          ref="searchInput"
          v-model="searchQuery"
          class="conversation-search-input min-w-0 flex-1 border-0 bg-transparent text-sm text-[var(--text)] outline-none placeholder:text-[var(--text-muted)]"
          type="search"
          autocomplete="off"
          :placeholder="tr('conversation.searchPlaceholder')"
          :aria-label="tr('conversation.searchPlaceholder')"
          @keydown.enter.prevent="moveSearchMatch($event.shiftKey ? -1 : 1)"
          @keydown.esc.prevent.stop="closeSearch"
        />
        <span class="conversation-search-result shrink-0 font-mono text-[calc(10px+var(--font-size-delta))] text-[var(--text-muted)]" aria-live="polite">{{ searchResultLabel }}</span>
        <button class="conversation-search-control inline-grid size-7 place-items-center rounded-md border-0 bg-transparent text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] disabled:cursor-not-allowed disabled:opacity-40" type="button" :title="tr('conversation.previousSearchResult')" :aria-label="tr('conversation.previousSearchResult')" :disabled="!searchMatches.length" @click="moveSearchMatch(-1)">
          <ChevronUp :size="15" aria-hidden="true" />
        </button>
        <button class="conversation-search-control inline-grid size-7 place-items-center rounded-md border-0 bg-transparent text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] disabled:cursor-not-allowed disabled:opacity-40" type="button" :title="tr('conversation.nextSearchResult')" :aria-label="tr('conversation.nextSearchResult')" :disabled="!searchMatches.length" @click="moveSearchMatch(1)">
          <ChevronDown :size="15" aria-hidden="true" />
        </button>
        <button class="conversation-search-close inline-grid size-7 place-items-center rounded-md border-0 bg-transparent text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)]" type="button" :title="tr('conversation.closeSearch')" :aria-label="tr('conversation.closeSearch')" @click="closeSearch">
          <X :size="17" aria-hidden="true" />
        </button>
      </div>
    </div>
    <div class="conversation-scroll-region relative min-h-0 min-w-0 overflow-hidden">
      <nav v-if="navigationItems.length" class="conversation-outline" :aria-label="tr('conversation.navigation')">
        <div class="conversation-outline-scroll" @scroll="hideHoveredNavigation">
          <div class="conversation-outline-list">
            <button
              v-for="(item, index) in navigationItems"
              :key="item.messageId"
              class="conversation-outline-item"
              :class="{ 'is-active': item.messageId === activeNavigationId, 'is-hovered': item.messageId === hoveredNavigationId }"
              type="button"
              :aria-label="tr('conversation.navigationItem', { index: index + 1, title: item.title })"
              @mouseenter="setHoveredNavigation(item.messageId, $event)"
              @mouseleave="clearHoveredNavigation"
              @focus="setHoveredNavigation(item.messageId, $event)"
              @blur="clearHoveredNavigation"
              @click="void scrollToNavigation(item)"
            >
              <span class="conversation-outline-line" aria-hidden="true" />
            </button>
          </div>
        </div>
        <aside
          v-if="hoveredNavigationItem"
          class="conversation-outline-preview"
          :style="{ top: `${hoveredNavigationTop}px` }"
          role="tooltip"
          @mouseenter="keepHoveredNavigation"
          @mouseleave="clearHoveredNavigation"
        >
          <strong>{{ hoveredNavigationItem.title }}</strong>
          <span><em>{{ tr("conversation.navigationAnswer") }}</em>{{ hoveredNavigationItem.answer }}</span>
        </aside>
      </nav>
      <div ref="timeline" class="timeline h-full w-full min-w-0 overflow-x-clip overflow-y-auto" role="log" aria-live="polite" @scroll="onTimelineScroll">
      <div v-if="appStore.activeSessionOperation === tr('topbar.compacting')" class="conversation-operation-banner mb-4 inline-flex items-center gap-2 rounded-lg border border-[var(--border)] bg-[var(--bg-raised)] px-3 py-2 text-xs text-[var(--text-secondary)] shadow-sm" role="status" aria-live="polite">
        <LoaderCircle :size="14" class="is-spinning" aria-hidden="true" />
        <span>{{ tr("topbar.compacting") }}</span>
      </div>
      <div v-if="!appStore.activeThread" class="empty-workspace h-full" aria-hidden="true" />

      <div v-else-if="messages.length === 0" class="empty-thread mx-auto grid min-h-80 max-w-xl content-center justify-items-start gap-3 text-left text-[var(--text-secondary)]">
        <LoaderCircle v-if="appStore.transcriptStateByThread[appStore.activeThread.id] === 'loading'" :size="22" class="is-spinning" />
        <History v-else-if="appStore.activeThread.sessionFile" :size="22" />
        <CircleDot v-else :size="22" />
        <strong class="font-display text-lg font-semibold tracking-[-0.02em] text-[var(--text)]">{{ appStore.activeThread.sessionFile ? tr("conversation.previous") : tr("conversation.startIn", { workspace: appStore.activeThread.workspace }) }}</strong>
        <span class="max-w-[60ch] text-sm leading-relaxed">{{ appStore.activeThread.messageCount ? tr("conversation.savedMessages", { count: appStore.activeThread.messageCount }) : appStore.activeThread.trust === "approve" ? tr("conversation.resourcesEnabled") : tr("conversation.resourcesDisabled") }}</span>
        <button
          v-if="appStore.activeThread.sessionFile && appStore.transcriptStateByThread[appStore.activeThread.id] !== 'loading'"
          class="text-button inline-flex h-9 items-center whitespace-nowrap rounded-lg border border-[var(--border-strong)] bg-[var(--bg-workspace)] px-3 text-sm font-medium text-[var(--text-secondary)] shadow-sm hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] focus-visible:outline-2 focus-visible:outline-[var(--text)]" :class="ui.button"
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
          <ConversationMessage :message="messages[row.index]" :search-query="searchQuery" :search-active="messages[row.index]?.id === activeSearchMessageId" />
        </div>
      </div>

      <ConversationMessage v-else v-for="message in messages" :key="message.id" :message="message" :search-query="searchQuery" :search-active="message.id === activeSearchMessageId" />
      <div
        v-if="appStore.activeWaitingForOutput && !appStore.activeRetry"
        class="waiting-for-output mt-3 inline-flex size-8 items-center justify-center rounded-full border border-[var(--border)] bg-[var(--bg-raised)] text-[var(--text-secondary)] shadow-sm"
        role="status"
        aria-live="polite"
        :aria-label="tr('conversation.waitingForOutput')"
      >
        <LoaderCircle :size="14" class="is-spinning" aria-hidden="true" />
      </div>
      </div>
    </div>
    <ComposerBar v-if="appStore.activeThread" />
  </section>
</template>
