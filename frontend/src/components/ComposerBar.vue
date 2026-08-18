<script setup lang="ts">
import { ArrowDownToLine, ArrowUp, ArrowUpFromLine, BrainCircuit, Check, ChevronDown, CornerDownRight, Database, File, Forward, Gauge, ImagePlus, LoaderCircle, Pencil, ShieldAlert, ShieldCheck, Slash, SlidersHorizontal, Square, Trash2, X } from "lucide-vue-next";
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useAppStore, type PiModel, type SettingsSection, type SlashCommand } from "../stores/app";
import { MAX_ATTACHED_IMAGES, MAX_IMAGE_BASE64_CHARS, prepareImage, type PreparedImage } from "../utils/imageAttachments";
import { formatFileMention } from "../utils/fileMentions";
import { parsePiDeskTodoWidget, PI_DESK_TODO_WIDGET_KEY } from "../utils/todoWidget";
import { tr } from "../i18n";
import ImagePreviewDialog from "./ImagePreviewDialog.vue";
import PiDeskTodoPanel from "./PiDeskTodoPanel.vue";

const appStore = useAppStore();
const textarea = ref<HTMLTextAreaElement>();
const commandMenu = ref<HTMLElement>();
const commandButton = ref<HTMLElement>();
const modelMenu = ref<HTMLElement>();
const modelMenuOpen = ref(false);
const modelChanging = ref(false);
const modelCatalogRefreshing = ref(false);
const accessMenuOpen = ref(false);
const accessMenu = ref<HTMLElement>();
const commandIndex = ref(0);
const commandButtonOpen = ref(false);
const commandRefreshing = ref(false);
const mentionIndex = ref(0);
const commandDismissed = ref(false);
const mentionDismissed = ref(false);
const attachmentError = ref("");
const previewImage = ref<PreparedImage>();
const processingImages = ref(false);
const dragActive = ref(false);
const editingPromptId = ref("");
const editingPromptText = ref("");
const editingPromptImages = ref<PreparedImage[]>([]);
const editingPromptError = ref("");
const editingPromptProcessing = ref(false);

interface DesktopSlashCommand {
  name: "skill" | "prompt";
  description: string;
  source: "desktop";
  settingsSection: SettingsSection;
}

type ComposerSlashCommand = SlashCommand | DesktopSlashCommand;

const draft = computed({
  get: () => appStore.activeDraft,
  set: (value: string) => appStore.updateDraft(value),
});
const commandMenuOpen = computed(() => !modelMenuOpen.value && !accessMenuOpen.value && (commandButtonOpen.value || (!commandDismissed.value && draft.value.startsWith("/") && !draft.value.slice(1).includes(" "))));
const mentionMatch = computed(() => /(^|\s)@([^\s"]*)$/.exec(draft.value));
const matchingFiles = computed(() => {
  const query = mentionMatch.value?.[2].toLocaleLowerCase();
  if (query === undefined) return [];
  return (appStore.activeRepository?.files ?? [])
    .filter((file) => !query || `${file.name} ${file.path}`.toLocaleLowerCase().includes(query))
    .slice(0, 8);
});
const mentionMenuOpen = computed(() => !commandButtonOpen.value && !modelMenuOpen.value && !accessMenuOpen.value && !mentionDismissed.value && Boolean(mentionMatch.value) && matchingFiles.value.length > 0);
const currentModel = computed(() => appStore.activeSessionState?.model);
const modelButtonLabel = computed(() => {
  const label = modelLabel(currentModel.value);
  const level = appStore.activeSessionState?.thinkingLevel;
  return level ? `${label} · ${level}` : label;
});
const sessionStats = computed(() => appStore.activeSessionStats);
const tokenUsage = computed(() => sessionStats.value?.tokens);
const contextTokens = computed(() => sessionStats.value?.contextUsage?.tokens);
const contextEstimated = computed(() => sessionStats.value?.contextUsage?.estimated === true);
const contextWindow = computed(() => sessionStats.value?.contextUsage?.contextWindow ?? currentModel.value?.contextWindow);
const inputTokens = computed(() => tokenUsage.value?.input);
const outputTokens = computed(() => tokenUsage.value?.output);
const cacheTokens = computed(() => {
  const usage = tokenUsage.value;
  if (!usage) return undefined;
  return (typeof usage.cacheRead === "number" ? usage.cacheRead : 0)
    + (typeof usage.cacheWrite === "number" ? usage.cacheWrite : 0);
});
const contextPercent = computed(() => {
  const reported = sessionStats.value?.contextUsage?.percent;
  if (typeof reported === "number" && Number.isFinite(reported)) return Math.min(100, Math.max(0, reported));
  const used = contextTokens.value;
  const limit = contextWindow.value;
  return typeof used === "number" && typeof limit === "number" && limit > 0
    ? Math.min(100, Math.max(0, (used / limit) * 100))
    : 0;
});
const piStarting = computed(() => appStore.activeThread?.status === "starting");
const agentRunning = computed(() => appStore.activeThread?.status === "running");
const bashRunning = computed(() => appStore.activeBashRunning);
const running = computed(() => agentRunning.value || bashRunning.value);
const bashDraft = computed(() => draft.value.trim().startsWith("!") && appStore.activeAttachments.length === 0);
const accessBusy = computed(() => appStore.activeWorkspaceTrustBusy);
const queuedMessages = computed(() => appStore.activePendingPrompts);
const rawWidgetsAbove = computed(() => appStore.activeExtensionWidgets.filter((widget) => widget.placement === "aboveEditor"));
const piDeskTodoWidget = computed(() => rawWidgetsAbove.value.find((widget) => widget.key.toLocaleLowerCase() === PI_DESK_TODO_WIDGET_KEY));
const piDeskTodo = computed(() => parsePiDeskTodoWidget(piDeskTodoWidget.value));
const piDeskTodoKey = computed(() => `${appStore.activeThreadId}:${piDeskTodoWidget.value?.instance ?? PI_DESK_TODO_WIDGET_KEY}`);
const widgetsAbove = computed(() => rawWidgetsAbove.value.filter((widget) => widget !== piDeskTodoWidget.value || !piDeskTodo.value));
const widgetsBelow = computed(() => appStore.activeExtensionWidgets.filter((widget) => widget.placement === "belowEditor"));
const desktopCommands = computed<DesktopSlashCommand[]>(() => [
  { name: "skill", description: tr("composer.openSkillManagement"), source: "desktop", settingsSection: "skillManagement" },
  { name: "prompt", description: tr("composer.openPromptManagement"), source: "desktop", settingsSection: "promptManagement" },
]);
const hiddenRpcCommandNames = new Set(["todo", "llama"]);
const matchingCommands = computed<ComposerSlashCommand[]>(() => {
  const query = commandButtonOpen.value ? "" : draft.value.slice(1).toLocaleLowerCase();
  return [...desktopCommands.value, ...appStore.activeCommands]
    .filter((command) => !hiddenRpcCommandNames.has(command.name.toLocaleLowerCase()))
    .filter((command) => command.name.toLocaleLowerCase().includes(query));
});

function commandSourceLabel(source: ComposerSlashCommand["source"]): string {
  return tr(`composer.commandSource${source.charAt(0).toUpperCase()}${source.slice(1)}`);
}

function commandTitle(command: ComposerSlashCommand): string {
  return [commandSourceLabel(command.source), "path" in command ? command.path : undefined].filter(Boolean).join(" · ");
}

async function refreshSlashCommands() {
  const thread = appStore.activeThread;
  if (!thread?.started || commandRefreshing.value) return;
  commandRefreshing.value = true;
  try {
    await appStore.refreshCommands(thread.id);
  } catch {
    // Keep the last known command list when the runtime is restarting.
  } finally {
    commandRefreshing.value = false;
  }
}

watch(draft, async (value, previousValue) => {
  if (value !== previousValue) commandButtonOpen.value = false;
  commandIndex.value = 0;
  mentionIndex.value = 0;
  commandDismissed.value = false;
  mentionDismissed.value = false;
  if (value.startsWith("/") && !previousValue.startsWith("/")) void refreshSlashCommands();
  await nextTick();
  if (!textarea.value) return;
  textarea.value.style.height = "0";
  textarea.value.style.height = `${Math.min(textarea.value.scrollHeight, 180)}px`;
});

watch(matchingCommands, (commands) => {
  commandIndex.value = Math.min(commandIndex.value, Math.max(0, commands.length - 1));
});

watch(commandIndex, async () => {
  await nextTick();
  const selected = commandMenu.value?.querySelector<HTMLElement>('[aria-selected="true"]');
  if (selected && typeof selected.scrollIntoView === "function") selected.scrollIntoView({ block: "nearest" });
});

watch(matchingFiles, (files) => {
  mentionIndex.value = Math.min(mentionIndex.value, Math.max(0, files.length - 1));
});

watch(() => appStore.activeThreadId, () => {
  previewImage.value = undefined;
});

function submit() {
  if (!draft.value.trim() && appStore.activeAttachments.length === 0) return;
  if (bashDraft.value) {
    void appStore.sendActiveBash();
    return;
  }
  void appStore.sendActivePrompt();
}

async function prepareImageFiles(source: FileList | File[], existing: PreparedImage[], setError: (message: string) => void): Promise<PreparedImage[]> {
  const remaining = MAX_ATTACHED_IMAGES - existing.length;
  const files = Array.from(source).filter((file) => file.type.startsWith("image/")).slice(0, Math.max(0, remaining));
  if (!files.length) {
    setError(remaining <= 0 ? `A prompt can include at most ${MAX_ATTACHED_IMAGES} images` : "No supported images selected");
    return [];
  }
  setError("");
  const prepared: PreparedImage[] = [];
  let encodedChars = existing.reduce((total, image) => total + image.data.length, 0);
  for (const file of files) {
    try {
      const image = await prepareImage(file);
      if (encodedChars + image.data.length > MAX_IMAGE_BASE64_CHARS) {
        setError("Image attachments exceed the Pi RPC size limit");
        break;
      }
      encodedChars += image.data.length;
      prepared.push(image);
    } catch (error) {
      setError(error instanceof Error ? error.message : String(error));
    }
  }
  return prepared;
}

async function addImageFiles(source: FileList | File[]) {
  const threadId = appStore.activeThreadId;
  processingImages.value = true;
  try {
    const prepared = await prepareImageFiles(source, appStore.activeAttachments, (message) => { attachmentError.value = message; });
    if (appStore.activeThreadId === threadId) appStore.addActiveAttachments(prepared);
  } finally {
    processingImages.value = false;
  }
}

function onPaste(event: ClipboardEvent) {
  const imageFiles = Array.from(event.clipboardData?.items ?? [])
    .filter((item) => item.kind === "file" && item.type.startsWith("image/"))
    .map((item) => item.getAsFile())
    .filter((file): file is File => Boolean(file));
  if (!imageFiles.length) return;
  event.preventDefault();
  void addImageFiles(imageFiles);
}

function onDrop(event: DragEvent) {
  dragActive.value = false;
  const files = event.dataTransfer?.files;
  if (!files?.length) return;
  event.preventDefault();
  void addImageFiles(files);
}

function onKeydown(event: KeyboardEvent) {
  if (mentionMenuOpen.value) {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const direction = event.key === "ArrowDown" ? 1 : -1;
      mentionIndex.value = (mentionIndex.value + direction + matchingFiles.value.length) % matchingFiles.value.length;
      return;
    }
    if (event.key === "Tab" || (event.key === "Enter" && !event.shiftKey && !event.isComposing)) {
      event.preventDefault();
      chooseFileMention(matchingFiles.value[mentionIndex.value].path);
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      mentionDismissed.value = true;
      return;
    }
  }
  if (commandMenuOpen.value && matchingCommands.value.length) {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const direction = event.key === "ArrowDown" ? 1 : -1;
      commandIndex.value = (commandIndex.value + direction + matchingCommands.value.length) % matchingCommands.value.length;
      return;
    }
    if (event.key === "Tab" || (event.key === "Enter" && !event.shiftKey && !event.isComposing)) {
      event.preventDefault();
      chooseCommand(matchingCommands.value[commandIndex.value]);
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      if (commandButtonOpen.value) commandButtonOpen.value = false;
      else commandDismissed.value = true;
      return;
    }
  }
  if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
    event.preventDefault();
    submit();
  }
}

function chooseFileMention(path: string) {
  const match = mentionMatch.value;
  if (!match) return;
  const prefix = `${draft.value.slice(0, match.index)}${match[1]}`;
  draft.value = `${prefix}${formatFileMention(path)} `;
  textarea.value?.focus();
}

function toggleCommandMenu() {
  if (commandButtonOpen.value) {
    commandButtonOpen.value = false;
    return;
  }
  modelMenuOpen.value = false;
  accessMenuOpen.value = false;
  commandDismissed.value = false;
  mentionDismissed.value = true;
  commandIndex.value = 0;
  commandButtonOpen.value = true;
  void refreshSlashCommands();
  textarea.value?.focus();
}

function commandArguments(value: string): string {
  const trimmed = value.trim();
  if (!trimmed.startsWith("/")) return trimmed;
  const separator = trimmed.search(/\s/);
  return separator < 0 ? "" : trimmed.slice(separator).trim();
}

function chooseCommand(command: ComposerSlashCommand) {
  const openedFromButton = commandButtonOpen.value;
  commandButtonOpen.value = false;
  commandDismissed.value = true;
  if (command.source === "desktop") {
    if (!openedFromButton) draft.value = "";
    appStore.openSettings(command.settingsSection);
    return;
  }
  if (openedFromButton) {
    const argumentsText = commandArguments(draft.value);
    draft.value = argumentsText ? `/${command.name} ${argumentsText}` : `/${command.name} `;
  } else {
    draft.value = `/${command.name} `;
  }
  textarea.value?.focus();
}

function beginQueueEdit(promptId: string, text: string, images: PreparedImage[]) {
  editingPromptId.value = promptId;
  editingPromptText.value = text;
  editingPromptImages.value = images.map((image) => ({ ...image }));
  editingPromptError.value = "";
}

function cancelQueueEdit() {
  editingPromptId.value = "";
  editingPromptText.value = "";
  editingPromptImages.value = [];
  editingPromptError.value = "";
}

function removeQueueEditImage(imageId: string) {
  editingPromptImages.value = editingPromptImages.value.filter((image) => image.id !== imageId);
  editingPromptError.value = "";
}

async function addQueueEditImages(source: FileList | File[]) {
  const promptId = editingPromptId.value;
  if (!promptId) return;
  editingPromptProcessing.value = true;
  try {
    const prepared = await prepareImageFiles(source, editingPromptImages.value, (message) => { editingPromptError.value = message; });
    if (editingPromptId.value === promptId) editingPromptImages.value = [...editingPromptImages.value, ...prepared];
  } finally {
    editingPromptProcessing.value = false;
  }
}

function openQueueImagePicker(event: MouseEvent) {
  const editor = (event.currentTarget as HTMLElement).closest(".queue-editor");
  editor?.querySelector<HTMLInputElement>('input[type="file"]')?.click();
}

function onQueueImageInput(event: Event) {
  const input = event.target as HTMLInputElement;
  if (input.files?.length) void addQueueEditImages(input.files);
  input.value = "";
}

function onQueueEditPaste(event: ClipboardEvent) {
  const imageFiles = Array.from(event.clipboardData?.items ?? [])
    .filter((item) => item.kind === "file" && item.type.startsWith("image/"))
    .map((item) => item.getAsFile())
    .filter((file): file is File => Boolean(file));
  if (!imageFiles.length) return;
  event.preventDefault();
  void addQueueEditImages(imageFiles);
}

function onQueueEditDrop(event: DragEvent) {
  const files = event.dataTransfer?.files;
  if (!files?.length) return;
  event.preventDefault();
  void addQueueEditImages(files);
}

function saveQueueEdit(promptId: string) {
  if (editingPromptProcessing.value || (!editingPromptText.value.trim() && editingPromptImages.value.length === 0)) return;
  appStore.updatePendingPrompt(promptId, editingPromptText.value, editingPromptImages.value);
  cancelQueueEdit();
}

async function moveQueueEditToComposer(promptId: string) {
  if (editingPromptProcessing.value || (!editingPromptText.value.trim() && editingPromptImages.value.length === 0)) return;
  appStore.movePendingPromptToDraft(promptId, editingPromptText.value, editingPromptImages.value);
  cancelQueueEdit();
  await nextTick();
  textarea.value?.focus();
}

function modelLabel(model?: PiModel): string {
  return model?.name || model?.id || "Auto";
}

function formatTokens(value: number | null | undefined): string {
  if (typeof value !== "number" || !Number.isFinite(value)) return "—";
  const absolute = Math.abs(value);
  if (absolute >= 1_000_000) return `${(value / 1_000_000).toFixed(absolute >= 10_000_000 ? 0 : 1).replace(/\.0$/, "")}M`;
  if (absolute >= 1_000) return `${(value / 1_000).toFixed(absolute >= 100_000 ? 0 : 1).replace(/\.0$/, "")}K`;
  return Math.round(value).toLocaleString();
}

function exactTokens(value: number | null | undefined): string {
  return typeof value === "number" && Number.isFinite(value) ? Math.round(value).toLocaleString() : "—";
}

async function toggleModelMenu() {
  if (modelMenuOpen.value) {
    modelMenuOpen.value = false;
    return;
  }
  modelMenuOpen.value = true;
  modelCatalogRefreshing.value = true;
  try {
    await appStore.refreshConfiguredModels();
  } finally {
    modelCatalogRefreshing.value = false;
  }
}

async function chooseModel(model: PiModel) {
  modelChanging.value = true;
  try {
    await appStore.chooseModel(model);
  } finally {
    modelChanging.value = false;
  }
}

function toggleAccessMenu() {
  accessMenuOpen.value = !accessMenuOpen.value;
}

function chooseAccess(trust: "approve" | "deny") {
  if (appStore.activeThread?.trust === trust) {
    accessMenuOpen.value = false;
    return;
  }
  void appStore.setActiveWorkspaceTrust(trust).then((changed) => {
    if (changed) accessMenuOpen.value = false;
  });
}

function closeMenus(event: PointerEvent) {
  const target = event.target as Node;
  if (!accessMenu.value?.contains(target)) accessMenuOpen.value = false;
  if (!modelMenu.value?.contains(target)) modelMenuOpen.value = false;
  if (!commandMenu.value?.contains(target) && !commandButton.value?.contains(target)) commandButtonOpen.value = false;
}

onMounted(() => document.addEventListener("pointerdown", closeMenus));
onBeforeUnmount(() => document.removeEventListener("pointerdown", closeMenus));
</script>

<template>
  <div class="composer-wrap">
    <div v-for="widget in widgetsAbove" :key="widget.key" class="extension-widget" :data-placement="widget.placement"><pre>{{ widget.lines.join("\n") }}</pre></div>
    <div v-if="appStore.activeRetry" class="retry-banner" role="status">
      <LoaderCircle :size="14" class="is-spinning" />
      <span>Retry {{ appStore.activeRetry.attempt }} of {{ appStore.activeRetry.maxAttempts }}</span>
      <small v-if="appStore.activeRetry.errorMessage">{{ appStore.activeRetry.errorMessage }}</small>
      <button type="button" title="Stop retry" @click="void appStore.abortActiveRetry()"><X :size="14" /></button>
    </div>
    <div class="composer-input-stack" :class="{ 'has-todo': Boolean(piDeskTodo), 'has-queue': queuedMessages.length > 0 }">
      <PiDeskTodoPanel v-if="piDeskTodo" :key="piDeskTodoKey" :todo="piDeskTodo" />
      <div v-if="queuedMessages.length" class="queue-panel composer-stack-panel" aria-live="polite">
      <div class="queue-list">
        <div v-for="item in queuedMessages" :key="item.id" class="queue-row">
          <CornerDownRight :size="15" />
          <div
            v-if="editingPromptId === item.id"
            class="queue-editor"
            @dragover.prevent
            @drop="onQueueEditDrop"
          >
            <div v-if="editingPromptImages.length" class="queue-editor-images">
              <div v-for="image in editingPromptImages" :key="image.id" class="queue-editor-image">
                <img :src="image.previewUrl" :alt="image.name" />
                <button type="button" :title="tr('composer.removeQueuedImage')" @click="removeQueueEditImage(image.id)"><X :size="11" /></button>
              </div>
            </div>
            <div class="queue-editor-controls">
              <input v-model="editingPromptText" :aria-label="tr('composer.editQueued')" @paste="onQueueEditPaste" @keydown.enter.prevent="saveQueueEdit(item.id)" @keydown.esc="cancelQueueEdit" />
              <input class="visually-hidden" type="file" accept="image/png,image/jpeg,image/gif,image/webp" multiple tabindex="-1" @change="onQueueImageInput" />
              <button type="button" :title="tr('composer.addQueuedImages')" :disabled="editingPromptProcessing || editingPromptImages.length >= MAX_ATTACHED_IMAGES" @click="openQueueImagePicker"><ImagePlus :size="15" /></button>
              <button type="button" :title="tr('composer.sendQueuedToEditor')" :disabled="editingPromptProcessing || (!editingPromptText.trim() && editingPromptImages.length === 0)" @click="void moveQueueEditToComposer(item.id)"><ArrowDownToLine :size="15" /></button>
              <button type="button" :title="tr('composer.saveEdit')" :disabled="editingPromptProcessing || (!editingPromptText.trim() && editingPromptImages.length === 0)" @click="saveQueueEdit(item.id)"><Check :size="15" /></button>
              <button type="button" :title="tr('composer.cancelEdit')" :disabled="editingPromptProcessing" @click="cancelQueueEdit"><X :size="15" /></button>
            </div>
            <span v-if="editingPromptError" class="queue-editor-error" role="alert">{{ editingPromptError }}</span>
          </div>
          <template v-else>
            <img v-if="item.images[0]" class="queue-thumbnail" :src="item.images[0].previewUrl" :alt="item.images[0].name" />
            <span class="queue-text" :title="item.text || tr('composer.image')">{{ item.text || tr("composer.image") }}</span>
            <span class="queue-actions">
              <button class="queue-steer" type="button" :title="tr('composer.steerNow')" :disabled="!agentRunning" @click="void appStore.steerPendingPrompt(item.id)">
                <Forward :size="14" /><span>{{ tr("composer.adjustDirection") }}</span>
              </button>
              <button type="button" :title="tr('composer.editQueued')" @click="beginQueueEdit(item.id, item.text, item.images)"><Pencil :size="14" /></button>
              <button type="button" :title="tr('composer.deleteQueued')" @click="appStore.removePendingPrompt(item.id)"><Trash2 :size="14" /></button>
            </span>
          </template>
        </div>
      </div>
    </div>
    <div
      class="composer"
      :class="{ 'has-draft': draft.trim().length > 0 || appStore.activeAttachments.length > 0, 'drag-active': dragActive }"
      @dragenter.prevent="dragActive = true"
      @dragover.prevent="dragActive = true"
      @dragleave.self="dragActive = false"
      @drop="onDrop"
    >
      <div v-if="appStore.activeAttachments.length" class="attachment-strip">
        <div v-for="image in appStore.activeAttachments" :key="image.id" class="attachment-preview">
          <button class="attachment-preview-open" type="button" :title="tr('composer.viewImage')" @click="previewImage = image">
            <img :src="image.previewUrl" :alt="image.name" />
          </button>
          <button class="attachment-preview-remove" type="button" title="Remove image" @click.stop="appStore.removeActiveAttachment(image.id)"><X :size="12" /></button>
        </div>
      </div>
      <div v-if="attachmentError" class="attachment-error" role="alert">{{ attachmentError }}</div>
      <textarea
        ref="textarea"
        v-model="draft"
        rows="1"
        :placeholder="tr('composer.placeholder')"
        :aria-label="tr('composer.promptLabel')"
        @keydown="onKeydown"
        @paste="onPaste"
      />
      <div v-if="commandMenuOpen && matchingCommands.length" ref="commandMenu" class="completion-menu" role="listbox" :aria-label="tr('composer.commands')">
        <button
          v-for="(command, index) in matchingCommands"
          :key="`${command.source}:${command.name}`"
          type="button"
          role="option"
          :aria-selected="index === commandIndex"
          :title="commandTitle(command)"
          @mouseenter="commandIndex = index"
          @click="chooseCommand(command)"
        >
          <strong>/{{ command.name }}</strong>
          <span><small>{{ commandSourceLabel(command.source) }}</small>{{ command.description || command.source }}</span>
        </button>
      </div>
      <div v-if="mentionMenuOpen" class="completion-menu file-completion-menu" role="listbox" :aria-label="tr('composer.files')">
        <button
          v-for="(file, index) in matchingFiles"
          :key="file.path"
          type="button"
          role="option"
          :aria-selected="index === mentionIndex"
          @mouseenter="mentionIndex = index"
          @click="chooseFileMention(file.path)"
        >
          <File :size="14" />
          <span>{{ file.path }}</span>
        </button>
      </div>
      <div class="composer-toolbar">
        <div class="composer-tools">
          <button
            ref="commandButton"
            class="tool-button composer-command-button"
            type="button"
            :title="tr('composer.commands')"
            :aria-label="tr('composer.commands')"
            :aria-expanded="commandButtonOpen"
            :disabled="!appStore.activeThread"
            @click="toggleCommandMenu"
          >
            <Slash :size="15" />
          </button>
          <span v-if="piStarting" class="composer-starting" role="status" :title="tr('composer.modelsStarting')">
            <LoaderCircle :size="13" class="is-spinning" />
            <span>{{ tr("composer.modelsStarting") }}</span>
          </span>
          <div ref="modelMenu" class="menu-anchor">
            <button class="model-button" type="button" :title="tr('composer.modelAndReasoning')" :aria-expanded="modelMenuOpen" :aria-busy="modelChanging || modelCatalogRefreshing" @click="void toggleModelMenu()">
              <SlidersHorizontal :size="14" />
              <span>{{ modelButtonLabel }}</span>
              <ChevronDown :size="13" />
            </button>
            <div v-if="modelMenuOpen" class="model-menu" role="menu" @pointerdown.stop>
              <div class="menu-section-label">
                {{ tr("composer.model") }}
                <LoaderCircle v-if="modelCatalogRefreshing" :size="11" class="is-spinning" />
              </div>
              <div class="model-menu-options">
                <button
                  v-for="model in appStore.activeModels"
                  :key="`${model.provider}/${model.id}`"
                  type="button"
                  role="menuitemradio"
                  :aria-checked="currentModel?.provider === model.provider && currentModel?.id === model.id"
                  :disabled="modelChanging || modelCatalogRefreshing"
                  @click="void chooseModel(model)"
                >
                  <span>{{ modelLabel(model) }}</span><small>{{ model.provider }}</small>
                </button>
                <p v-if="appStore.activeModels.length === 0">{{ tr("composer.modelsUnavailable") }}</p>
              </div>
              <template v-if="modelChanging || appStore.activeThinkingLevels.length">
                <div class="menu-section-label">
                  {{ tr("composer.reasoning") }}
                  <LoaderCircle v-if="modelChanging" :size="11" class="is-spinning" />
                </div>
                <div class="thinking-level-grid" :class="{ 'is-loading': modelChanging }" :aria-busy="modelChanging">
                  <div v-if="modelChanging" class="thinking-level-loading" role="status">
                    <LoaderCircle :size="14" class="is-spinning" />
                  </div>
                  <button
                    v-for="level in modelChanging ? [] : appStore.activeThinkingLevels"
                    :key="level"
                    type="button"
                    role="menuitemradio"
                    :aria-checked="appStore.activeSessionState?.thinkingLevel === level"
                    @click="void appStore.chooseThinkingLevel(level); modelMenuOpen = false"
                  >
                    <BrainCircuit :size="14" /><span>{{ level }}</span>
                  </button>
                </div>
              </template>
            </div>
          </div>
          <div ref="accessMenu" class="menu-anchor access-menu-anchor">
            <button
              class="access-button"
              :class="{ 'is-full': appStore.activeThread?.trust === 'approve' }"
              type="button"
              :aria-expanded="accessMenuOpen"
              :title="tr('composer.accessTitle')"
              @click="toggleAccessMenu"
            >
              <ShieldCheck v-if="appStore.activeThread?.trust === 'approve'" :size="15" />
              <ShieldAlert v-else :size="15" />
              <span>{{ appStore.activeThread?.trust === "approve" ? tr("composer.fullAccess") : tr("composer.restrictedAccess") }}</span>
              <ChevronDown :size="12" />
            </button>
            <div v-if="accessMenuOpen" class="access-menu" role="menu" :aria-label="tr('composer.accessMode')">
              <div class="access-menu-heading">
                <strong>{{ tr("composer.accessMode") }}</strong>
                <small>{{ tr("composer.accessScope") }}</small>
              </div>
              <button
                type="button"
                role="menuitemradio"
                :aria-checked="appStore.activeThread?.trust === 'approve'"
                :disabled="appStore.activeWorkspaceTrustUpdating || accessBusy"
                @click="chooseAccess('approve')"
              >
                <ShieldCheck :size="17" />
                <span><strong>{{ tr("composer.fullAccess") }}</strong><small>{{ tr("composer.fullAccessHelp") }}</small></span>
                <Check v-if="appStore.activeThread?.trust === 'approve'" :size="16" />
              </button>
              <button
                type="button"
                role="menuitemradio"
                :aria-checked="appStore.activeThread?.trust === 'deny'"
                :disabled="appStore.activeWorkspaceTrustUpdating || accessBusy"
                @click="chooseAccess('deny')"
              >
                <ShieldAlert :size="17" />
                <span><strong>{{ tr("composer.restrictedAccess") }}</strong><small>{{ tr("composer.restrictedAccessHelp") }}</small></span>
                <Check v-if="appStore.activeThread?.trust === 'deny'" :size="16" />
              </button>
              <p v-if="accessBusy">{{ tr("composer.accessBusy") }}</p>
              <p v-else-if="appStore.workspaceTrustError" class="access-menu-error">{{ appStore.workspaceTrustError }}</p>
            </div>
          </div>
        </div>
        <div class="composer-actions">
          <button
            v-if="running"
            class="stop-button"
            type="button"
            :title="bashRunning ? tr('composer.stopCommand') : tr('composer.stopGenerating')"
            @click="bashRunning ? appStore.abortActiveBash() : appStore.abortActiveThread()"
          >
            <Square :size="13" fill="currentColor" />
          </button>
          <button
            class="send-button"
            type="button"
            :title="bashDraft ? agentRunning ? 'Wait for Pi before running a command' : 'Run command with Pi' : agentRunning ? tr('composer.queueMessage') : tr('composer.send')"
            :disabled="(!draft.trim() && appStore.activeAttachments.length === 0) || appStore.activeThread?.status === 'starting' || bashRunning || (bashDraft && agentRunning) || processingImages"
            @click="submit()"
          >
            <ArrowUp :size="17" />
          </button>
        </div>
      </div>
    </div>
    </div>
    <div class="composer-token-metrics" :aria-label="tr('composer.tokenUsage')">
          <div
            class="composer-token-metric is-context"
            :title="`${tr('composer.contextTokens')}: ${contextEstimated ? '~' : ''}${exactTokens(contextTokens)} / ${exactTokens(contextWindow)} ${tr('composer.tokens')}`"
          >
            <Gauge :size="14" aria-hidden="true" />
            <span><small>{{ tr("composer.contextTokens") }}</small><strong>{{ contextEstimated ? "~" : "" }}{{ formatTokens(contextTokens) }} <em>/ {{ formatTokens(contextWindow) }}</em></strong></span>
            <i class="context-token-meter" aria-hidden="true"><b :style="{ width: `${contextPercent}%` }" /></i>
          </div>
          <div class="composer-token-metric is-input" :title="`${tr('composer.inputTokens')}: ${exactTokens(inputTokens)} ${tr('composer.tokens')}`">
            <ArrowUpFromLine :size="14" aria-hidden="true" />
            <span><small>{{ tr("composer.inputTokens") }}</small><strong>{{ formatTokens(inputTokens) }}</strong></span>
          </div>
          <div class="composer-token-metric is-output" :title="`${tr('composer.outputTokens')}: ${exactTokens(outputTokens)} ${tr('composer.tokens')}`">
            <ArrowDownToLine :size="14" aria-hidden="true" />
            <span><small>{{ tr("composer.outputTokens") }}</small><strong>{{ formatTokens(outputTokens) }}</strong></span>
          </div>
          <div
            class="composer-token-metric is-cache"
            :title="`${tr('composer.cacheTokens')}: ${exactTokens(cacheTokens)} ${tr('composer.tokens')} · ${tr('composer.cacheReadTokens')}: ${exactTokens(tokenUsage?.cacheRead)} · ${tr('composer.cacheWriteTokens')}: ${exactTokens(tokenUsage?.cacheWrite)}`"
          >
            <Database :size="14" aria-hidden="true" />
            <span><small>{{ tr("composer.cacheTokens") }}</small><strong>{{ formatTokens(cacheTokens) }}</strong></span>
          </div>
        </div>
    <div v-if="appStore.activeExtensionStatuses.length" class="extension-status-list" aria-live="polite">
      <span v-for="status in appStore.activeExtensionStatuses" :key="status.key" :title="status.key">{{ status.text }}</span>
    </div>
    <div v-for="widget in widgetsBelow" :key="widget.key" class="extension-widget" :data-placement="widget.placement"><pre>{{ widget.lines.join("\n") }}</pre></div>
    <ImagePreviewDialog v-if="previewImage" :image="previewImage" @close="previewImage = undefined" />
  </div>
</template>
