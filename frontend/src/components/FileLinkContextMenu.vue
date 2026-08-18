<script setup lang="ts">
import { Clipboard } from "@wailsio/runtime";
import { AppWindow, Check, Copy, ExternalLink, FolderOpen, LoaderCircle, Save } from "lucide-vue-next";
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { tr } from "../i18n";
import { repositoryService } from "../services/repository";
import type { WorkspaceFileLink } from "../utils/fileLinks";

const props = defineProps<{
  file: WorkspaceFileLink;
  workspacePath: string;
  x: number;
  y: number;
}>();
const emit = defineEmits<{ close: [] }>();
const menu = ref<HTMLElement>();
const busy = ref("");
const error = ref("");
const copied = ref(false);
const menuStyle = computed(() => {
  const width = 238;
  const height = 232;
  const viewportWidth = typeof window === "undefined" ? props.x + width : window.innerWidth;
  const viewportHeight = typeof window === "undefined" ? props.y + height : window.innerHeight;
  return {
    left: `${Math.max(8, Math.min(props.x, viewportWidth - width - 8))}px`,
    top: `${Math.max(8, Math.min(props.y, viewportHeight - height - 8))}px`,
  };
});

function errorMessage(value: unknown): string {
  return value instanceof Error ? value.message : String(value ?? tr("files.actionFailed"));
}

async function run(action: "open" | "openWith" | "save" | "copy" | "reveal") {
  if (busy.value) return;
  busy.value = action;
  error.value = "";
  try {
    if (action === "open") await repositoryService.openFile(props.workspacePath, props.file.relativePath);
    else if (action === "openWith") await repositoryService.openFileWith(props.workspacePath, props.file.relativePath);
    else if (action === "save") await repositoryService.saveFileAs(props.workspacePath, props.file.relativePath, props.file.absolutePath);
    else if (action === "reveal") await repositoryService.revealFile(props.workspacePath, props.file.relativePath);
    else {
      try {
        await Clipboard.SetText(props.file.absolutePath);
      } catch {
        await navigator.clipboard.writeText(props.file.absolutePath);
      }
      copied.value = true;
    }
    if (action === "copy") window.setTimeout(() => emit("close"), 500);
    else emit("close");
  } catch (actionError) {
    error.value = errorMessage(actionError);
  } finally {
    busy.value = "";
  }
}

function onPointerDown(event: PointerEvent) {
  if (!menu.value?.contains(event.target as Node)) emit("close");
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    emit("close");
  }
}

function onWindowBlur() {
  emit("close");
}

onMounted(async () => {
  document.addEventListener("pointerdown", onPointerDown, true);
  document.addEventListener("keydown", onKeydown, true);
  window.addEventListener("blur", onWindowBlur);
  await nextTick();
  menu.value?.querySelector<HTMLElement>("button")?.focus();
});

onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", onPointerDown, true);
  document.removeEventListener("keydown", onKeydown, true);
  window.removeEventListener("blur", onWindowBlur);
});
</script>

<template>
  <Teleport to="body">
    <div ref="menu" class="file-link-context-menu" role="menu" :style="menuStyle" :aria-label="tr('files.actions')">
      <div class="file-link-context-path" :title="file.absolutePath">{{ file.absolutePath }}</div>
      <button type="button" role="menuitem" :disabled="Boolean(busy)" @click="void run('open')">
        <LoaderCircle v-if="busy === 'open'" :size="15" class="is-spinning" /><ExternalLink v-else :size="15" />
        <span>{{ tr("files.open") }}</span>
      </button>
      <button type="button" role="menuitem" :disabled="Boolean(busy)" @click="void run('openWith')">
        <LoaderCircle v-if="busy === 'openWith'" :size="15" class="is-spinning" /><AppWindow v-else :size="15" />
        <span>{{ tr("files.openWith") }}</span>
      </button>
      <div class="file-link-context-separator" />
      <button type="button" role="menuitem" :disabled="Boolean(busy)" @click="void run('save')">
        <LoaderCircle v-if="busy === 'save'" :size="15" class="is-spinning" /><Save v-else :size="15" />
        <span>{{ tr("files.saveAs") }}</span>
      </button>
      <button type="button" role="menuitem" :disabled="Boolean(busy)" @click="void run('copy')">
        <LoaderCircle v-if="busy === 'copy'" :size="15" class="is-spinning" /><Check v-else-if="copied" :size="15" /><Copy v-else :size="15" />
        <span>{{ tr("files.copyPath") }}</span>
      </button>
      <button type="button" role="menuitem" :disabled="Boolean(busy)" @click="void run('reveal')">
        <LoaderCircle v-if="busy === 'reveal'" :size="15" class="is-spinning" /><FolderOpen v-else :size="15" />
        <span>{{ tr("files.reveal") }}</span>
      </button>
      <p v-if="error" class="file-link-context-error" role="alert">{{ error }}</p>
    </div>
  </Teleport>
</template>
