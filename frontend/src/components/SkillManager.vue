<script setup lang="ts">
import { ui } from "../ui/classes";
import { CheckCircle2, FilePlus2, RefreshCw, Save, ScrollText, Trash2, XCircle } from "lucide-vue-next";
import { computed, onMounted, reactive, ref } from "vue";
import { SkillScope, type ManagedSkillSummary } from "../../bindings/pi-desk/internal/domain";
import { tr } from "../i18n";
import { managedSkillService, type ManagedSkillSnapshot } from "../services/skills";
import { useAppStore } from "../stores/app";

const appStore = useAppStore();
const snapshot = ref<ManagedSkillSnapshot>();
const loading = ref(true);
const saving = ref(false);
const loadError = ref("");
const formError = ref("");
const notice = ref("");
const selectedKey = ref("");
const deleteArmed = ref(false);
const savedContent = ref("");
const createDialog = ref(false);
const creating = ref(false);
const createError = ref("");
const createForm = reactive({
  scope: SkillScope.SkillScopeGlobal,
  name: "",
  description: "",
});
const editor = reactive({
  scope: SkillScope.SkillScopeGlobal,
  relativePath: "",
  name: "",
  content: "",
});

const skills = computed(() => snapshot.value?.skills ?? []);
const workspacePath = computed(() => appStore.activeThread?.workspacePath || appStore.workspaces.find((workspace) => workspace.id === appStore.activeThread?.workspaceId)?.path || "");
const globalSkills = computed(() => skills.value.filter((skill) => skill.scope === SkillScope.SkillScopeGlobal));
const projectSkills = computed(() => skills.value.filter((skill) => skill.scope === SkillScope.SkillScopeProject));
const runtimeSkills = computed(() => appStore.activeCommands.filter((command) => command.source === "skill"));
const dirty = computed(() => editor.content !== savedContent.value);
const isExisting = computed(() => Boolean(editor.relativePath));

function keyOf(skill: Pick<ManagedSkillSummary, "scope" | "rootDirectory" | "relativePath">) {
  return `${skill.scope}:${skill.rootDirectory}:${skill.relativePath}`;
}

function resetEditor() {
  selectedKey.value = "";
  deleteArmed.value = false;
  formError.value = "";
  notice.value = "";
  editor.scope = SkillScope.SkillScopeGlobal;
  editor.relativePath = "";
  editor.name = "";
  editor.content = "";
  savedContent.value = "";
}

async function loadSkills(preferredKey = selectedKey.value) {
  loading.value = true;
  loadError.value = "";
  try {
    snapshot.value = await managedSkillService.list({ workspacePath: workspacePath.value });
    const selected = (snapshot.value.skills ?? []).find((skill) => keyOf(skill) === preferredKey);
    if (selected) await selectSkill(selected);
    else if (skills.value[0]) await selectSkill(skills.value[0]);
    else resetEditor();
  } catch (cause) {
    loadError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    loading.value = false;
  }
}

async function selectSkill(skill: ManagedSkillSummary) {
  if (saving.value) return;
  formError.value = "";
  notice.value = "";
  deleteArmed.value = false;
  selectedKey.value = keyOf(skill);
  try {
    const loaded = await managedSkillService.get({
      scope: skill.scope,
      workspacePath: workspacePath.value,
      rootDirectory: skill.rootDirectory,
      relativePath: skill.relativePath,
    });
    editor.scope = loaded.scope;
    editor.relativePath = loaded.relativePath;
    editor.name = loaded.name;
    editor.content = loaded.content;
    savedContent.value = loaded.content;
  } catch (cause) {
    formError.value = cause instanceof Error ? cause.message : String(cause);
  }
}

function openCreateDialog() {
  createDialog.value = true;
  createError.value = "";
  createForm.scope = SkillScope.SkillScopeGlobal;
  createForm.name = "";
  createForm.description = "";
}

async function createSkill() {
  if (creating.value) return;
  createError.value = "";
  if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(createForm.name.trim())) {
    createError.value = tr("settings.skillNameInvalid");
    return;
  }
  if (!createForm.description.trim()) {
    createError.value = tr("settings.skillDescriptionRequired");
    return;
  }
  creating.value = true;
  try {
    const created = await managedSkillService.create({
      scope: createForm.scope,
      workspacePath: workspacePath.value,
      name: createForm.name.trim(),
      description: createForm.description.trim(),
    });
    createDialog.value = false;
    await loadSkills(keyOf(created));
    notice.value = tr("settings.skillCreated");
  } catch (cause) {
    createError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    creating.value = false;
  }
}

async function saveSkill() {
  if (!isExisting.value || saving.value || !editor.content.trim()) return;
  saving.value = true;
  formError.value = "";
  notice.value = "";
  try {
    const selected = (snapshot.value?.skills ?? []).find((skill) => keyOf(skill) === selectedKey.value);
    const saved = await managedSkillService.update({
      scope: editor.scope,
      workspacePath: workspacePath.value,
      rootDirectory: selected?.rootDirectory,
      relativePath: editor.relativePath,
      content: editor.content,
    });
    editor.content = saved.content;
    savedContent.value = saved.content;
    selectedKey.value = keyOf(saved);
    await loadSkills(selectedKey.value);
    notice.value = tr("settings.skillSaved");
  } catch (cause) {
    formError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    saving.value = false;
  }
}

async function deleteSkill() {
  if (!isExisting.value || saving.value) return;
  if (!deleteArmed.value) {
    deleteArmed.value = true;
    window.setTimeout(() => { deleteArmed.value = false; }, 5000);
    return;
  }
  saving.value = true;
  formError.value = "";
  try {
    const selected = (snapshot.value?.skills ?? []).find((skill) => keyOf(skill) === selectedKey.value);
    await managedSkillService.delete({
      scope: editor.scope,
      workspacePath: workspacePath.value,
      rootDirectory: selected?.rootDirectory,
      relativePath: editor.relativePath,
    });
    resetEditor();
    await loadSkills();
    notice.value = tr("settings.skillDeleted");
  } catch (cause) {
    formError.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    saving.value = false;
    deleteArmed.value = false;
  }
}

onMounted(() => { void loadSkills(); });
</script>

<template>
  <div class="settings-content model-config-content skill-config-content" :class="ui.settingsContent">
    <div v-if="loading" class="settings-empty" :class="ui.empty"><RefreshCw :size="18" class="is-spinning" /><span>{{ tr("settings.loadingSkills") }}</span></div>
    <div v-else-if="loadError" class="settings-empty is-error" :class="ui.empty"><XCircle :size="18" /><span>{{ loadError }}</span></div>
    <div v-else class="prompt-manager-layout" :class="ui.managerLayout">
      <aside class="prompt-config-list" :class="ui.managerList" :aria-label="tr('settings.skills')">
        <button class="model-config-add-row" :class="ui.listItem" type="button" :disabled="saving" @click="openCreateDialog()"><FilePlus2 :size="14" /><span>{{ tr("settings.addSkill") }}</span></button>
        <section class="prompt-config-scope" :class="ui.group">
          <header><strong>{{ tr("settings.globalSkills") }}</strong><span>{{ globalSkills.length }}</span></header>
          <button v-for="skill in globalSkills" :key="keyOf(skill)" type="button" :class="{ 'is-active': selectedKey === keyOf(skill) }" :disabled="saving" @click="void selectSkill(skill)"><span><strong>{{ skill.name }}</strong><small>{{ skill.description || tr("settings.skillNoDescription") }}</small></span><CheckCircle2 v-if="selectedKey === keyOf(skill)" :size="13" /></button>
        </section>
        <section class="prompt-config-scope" :class="ui.group">
          <header><strong>{{ tr("settings.projectSkills") }}</strong><span>{{ projectSkills.length }}</span></header>
          <p v-if="!snapshot?.projectEnabled" class="settings-inline-note">{{ snapshot?.projectNotice || tr("settings.projectSkillsUnavailable") }}</p>
          <button v-for="skill in projectSkills" :key="keyOf(skill)" type="button" :class="{ 'is-active': selectedKey === keyOf(skill) }" :disabled="saving" @click="void selectSkill(skill)"><span><strong>{{ skill.name }}</strong><small>{{ skill.description || tr("settings.skillNoDescription") }}</small></span><CheckCircle2 v-if="selectedKey === keyOf(skill)" :size="13" /></button>
        </section>
        <section class="prompt-config-scope runtime-resource-scope" :class="ui.group">
          <header><strong>{{ tr("settings.runtimeLoadedReadonly") }}</strong><span>{{ runtimeSkills.length }}</span></header>
          <div v-for="skill in runtimeSkills" :key="`${skill.name}-${skill.path}`" class="runtime-resource-item" :class="ui.listItem" :title="skill.path">
            <span><strong>/{{ skill.name }}</strong><small>{{ skill.description || skill.path || tr("settings.skillNoDescription") }}</small></span>
          </div>
          <p v-if="!appStore.activeThread?.started" class="prompt-config-notice">{{ tr("settings.startForRuntimeResources") }}</p>
        </section>
      </aside>
      <form v-if="isExisting" class="prompt-editor flex flex-col" :class="ui.managerEditor" @submit.prevent="void saveSkill()">
        <div class="model-editor-title"><div><strong>{{ editor.name }}</strong><small>{{ (snapshot?.skills ?? []).find((skill) => keyOf(skill) === selectedKey)?.rootDirectory }}</small></div><span v-if="dirty" class="model-dirty">{{ tr("settings.unsaved") }}</span></div>
        <p class="skill-directory"><strong>{{ tr("settings.skillDirectory") }}</strong><code>{{ editor.relativePath }}</code></p>
        <label class="model-field min-h-0 flex-1 grid-rows-[auto_minmax(0,1fr)]" :class="ui.field"><span>{{ tr("settings.skillContent") }}</span><textarea class="h-full min-h-0 resize-none" :class="ui.textarea" v-model="editor.content" spellcheck="false" /></label>
        <p v-if="formError" class="form-error">{{ formError }}</p>
        <p v-if="notice" class="setting-status">{{ notice }}</p>
        <footer class="sticky bottom-0 z-10 mt-auto flex items-center justify-end gap-2 border-t border-[var(--border)] bg-[var(--bg-raised)] py-3"><button class="text-button danger" :class="ui.buttonDanger" type="button" :disabled="saving" @click="void deleteSkill()"><Trash2 :size="14" />{{ deleteArmed ? tr("settings.confirmDeleteSkill") : tr("settings.deleteSkill") }}</button><button class="text-button primary" :class="ui.buttonPrimary" type="submit" :disabled="saving || !dirty"><Save :size="14" />{{ saving ? tr("settings.savingSkill") : tr("settings.saveSkill") }}</button></footer>
      </form>
      <div v-else class="settings-empty" :class="ui.empty"><ScrollText :size="18" /><span>{{ tr("settings.noManagedSkills") }}</span></div>
    </div>
    <div v-if="createDialog" class="resource-create-backdrop" :class="ui.dialogBackdrop" @mousedown.self="createDialog = false">
      <form class="resource-create-dialog" :class="[ui.panel, 'grid w-[min(560px,calc(100%_-_32px))] gap-4 p-5 max-[520px]:h-dvh max-[520px]:w-full max-[520px]:overflow-y-auto max-[520px]:rounded-none']" @submit.prevent="void createSkill()">
        <header :class="ui.dialogHeader"><strong>{{ tr("settings.newSkill") }}</strong><button class="icon-button" :class="ui.iconButton" type="button" :title="tr('common.close')" @click="createDialog = false"><XCircle :size="15" /></button></header>
        <label class="model-field" :class="ui.field"><span>{{ tr("settings.skillScope") }}</span><select :class="ui.select" v-model="createForm.scope"><option :value="SkillScope.SkillScopeGlobal">{{ tr("settings.globalSkills") }}</option><option :value="SkillScope.SkillScopeProject" :disabled="!snapshot?.projectEnabled">{{ tr("settings.projectSkills") }}</option></select></label>
        <label class="model-field" :class="ui.field"><span>{{ tr("settings.skillName") }}</span><input :class="ui.input" v-model="createForm.name" spellcheck="false" placeholder="code-review" /><small>{{ tr("settings.skillNameHelp") }}</small></label>
        <label class="model-field" :class="ui.field"><span>{{ tr("settings.skillDescription") }}</span><textarea :class="ui.textarea" v-model="createForm.description" /><small>{{ tr("settings.skillDescriptionHelp") }}</small></label>
        <p v-if="createError" class="form-error">{{ createError }}</p>
        <footer :class="ui.dialogFooter"><button class="text-button" :class="ui.button" type="button" :disabled="creating" @click="createDialog = false">{{ tr("common.cancel") }}</button><button class="text-button primary" :class="ui.buttonPrimary" type="submit" :disabled="creating"><FilePlus2 :size="14" />{{ creating ? tr("settings.creatingSkill") : tr("settings.createSkill") }}</button></footer>
      </form>
    </div>
  </div>
</template>
