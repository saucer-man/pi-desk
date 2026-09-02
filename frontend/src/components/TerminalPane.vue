<script setup lang="ts">
import { ui } from "../ui/classes";
import "@xterm/xterm/css/xterm.css";
import { FitAddon } from "@xterm/addon-fit";
import { Terminal } from "@xterm/xterm";
import { LoaderCircle, Play, Square, Trash2 } from "lucide-vue-next";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { onTerminalEvent, terminalService, type TerminalEvent } from "../services/terminal";
import { useAppStore } from "../stores/app";

const appStore = useAppStore();
const host = ref<HTMLElement>();
const loading = ref(false);
const running = ref(false);
const shell = ref("");
const error = ref("");
const activeThread = computed(() => appStore.activeThread);
const workspacePath = computed(() => activeThread.value?.workspacePath || "");
const workspaceLabel = computed(() => {
  const thread = activeThread.value;
  return thread ? appStore.remoteWorkspaceForThread(thread)?.remoteRoot || thread.workspacePath : "";
});
const shellName = computed(() => shell.value.split(/[\\/]/).pop() || "Terminal");

let terminal: Terminal | undefined;
let fitAddon: FitAddon | undefined;
let resizeObserver: ResizeObserver | undefined;
let disposeEvent: (() => void) | undefined;
let disposeInput: { dispose(): void } | undefined;
let disposeResize: { dispose(): void } | undefined;
let resizeTimer: ReturnType<typeof setTimeout> | undefined;
let loadToken = 0;
let lastSequence = 0;
let terminalGeneration = 0;
let hydrated = false;
let pendingEvents: TerminalEvent[] = [];
let inputChain = Promise.resolve();
let recoveringGap = false;
let pendingRemoteStartThreadID = "";

function terminalTheme() {
  const shellElement = document.querySelector<HTMLElement>(".app-shell");
  const styles = getComputedStyle(shellElement ?? document.documentElement);
  const color = (name: string, fallback: string) => styles.getPropertyValue(name).trim() || fallback;
  return {
    background: color("--bg-panel", "#1b1c1d"),
    foreground: color("--text-secondary", "#e3e3df"),
    cursor: color("--text", "#d6d7d3"),
    cursorAccent: color("--bg-panel", "#1b1c1d"),
    selectionBackground: color("--bg-active", "#46554f"),
    black: color("--bg-app", "#252627"),
    red: color("--red", "#dd6b68"),
    green: color("--green", "#49b98f"),
    yellow: color("--amber", "#d7a94d"),
    blue: color("--blue", "#79a6e8"),
    magenta: "#9b63b5",
    cyan: "#318d86",
    white: color("--text-secondary", "#d6d7d3"),
    brightBlack: color("--text-muted", "#6f716e"),
    brightWhite: color("--text", "#f1f1ee"),
  };
}

async function applyTerminalTheme() {
  await nextTick();
  if (terminal) terminal.options.theme = terminalTheme();
}

function decodeBase64(value?: string): Uint8Array {
  if (!value) return new Uint8Array();
  const decoded = atob(value);
  const bytes = new Uint8Array(decoded.length);
  for (let index = 0; index < decoded.length; index++) bytes[index] = decoded.charCodeAt(index);
  return bytes;
}

function writeOutput(value?: string) {
  const bytes = decodeBase64(value);
  if (bytes.length) terminal?.write(bytes);
}

function applyEvent(event: TerminalEvent) {
  if (event.generation && event.generation !== terminalGeneration) return;
  if (event.sequence <= lastSequence) return;
  if (event.sequence > lastSequence + 1) {
    if (!recoveringGap) {
      recoveringGap = true;
      const threadId = activeThread.value?.id;
      if (threadId) {
        const workspaceId = activeThread.value && appStore.remoteWorkspaceForThread(activeThread.value)?.id;
        void hydrate(() => terminalService.snapshot(threadId, workspaceId)).finally(() => { recoveringGap = false; });
      }
    }
    return;
  }
  lastSequence = event.sequence;
  if (event.type === "output") writeOutput(event.dataB64);
  if (event.type === "error") {
    error.value = event.error || "Terminal stream failed";
    appStore.handleTerminalEvent(event);
  }
  if (event.type === "exit") {
    running.value = false;
    if (event.error) error.value = event.error;
    appStore.handleTerminalEvent(event);
  }
}

function handleEvent(event: TerminalEvent) {
  if (event.threadId !== activeThread.value?.id) return;
  if (!hydrated) {
    pendingEvents.push(event);
    return;
  }
  applyEvent(event);
}

async function hydrate(load: () => ReturnType<typeof terminalService.snapshot>) {
  const token = ++loadToken;
  hydrated = false;
  pendingEvents = [];
  loading.value = true;
  error.value = "";
  try {
    const state = await load();
    if (token !== loadToken) return;
    terminal?.reset();
    lastSequence = state.sequence;
    terminalGeneration = state.generation || 0;
    appStore.setTerminalGeneration(state.threadId, state.generation);
    running.value = state.running;
    shell.value = state.shell || "";
    writeOutput(state.outputB64);
    hydrated = true;
    for (const event of pendingEvents.sort((left, right) => left.sequence - right.sequence)) applyEvent(event);
    pendingEvents = [];
    await nextTick();
    fitAddon?.fit();
    if (running.value) terminal?.focus();
  } catch (cause) {
    if (token === loadToken) {
      const threadID = activeThread.value?.id;
      error.value = threadID ? appStore.remoteFailureMessage(threadID, cause) : cause instanceof Error ? cause.message : String(cause);
    }
  } finally {
    if (token === loadToken) {
      loading.value = false;
      hydrated = true;
    }
  }
}

async function loadActiveTerminal() {
  const thread = activeThread.value;
  terminal?.reset();
  running.value = false;
  shell.value = "";
  error.value = "";
  lastSequence = 0;
  terminalGeneration = 0;
  if (!thread) {
    loadToken++;
    return;
  }
  const workspaceId = appStore.remoteWorkspaceForThread(thread)?.id;
  if (workspaceId && (!thread.started || !appStore.remoteReadyByWorkspace[workspaceId])) {
    loadToken++;
    loading.value = false;
    hydrated = true;
    pendingEvents = [];
    return;
  }
  await hydrate(() => terminalService.snapshot(thread.id, workspaceId));
}

async function startTerminal() {
  const thread = activeThread.value;
  if (!thread || (!thread.workspaceId && !workspacePath.value) || loading.value || pendingRemoteStartThreadID === thread.id) return;
  const remoteWorkspace = appStore.remoteWorkspaceForThread(thread);
  if (remoteWorkspace && (!thread.started || !appStore.remoteReadyByWorkspace[remoteWorkspace.id])) {
    pendingRemoteStartThreadID = thread.id;
    if (!appStore.requestRemoteReconnect(thread, "terminal")) appStore.startThreadInBackground(thread.id);
    return;
  }
  const workspace = remoteWorkspace ? { workspaceId: remoteWorkspace.id } : workspacePath.value;
  await hydrate(() => terminalService.start(thread.id, workspace, terminal?.cols || 80, terminal?.rows || 24));
}

async function stopTerminal() {
  const threadID = activeThread.value?.id;
  if (!threadID || !running.value || loading.value) return;
  loading.value = true;
  error.value = "";
  try {
    await terminalService.stop(threadID);
  } catch (cause) {
    error.value = appStore.remoteFailureMessage(threadID, cause);
  } finally {
    loading.value = false;
  }
}

function scheduleResize(columns: number, rows: number) {
  if (resizeTimer) clearTimeout(resizeTimer);
  resizeTimer = setTimeout(() => {
    const threadID = activeThread.value?.id;
    if (!threadID || !running.value) return;
    void terminalService.resize(threadID, columns, rows).catch((cause) => {
      error.value = appStore.remoteFailureMessage(threadID, cause);
    });
  }, 80);
}

onMounted(() => {
  terminal = new Terminal({
    cursorBlink: true,
    cursorStyle: "bar",
    fontFamily: '"Cascadia Mono", "Cascadia Code", Consolas, monospace',
    fontSize: 12,
    lineHeight: 1.25,
    scrollback: 5000,
    theme: terminalTheme(),
  });
  fitAddon = new FitAddon();
  terminal.loadAddon(fitAddon);
  if (host.value) terminal.open(host.value);
  terminal.attachCustomKeyEventHandler((event) => {
    if (event.type !== "keydown" || !event.ctrlKey || !event.shiftKey) return true;
    if (event.key.toLocaleLowerCase() === "c" && terminal?.hasSelection()) {
      void navigator.clipboard?.writeText(terminal.getSelection()).catch(() => undefined);
      return false;
    }
    if (event.key.toLocaleLowerCase() === "v") {
      void navigator.clipboard?.readText().then((text) => terminal?.paste(text)).catch(() => undefined);
      return false;
    }
    return true;
  });
  disposeInput = terminal.onData((data) => {
    const threadID = activeThread.value?.id;
    if (!threadID || !running.value) return;
    inputChain = inputChain.then(async () => {
      appStore.markRemoteRepositoryStale(threadID);
      await terminalService.write(threadID, data);
    }).catch((cause) => {
      error.value = appStore.remoteFailureMessage(threadID, cause);
    });
  });
  disposeResize = terminal.onResize(({ cols, rows }) => scheduleResize(cols, rows));
  disposeEvent = onTerminalEvent(handleEvent);
  resizeObserver = new ResizeObserver(() => fitAddon?.fit());
  if (host.value) resizeObserver.observe(host.value);
  fitAddon.fit();
  void loadActiveTerminal();
});

watch(() => activeThread.value?.id, (threadID) => {
  if (pendingRemoteStartThreadID && pendingRemoteStartThreadID !== threadID) pendingRemoteStartThreadID = "";
  void loadActiveTerminal();
});
watch(() => [activeThread.value?.id, activeThread.value?.started, activeThread.value?.status] as const, ([threadID, started, status]) => {
  if (!threadID || pendingRemoteStartThreadID !== threadID) return;
  if (started) {
    void (async () => {
      await loadActiveTerminal();
      if (activeThread.value?.id !== threadID || pendingRemoteStartThreadID !== threadID) return;
      pendingRemoteStartThreadID = "";
      await startTerminal();
    })();
  } else if (status === "attention") {
    pendingRemoteStartThreadID = "";
  }
});
watch(() => appStore.remoteReconnectOpen, (open) => {
  if (open || !pendingRemoteStartThreadID) return;
  const thread = appStore.threads.find((item) => item.id === pendingRemoteStartThreadID);
  const workspace = thread ? appStore.remoteWorkspaceForThread(thread) : undefined;
  if (!workspace || !appStore.remoteReadyByWorkspace[workspace.id]) pendingRemoteStartThreadID = "";
});
watch(() => appStore.appearance, () => void applyTerminalTheme());

onBeforeUnmount(() => {
  pendingRemoteStartThreadID = "";
  loadToken++;
  if (resizeTimer) clearTimeout(resizeTimer);
  resizeObserver?.disconnect();
  disposeEvent?.();
  disposeInput?.dispose();
  disposeResize?.dispose();
  terminal?.dispose();
});
</script>

<template>
  <div class="inspector-content terminal-panel" :class="ui.root">
    <div class="terminal-toolbar" :class="ui.toolbar">
      <span class="terminal-status" :class="{ 'is-running': running }" :title="running ? 'Terminal running' : 'Terminal stopped'" />
      <strong :title="shell">{{ activeThread ? shellName : "Terminal" }}</strong>
      <span v-if="activeThread" class="terminal-cwd" :title="workspaceLabel">{{ workspaceLabel }}</span>
      <div class="terminal-actions">
        <LoaderCircle v-if="loading" :size="14" class="is-spinning" />
        <button v-else-if="!running && activeThread" class="icon-button" :class="ui.iconButton" type="button" title="Start terminal" @click="void startTerminal()"><Play :size="14" /></button>
        <button v-else-if="running" class="icon-button" :class="ui.iconButton" type="button" title="Stop terminal" @click="void stopTerminal()"><Square :size="13" /></button>
        <button class="icon-button" :class="ui.iconButton" type="button" title="Clear terminal" :disabled="!activeThread" @click="terminal?.clear()"><Trash2 :size="14" /></button>
      </div>
    </div>
    <div class="terminal-stage">
      <div ref="host" class="terminal-host" />
      <div v-if="!activeThread" class="terminal-empty" :class="ui.empty"><span>Select a task to open its terminal.</span></div>
      <div v-else-if="!running && !loading && !error" class="terminal-empty" :class="ui.empty">
        <button class="text-button" :class="ui.button" type="button" @click="void startTerminal()"><Play :size="14" />Start terminal</button>
      </div>
    </div>
    <div v-if="error" class="terminal-error" role="alert">{{ error }}</div>
  </div>
</template>
