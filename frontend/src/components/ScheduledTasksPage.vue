<script setup lang="ts">
import {
  CalendarClock,
  CheckCircle2,
  Clock3,
  ExternalLink,
  Pencil,
  Play,
  Plus,
  RotateCcw,
  Trash2,
  X,
  XCircle,
} from "lucide-vue-next";
import { computed, nextTick, onBeforeUnmount, ref } from "vue";
import { tr } from "../i18n";
import { useAppStore, type PiModel } from "../stores/app";
import { ui } from "../ui/classes";
import {
  newScheduledTaskDraft,
  scheduledTaskThinkingLevels,
  toLocalDateTimeInput,
  type ScheduledTask,
  type ScheduledTaskDraft,
  type ScheduledTaskFrequency,
} from "../utils/scheduledTasks";

const appStore = useAppStore();
const filter = ref<"active" | "paused" | "all">("active");
const editorDialog = ref<HTMLDialogElement>();
const editorID = ref("");
const editorError = ref("");
const draft = ref<ScheduledTaskDraft>(newScheduledTaskDraft());
const deletedTask = ref<ScheduledTask>();
let undoTimer: ReturnType<typeof setTimeout> | undefined;

const trustedLocalWorkspaces = computed(() => appStore.workspaces.filter((workspace) => workspace.kind !== "ssh" && workspace.trust === "approve" && !workspace.discovered));
const visibleTasks = computed(() => appStore.scheduledTasks
  .filter((task) => filter.value === "all" || (filter.value === "active" ? task.enabled : !task.enabled))
  .sort((left, right) => {
    if (left.enabled !== right.enabled) return left.enabled ? -1 : 1;
    return Date.parse(left.nextRunAt || "") - Date.parse(right.nextRunAt || "");
  }));
const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || "local time";
const weekdays = ["sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"] as const;
const frequencies: ScheduledTaskFrequency[] = ["once", "hourly", "daily", "weekdays", "weekly"];
const hourlyMinute = computed({
  get: () => Number(draft.value.time.slice(3)) || 0,
  set: (value: number | string) => {
    const minute = Math.max(0, Math.min(59, Number(value) || 0));
    draft.value.time = `00:${String(minute).padStart(2, "0")}`;
  },
});
const modelOptions = computed<PiModel[]>(() => {
  const models = [...appStore.scheduledTaskModels];
  if (draft.value.modelProvider && draft.value.modelId && !models.some((model) => model.provider === draft.value.modelProvider && model.id === draft.value.modelId)) {
    models.push({ provider: draft.value.modelProvider, id: draft.value.modelId, name: draft.value.modelName || undefined });
  }
  return models;
});
const selectedModelKey = computed({
  get: () => draft.value.modelProvider && draft.value.modelId ? modelOptionValue({ provider: draft.value.modelProvider, id: draft.value.modelId }) : "",
  set: (value: string) => {
    const model = modelOptions.value.find((candidate) => modelOptionValue(candidate) === value);
    draft.value.modelProvider = model?.provider || "";
    draft.value.modelId = model?.id || "";
    draft.value.modelName = model?.name || "";
    const current = appStore.activeSessionState;
    const isCurrent = Boolean(model && current?.model?.provider === model.provider && current.model.id === model.id);
    draft.value.thinkingLevel = isCurrent
      ? current?.thinkingLevel || (appStore.activeThinkingLevels.length === 1 ? appStore.activeThinkingLevels[0] : "")
      : model?.reasoning === false ? "off" : "";
  },
});
const thinkingLevelOptions = computed(() => {
  const model = modelOptions.value.find((candidate) => modelOptionValue(candidate) === selectedModelKey.value);
  if (!model) return [];
  const current = appStore.activeSessionState;
  const isCurrent = current?.model?.provider === model.provider && current.model.id === model.id;
  const levels = isCurrent && appStore.activeThinkingLevels.length
    ? [...appStore.activeThinkingLevels]
    : model.reasoning === false ? ["off"] : [...scheduledTaskThinkingLevels];
  if (draft.value.thinkingLevel && !levels.includes(draft.value.thinkingLevel)) levels.push(draft.value.thinkingLevel);
  return levels;
});

function modelOptionValue(model: { provider?: string; id?: string }): string {
  return `${encodeURIComponent(model.provider || "")}::${encodeURIComponent(model.id || "")}`;
}

function modelLabel(model: { modelProvider?: string; modelName?: string; modelId?: string }): string {
  const name = model.modelName || model.modelId;
  return name ? `${name}${model.modelProvider ? ` · ${model.modelProvider}` : ""}` : tr("scheduledTasks.modelMissing");
}

function thinkingLevelLabel(level?: string): string {
  return level ? tr(`scheduledTasks.thinkingLevels.${level}`) : tr("scheduledTasks.thinkingLevelMissing");
}

function workspaceName(task: ScheduledTask): string {
  return appStore.workspaces.find((workspace) => workspace.id === task.workspaceId)?.name || tr("common.unavailable");
}

function formatDate(value?: string): string {
  if (!value) return tr("scheduledTasks.neverRun");
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return tr("common.unavailable");
  return new Intl.DateTimeFormat(appStore.language === "zh-CN" ? "zh-CN" : "en", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function scheduleLabel(task: ScheduledTask): string {
  if (task.frequency === "once") return `${tr("scheduledTasks.once")} · ${formatDate(task.runAt)}`;
  if (task.frequency === "hourly") return `${tr("scheduledTasks.hourly")} · :${task.time?.slice(3) || "00"}`;
  if (task.frequency === "weekly") return `${tr(`scheduledTasks.${weekdays[task.weekday ?? 1]}`)} · ${task.time}`;
  return `${tr(`scheduledTasks.${task.frequency}`)} · ${task.time}`;
}

async function openEditor(task?: ScheduledTask) {
  editorID.value = task?.id || "";
  editorError.value = "";
  await appStore.refreshConfiguredModels();
  const defaultModel = appStore.activeSessionState?.model ?? (appStore.scheduledTaskModels.length === 1 ? appStore.scheduledTaskModels[0] : undefined);
  const defaultThinkingLevel = defaultModel?.provider === appStore.activeSessionState?.model?.provider && defaultModel?.id === appStore.activeSessionState?.model?.id
    ? appStore.activeSessionState?.thinkingLevel || (appStore.activeThinkingLevels.length === 1 ? appStore.activeThinkingLevels[0] : "")
    : defaultModel?.reasoning === false ? "off" : "";
  draft.value = task ? {
    name: task.name,
    prompt: task.prompt,
    workspaceId: task.workspaceId,
    modelProvider: task.modelProvider || "",
    modelId: task.modelId || "",
    modelName: task.modelName || "",
    thinkingLevel: task.thinkingLevel || "",
    frequency: task.frequency,
    time: task.time || "09:00",
    weekday: task.weekday ?? 1,
    runAt: toLocalDateTimeInput(task.runAt || new Date()),
  } : {
    ...newScheduledTaskDraft(trustedLocalWorkspaces.value[0]?.id || ""),
    modelProvider: defaultModel?.provider || "",
    modelId: defaultModel?.id || "",
    modelName: defaultModel?.name || "",
    thinkingLevel: defaultThinkingLevel,
  };
  await nextTick();
  editorDialog.value?.showModal();
  await nextTick();
  editorDialog.value?.querySelector<HTMLInputElement>("input")?.focus();
}

function closeEditor() {
  editorDialog.value?.close();
  editorError.value = "";
}

function saveTask() {
  try {
    appStore.saveScheduledTask(draft.value, editorID.value);
    closeEditor();
  } catch (error) {
    editorError.value = error instanceof Error ? error.message : String(error);
  }
}

function removeTask(taskID: string) {
  if (undoTimer) clearTimeout(undoTimer);
  deletedTask.value = appStore.deleteScheduledTask(taskID);
  undoTimer = setTimeout(() => {
    deletedTask.value = undefined;
    undoTimer = undefined;
  }, 8_000);
}

function undoDelete() {
  if (!deletedTask.value) return;
  appStore.restoreScheduledTask(deletedTask.value);
  deletedTask.value = undefined;
  if (undoTimer) clearTimeout(undoTimer);
  undoTimer = undefined;
}

function openLatestRun(task: ScheduledTask) {
  if (task.lastThreadId) appStore.selectThread(task.lastThreadId);
}

function onDialogBackdrop(event: MouseEvent) {
  if (event.target === editorDialog.value) closeEditor();
}

onBeforeUnmount(() => {
  if (undoTimer) clearTimeout(undoTimer);
});
</script>

<template>
  <section class="scheduled-tasks-page" :class="ui.root" aria-labelledby="scheduled-tasks-title">
    <header class="scheduled-tasks-header">
      <div>
        <h1 id="scheduled-tasks-title">{{ tr("scheduledTasks.title") }}</h1>
        <p>{{ tr("scheduledTasks.description") }}</p>
      </div>
      <button :class="ui.buttonPrimary" type="button" :disabled="trustedLocalWorkspaces.length === 0" @click="void openEditor()">
        <Plus :size="16" />
        <span>{{ tr("scheduledTasks.create") }}</span>
      </button>
    </header>

    <p v-if="trustedLocalWorkspaces.length === 0" class="scheduled-tasks-notice" role="status">
      <XCircle :size="16" />
      <span>{{ tr("scheduledTasks.noWorkspace") }}</span>
    </p>

    <div class="scheduled-tasks-toolbar" role="tablist" :aria-label="tr('scheduledTasks.title')">
      <button v-for="item in (['active', 'paused', 'all'] as const)" :key="item" :class="ui.tab" type="button" role="tab" :aria-selected="filter === item" @click="filter = item">
        {{ tr(`scheduledTasks.${item}`) }}
      </button>
    </div>

    <div v-if="visibleTasks.length" class="scheduled-task-list">
      <article v-for="task in visibleTasks" :key="task.id" class="scheduled-task-row" :class="{ 'is-paused': !task.enabled }">
        <div class="scheduled-task-leading" aria-hidden="true">
          <CalendarClock :size="19" />
        </div>
        <div class="scheduled-task-copy">
          <div class="scheduled-task-title-row">
            <h2>{{ task.name }}</h2>
            <span v-if="task.lastStatus" class="scheduled-task-run-status" :class="task.lastStatus">
              <CheckCircle2 v-if="task.lastStatus === 'started'" :size="13" />
              <XCircle v-else :size="13" />
              {{ tr(`scheduledTasks.${task.lastStatus}`) }}
            </span>
          </div>
          <p class="scheduled-task-prompt">{{ task.prompt }}</p>
          <dl>
            <div>
              <dt>{{ tr("scheduledTasks.workspace") }}</dt>
              <dd>{{ workspaceName(task) }}</dd>
            </div>
            <div>
              <dt>{{ tr("scheduledTasks.model") }}</dt>
              <dd>{{ modelLabel(task) }}</dd>
            </div>
            <div>
              <dt>{{ tr("scheduledTasks.thinkingLevel") }}</dt>
              <dd>{{ thinkingLevelLabel(task.thinkingLevel) }}</dd>
            </div>
            <div>
              <dt>{{ tr("scheduledTasks.frequency") }}</dt>
              <dd>{{ scheduleLabel(task) }}</dd>
            </div>
            <div>
              <dt>{{ tr("scheduledTasks.nextRun") }}</dt>
              <dd>{{ task.enabled ? formatDate(task.nextRunAt) : tr("scheduledTasks.paused") }}</dd>
            </div>
            <div>
              <dt>{{ tr("scheduledTasks.lastRun") }}</dt>
              <dd>{{ formatDate(task.lastRunAt) }}</dd>
            </div>
          </dl>
          <p v-if="task.lastError" class="scheduled-task-error" role="alert">{{ task.lastError }}</p>
        </div>
        <div class="scheduled-task-actions">
          <label class="scheduled-task-switch">
            <input type="checkbox" :checked="task.enabled" :disabled="!task.modelProvider || !task.modelId || !task.thinkingLevel" :aria-label="tr('scheduledTasks.enable', { name: task.name })" @change="appStore.toggleScheduledTask(task.id)" />
            <span aria-hidden="true" />
          </label>
          <button :class="ui.iconButton" type="button" :title="tr('scheduledTasks.runNow')" :aria-label="tr('scheduledTasks.runNow')" :disabled="appStore.scheduledTaskRunningByID[task.id] || !task.modelProvider || !task.modelId || !task.thinkingLevel" @click="void appStore.runScheduledTask(task.id)">
            <RotateCcw v-if="appStore.scheduledTaskRunningByID[task.id]" class="is-spinning" :size="16" />
            <Play v-else :size="16" />
          </button>
          <button v-if="task.lastThreadId" :class="ui.iconButton" type="button" :title="tr('scheduledTasks.openRun')" :aria-label="tr('scheduledTasks.openRun')" @click="openLatestRun(task)">
            <ExternalLink :size="16" />
          </button>
          <button :class="ui.iconButton" type="button" :title="tr('scheduledTasks.editNamed', { name: task.name })" :aria-label="tr('scheduledTasks.editNamed', { name: task.name })" @click="void openEditor(task)">
            <Pencil :size="16" />
          </button>
          <button :class="[ui.iconButton, 'scheduled-task-delete']" type="button" :title="tr('scheduledTasks.deleteNamed', { name: task.name })" :aria-label="tr('scheduledTasks.deleteNamed', { name: task.name })" @click="removeTask(task.id)">
            <Trash2 :size="16" />
          </button>
        </div>
      </article>
    </div>

    <div v-else class="scheduled-task-empty">
      <Clock3 :size="24" />
      <strong>{{ tr("scheduledTasks.emptyTitle") }}</strong>
      <p>{{ tr("scheduledTasks.emptyBody") }}</p>
      <button v-if="trustedLocalWorkspaces.length" :class="ui.button" type="button" @click="void openEditor()">
        <Plus :size="16" />
        <span>{{ tr("scheduledTasks.create") }}</span>
      </button>
    </div>

    <div v-if="deletedTask" class="scheduled-task-undo" role="status" aria-live="polite">
      <span>{{ tr("scheduledTasks.deleted") }}</span>
      <button type="button" @click="undoDelete">{{ tr("scheduledTasks.undo") }}</button>
    </div>

    <dialog ref="editorDialog" class="scheduled-task-dialog" :aria-labelledby="editorID ? 'scheduled-task-edit-title' : 'scheduled-task-create-title'" @click="onDialogBackdrop">
      <form method="dialog" @submit.prevent="saveTask">
        <header>
          <h2 :id="editorID ? 'scheduled-task-edit-title' : 'scheduled-task-create-title'">{{ tr(editorID ? "scheduledTasks.edit" : "scheduledTasks.create") }}</h2>
          <button :class="ui.iconButton" type="button" :title="tr('scheduledTasks.closeEditor')" :aria-label="tr('scheduledTasks.closeEditor')" @click="closeEditor"><X :size="17" /></button>
        </header>
        <div class="scheduled-task-form">
          <label :class="ui.field">
            <span>{{ tr("scheduledTasks.name") }}</span>
            <input v-model="draft.name" :class="ui.input" maxlength="200" required :placeholder="tr('scheduledTasks.namePlaceholder')" />
            <small aria-hidden="true">&nbsp;</small>
          </label>
          <label :class="ui.field">
            <span>{{ tr("scheduledTasks.workspace") }}</span>
            <select v-model="draft.workspaceId" :class="ui.select" required>
              <option v-for="workspace in trustedLocalWorkspaces" :key="workspace.id" :value="workspace.id">{{ workspace.name }}</option>
            </select>
            <small>{{ tr("scheduledTasks.workspaceHelp") }}</small>
          </label>
          <label class="scheduled-task-prompt-field" :class="ui.field">
            <span>{{ tr("scheduledTasks.prompt") }}</span>
            <textarea v-model="draft.prompt" :class="ui.textarea" maxlength="1048576" required :placeholder="tr('scheduledTasks.promptPlaceholder')" />
            <small aria-hidden="true">&nbsp;</small>
          </label>
          <label class="scheduled-task-parameter-field" :class="ui.field">
            <span>{{ tr("scheduledTasks.model") }}</span>
            <select v-model="selectedModelKey" :class="ui.select" required>
              <option disabled value="">{{ tr("scheduledTasks.modelPlaceholder") }}</option>
              <option v-for="model in modelOptions" :key="modelOptionValue(model)" :value="modelOptionValue(model)">{{ model.name || model.id }} · {{ model.provider }}</option>
            </select>
            <small>{{ tr(modelOptions.length ? "scheduledTasks.modelHelp" : "scheduledTasks.modelsUnavailable") }}</small>
          </label>
          <label class="scheduled-task-parameter-field" :class="ui.field">
            <span>{{ tr("scheduledTasks.thinkingLevel") }}</span>
            <select v-model="draft.thinkingLevel" :class="ui.select" required :disabled="!selectedModelKey">
              <option disabled value="">{{ tr("scheduledTasks.thinkingLevelPlaceholder") }}</option>
              <option v-for="level in thinkingLevelOptions" :key="level" :value="level">{{ thinkingLevelLabel(level) }}</option>
            </select>
            <small>{{ tr("scheduledTasks.thinkingLevelHelp") }}</small>
          </label>
          <label :class="ui.field">
            <span>{{ tr("scheduledTasks.frequency") }}</span>
            <select v-model="draft.frequency" :class="ui.select">
              <option v-for="frequency in frequencies" :key="frequency" :value="frequency">{{ tr(`scheduledTasks.${frequency}`) }}</option>
            </select>
            <small>{{ tr("scheduledTasks.timezone", { timezone }) }}</small>
          </label>
          <label v-if="draft.frequency === 'once'" :class="ui.field">
            <span>{{ tr("scheduledTasks.runAt") }}</span>
            <input v-model="draft.runAt" :class="ui.input" type="datetime-local" required />
            <small aria-hidden="true">&nbsp;</small>
          </label>
          <label v-else :class="ui.field">
            <span>{{ tr(draft.frequency === 'hourly' ? "scheduledTasks.minute" : "scheduledTasks.time") }}</span>
            <input v-if="draft.frequency === 'hourly'" v-model.number="hourlyMinute" :class="ui.input" type="number" min="0" max="59" step="1" required />
            <input v-else v-model="draft.time" :class="ui.input" type="time" required />
            <small aria-hidden="true">&nbsp;</small>
          </label>
          <label v-if="draft.frequency === 'weekly'" :class="ui.field">
            <span>{{ tr("scheduledTasks.weekday") }}</span>
            <select v-model.number="draft.weekday" :class="ui.select">
              <option v-for="(weekday, index) in weekdays" :key="weekday" :value="index">{{ tr(`scheduledTasks.${weekday}`) }}</option>
            </select>
            <small aria-hidden="true">&nbsp;</small>
          </label>
          <p v-if="editorError" class="scheduled-task-form-error" role="alert">{{ editorError }}</p>
        </div>
        <footer>
          <button :class="ui.button" type="button" @click="closeEditor">{{ tr("common.cancel") }}</button>
          <button :class="ui.buttonPrimary" type="submit">{{ tr("scheduledTasks.save") }}</button>
        </footer>
      </form>
    </dialog>
  </section>
</template>

<style scoped>
/* Hallmark · macrostructure: Workbench · tone: technical/austere · anchor hue: neutral
 * audience: Pi Desk developers · use: create and operate scheduled Pi tasks
 * theme: locked Pi Desk neutral · enrichment: none · motion: state feedback only · knob delta: scheduler queue + modal editor, no split pane
 * pre-emit critique: P5 H5 E5 S5 R5 V4 · contrast/slop/tokens/icons/mobile: pass */
.scheduled-tasks-page {
  min-width: 0;
  min-height: 0;
  overflow-y: auto;
  padding: var(--space-8) clamp(var(--space-4), 4vw, var(--space-8));
  background: var(--bg-workspace);
}

.scheduled-tasks-header {
  display: flex;
  max-width: 1040px;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-6);
  margin-inline: auto;
}

.scheduled-tasks-header h1 {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
  font-family: var(--font-interface-display);
  font-size: calc(24px + var(--font-size-delta));
  font-style: normal;
  line-height: 1.15;
  letter-spacing: -0.025em;
}

.scheduled-tasks-header p,
.scheduled-task-empty p {
  max-width: 65ch;
  margin: var(--space-2) 0 0;
  color: var(--text-secondary);
  line-height: 1.55;
}

.scheduled-tasks-notice {
  display: flex;
  max-width: 1040px;
  align-items: center;
  gap: var(--space-2);
  margin: var(--space-6) auto 0;
  padding: var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--bg-raised);
  color: var(--text-secondary);
}

.scheduled-tasks-toolbar {
  display: flex;
  max-width: 1040px;
  margin: var(--space-8) auto 0;
  border-bottom: 1px solid var(--border);
}

.scheduled-task-list {
  display: grid;
  max-width: 1040px;
  gap: var(--space-3);
  margin: var(--space-4) auto var(--space-8);
}

.scheduled-task-row {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  gap: var(--space-4);
  padding: var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--bg-raised);
}

.scheduled-task-row.is-paused {
  background: var(--bg-app);
}

.scheduled-task-leading {
  display: grid;
  width: 36px;
  height: 36px;
  place-items: center;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  color: var(--text-secondary);
}

.scheduled-task-copy {
  min-width: 0;
}

.scheduled-task-title-row {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: var(--space-2);
}

.scheduled-task-title-row h2 {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
  font-family: var(--font-interface-display);
  font-size: calc(16px + var(--font-size-delta));
  font-style: normal;
  line-height: 1.3;
}

.scheduled-task-run-status {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: var(--space-1);
  color: var(--diff-add-text);
  font-size: calc(11px + var(--font-size-delta));
}

.scheduled-task-run-status.failed,
.scheduled-task-error,
.scheduled-task-form-error {
  color: var(--red);
}

.scheduled-task-prompt {
  display: -webkit-box;
  max-width: 72ch;
  margin: var(--space-2) 0 0;
  overflow: hidden;
  color: var(--text-secondary);
  line-height: 1.5;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.scheduled-task-copy dl {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--space-3);
  margin: var(--space-4) 0 0;
}

.scheduled-task-copy dl div {
  min-width: 0;
}

.scheduled-task-copy dt {
  color: var(--text-secondary);
  font-size: calc(11px + var(--font-size-delta));
}

.scheduled-task-copy dd {
  margin: var(--space-1) 0 0;
  overflow: hidden;
  color: var(--text-secondary);
  font-size: calc(12px + var(--font-size-delta));
  font-variant-numeric: tabular-nums;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.scheduled-task-error {
  margin: var(--space-3) 0 0;
  font-size: calc(12px + var(--font-size-delta));
  line-height: 1.45;
}

.scheduled-task-actions {
  display: flex;
  align-items: flex-start;
  gap: var(--space-1);
}

.scheduled-task-switch {
  position: relative;
  display: grid;
  width: 44px;
  height: 44px;
  cursor: pointer;
  place-items: center;
}

.scheduled-task-switch input {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
}

.scheduled-task-switch span {
  position: relative;
  width: 30px;
  height: 18px;
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-pill);
  background: var(--bg-active);
}

.scheduled-task-switch span::after {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: var(--text-muted);
  content: "";
  transition: transform var(--motion-fast) var(--ease-out), background-color var(--motion-fast) var(--ease-out);
}

.scheduled-task-switch input:checked + span {
  background: var(--text);
}

.scheduled-task-switch input:checked + span::after {
  background: var(--bg-workspace);
  transform: translateX(12px);
}

.scheduled-task-switch input:focus-visible + span {
  outline: 2px solid var(--focus);
  outline-offset: 2px;
}

.scheduled-task-switch input:active + span {
  border-color: var(--text);
}

.scheduled-task-switch:has(input:disabled) {
  cursor: not-allowed;
  opacity: 0.5;
}

.scheduled-task-delete:hover,
.scheduled-task-delete:focus-visible {
  color: var(--red);
}

.scheduled-task-empty {
  display: grid;
  max-width: 1040px;
  min-height: 280px;
  place-items: center;
  align-content: center;
  gap: var(--space-2);
  margin: var(--space-4) auto;
  border-bottom: 1px solid var(--border);
  color: var(--text-muted);
  text-align: center;
}

.scheduled-task-empty strong {
  color: var(--text);
  font-family: var(--font-interface-display);
  font-size: calc(16px + var(--font-size-delta));
}

.scheduled-task-empty p {
  margin: 0 0 var(--space-3);
}

.scheduled-task-undo {
  position: fixed;
  right: var(--space-6);
  bottom: var(--space-6);
  z-index: var(--z-toast);
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-md);
  background: var(--text);
  color: var(--text-inverse);
  box-shadow: var(--shadow-menu);
}

.scheduled-task-undo button {
  border: 0;
  background: transparent;
  color: inherit;
  cursor: pointer;
  font-weight: 700;
  text-decoration: underline;
  text-underline-offset: 3px;
  white-space: nowrap;
}

.scheduled-task-undo button:hover {
  opacity: 0.8;
}

.scheduled-task-undo button:active {
  opacity: 0.6;
}

.scheduled-task-undo button:focus-visible {
  outline: 2px solid var(--text-inverse);
  outline-offset: 2px;
}

.scheduled-task-undo button:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.scheduled-task-dialog {
  position: fixed;
  inset: 0;
  width: min(680px, calc(100% - var(--space-8)));
  max-height: min(84dvh, 760px);
  margin: auto;
  padding: 0;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--bg-panel);
  color: var(--text);
  box-shadow: var(--shadow-dialog);
}

.scheduled-task-dialog::backdrop {
  background: var(--overlay);
}

.scheduled-task-dialog form {
  display: grid;
  max-height: inherit;
  grid-template-rows: auto minmax(0, 1fr) auto;
}

.scheduled-task-dialog header,
.scheduled-task-dialog footer {
  display: flex;
  min-height: 60px;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--border);
}

.scheduled-task-dialog h2 {
  margin: 0;
  font-family: var(--font-interface-display);
  font-size: calc(18px + var(--font-size-delta));
  font-style: normal;
}

.scheduled-task-dialog footer {
  justify-content: flex-end;
  border-top: 1px solid var(--border);
  border-bottom: 0;
}

.scheduled-task-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  align-items: start;
  align-content: start;
  gap: var(--space-4);
  overflow-y: auto;
  padding: var(--space-4);
}

.scheduled-task-form :deep(small),
.scheduled-task-form :deep(input::placeholder),
.scheduled-task-form :deep(textarea::placeholder) {
  color: var(--text-secondary);
}

.scheduled-task-prompt-field,
.scheduled-task-form-error {
  grid-column: 1 / -1;
}

.scheduled-task-form-error {
  margin: 0;
}

@media (max-width: 60rem) {
  .scheduled-task-copy dl {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .scheduled-task-row {
    grid-template-columns: auto minmax(0, 1fr);
  }

  .scheduled-task-actions {
    grid-column: 2;
    justify-content: flex-start;
  }
}

@media (max-width: 40rem) {
  .scheduled-tasks-page {
    padding: var(--space-4);
  }

  .scheduled-tasks-header {
    flex-direction: column;
    gap: var(--space-4);
  }

  .scheduled-task-row {
    grid-template-columns: minmax(0, 1fr);
  }

  .scheduled-task-leading {
    display: none;
  }

  .scheduled-task-actions {
    grid-column: 1;
    flex-wrap: wrap;
  }

  .scheduled-task-copy dl,
  .scheduled-task-form {
    grid-template-columns: minmax(0, 1fr);
  }

  .scheduled-task-prompt-field,
  .scheduled-task-form-error {
    grid-column: 1;
  }

  .scheduled-task-dialog {
    width: calc(100% - var(--space-4));
  }

  .scheduled-task-undo {
    right: var(--space-4);
    bottom: var(--space-4);
    left: var(--space-4);
    justify-content: space-between;
  }
}

@media (prefers-reduced-motion: reduce) {
  .scheduled-task-switch span::after {
    transition-duration: 0ms;
  }
}
</style>
