<script setup lang="ts">
import { X } from "lucide-vue-next";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useModalFocus } from "../composables/useModalFocus";
import { useAppStore } from "../stores/app";
import BatchQuestionForm from "./BatchQuestionForm.vue";
import { tr } from "../i18n";

const appStore = useAppStore();
const request = computed(() => appStore.extensionRequestByThread[appStore.activeThreadId]);
const value = ref("");
const dialog = ref<HTMLElement | null>(null);
const dialogTitle = computed(() => request.value?.method === "batch_ask"
  ? tr("extension.batchTitle", { count: request.value.batchQuestions?.length ?? 0 })
  : request.value?.title || tr("extension.title"));
let timeoutID: ReturnType<typeof setTimeout> | undefined;

useModalFocus(dialog, () => appStore.respondToExtension(undefined, true));

watch(request, (next) => {
  if (timeoutID) clearTimeout(timeoutID);
  value.value = next?.prefill ?? "";
  if (next?.timeout && Number.isFinite(next.timeout) && next.timeout > 0) {
    timeoutID = setTimeout(() => appStore.dismissExtensionRequest(next.id), next.timeout);
  }
}, { immediate: true });

onBeforeUnmount(() => {
  if (timeoutID) clearTimeout(timeoutID);
});
</script>

<template>
  <div v-if="request" class="dialog-backdrop">
    <section ref="dialog" class="dialog-window extension-dialog" :class="{ 'batch-extension-dialog': request.method === 'batch_ask' }" role="dialog" aria-modal="true" :aria-labelledby="`extension-title-${request.id}`" tabindex="-1">
      <header>
        <h2 :id="`extension-title-${request.id}`">{{ dialogTitle }}</h2>
        <button class="icon-button" type="button" :title="tr('extension.cancel')" @click="appStore.respondToExtension(undefined, true)"><X :size="17" /></button>
      </header>
      <div class="dialog-body" :class="{ 'batch-question-dialog-body': request.method === 'batch_ask' }">
        <BatchQuestionForm
          v-if="request.method === 'batch_ask' && request.batchQuestions"
          :key="request.id"
          :questions="request.batchQuestions"
          :review="Boolean(request.batchReview)"
          @submit="appStore.respondToExtension($event)"
        />
        <p v-else-if="request.message">{{ request.message }}</p>
        <div v-if="request.method === 'select'" class="select-options">
          <button v-for="option in request.options" :key="option" class="text-button" type="button" @click="appStore.respondToExtension(option)">{{ option }}</button>
        </div>
        <textarea v-else-if="request.method === 'editor'" v-model="value" rows="10" :placeholder="request.placeholder" autofocus />
        <input v-else-if="request.method === 'input'" v-model="value" :placeholder="request.placeholder" autofocus @keydown.enter="appStore.respondToExtension(value)" />
      </div>
      <footer v-if="request.method !== 'select' && request.method !== 'batch_ask'">
        <button class="text-button" type="button" @click="appStore.respondToExtension(undefined, true)">{{ tr("extension.cancel") }}</button>
        <template v-if="request.method === 'confirm'">
          <button class="text-button" type="button" @click="appStore.respondToExtension(false)">{{ tr("extension.no") }}</button>
          <button class="text-button primary" type="button" @click="appStore.respondToExtension(true)">{{ tr("extension.yes") }}</button>
        </template>
        <button v-else class="text-button primary" type="button" @click="appStore.respondToExtension(value)">{{ tr("extension.submit") }}</button>
      </footer>
    </section>
  </div>
</template>
