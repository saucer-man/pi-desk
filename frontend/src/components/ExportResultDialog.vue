<script setup lang="ts">
import { ui } from "../ui/classes";
import { AlertCircle, CheckCircle2, X } from "lucide-vue-next";
import { computed, ref } from "vue";
import { useModalFocus } from "../composables/useModalFocus";
import { tr } from "../i18n";
import { useAppStore } from "../stores/app";

const appStore = useAppStore();
const dialog = ref<HTMLElement | null>(null);
const failed = computed(() => Boolean(appStore.exportResultError));
useModalFocus(dialog, () => appStore.closeExportDialog());
</script>

<template>
  <div class="dialog-backdrop" :class="ui.dialogBackdrop" @mousedown.self="appStore.closeExportDialog()">
    <section ref="dialog" class="dialog-window export-result-dialog" :class="ui.dialog" role="dialog" aria-modal="true" aria-labelledby="export-result-title" tabindex="-1">
      <header :class="ui.dialogHeader">
        <h2 id="export-result-title">{{ failed ? tr("exportResult.failedTitle") : tr("exportResult.successTitle") }}</h2>
        <button class="icon-button" :class="ui.iconButton" type="button" :title="tr('common.close')" @click="appStore.closeExportDialog()"><X :size="17" /></button>
      </header>
      <div class="dialog-body delete-result export-result" :class="[ui.dialogBody, { 'is-error': failed }]">
        <AlertCircle v-if="failed" :size="22" />
        <CheckCircle2 v-else :size="22" />
        <div>
          <strong>{{ failed ? tr("exportResult.failed") : tr("exportResult.saved") }}</strong>
          <code :title="appStore.exportResultError || appStore.exportResultPath">{{ appStore.exportResultError || appStore.exportResultPath }}</code>
        </div>
      </div>
      <footer :class="ui.dialogFooter">
        <button class="text-button primary" :class="ui.buttonPrimary" type="button" @click="appStore.closeExportDialog()">{{ tr("exportResult.done") }}</button>
      </footer>
    </section>
  </div>
</template>
