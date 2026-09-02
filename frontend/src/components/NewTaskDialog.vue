<script setup lang="ts">
import { ui } from "../ui/classes";
import { FolderGit2, FolderOpen, LoaderCircle, Server, ShieldCheck, ShieldOff, X } from "lucide-vue-next";
import { computed, nextTick, ref, watch } from "vue";
import { useModalFocus } from "../composables/useModalFocus";
import { remoteWorkspaceService, type RemoteAliasSummary, type RemoteRootCandidate } from "../services/remoteWorkspaces";
import { useAppStore } from "../stores/app";
import { tr } from "../i18n";

const appStore = useAppStore();
const mode = ref<"local" | "ssh">("local");
const workspacePath = ref("");
const trust = ref<"approve" | "deny">("approve");
const validationError = ref("");
const creating = ref(false);
const browsing = ref(false);
const dialog = ref<HTMLElement | null>(null);
const remoteAliases = ref<RemoteAliasSummary[]>([]);
const remoteName = ref("");
const remoteAlias = ref("");
const remoteRoot = ref("");
const remoteNameCustomized = ref(false);
const remoteAliasOpen = ref(false);
const remoteAliasActiveIndex = ref(-1);
const candidate = ref<RemoteRootCandidate>();
const connectedTargetID = ref("");
type RemoteProgressStatus = "pending" | "active" | "complete" | "error";
type RemoteProgressStep = { id: string; label: string; status: RemoteProgressStatus };

const remoteProgressPanel = ref<HTMLElement | null>(null);
const remoteProgress = ref<RemoteProgressStep[]>([]);
const visibleRemoteProgress = computed(() => remoteProgress.value.filter((step) => step.status !== "pending"));
const remoteProgressDefinitions: RemoteProgressStep[] = [
  { id: "resolve", label: "newTask.sshStepResolve", status: "pending" },
  { id: "connect", label: "newTask.sshStepConnect", status: "pending" },
  { id: "target", label: "newTask.sshStepTarget", status: "pending" },
  { id: "platform", label: "newTask.sshStepPlatform", status: "pending" },
  { id: "artifact", label: "newTask.sshStepArtifact", status: "pending" },
  { id: "install", label: "newTask.sshStepInstall", status: "pending" },
  { id: "handshake", label: "newTask.sshStepHandshake", status: "pending" },
  { id: "root", label: "newTask.sshStepRoot", status: "pending" },
  { id: "candidate", label: "newTask.sshStepCandidate", status: "pending" },
  { id: "models", label: "newTask.sshStepModels", status: "pending" },
  { id: "approve", label: "newTask.sshStepApprove", status: "pending" },
  { id: "workspace", label: "newTask.sshStepWorkspace", status: "pending" },
  { id: "thread", label: "newTask.sshStepThread", status: "pending" },
];

const remoteBusy = computed(() => creating.value || browsing.value);

function defaultRemoteName() {
  const alias = remoteAlias.value.trim();
  const segments = remoteRoot.value.trim().replace(/[\\/]+$/, "").split(/[\\/]+/).filter(Boolean);
  const project = segments.at(-1) || "root";
  return alias ? `${alias}/${project}` : project;
}

function syncRemoteName() {
  if (!remoteNameCustomized.value) remoteName.value = defaultRemoteName();
}

function openRemoteAliasPicker() {
  if (remoteBusy.value) return;
  remoteAliasOpen.value = true;
  const selectedIndex = remoteAliases.value.findIndex((alias) => alias.name === remoteAlias.value);
  remoteAliasActiveIndex.value = selectedIndex >= 0 ? selectedIndex : (remoteAliases.value.length ? 0 : -1);
}

function closeRemoteAliasPicker() {
  remoteAliasOpen.value = false;
  remoteAliasActiveIndex.value = -1;
}

function toggleRemoteAliasPicker() {
  if (remoteAliasOpen.value) closeRemoteAliasPicker();
  else openRemoteAliasPicker();
}

function chooseRemoteAlias(alias: RemoteAliasSummary) {
  remoteAlias.value = alias.name;
  closeRemoteAliasPicker();
}

function handleRemoteAliasKeydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    closeRemoteAliasPicker();
    return;
  }
  if (event.key === "Enter" || event.key === " ") {
    event.preventDefault();
    if (!remoteAliasOpen.value) {
      openRemoteAliasPicker();
      return;
    }
    const alias = remoteAliases.value[remoteAliasActiveIndex.value];
    if (alias) chooseRemoteAlias(alias);
    return;
  }
  if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
  event.preventDefault();
  if (!remoteAliasOpen.value) {
    openRemoteAliasPicker();
    return;
  }
  const direction = event.key === "ArrowDown" ? 1 : -1;
  const count = remoteAliases.value.length;
  if (!count) return;
  remoteAliasActiveIndex.value = (remoteAliasActiveIndex.value + direction + count) % count;
}

function scrollRemoteProgress() {
  void nextTick(() => {
    const panel = remoteProgressPanel.value;
    if (panel) panel.scrollTo({ top: panel.scrollHeight, behavior: "smooth" });
  });
}

function resetRemoteProgress() {
  remoteProgress.value = remoteProgressDefinitions.map((step) => ({ ...step }));
  scrollRemoteProgress();
}

function startRemoteStep(id: string) {
  const index = remoteProgress.value.findIndex((step) => step.id === id);
  if (index < 0) return;
  remoteProgress.value = remoteProgress.value.map((step, stepIndex) => ({
    ...step,
    status: stepIndex < index ? "complete" : stepIndex === index ? "active" : "pending",
  }));
  scrollRemoteProgress();
}

function completeRemoteStep(id: string) {
  remoteProgress.value = remoteProgress.value.map((step) => step.id === id ? { ...step, status: "complete" } : step);
  scrollRemoteProgress();
}

function failRemoteStep() {
  remoteProgress.value = remoteProgress.value.map((step) => step.status === "active" ? { ...step, status: "error" } : step);
  scrollRemoteProgress();
}

function workspaceTrust(path: string): "approve" | "deny" | undefined {
  const normalized = path.trim().replace(/[\\/]+$/, "").replaceAll("\\", "/").toLocaleLowerCase();
  return appStore.workspaces.find((workspace) => workspace.path.replace(/[\\/]+$/, "").replaceAll("\\", "/").toLocaleLowerCase() === normalized)?.trust;
}

async function releaseRemote() {
  const pendingTargetID = candidate.value?.targetId || "";
  candidate.value = undefined;
  const targetID = connectedTargetID.value || pendingTargetID;
  connectedTargetID.value = "";
  if (targetID) {
    try { await appStore.disconnectRemoteTarget(targetID); } catch { /* host clears the one-shot candidate before bounded shutdown */ }
  }
}

async function close() {
  if (remoteBusy.value) return;
  await releaseRemote();
  appStore.newTaskOpen = false;
}

useModalFocus(dialog, () => { void close(); }, { canClose: () => !remoteBusy.value });

watch(() => appStore.newTaskOpen, (open) => {
  if (!open) return;
  mode.value = "local";
  workspacePath.value = appStore.bootstrap?.workingDirectory || appStore.workspaces.find((workspace) => workspace.kind !== "ssh")?.path || "";
  trust.value = workspaceTrust(workspacePath.value) ?? "approve";
  validationError.value = "";
  remoteAliases.value = [];
  remoteName.value = "";
  remoteAlias.value = "";
  remoteRoot.value = "";
  remoteNameCustomized.value = false;
  closeRemoteAliasPicker();
  candidate.value = undefined;
  connectedTargetID.value = "";
  remoteProgress.value = [];
}, { immediate: true });

watch(workspacePath, (value) => { trust.value = workspaceTrust(value) ?? "approve"; });
watch(remoteAlias, syncRemoteName);
watch(remoteRoot, syncRemoteName);

async function selectMode(next: "local" | "ssh") {
  if (mode.value === next || remoteBusy.value) return;
  if (mode.value === "ssh") await releaseRemote();
  closeRemoteAliasPicker();
  mode.value = next;
  validationError.value = "";
  if (next !== "ssh") return;
  try {
    remoteAliases.value = await remoteWorkspaceService.discover();
  } catch (error) {
    validationError.value = error instanceof Error ? error.message : String(error);
  }
}

async function browse() {
  validationError.value = "";
  browsing.value = true;
  try {
    const selected = await appStore.pickWorkspace(workspacePath.value);
    if (selected) workspacePath.value = selected;
  } catch (error) {
    validationError.value = error instanceof Error ? error.message : String(error);
  } finally {
    browsing.value = false;
  }
}

async function createLocal() {
  await appStore.createThread(workspacePath.value, trust.value);
  if (appStore.activeThreadId) appStore.startThreadInBackground(appStore.activeThreadId);
}

async function rejectRemoteRoot() {
  if (!candidate.value || remoteBusy.value) return;
  validationError.value = "";
  creating.value = true;
  try {
    const targetID = candidate.value.targetId;
    const stopFailure = await appStore.stopRemoteTargetThreads(targetID);
    const denied = await remoteWorkspaceService.decideRoot(candidate.value.token, "deny");
    appStore.recordRemoteWorkspace(denied);
    candidate.value = undefined;
    connectedTargetID.value = "";
    if (stopFailure) throw new Error(stopFailure);
    appStore.newTaskOpen = false;
  } catch (error) {
    validationError.value = error instanceof Error ? error.message : String(error);
    await releaseRemote();
  } finally {
    creating.value = false;
  }
}

async function createRemote() {
  if (candidate.value) {
    startRemoteStep("models");
    await appStore.refreshConfiguredModels(true);
    completeRemoteStep("models");
    startRemoteStep("approve");
    const token = candidate.value.token;
    const targetID = candidate.value.targetId;
    const epoch = appStore.remoteTargetEpoch(targetID);
    try {
      const approved = await remoteWorkspaceService.decideRoot(token, "approve");
      completeRemoteStep("approve");
      startRemoteStep("workspace");
      appStore.assertRemoteTargetEpoch(targetID, epoch);
      await appStore.createRemoteThread(approved);
      completeRemoteStep("workspace");
      startRemoteStep("thread");
      candidate.value = undefined;
      connectedTargetID.value = "";
      if (appStore.activeThreadId) appStore.startThreadInBackground(appStore.activeThreadId);
      completeRemoteStep("thread");
    } catch (error) {
      await releaseRemote();
      throw error;
    }
    return;
  }
  resetRemoteProgress();
  startRemoteStep("resolve");
  const targetID = await remoteWorkspaceService.connectNew(remoteName.value, remoteAlias.value);
  completeRemoteStep("resolve");
  completeRemoteStep("connect");
  completeRemoteStep("target");
  const targetEpoch = appStore.remoteTargetEpoch(targetID);
  connectedTargetID.value = targetID;
  startRemoteStep("platform");
  const prepared = await remoteWorkspaceService.prepareRoot(targetID, remoteName.value, remoteRoot.value);
  completeRemoteStep("platform");
  completeRemoteStep("artifact");
  completeRemoteStep("install");
  completeRemoteStep("handshake");
  completeRemoteStep("root");
  completeRemoteStep("candidate");
  appStore.assertRemoteTargetEpoch(targetID, targetEpoch);
  candidate.value = prepared;
}

async function create() {
  validationError.value = "";
  creating.value = true;
  try {
    if (mode.value === "ssh") await createRemote();
    else await createLocal();
  } catch (error) {
    if (mode.value === "ssh") failRemoteStep();
    validationError.value = error instanceof Error ? error.message : String(error);
    if (mode.value === "ssh" && connectedTargetID.value && !candidate.value) await releaseRemote();
  } finally {
    creating.value = false;
  }
}
</script>

<template>
  <div class="dialog-backdrop" :class="ui.dialogBackdrop" @mousedown.self="void close()">
    <section ref="dialog" class="dialog-window new-task-dialog" :class="ui.dialog" role="dialog" aria-modal="true" aria-labelledby="new-task-title" tabindex="-1" @click="closeRemoteAliasPicker">
      <header :class="ui.dialogHeader">
        <h2 id="new-task-title">{{ tr("newTask.title") }}</h2>
        <button class="icon-button" :class="ui.iconButton" type="button" :title="tr('common.close')" :disabled="remoteBusy" @click="void close()"><X :size="17" /></button>
      </header>
      <div class="dialog-body" :class="ui.dialogBody">
        <div class="segmented-control" role="group" :aria-label="tr('newTask.location')">
          <button type="button" :class="[ui.tab, { active: mode === 'local' }]" :aria-pressed="mode === 'local'" @click="void selectMode('local')"><FolderGit2 :size="15" />{{ tr("newTask.local") }}</button>
          <button type="button" :class="[ui.tab, { active: mode === 'ssh' }]" :aria-pressed="mode === 'ssh'" @click="void selectMode('ssh')"><Server :size="15" />{{ tr("newTask.ssh") }}</button>
        </div>

        <template v-if="mode === 'local'">
          <label for="workspace-path">{{ tr("newTask.workspace") }}</label>
          <div class="path-input focus-within:border-[var(--text-secondary)] focus-within:outline-2 focus-within:outline-offset-1 focus-within:outline-[var(--text)]">
            <FolderGit2 :size="16" />
            <input class="h-full w-full min-w-0 border-0 bg-transparent p-0 text-sm text-[var(--text)] outline-none placeholder:text-[var(--text-muted)]" id="workspace-path" v-model="workspacePath" autofocus spellcheck="false" placeholder="D:\projects\my-project" @keydown.enter="create" />
            <button class="icon-button inline-grid size-7 place-items-center rounded-md border-0 bg-transparent text-[var(--text-muted)] hover:bg-[var(--bg-hover)] hover:text-[var(--text)] active:bg-[var(--bg-active)] focus-visible:outline-2 focus-visible:outline-offset-0 focus-visible:outline-[var(--text)] disabled:cursor-not-allowed disabled:opacity-50" type="button" :title="tr('newTask.browse')" :disabled="browsing" @click="browse">
              <LoaderCircle v-if="browsing" :size="15" class="is-spinning" /><FolderOpen v-else :size="15" />
            </button>
          </div>
          <fieldset class="trust-options">
            <legend>{{ tr("newTask.resources") }}</legend>
            <label :class="[ui.listItem, { 'is-selected': trust === 'deny' }]"><input v-model="trust" type="radio" value="deny" /><ShieldOff :size="18" /><span><strong>{{ tr("newTask.restricted") }}</strong><small>{{ tr("newTask.restrictedHelp") }}</small></span></label>
            <label :class="[ui.listItem, { 'is-selected': trust === 'approve' }]"><input v-model="trust" type="radio" value="approve" /><ShieldCheck :size="18" /><span><strong>{{ tr("newTask.trusted") }}</strong><small>{{ tr("newTask.trustedHelp") }}</small></span></label>
          </fieldset>
        </template>

        <template v-else>
          <h3 class="remote-project-title">{{ tr("newTask.sshProject") }}</h3>
          <label for="remote-alias">{{ tr("newTask.sshAlias") }}</label>
          <div class="remote-alias-picker" :class="{ 'is-open': remoteAliasOpen }" @click.stop>
            <button
              id="remote-alias"
              class="remote-alias-input"
              type="button"
              role="combobox"
              :aria-expanded="remoteAliasOpen"
              aria-controls="remote-alias-options"
              :aria-activedescendant="remoteAliasActiveIndex >= 0 ? `remote-alias-option-${remoteAliasActiveIndex}` : undefined"
              @click="toggleRemoteAliasPicker"
              @keydown="handleRemoteAliasKeydown"
            >
              <span :class="{ 'is-placeholder': !remoteAlias }">{{ remoteAlias || "my-server" }}</span>
            </button>
            <div v-if="remoteAliasOpen" id="remote-alias-options" class="remote-alias-options" :class="ui.menu" role="listbox">
              <button
                v-for="(alias, index) in remoteAliases"
                :id="`remote-alias-option-${index}`"
                :key="alias.name"
                class="remote-alias-option"
                :class="[ui.menuItem, { 'is-active': index === remoteAliasActiveIndex, 'is-risky': alias.risky }]"
                type="button"
                role="option"
                :aria-selected="alias.name === remoteAlias"
                @mouseenter="remoteAliasActiveIndex = index"
                @click="chooseRemoteAlias(alias)"
              >
                <span>{{ alias.name }}</span>
              </button>
              <span v-if="!remoteAliases.length" class="remote-alias-empty">{{ tr("newTask.sshNoAliases") }}</span>
            </div>
          </div>
          <label for="remote-root">{{ tr("newTask.sshRoot") }}</label>
          <input :class="ui.input" id="remote-root" v-model="remoteRoot" spellcheck="false" placeholder="/home/me/project" />
          <label for="remote-name">{{ tr("newTask.sshName") }}</label>
          <input :class="ui.input" id="remote-name" v-model="remoteName" maxlength="100" @input="remoteNameCustomized = true" />
          <div v-if="candidate" class="remote-root-review">
            <strong>{{ tr("newTask.sshReview") }}</strong>
            <span>{{ candidate.hostAlias }}</span>
            <code>{{ candidate.hostKeyAlgorithm }} {{ candidate.hostKeySha256 }}</code>
            <code>{{ candidate.canonicalRoot }}</code>
            <small>{{ tr("newTask.sshIdentity", { device: candidate.device, inode: candidate.inode }) }}</small>
          </div>
          <div v-if="mode === 'ssh' && visibleRemoteProgress.length" ref="remoteProgressPanel" class="remote-setup-progress" role="status" aria-live="polite">
            <div v-for="step in visibleRemoteProgress" :key="step.id" class="remote-progress-step" :class="step.status" :aria-current="step.status === 'active' ? 'step' : undefined">
              <span class="remote-setup-marker" aria-hidden="true">{{ step.status === "complete" ? "✓" : step.status === "error" ? "!" : step.status === "active" ? "›" : "·" }}</span>
              <span>{{ tr(step.label) }}</span>
            </div>
          </div>
        </template>

        <p v-if="validationError" class="form-error">{{ validationError }}</p>
      </div>
      <footer :class="ui.dialogFooter">
        <button class="text-button" :class="ui.button" type="button" :disabled="remoteBusy" @click="void close()">{{ tr("common.cancel") }}</button>
        <button v-if="candidate" class="text-button danger-button" :class="ui.buttonDanger" type="button" :disabled="remoteBusy" @click="void rejectRemoteRoot()">{{ tr("newTask.sshReject") }}</button>
        <button class="text-button primary" :class="ui.buttonPrimary" type="button" :disabled="creating || (mode === 'local' ? !workspacePath.trim() : (!candidate && (!remoteName.trim() || !remoteAlias.trim() || !remoteRoot.trim())))" @click="create">
          <LoaderCircle v-if="creating" :size="14" class="is-spinning" />
          {{ creating ? tr("newTask.creating") : candidate ? tr("newTask.sshApprove") : mode === 'ssh' ? tr("newTask.sshCheckConnect") : tr("newTask.create") }}
        </button>
      </footer>
    </section>
  </div>
</template>
