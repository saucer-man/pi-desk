<script setup lang="ts">
import { LoaderCircle, RefreshCw, X } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import type { OrphanSessionSummary } from "../../bindings/pi-desk/internal/domain";
import { useModalFocus } from "../composables/useModalFocus";
import { orphanSessionService } from "../services/orphanSessions";
import { useAppStore } from "../stores/app";
import { tr } from "../i18n";

interface TranscriptMessage {
  id: string;
  role: string;
  text: string;
  timestamp: string;
}

const appStore = useAppStore();
const dialog = ref<HTMLElement | null>(null);
const sessions = ref<OrphanSessionSummary[]>([]);
const selectedPath = ref("");
const messages = ref<TranscriptMessage[]>([]);
const loading = ref(true);
const loadingTranscript = ref(false);
const busy = ref(false);
const errorText = ref("");
const resultText = ref("");
let selectionGeneration = 0;

const selected = computed(() => sessions.value.find((session) => session.path === selectedPath.value));
const matchingWorkspace = computed(() => {
  const session = selected.value;
  if (!session?.targetId || !session.remoteRoot) return undefined;
  return appStore.workspaces.find((workspace) => workspace.kind === "ssh"
    && workspace.targetId === session.targetId && workspace.remoteRoot === session.remoteRoot);
});

function close() {
  if (!busy.value) appStore.closeOrphanSessions();
}

useModalFocus(dialog, close, { canClose: () => !busy.value });

function textContent(value: unknown): string {
  if (typeof value === "string") return value;
  if (!Array.isArray(value)) return "";
  return value.map((part) => {
    if (typeof part === "string") return part;
    if (!part || typeof part !== "object") return "";
    const item = part as Record<string, unknown>;
    if (typeof item.text === "string") return item.text;
    if (item.type === "toolCall" && typeof item.name === "string") return `[${tr("orphan.toolCall")}: ${item.name}]`;
    return "";
  }).filter(Boolean).join("\n");
}

function projectMessages(entries: unknown[] | null | undefined): TranscriptMessage[] {
  if (!entries) return [];
  const result: TranscriptMessage[] = [];
  for (const entryValue of entries) {
    if (!entryValue || typeof entryValue !== "object") continue;
    const entry = entryValue as Record<string, unknown>;
    if (entry.type !== "message" || !entry.message || typeof entry.message !== "object") continue;
    const message = entry.message as Record<string, unknown>;
    const role = typeof message.role === "string" ? message.role : "message";
    const text = textContent(message.content).slice(0, 100_000);
    if (!text) continue;
    result.push({
      id: typeof entry.id === "string" ? entry.id : `${result.length}`,
      role,
      text,
      timestamp: typeof entry.timestamp === "string" ? entry.timestamp : "",
    });
  }
  return result;
}

async function loadSessions() {
  loading.value = true;
  errorText.value = "";
  try {
    sessions.value = await orphanSessionService.list();
    if (sessions.value.length) await selectSession(sessions.value[0].path);
  } catch (error) {
    errorText.value = error instanceof Error ? error.message : String(error);
  } finally {
    loading.value = false;
  }
}

async function selectSession(path: string) {
  const generation = ++selectionGeneration;
  selectedPath.value = path;
  messages.value = [];
  resultText.value = "";
  errorText.value = "";
  loadingTranscript.value = true;
  try {
    const snapshot = await orphanSessionService.snapshot(path);
    if (generation !== selectionGeneration) return;
    messages.value = projectMessages(snapshot.messages);
  } catch (error) {
    if (generation === selectionGeneration) errorText.value = error instanceof Error ? error.message : String(error);
  } finally {
    if (generation === selectionGeneration) loadingTranscript.value = false;
  }
}

async function restoreSelected() {
  const session = selected.value;
  const workspace = matchingWorkspace.value;
  if (!session || !workspace || busy.value) return;
  busy.value = true;
  errorText.value = "";
  resultText.value = "";
  try {
    await orphanSessionService.restore(session.path, workspace.id);
    resultText.value = tr("orphan.restored", { workspace: workspace.name });
    sessions.value = sessions.value.filter((item) => item.path !== session.path);
    selectedPath.value = sessions.value[0]?.path ?? "";
    messages.value = [];
    if (selectedPath.value) await selectSession(selectedPath.value);
    await appStore.syncLocalSessions();
  } catch (error) {
    errorText.value = error instanceof Error ? error.message : String(error);
  } finally {
    busy.value = false;
  }
}

onMounted(() => { void loadSessions(); });
</script>

<template>
  <div class="dialog-backdrop" @mousedown.self="close">
    <section ref="dialog" class="dialog-window orphan-dialog" role="dialog" aria-modal="true" aria-labelledby="orphan-title" tabindex="-1">
      <header>
        <h2 id="orphan-title">{{ tr("orphan.title") }}</h2>
        <button class="icon-button" type="button" :title="tr('common.close')" :disabled="busy" @click="close"><X :size="17" /></button>
      </header>
      <div class="orphan-body">
        <aside class="orphan-list" :aria-label="tr('orphan.sessions')">
          <p v-if="loading" class="sidebar-empty"><LoaderCircle :size="15" class="is-spinning" /> {{ tr("orphan.loading") }}</p>
          <p v-else-if="sessions.length === 0" class="sidebar-empty">{{ tr("orphan.empty") }}</p>
          <button v-for="session in sessions" :key="session.path" type="button" :class="{ active: selectedPath === session.path }" @click="void selectSession(session.path)">
            <strong>{{ session.title || session.name || tr("orphan.untitled") }}</strong>
            <small>{{ session.firstMessage }}</small>
            <span>{{ tr("orphan.messageCount", { count: session.messageCount }) }}</span>
          </button>
        </aside>
        <main class="orphan-transcript">
          <template v-if="selected">
            <header>
              <div>
                <strong>{{ selected.title || selected.name || tr("orphan.untitled") }}</strong>
                <small>{{ matchingWorkspace ? tr("orphan.matchFound", { workspace: matchingWorkspace.name }) : tr("orphan.matchMissing") }}</small>
              </div>
              <div class="orphan-actions">
                <button class="text-button" type="button" :disabled="busy || !matchingWorkspace" @click="void restoreSelected()"><RefreshCw :size="14" />{{ tr("orphan.restore") }}</button>
              </div>
            </header>
            <div class="orphan-messages" aria-live="polite">
              <p v-if="loadingTranscript && messages.length === 0" class="sidebar-empty"><LoaderCircle :size="15" class="is-spinning" /> {{ tr("orphan.loadingTranscript") }}</p>
              <article v-for="message in messages" :key="message.id" :data-role="message.role">
                <header><strong>{{ message.role }}</strong><time v-if="message.timestamp">{{ message.timestamp }}</time></header>
                <pre>{{ message.text }}</pre>
              </article>
              <p v-if="!loadingTranscript && messages.length === 0" class="sidebar-empty">{{ tr("orphan.noMessages") }}</p>
            </div>
          </template>
          <p v-else class="sidebar-empty">{{ tr("orphan.select") }}</p>
        </main>
      </div>
      <footer>
        <p v-if="errorText" class="form-error" role="alert">{{ errorText }}</p>
        <p v-else-if="resultText" class="form-success">{{ resultText }}</p>
        <button class="text-button" type="button" :disabled="busy" @click="close">{{ tr("common.close") }}</button>
      </footer>
    </section>
  </div>
</template>
