<script setup lang="ts">
import { LoaderCircle, Server, X } from "lucide-vue-next";
import { useModalFocus } from "../composables/useModalFocus";
import { useAppStore } from "../stores/app";
import { tr } from "../i18n";
import { nextTick, ref, watch } from "vue";

const appStore = useAppStore();
const dialog = ref<HTMLElement | null>(null);
const progressPanel = ref<HTMLElement | null>(null);

function scrollProgress() {
  void nextTick(() => {
    progressPanel.value?.scrollTo({ top: progressPanel.value.scrollHeight, behavior: "smooth" });
  });
}

watch(() => appStore.remoteReconnectProgress, scrollProgress, { deep: true, flush: "post" });

function close() {
  appStore.cancelRemoteReconnect();
}

useModalFocus(dialog, close, { canClose: () => !appStore.remoteReconnectBusy });
</script>

<template>
  <div class="dialog-backdrop" @mousedown.self="close">
    <section ref="dialog" class="dialog-window remote-reconnect-dialog" role="dialog" aria-modal="true" aria-labelledby="remote-reconnect-title" tabindex="-1">
      <header>
        <h2 id="remote-reconnect-title">{{ tr("remoteReconnect.title") }}</h2>
        <button class="icon-button" type="button" :title="tr('common.close')" :disabled="appStore.remoteReconnectBusy" @click="close"><X :size="17" /></button>
      </header>
      <div class="dialog-body">
        <div class="remote-reconnect-target">
          <Server :size="20" />
          <div>
            <strong>{{ appStore.remoteReconnectThread?.workspace }}</strong>
            <code>{{ appStore.workspaces.find((workspace) => workspace.id === appStore.remoteReconnectThread?.workspaceId)?.remoteRoot }}</code>
          </div>
        </div>
        <p>{{ tr("remoteReconnect.description") }}</p>
        <div v-if="appStore.remoteReconnectProgress.length" ref="progressPanel" class="remote-setup-progress" role="status" aria-live="polite">
          <div v-for="step in appStore.remoteReconnectProgress" :key="step.id" class="remote-progress-step" :class="step.status" :aria-current="step.status === 'active' ? 'step' : undefined">
            <span class="remote-setup-marker" aria-hidden="true">{{ step.status === "complete" ? "✓" : step.status === "error" ? "!" : step.status === "active" ? "›" : "·" }}</span>
            <span>{{ tr(step.label) }}</span>
          </div>
        </div>
        <p v-if="appStore.remoteReconnectError" class="form-error" role="alert">{{ appStore.remoteReconnectError }}</p>
      </div>
      <footer>
        <button class="text-button" type="button" :disabled="appStore.remoteReconnectBusy" @click="close">{{ tr("common.cancel") }}</button>
        <button class="text-button primary" type="button" :disabled="appStore.remoteReconnectBusy" @click="void appStore.confirmRemoteReconnect()">
          <LoaderCircle v-if="appStore.remoteReconnectBusy" :size="14" class="is-spinning" />
          {{ appStore.remoteReconnectBusy ? tr("remoteReconnect.connecting") : tr("remoteReconnect.connect") }}
        </button>
      </footer>
    </section>
  </div>
</template>
