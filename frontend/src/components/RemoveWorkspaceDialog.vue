<script setup lang="ts">
import { LoaderCircle, Trash2, X } from "lucide-vue-next";
import { ref } from "vue";
import { useModalFocus } from "../composables/useModalFocus";
import { tr } from "../i18n";
import { ui } from "../ui/classes";

const props = defineProps<{ workspaceName: string; busy: boolean; error?: string }>();
const emit = defineEmits<{ cancel: []; remove: []; delete: [] }>();
const dialog = ref<HTMLElement | null>(null);
useModalFocus(dialog, () => emit("cancel"), { canClose: () => !props.busy });
</script>

<template>
  <div class="dialog-backdrop" :class="ui.dialogBackdrop" @mousedown.self="!busy && emit('cancel')">
    <section ref="dialog" class="dialog-window delete-session-dialog" :class="ui.dialog" role="alertdialog" aria-modal="true" aria-labelledby="remove-workspace-title" tabindex="-1">
      <header :class="ui.dialogHeader">
        <h2 id="remove-workspace-title">{{ tr("workspaceRemoval.title") }}</h2>
        <button class="icon-button" :class="ui.iconButton" type="button" :title="tr('common.cancel')" :disabled="busy" @click="emit('cancel')"><X :size="17" /></button>
      </header>
      <div class="dialog-body delete-warning" :class="ui.dialogBody">
        <Trash2 :size="22" />
        <div>
          <strong>{{ workspaceName }}</strong>
          <p>{{ tr("workspaceRemoval.removeDescription") }}</p>
          <p>{{ tr("workspaceRemoval.deleteDescription") }}</p>
          <p v-if="error" class="form-error">{{ error }}</p>
        </div>
      </div>
      <footer :class="ui.dialogFooter">
        <button class="text-button" :class="ui.button" type="button" :disabled="busy" @click="emit('cancel')">{{ tr("common.cancel") }}</button>
        <button class="text-button" :class="ui.button" type="button" :disabled="busy" @click="emit('remove')">{{ tr("workspaceRemoval.removeOnly") }}</button>
        <button class="text-button danger-button" :class="ui.buttonDanger" type="button" :disabled="busy" @click="emit('delete')">
          <LoaderCircle v-if="busy" :size="14" class="is-spinning" />
          {{ tr("workspaceRemoval.deletePermanently") }}
        </button>
      </footer>
    </section>
  </div>
</template>
