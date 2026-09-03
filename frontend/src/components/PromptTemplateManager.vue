<script setup lang="ts">
import { ui } from "../ui/classes";
import { CheckCircle2, CircleHelp, FilePlus2, RefreshCw, Save, Trash2, XCircle } from "lucide-vue-next";
import { computed, onMounted, reactive, ref } from "vue";
import { PromptTemplateScope, type PromptTemplateSummary } from "../../bindings/pi-desk/internal/domain";
import { tr } from "../i18n";
import { promptTemplateService, type PromptTemplateSnapshot } from "../services/prompts";
import { useAppStore } from "../stores/app";

const appStore = useAppStore();
const snapshot = ref<PromptTemplateSnapshot>();
const loading = ref(true);
const saving = ref(false);
const loadError = ref("");
const formError = ref("");
const notice = ref("");
const selectedKey = ref("");
const deleteArmed = ref(false);
const savedFingerprint = ref("");
const editor = reactive({
  scope: PromptTemplateScope.PromptTemplateScopeGlobal,
  originalName: "",
  name: "",
  content: "",
});

const workspacePath = computed(() => appStore.activeThread?.workspacePath ?? "");
const globalTemplates = computed(() => (snapshot.value?.templates ?? []).filter((template) => template.scope === PromptTemplateScope.PromptTemplateScopeGlobal));
const projectTemplates = computed(() => (snapshot.value?.templates ?? []).filter((template) => template.scope === PromptTemplateScope.PromptTemplateScopeProject));
const runtimePrompts = computed(() => appStore.activeCommands.filter((command) => command.source === "prompt"));
const isExisting = computed(() => Boolean(editor.originalName));
const dirty = computed(() => fingerprint() !== savedFingerprint.value);
const projectAvailable = computed(() => Boolean(snapshot.value?.projectEnabled));

function fingerprint() {
  return JSON.stringify({ scope: editor.scope, originalName: editor.originalName, name: editor.name, content: editor.content });
}

function defaultContent() {
  return "---\ndescription: Describe when this prompt template should be used\nargument-hint: \"[optional arguments]\"\n---\n\nWrite the prompt template here. Use $1, $2, or $@ for positional arguments.\n";
}

function keyOf(template: Pick<PromptTemplateSummary, "scope" | "name">) {
  return `${template.scope}:${template.name}`;
}

function resetEditor(scope = PromptTemplateScope.PromptTemplateScopeGlobal) {
  selectedKey.value = "new";
  deleteArmed.value = false;
  formError.value = "";
  notice.value = "";
  editor.scope = scope;
  editor.originalName = "";
  editor.name = "";
  editor.content = defaultContent();
  savedFingerprint.value = fingerprint();
}

async function loadTemplates(preferredKey = selectedKey.value) {
  loading.value = true;
  loadError.value = "";
  try {
    snapshot.value = await promptTemplateService.list({ workspacePath: workspacePath.value || undefined });
    const selected = (snapshot.value.templates ?? []).find((template) => keyOf(template) === preferredKey);
    if (selected) await selectTemplate(selected);
    else if (globalTemplates.value[0]) await selectTemplate(globalTemplates.value[0]);
    else resetEditor();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    loading.value = false;
  }
}

async function selectTemplate(template: PromptTemplateSummary) {
  if (saving.value) return;
  formError.value = "";
  notice.value = "";
  deleteArmed.value = false;
  selectedKey.value = keyOf(template);
  try {
    const loaded = await promptTemplateService.get({
      scope: template.scope,
      workspacePath: template.scope === PromptTemplateScope.PromptTemplateScopeProject ? workspacePath.value : undefined,
      name: template.name,
    });
    editor.scope = loaded.scope;
    editor.originalName = loaded.name;
    editor.name = loaded.name;
    editor.content = loaded.content;
    savedFingerprint.value = fingerprint();
  } catch (cause) {
    formError.value = cause instanceof Error ? cause.message : String(cause);
  }
}

function selectScope(event: Event) {
  const scope = (event.target as HTMLSelectElement).value as PromptTemplateScope;
  if (scope === PromptTemplateScope.PromptTemplateScopeProject && !projectAvailable.value) return;
  editor.scope = scope;
}

function validForm() {
  formError.value = "";
  if (!editor.name.trim()) formError.value = tr("settings.promptNameRequired");
  else if (!/^[\p{L}\p{N}_-]+$/u.test(editor.name.trim())) formError.value = tr("settings.promptNameInvalid");
  else if (!editor.content.trim()) formError.value = tr("settings.promptContentRequired");
  else if (editor.scope === PromptTemplateScope.PromptTemplateScopeProject && !projectAvailable.value) formError.value = snapshot.value?.projectNotice || tr("settings.projectPromptsUnavailable");
  return !formError.value;
}

async function refreshRunningCommands() {
  const thread = appStore.activeThread;
  if (!thread?.started) return;
  try {
    await appStore.refreshCommands(thread.id);
  } catch {
    // The template itself is saved. A failed observational refresh must not turn a successful save into an error.
  }
}

async function saveTemplate() {
  if (!validForm() || saving.value) return;
  saving.value = true;
  notice.value = "";
  try {
    const saved = await promptTemplateService.upsert({
      scope: editor.scope,
      workspacePath: editor.scope === PromptTemplateScope.PromptTemplateScopeProject ? workspacePath.value : undefined,
      originalName: editor.originalName || undefined,
      name: editor.name.trim(),
      content: editor.content,
    });
    selectedKey.value = keyOf(saved);
    editor.scope = saved.scope;
    editor.originalName = saved.name;
    editor.name = saved.name;
    editor.content = saved.content;
    savedFingerprint.value = fingerprint();
    notice.value = tr("settings.promptSaved");
    await loadTemplates(selectedKey.value);
    await refreshRunningCommands();
  } catch (cause) {
    formError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    saving.value = false;
  }
}

async function deleteTemplate() {
  if (!isExisting.value || saving.value) return;
  if (!deleteArmed.value) {
    deleteArmed.value = true;
    window.setTimeout(() => { deleteArmed.value = false; }, 5000);
    return;
  }
  saving.value = true;
  formError.value = "";
  try {
    await promptTemplateService.delete({
      scope: editor.scope,
      workspacePath: editor.scope === PromptTemplateScope.PromptTemplateScopeProject ? workspacePath.value : undefined,
      name: editor.originalName,
    });
    notice.value = tr("settings.promptDeleted");
    resetEditor(editor.scope);
    await loadTemplates("new");
    await refreshRunningCommands();
  } catch (cause) {
    formError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    saving.value = false;
    deleteArmed.value = false;
  }
}

onMounted(() => { void loadTemplates(); });
</script>

<template>
  <div class="settings-content model-config-content prompt-config-content" :class="ui.settingsContent">
    <div v-if="loading" class="settings-empty" :class="ui.empty"><RefreshCw :size="18" class="is-spinning" /><span>{{ tr("settings.loadingPrompts") }}</span></div>
    <div v-else-if="loadError" class="settings-empty is-error" :class="ui.empty"><XCircle :size="18" /><span>{{ loadError }}</span></div>
    <div v-else class="prompt-manager-layout" :class="ui.managerLayout">
      <aside class="prompt-config-list" :class="ui.managerList" :aria-label="tr('settings.promptTemplates')">
        <button class="model-config-add-row" type="button" :class="[ui.listItem, { 'is-active': selectedKey === 'new' }]" :disabled="saving" @click="resetEditor()"><FilePlus2 :size="14" /><span>{{ tr("settings.addPrompt") }}</span></button>
        <section data-testid="global-prompt-group" class="prompt-config-scope" :class="ui.group">
          <header><strong>{{ tr("settings.globalPrompts") }}</strong><span>{{ globalTemplates.length }}</span></header>
          <button v-for="template in globalTemplates" :key="keyOf(template)" type="button" :class="{ 'is-active': selectedKey === keyOf(template) }" :disabled="saving" @click="void selectTemplate(template)">
            <span><strong>/{{ template.name }}</strong><small>{{ template.description || tr("settings.promptNoDescription") }}</small></span>
            <CheckCircle2 v-if="selectedKey === keyOf(template)" :size="13" />
          </button>
        </section>
        <section data-testid="project-prompt-group" class="prompt-config-scope" :class="[ui.group, { 'is-disabled': !projectAvailable }]">
          <header><strong>{{ tr("settings.projectPrompts") }}</strong><span>{{ projectTemplates.length }}</span></header>
          <button v-for="template in projectTemplates" :key="keyOf(template)" type="button" :class="{ 'is-active': selectedKey === keyOf(template) }" :disabled="saving || !projectAvailable" @click="void selectTemplate(template)">
            <span><strong>/{{ template.name }}</strong><small>{{ template.description || tr("settings.promptNoDescription") }}</small></span>
            <CheckCircle2 v-if="selectedKey === keyOf(template)" :size="13" />
          </button>
          <p v-if="!projectAvailable" class="prompt-config-notice">{{ snapshot?.projectNotice || tr("settings.projectPromptsUnavailable") }}</p>
        </section>
        <section class="prompt-config-scope runtime-resource-scope" :class="ui.group">
          <header><strong>{{ tr("settings.runtimeLoadedReadonly") }}</strong><span>{{ runtimePrompts.length }}</span></header>
          <div v-for="prompt in runtimePrompts" :key="`${prompt.name}-${prompt.path}`" class="runtime-resource-item" :class="ui.listItem" :title="prompt.path">
            <span><strong>/{{ prompt.name }}</strong><small>{{ prompt.description || prompt.path || tr("settings.promptNoDescription") }}</small></span>
          </div>
          <p v-if="!appStore.activeThread?.started" class="prompt-config-notice">{{ tr("settings.startForRuntimeResources") }}</p>
        </section>
      </aside>
      <form class="prompt-editor flex flex-col" :class="ui.managerEditor" @submit.prevent="void saveTemplate()">
        <div class="model-editor-title">
          <div><strong>{{ isExisting ? `/${editor.name}` : tr("settings.newPrompt") }}</strong><small>{{ tr("settings.promptConfigScope") }}</small></div>
          <span v-if="dirty" class="model-dirty">{{ tr("settings.unsaved") }}</span>
        </div>
        <div class="model-form-grid min-h-0 flex-1 grid-rows-[auto_minmax(0,1fr)] max-[760px]:grid-rows-[auto_auto_minmax(0,1fr)]" :class="ui.formGrid">
          <label class="model-field" :class="ui.field">
            <span>{{ tr("settings.promptScope") }}</span>
            <select :class="ui.select" :value="editor.scope" :disabled="saving || isExisting" @change="selectScope">
              <option :value="PromptTemplateScope.PromptTemplateScopeGlobal">{{ tr("settings.globalPrompts") }}</option>
              <option :value="PromptTemplateScope.PromptTemplateScopeProject" :disabled="!projectAvailable">{{ tr("settings.projectPrompts") }}</option>
            </select>
          </label>
          <label class="model-field" :class="ui.field">
            <span>{{ tr("settings.promptName") }}</span>
            <input :class="ui.input" v-model="editor.name" spellcheck="false" :placeholder="tr('settings.promptNameHelp')" />
          </label>
          <label class="model-field model-field-wide min-h-0 grid-rows-[auto_minmax(0,1fr)]" :class="ui.field">
            <span>{{ tr("settings.promptContent") }}</span>
            <textarea class="h-full min-h-0 resize-none" :class="ui.textarea" v-model="editor.content" spellcheck="false" />
          </label>
        </div>
        <p v-if="formError" class="form-error">{{ formError }}</p>
        <p v-if="notice" class="setting-status">{{ notice }}</p>
        <footer class="sticky bottom-0 z-10 mt-auto flex items-center justify-end gap-2 border-t border-[var(--border)] bg-[var(--bg-raised)] py-3">
          <button v-if="isExisting" class="text-button danger mr-auto" :class="ui.buttonDanger" type="button" :disabled="saving" @click="void deleteTemplate()"><Trash2 :size="14" />{{ deleteArmed ? tr("settings.confirmDeletePrompt") : tr("settings.deletePrompt") }}</button>
          <div class="group relative">
            <button class="icon-button" :class="ui.iconButton" type="button" :aria-label="tr('settings.promptUsageTitle')" aria-describedby="prompt-usage-tooltip"><CircleHelp :size="14" /></button>
            <aside id="prompt-usage-tooltip" class="pointer-events-none absolute bottom-[calc(100%+8px)] right-0 z-20 hidden w-[min(420px,calc(100vw-64px))] rounded-lg border border-[var(--border)] bg-[var(--bg-menu)] p-3 text-left text-xs leading-relaxed text-[var(--text-secondary)] shadow-xl group-hover:block group-focus-within:block" role="tooltip">
              <strong class="mb-2 block text-sm text-[var(--text)]">{{ tr("settings.promptUsageTitle") }}</strong>
              <ul class="m-0 grid list-disc gap-1.5 pl-4">
                <li>{{ tr("settings.promptUsageInvoke") }}</li>
                <li>{{ tr("settings.promptUsageMetadata", { required: "<required>", optional: "[optional]" }) }}</li>
                <li>{{ tr("settings.promptUsageArguments", { one: "$1", two: "$2", all: "$@", arguments: "$ARGUMENTS" }) }}</li>
                <li>{{ tr("settings.promptUsageDefaults", { defaultValue: "${1:-default}", sliceFrom: "${@:N}", sliceLength: "${@:N:L}" }) }}</li>
                <li>{{ tr("settings.promptUsageScope") }}</li>
              </ul>
            </aside>
          </div>
          <button class="text-button primary" :class="ui.buttonPrimary" type="submit" :disabled="saving || !dirty"><Save :size="14" />{{ saving ? tr("settings.savingPrompt") : tr("settings.savePrompt") }}</button>
        </footer>
      </form>
    </div>
  </div>
</template>
