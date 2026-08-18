<script setup lang="ts">
import { CheckCircle2, LoaderCircle, Trash2, X } from "lucide-vue-next";
import { ref } from "vue";
import { useModalFocus } from "../composables/useModalFocus";
import { useAppStore } from "../stores/app";
import { tr } from "../i18n";

const appStore = useAppStore();
const dialog = ref<HTMLElement | null>(null);
useModalFocus(dialog, () => appStore.closeDeleteDialog(), { canClose: () => !appStore.activeSessionOperation });
</script>

<template>
  <div class="dialog-backdrop" @mousedown.self="!appStore.activeSessionOperation && appStore.closeDeleteDialog()">
    <section ref="dialog" class="dialog-window delete-session-dialog" role="alertdialog" aria-modal="true" aria-labelledby="delete-session-title" tabindex="-1">
      <header>
        <h2 id="delete-session-title">{{ appStore.deletedRecoveryPath ? tr("deletion.removed") : appStore.deleteHasSession ? tr("deletion.deleteSessionQuestion") : tr("deletion.deleteTaskQuestion") }}</h2>
        <button class="icon-button" type="button" :title="tr('deletion.close')" :disabled="Boolean(appStore.activeSessionOperation)" @click="appStore.closeDeleteDialog()"><X :size="17" /></button>
      </header>
      <div v-if="appStore.deletedRecoveryPath" class="dialog-body delete-result">
        <CheckCircle2 :size="22" />
        <div>
          <strong>{{ appStore.deleteSessionTitle }}</strong>
          <p>{{ tr("deletion.recovery") }}</p>
          <code :title="appStore.deletedRecoveryPath">{{ appStore.deletedRecoveryPath }}</code>
          <p v-if="appStore.deleteSessionError" class="form-error">{{ appStore.deleteSessionError }}</p>
        </div>
      </div>
      <div v-else class="dialog-body delete-warning">
        <Trash2 :size="22" />
        <div>
          <strong>{{ appStore.deleteSessionTitle }}</strong>
          <p v-if="appStore.deleteHasSession">{{ tr("deletion.sessionWarning") }}</p>
          <p v-else>{{ tr("deletion.unsavedWarning") }}</p>
          <p v-if="appStore.deleteSessionError" class="form-error">{{ appStore.deleteSessionError }}</p>
        </div>
      </div>
      <footer>
        <button v-if="appStore.deletedRecoveryPath" class="text-button primary" type="button" @click="appStore.closeDeleteDialog()">{{ tr("deletion.done") }}</button>
        <template v-else>
          <button class="text-button" type="button" :disabled="Boolean(appStore.activeSessionOperation)" @click="appStore.closeDeleteDialog()">{{ tr("common.cancel") }}</button>
          <button class="text-button danger-button" type="button" :disabled="Boolean(appStore.activeSessionOperation)" @click="void appStore.confirmDeleteSession()">
            <LoaderCircle v-if="appStore.activeSessionOperation" :size="14" class="is-spinning" />
            {{ appStore.activeSessionOperation ? tr("deletion.deleting") : appStore.deleteHasSession ? tr("deletion.deleteSession") : tr("deletion.deleteTask") }}
          </button>
        </template>
      </footer>
    </section>
  </div>
</template>
