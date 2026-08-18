<script setup lang="ts">
import { AlertTriangle, CheckCircle2, FlaskConical, LoaderCircle, X } from "lucide-vue-next";
import { computed, ref } from "vue";
import { useModalFocus } from "../composables/useModalFocus";
import { tr } from "../i18n";
import type { ModelTestResult } from "../services/modelconfig";

const props = defineProps<{
  modelName: string;
  prompt: string;
  testing: boolean;
  result?: ModelTestResult;
}>();
const emit = defineEmits<{
  close: [];
  submit: [];
  "update:prompt": [value: string];
}>();
const dialog = ref<HTMLElement | null>(null);
const responseText = computed(() => {
  if (!props.result) return tr("settings.modelTestResponsePending");
  return props.result.ok ? (props.result.response || "OK") : (props.result.error || tr("common.unavailable"));
});

function close() {
  if (!props.testing) emit("close");
}

function updatePrompt(event: Event) {
  emit("update:prompt", (event.target as HTMLTextAreaElement).value);
}

useModalFocus(dialog, close, { canClose: () => !props.testing });
</script>

<template>
  <Teleport to="body">
    <div class="dialog-backdrop model-test-backdrop" @mousedown.self="close">
      <section
        ref="dialog"
        class="dialog-window model-test-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="model-test-title"
        tabindex="-1"
      >
        <header>
          <div>
            <h2 id="model-test-title">{{ tr("settings.modelTestTitle") }}</h2>
            <small>{{ tr("settings.modelTestDescription", { model: props.modelName }) }}</small>
          </div>
          <button class="icon-button" type="button" :disabled="props.testing" :title="tr('common.close')" @click="close">
            <X :size="16" aria-hidden="true" />
          </button>
        </header>

        <div class="dialog-body model-test-body">
          <label class="model-test-field">
            <span>{{ tr("settings.modelTestPrompt") }}</span>
            <textarea
              autofocus
              data-testid="model-test-prompt"
              :value="props.prompt"
              :disabled="props.testing"
              spellcheck="false"
              @input="updatePrompt"
            />
            <small>{{ tr("settings.modelTestPromptHelp") }}</small>
          </label>

          <section class="model-test-response" :class="{ 'has-result': props.result, 'is-error': props.result && !props.result.ok }" aria-live="polite">
            <header>
              <span>{{ tr("settings.modelTestResponse") }}</span>
              <span v-if="props.result" class="model-test-response-meta">
                {{ props.result.status ? `HTTP ${props.result.status} · ` : "" }}{{ props.result.latencyMs }} ms
              </span>
            </header>
            <div v-if="props.testing" class="model-test-response-loading">
              <LoaderCircle :size="15" class="is-spinning" aria-hidden="true" />
              <span>{{ tr("settings.testingModel") }}</span>
            </div>
            <pre v-else>{{ responseText }}</pre>
            <div v-if="props.result" class="model-test-result-label" :class="props.result.ok ? 'is-success' : 'is-error'">
              <CheckCircle2 v-if="props.result.ok" :size="14" aria-hidden="true" />
              <AlertTriangle v-else :size="14" aria-hidden="true" />
              <span>{{ props.result.ok ? tr("settings.modelTestSucceeded", { latency: props.result.latencyMs }) : props.result.error }}</span>
            </div>
          </section>
        </div>

        <footer>
          <button class="text-button" type="button" :disabled="props.testing" @click="close">{{ tr("common.cancel") }}</button>
          <button
            class="text-button primary-button model-test-submit"
            type="button"
            :disabled="props.testing || !props.prompt.trim()"
            @click="emit('submit')"
          >
            <LoaderCircle v-if="props.testing" :size="14" class="is-spinning" aria-hidden="true" />
            <FlaskConical v-else :size="14" aria-hidden="true" />
            {{ props.result ? tr("settings.resendModelTest") : tr("settings.sendModelTest") }}
          </button>
        </footer>
      </section>
    </div>
  </Teleport>
</template>
