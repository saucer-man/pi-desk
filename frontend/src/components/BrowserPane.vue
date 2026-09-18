<script setup lang="ts">
import { ui } from "../ui/classes";
import { Globe, LoaderCircle, RefreshCw } from "lucide-vue-next";
import { onBeforeUnmount, onMounted, ref } from "vue";
import { browserService, onBrowserEvent, type BrowserEvent } from "../services/browser";
import { tr } from "../i18n";

const canvas = ref<HTMLCanvasElement>();
const loading = ref(false);
const attached = ref(false);
const hasFrame = ref(false);
const url = ref("");
const error = ref("");

let disposeEvent: (() => void) | undefined;
let latest: { cssWidth: number; cssHeight: number } | undefined;
let decodeQueued = false;
let retryTimer: ReturnType<typeof setTimeout> | undefined;
let disposed = false;

// The managed browser is launched by the extension on the agent's first
// browser tool call — usually after this panel has already mounted. Poll
// until it appears instead of staying stuck on the empty state.
function poll() {
  if (disposed || attached.value) return;
  if (!loading.value) {
    loading.value = true;
    void browserService
      .start()
      .then((status) => {
        attached.value = true;
        url.value = status.url || "";
        error.value = "";
      })
      .catch((cause) => {
        // "Browser not running" is the expected pre-first-use state, not a failure.
        const message = cause instanceof Error ? cause.message : String(cause);
        if (!/not running/i.test(message)) error.value = message;
      })
      .finally(() => {
        loading.value = false;
      });
  }
  if (disposed || attached.value) return;
  if (!retryTimer) retryTimer = setTimeout(() => { retryTimer = undefined; poll(); }, 2000);
}

// The canvas keeps the frame's natural resolution and CSS constrains it, so
// click mapping only needs the rect-to-image scale plus the frame's
// CSS-viewport width from CDP metadata.
function pagePoint(clientX: number, clientY: number): { x: number; y: number } | undefined {
  const canvasEl = canvas.value;
  if (!canvasEl || !latest) return undefined;
  const rect = canvasEl.getBoundingClientRect();
  if (rect.width === 0 || rect.height === 0) return undefined;
  const imageX = (clientX - rect.left) * (canvasEl.width / rect.width);
  const imageY = (clientY - rect.top) * (canvasEl.height / rect.height);
  return { x: imageX * (latest.cssWidth / canvasEl.width), y: imageY * (latest.cssHeight / canvasEl.height) };
}

function handleEvent(event: BrowserEvent) {
  if (event.type === "attached") {
    attached.value = true;
    error.value = "";
    if (event.url) url.value = event.url;
    return;
  }
  if (event.type === "detached") {
    attached.value = false;
    poll();
    return;
  }
  if (event.type === "navigated") {
    if (event.url) url.value = event.url;
    return;
  }
  if (event.type === "frame" && event.dataB64) {
    if (decodeQueued) return;
    decodeQueued = true;
    const image = new Image();
    image.onload = () => {
      decodeQueued = false;
      const canvasEl = canvas.value;
      if (!canvasEl) return;
      canvasEl.width = image.naturalWidth;
      canvasEl.height = image.naturalHeight;
      canvasEl.getContext("2d")?.drawImage(image, 0, 0);
      latest = {
        cssWidth: event.cssWidth || image.naturalWidth,
        cssHeight: event.cssHeight || image.naturalHeight,
      };
      hasFrame.value = true;
    };
    image.onerror = () => {
      decodeQueued = false;
    };
    image.src = `data:image/jpeg;base64,${event.dataB64}`;
  }
}

function modifiersBitmask(event: KeyboardEvent): number {
  return (event.altKey ? 1 : 0) | (event.ctrlKey ? 2 : 0) | (event.metaKey ? 4 : 0) | (event.shiftKey ? 8 : 0);
}

const SPECIAL_KEYS = new Set(["Enter", "Backspace", "Tab", "Escape", "ArrowLeft", "ArrowUp", "ArrowRight", "ArrowDown", "Delete", "Insert", "Home", "End", "PageUp", "PageDown"]);

function handleKeydown(event: KeyboardEvent) {
  if (!attached.value) return;
  event.preventDefault();
  const key = event.key === " " ? "space" : event.key;
  if (event.ctrlKey || event.metaKey || event.altKey || SPECIAL_KEYS.has(key) || /^F([1-9]|1[0-2])$/.test(key)) {
    void browserService.key(key, modifiersBitmask(event)).catch((cause) => {
      error.value = cause instanceof Error ? cause.message : String(cause);
    });
    return;
  }
  if (event.key.length === 1) {
    void browserService.type(event.key).catch((cause) => {
      error.value = cause instanceof Error ? cause.message : String(cause);
    });
  }
}

function handleClick(event: MouseEvent, clickCount = 1) {
  if (clickCount === 1 && event.detail > 1) return;
  const point = pagePoint(event.clientX, event.clientY);
  if (!point || !attached.value) return;
  canvas.value?.focus();
  const button = event.button === 2 ? "right" : event.button === 1 ? "middle" : "left";
  void browserService.click(point.x, point.y, button, clickCount).catch((cause) => {
    error.value = cause instanceof Error ? cause.message : String(cause);
  });
}

function handleWheel(event: WheelEvent) {
  const point = pagePoint(event.clientX, event.clientY);
  if (!point || !attached.value) return;
  void browserService.wheel(point.x, point.y, event.deltaX, event.deltaY).catch((cause) => {
    error.value = cause instanceof Error ? cause.message : String(cause);
  });
}

onMounted(() => {
  disposeEvent = onBrowserEvent(handleEvent);
  poll();
});

onBeforeUnmount(() => {
  disposed = true;
  if (retryTimer) clearTimeout(retryTimer);
  disposeEvent?.();
  void browserService.stop();
});
</script>

<template>
  <div class="inspector-content browser-panel" :class="ui.root">
    <div class="terminal-toolbar" :class="ui.toolbar">
      <span class="terminal-status" :class="{ 'is-running': attached }" :title="attached ? tr('browser.live') : tr('browser.disconnected')" />
      <strong>{{ tr("inspector.browser") }}</strong>
      <span v-if="url" class="terminal-cwd" :title="url">{{ url }}</span>
      <div class="terminal-actions">
        <LoaderCircle v-if="loading" :size="14" class="is-spinning" />
        <button v-else class="icon-button" :class="ui.iconButton" type="button" :title="tr('browser.reconnect')" @click="poll()"><RefreshCw :size="14" /></button>
      </div>
    </div>
    <div class="browser-stage">
      <canvas
        v-show="hasFrame"
        ref="canvas"
        class="browser-canvas"
        tabindex="0"
        @click="handleClick($event)"
        @dblclick="handleClick($event, 2)"
        @contextmenu.prevent="handleClick($event)"
        @wheel.prevent="handleWheel($event)"
        @keydown="handleKeydown($event)"
      />
      <div v-if="!attached && !error" class="terminal-empty" :class="ui.empty">
        <div class="browser-empty">
          <LoaderCircle :size="16" class="is-spinning" />
          <span>{{ tr("browser.waiting") }}</span>
          <small>{{ tr("browser.notRunning") }}</small>
        </div>
      </div>
    </div>
    <div v-if="error" class="terminal-error" role="alert">
      <Globe :size="13" />
      <span>{{ error }}</span>
    </div>
  </div>
</template>
