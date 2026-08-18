<script setup lang="ts">
import { FolderGit2, FolderOpen, LoaderCircle, ShieldCheck, ShieldOff, X } from "lucide-vue-next";
import { ref, watch } from "vue";
import { useModalFocus } from "../composables/useModalFocus";
import { useAppStore } from "../stores/app";
import { tr } from "../i18n";

const appStore = useAppStore();
const workspacePath = ref("");
const trust = ref<"approve" | "deny">("approve");
const validationError = ref("");
const creating = ref(false);
const browsing = ref(false);
const dialog = ref<HTMLElement | null>(null);

function workspaceTrust(path: string): "approve" | "deny" | undefined {
  const normalized = path.trim().replace(/[\\/]+$/, "").replaceAll("\\", "/").toLocaleLowerCase();
  return appStore.workspaces.find((workspace) => workspace.path.replace(/[\\/]+$/, "").replaceAll("\\", "/").toLocaleLowerCase() === normalized)?.trust;
}

function close() {
  if (!creating.value && !browsing.value) appStore.newTaskOpen = false;
}

useModalFocus(dialog, close, { canClose: () => !creating.value && !browsing.value });

watch(() => appStore.newTaskOpen, (open) => {
  if (!open) return;
  workspacePath.value = appStore.bootstrap?.workingDirectory || appStore.workspaces[0]?.path || "";
  trust.value = workspaceTrust(workspacePath.value) ?? "approve";
  validationError.value = "";
}, { immediate: true });

watch(workspacePath, (value) => {
  trust.value = workspaceTrust(value) ?? "approve";
});

async function browse() {
  validationError.value = "";
  browsing.value = true;
  try {
    const selected = await appStore.pickWorkspace(workspacePath.value);
    if (selected) {
      workspacePath.value = selected;
      trust.value = workspaceTrust(selected) ?? "approve";
    }
  } catch (error) {
    validationError.value = error instanceof Error ? error.message : String(error);
  } finally {
    browsing.value = false;
  }
}

async function create() {
  validationError.value = "";
  creating.value = true;
  try {
    await appStore.createThread(workspacePath.value, trust.value);
    if (appStore.activeThreadId) appStore.startThreadInBackground(appStore.activeThreadId);
  } catch (error) {
    validationError.value = error instanceof Error ? error.message : String(error);
  } finally {
    creating.value = false;
  }
}
</script>

<template>
  <div class="dialog-backdrop" @mousedown.self="close">
    <section ref="dialog" class="dialog-window new-task-dialog" role="dialog" aria-modal="true" aria-labelledby="new-task-title" tabindex="-1">
      <header>
        <h2 id="new-task-title">{{ tr("newTask.title") }}</h2>
        <button class="icon-button" type="button" :title="tr('common.close')" :disabled="creating || browsing" @click="close"><X :size="17" /></button>
      </header>
      <div class="dialog-body">
        <label for="workspace-path">{{ tr("newTask.workspace") }}</label>
        <div class="path-input">
          <FolderGit2 :size="16" />
          <input id="workspace-path" v-model="workspacePath" autofocus spellcheck="false" placeholder="D:\projects\my-project" @keydown.enter="create" />
          <button class="icon-button" type="button" :title="tr('newTask.browse')" :disabled="browsing" @click="browse">
            <LoaderCircle v-if="browsing" :size="15" class="is-spinning" />
            <FolderOpen v-else :size="15" />
          </button>
        </div>

        <fieldset class="trust-options">
          <legend>{{ tr("newTask.resources") }}</legend>
          <label :class="{ 'is-selected': trust === 'deny' }">
            <input v-model="trust" type="radio" value="deny" />
            <ShieldOff :size="18" />
            <span><strong>{{ tr("newTask.restricted") }}</strong><small>{{ tr("newTask.restrictedHelp") }}</small></span>
          </label>
          <label :class="{ 'is-selected': trust === 'approve' }">
            <input v-model="trust" type="radio" value="approve" />
            <ShieldCheck :size="18" />
            <span><strong>{{ tr("newTask.trusted") }}</strong><small>{{ tr("newTask.trustedHelp") }}</small></span>
          </label>
        </fieldset>

        <p v-if="validationError" class="form-error">{{ validationError }}</p>
      </div>
      <footer>
        <button class="text-button" type="button" :disabled="creating || browsing" @click="close">{{ tr("common.cancel") }}</button>
        <button class="text-button primary" type="button" :disabled="!workspacePath.trim() || creating" @click="create">
          <LoaderCircle v-if="creating" :size="14" class="is-spinning" />
          {{ creating ? tr("newTask.creating") : tr("newTask.create") }}
        </button>
      </footer>
    </section>
  </div>
</template>
