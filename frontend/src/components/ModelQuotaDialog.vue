<script setup lang="ts">
import { ui } from "../ui/classes";
import { AlertTriangle, CircleDollarSign, LoaderCircle, RefreshCw, X } from "lucide-vue-next";
import { ref } from "vue";
import { useModalFocus } from "../composables/useModalFocus";
import { tr } from "../i18n";
import type { ModelQuotaResult } from "../services/modelconfig";

const props = defineProps<{
  providerName: string;
  loading: boolean;
  result?: ModelQuotaResult;
}>();
const emit = defineEmits<{ close: []; retry: [] }>();
const dialog = ref<HTMLElement | null>(null);

function close() {
  if (!props.loading) emit("close");
}

useModalFocus(dialog, close, { canClose: () => !props.loading });
</script>

<template>
  <Teleport to="body">
    <div class="dialog-backdrop model-quota-backdrop" :class="ui.dialogBackdrop" @mousedown.self="close">
      <section
        ref="dialog"
        class="dialog-window model-quota-dialog" :class="[ui.dialog, ui.dialogLarge]"
        role="dialog"
        aria-modal="true"
        aria-labelledby="model-quota-title"
        tabindex="-1"
      >
        <header :class="ui.dialogHeader">
          <div>
            <h2 id="model-quota-title">{{ tr("settings.accountQuotaTitle") }}</h2>
            <small>{{ tr("settings.accountQuotaDescription", { provider: props.providerName }) }}</small>
          </div>
          <button class="icon-button" :class="ui.iconButton" type="button" :disabled="props.loading" :title="tr('common.close')" @click="close">
            <X :size="16" aria-hidden="true" />
          </button>
        </header>

        <div class="dialog-body model-quota-body" :class="ui.dialogBody" aria-live="polite">
          <div v-if="props.loading" class="model-quota-loading" role="status">
            <LoaderCircle :size="18" class="is-spinning" aria-hidden="true" />
            <span>{{ tr("settings.loadingAccountQuota") }}</span>
          </div>
          <template v-else-if="props.result?.ok">
            <dl class="model-quota-meta">
              <div><dt>{{ tr("settings.quotaEndpoint") }}</dt><dd>{{ props.result.endpoint }}</dd></div>
              <div><dt>HTTP</dt><dd>{{ props.result.status }} · {{ props.result.latencyMs }} ms</dd></div>
            </dl>
            <section class="model-quota-output">
              <h3><CircleDollarSign :size="15" aria-hidden="true" />{{ tr("settings.quotaSummary") }}</h3>
              <pre>{{ props.result.summary }}</pre>
            </section>
            <details class="model-quota-details">
              <summary>{{ tr("settings.quotaDetails") }}</summary>
              <pre>{{ props.result.detailsJson }}</pre>
            </details>
          </template>
          <div v-else class="model-quota-error" role="alert">
            <AlertTriangle :size="18" aria-hidden="true" />
            <div>
              <strong>{{ tr("settings.accountQuotaUnavailable") }}</strong>
              <p>{{ props.result?.error || tr("settings.accountQuotaUnavailableHelp") }}</p>
              <small v-if="props.result?.endpoint">{{ props.result.endpoint }}</small>
            </div>
          </div>
        </div>

        <footer :class="ui.dialogFooter">
          <button class="text-button" :class="ui.button" type="button" :disabled="props.loading" @click="close">{{ tr("common.close") }}</button>
          <button class="text-button primary-button" :class="ui.buttonPrimary" type="button" :disabled="props.loading" @click="emit('retry')">
            <RefreshCw :size="14" aria-hidden="true" />{{ tr("settings.retryAccountQuota") }}
          </button>
        </footer>
      </section>
    </div>
  </Teleport>
</template>
