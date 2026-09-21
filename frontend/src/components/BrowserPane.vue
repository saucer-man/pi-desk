<script setup lang="ts">
import { ChevronLeft, ChevronRight, Globe, RefreshCw, Square, PanelsTopLeft, Bookmark } from "lucide-vue-next";
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { browserService, onBrowserEvent } from "../services/browser";
import { useAppStore, type PanelTab } from "../stores/app";
import type { BrowserStatus } from "../../bindings/pi-desk/internal/domain";

const props = defineProps<{ tab: PanelTab }>();
const store = useAppStore();
const threadId = store.activeThreadId;
const address = ref(props.tab.url === "about:blank" ? "" : props.tab.url || "");
const viewport = ref<HTMLElement>();
const status = ref<BrowserStatus>();
const empty = computed(() => !status.value?.url || status.value.url === "about:blank");
const error = ref("");
const connecting = ref(false);
let disposed = false;
let disposeEvent: (() => void) | undefined;
let resize: ResizeObserver | undefined;
let mutations: MutationObserver | undefined;
let frame = 0;
let lastBounds = "";
let updating = false;
let updateAgain = false;

// Native children sit above DOM content. Hide them while a DOM overlay is open;
// CSS z-index cannot put a dialog above a separate WebView2 controller.
function blocked() {
  return !!document.querySelector(':popover-open, dialog[open], [aria-modal="true"], .modal-backdrop, .dialog-backdrop, .context-menu, .command-menu, [role="menu"]');
}
function scheduleBounds() {
  if (!frame) frame = requestAnimationFrame(() => { frame = 0; void syncBounds(); });
}
async function syncBounds() {
  if (updating) { updateAgain = true; return; }
  if (!status.value?.attached || !viewport.value || disposed) return;
  const box = viewport.value.getBoundingClientRect(), scale = window.devicePixelRatio || 1;
  const visible = !empty.value && !document.hidden && box.width > 0 && box.height > 0 && !blocked();
  const rect = {
    x: Math.max(0, Math.round(box.x * scale)), y: Math.max(0, Math.round(box.y * scale)),
    width: Math.max(0, Math.round(box.width * scale)), height: Math.max(0, Math.round(box.height * scale)),
  };
  const signature = JSON.stringify([rect, visible]);
  if (signature === lastBounds) return;
  updating = true;
  try { await browserService.bounds(props.tab.id, rect, visible); lastBounds = signature; }
  catch (cause) { if (!disposed) error.value = String(cause); }
  finally {
    updating = false;
    if (disposed) void hide();
    else if (updateAgain) { updateAgain = false; scheduleBounds(); }
  }
}
function hide() {
  return browserService.bounds(props.tab.id, { x: 0, y: 0, width: 0, height: 0 }, false).catch(() => undefined);
}
function receive(next: BrowserStatus) {
  status.value = next;
  if (document.activeElement?.getAttribute("aria-label") !== "浏览器地址") address.value = next.url === "about:blank" ? "" : next.url || "";
  error.value = next.error || "";
}
async function connect() {
  if (connecting.value) return;
  connecting.value = true; error.value = "";
  try {
    const next = await browserService.startTab(props.tab.id, threadId, props.tab.url);
    if (disposed) { await hide(); return; }
    receive(next); scheduleBounds();
  } catch (cause) { error.value = String(cause); }
  finally { connecting.value = false; }
}
async function run(action: () => Promise<unknown>) {
  error.value = "";
  try { await action(); } catch (cause) { error.value = String(cause); }
}
function navigate() {
  const value = address.value.trim();
  if (!value) return;
  void run(() => browserService.openUrl(/^(https?:\/\/|about:blank$)/i.test(value) ? value : `https://${value}`, props.tab.id));
}
function command(value: string) { void run(() => browserService.command(props.tab.id, value)); }

onMounted(() => {
  disposeEvent = onBrowserEvent(event => {
    if (event.tabId === props.tab.id && event.status && !disposed) { receive(event.status); scheduleBounds(); }
  });
  resize = new ResizeObserver(scheduleBounds);
  if (viewport.value) resize.observe(viewport.value);
  mutations = new MutationObserver(scheduleBounds);
  mutations.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ["open", "class", "aria-modal"] });
  document.addEventListener("toggle", scheduleBounds, true);
  document.addEventListener("visibilitychange", scheduleBounds);
  window.addEventListener("resize", scheduleBounds);
  void connect();
});
onBeforeUnmount(() => {
  disposed = true; disposeEvent?.(); resize?.disconnect(); mutations?.disconnect();
  cancelAnimationFrame(frame);
  document.removeEventListener("toggle", scheduleBounds, true);
  document.removeEventListener("visibilitychange", scheduleBounds);
  window.removeEventListener("resize", scheduleBounds);
  void hide();
});
</script>

<template>
  <div class="inspector-content browser-panel">
    <div class="browser-toolbar">
      <button class="icon-button" aria-label="后退" title="后退" :disabled="!status?.canGoBack" @click="command('back')"><ChevronLeft :size="20" aria-hidden="true" /></button>
      <button class="icon-button" aria-label="前进" title="前进" :disabled="!status?.canGoForward" @click="command('forward')"><ChevronRight :size="20" aria-hidden="true" /></button>
      <button class="icon-button" :aria-label="status?.loading ? '停止加载' : '刷新'" :title="status?.loading ? '停止加载' : '刷新'" :disabled="!status?.attached" @click="command(status?.loading ? 'stop' : 'reload')"><Square v-if="status?.loading" :size="18" aria-hidden="true" /><RefreshCw v-else :size="20" aria-hidden="true" /></button>
      <form class="browser-address" @submit.prevent="navigate"><input v-model="address" aria-label="浏览器地址" placeholder="输入网址后回车" spellcheck="false" autocomplete="off" :disabled="!status?.attached" /></form>
      <button v-if="status?.temporary" class="icon-button" aria-label="保留此页面" title="保留此页面" @click="run(() => browserService.keepTab(tab.id))"><Bookmark :size="20" aria-hidden="true" /></button>
      <button class="icon-button" aria-label="打开调试模式" title="打开调试模式（当前网页开发者工具）" :disabled="!status?.attached" @click="command('devtools')"><PanelsTopLeft :size="20" aria-hidden="true" /></button>
    </div>
    <div v-if="error" class="browser-error" role="alert">{{ error }}<button v-if="!status?.attached" @click="connect">重试</button></div>
    <div ref="viewport" class="browser-native-viewport">
      <span v-if="connecting" class="browser-placeholder">正在打开内嵌浏览器…</span>
      <div v-else-if="empty" class="browser-empty"><Globe :size="64" :stroke-width="1.5" aria-hidden="true" /><h3>浏览器</h3><p>粘贴或输入 URL 以打开网页。</p></div>
    </div>
  </div>
</template>
