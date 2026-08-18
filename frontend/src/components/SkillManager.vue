<script setup lang="ts">
import { CheckCircle2, Copy, FilePlus2, RefreshCw, Save, ScrollText, Trash2, XCircle } from "lucide-vue-next";
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
const copied = ref(false);
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
    snapshot.value = await managedSkillService.list({});
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
      scope: SkillScope.SkillScopeGlobal,
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

async function copyDirectory() {
  const selected = (snapshot.value?.skills ?? []).find((skill) => keyOf(skill) === selectedKey.value);
  const directory = selected?.rootDirectory ?? snapshot.value?.globalDirectory;
  if (!directory) return;
  await navigator.clipboard.writeText(directory);
  copied.value = true;
  window.setTimeout(() => { copied.value = false; }, 1200);
}

onMounted(() => { void loadSkills(); });
</script>

<template>
  <div class="settings-content skill-config-content">
    <div class="settings-content-header prompt-config-header">
      <div>
        <h3>{{ tr("settings.skillManagement") }}</h3>
        <button v-if="snapshot?.globalDirectory" class="model-config-path" type="button" :title="(snapshot?.globalDirectories ?? []).join('\n')" @click="void copyDirectory()"><ScrollText :size="12" /><span>{{ (snapshot?.globalDirectories ?? [snapshot?.globalDirectory]).join(' + ') }}</span><Copy :size="12" /></button>
      </div>
      <div class="settings-actions">
        <button class="icon-button" type="button" :title="tr('common.refresh')" :disabled="loading || saving" @click="void loadSkills()"><RefreshCw :size="14" :class="{ 'is-spinning': loading }" /></button>
        <button class="text-button" type="button" :disabled="saving" @click="openCreateDialog()"><FilePlus2 :size="14" />{{ tr("settings.addSkill") }}</button>
      </div>
    </div>
    <p v-if="copied" class="setting-status">{{ tr("settings.copied") }}</p>
    <div v-if="loading" class="settings-empty"><RefreshCw :size="18" class="is-spinning" /><span>{{ tr("settings.loadingSkills") }}</span></div>
    <div v-else-if="loadError" class="settings-empty is-error"><XCircle :size="18" /><span>{{ loadError }}</span></div>
    <div v-else class="prompt-manager-layout">
      <aside class="prompt-config-list" :aria-label="tr('settings.skills')">
        <button class="model-config-add-row" type="button" :disabled="saving" @click="openCreateDialog()"><FilePlus2 :size="14" /><span>{{ tr("settings.addSkill") }}</span></button>
        <section class="prompt-config-scope">
          <header><strong>{{ tr("settings.managedSkills") }}</strong><span>{{ skills.length }}</span></header>
          <button v-for="skill in skills" :key="keyOf(skill)" type="button" :class="{ 'is-active': selectedKey === keyOf(skill) }" :disabled="saving" @click="void selectSkill(skill)"><span><strong>{{ skill.name }}</strong><small>{{ skill.description || tr("settings.skillNoDescription") }}</small></span><CheckCircle2 v-if="selectedKey === keyOf(skill)" :size="13" /></button>
        </section>
        <section class="prompt-config-scope runtime-resource-scope">
          <header><strong>{{ tr("settings.runtimeLoadedReadonly") }}</strong><span>{{ runtimeSkills.length }}</span></header>
          <div v-for="skill in runtimeSkills" :key="`${skill.name}-${skill.path}`" class="runtime-resource-item" :title="skill.path">
            <span><strong>/{{ skill.name }}</strong><small>{{ skill.description || skill.path || tr("settings.skillNoDescription") }}</small></span>
          </div>
          <p v-if="!appStore.activeThread?.started" class="prompt-config-notice">{{ tr("settings.startForRuntimeResources") }}</p>
        </section>
      </aside>
      <form v-if="isExisting" class="prompt-editor" @submit.prevent="void saveSkill()">
        <div class="model-editor-title"><div><strong>{{ editor.name }}</strong><small>{{ (snapshot?.skills ?? []).find((skill) => keyOf(skill) === selectedKey)?.rootDirectory }}</small></div><span v-if="dirty" class="model-dirty">{{ tr("settings.unsaved") }}</span></div>
        <p class="skill-directory"><strong>{{ tr("settings.skillDirectory") }}</strong><code>{{ editor.relativePath }}</code></p>
        <label class="model-field"><span>{{ tr("settings.skillContent") }}</span><textarea v-model="editor.content" spellcheck="false" /><small>{{ tr("settings.skillContentHelp") }}</small></label>
        <p v-if="formError" class="form-error">{{ formError }}</p>
        <p v-if="notice" class="setting-status">{{ notice }}</p>
        <footer class="model-editor-footer"><button class="text-button danger" type="button" :disabled="saving" @click="void deleteSkill()"><Trash2 :size="14" />{{ deleteArmed ? tr("settings.confirmDeleteSkill") : tr("settings.deleteSkill") }}</button><span /><button class="text-button primary" type="submit" :disabled="saving || !dirty"><Save :size="14" />{{ saving ? tr("settings.savingSkill") : tr("settings.saveSkill") }}</button></footer>
      </form>
      <div v-else class="settings-empty"><ScrollText :size="18" /><span>{{ tr("settings.noManagedSkills") }}</span></div>
    </div>
    <div v-if="createDialog" class="resource-create-backdrop" @mousedown.self="createDialog = false">
      <form class="resource-create-dialog" @submit.prevent="void createSkill()">
        <header><strong>{{ tr("settings.newSkill") }}</strong><button class="icon-button" type="button" :title="tr('common.close')" @click="createDialog = false"><XCircle :size="15" /></button></header>

        <label class="model-field"><span>{{ tr("settings.skillName") }}</span><input v-model="createForm.name" spellcheck="false" placeholder="code-review" /><small>{{ tr("settings.skillNameHelp") }}</small></label>
        <label class="model-field"><span>{{ tr("settings.skillDescription") }}</span><textarea v-model="createForm.description" /><small>{{ tr("settings.skillDescriptionHelp") }}</small></label>
        <p v-if="createError" class="form-error">{{ createError }}</p>
        <footer><button class="text-button" type="button" :disabled="creating" @click="createDialog = false">{{ tr("common.cancel") }}</button><button class="text-button primary" type="submit" :disabled="creating"><FilePlus2 :size="14" />{{ creating ? tr("settings.creatingSkill") : tr("settings.createSkill") }}</button></footer>
      </form>
    </div>
  </div>
</template>
