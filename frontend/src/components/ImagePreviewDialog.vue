<script setup lang="ts">
import { ui } from "../ui/classes";
import { X } from "lucide-vue-next";
import { ref } from "vue";
import { useModalFocus } from "../composables/useModalFocus";
import { tr } from "../i18n";

const props = defineProps<{ image: { name: string; previewUrl: string } }>();
const emit = defineEmits<{ close: [] }>();
const dialog = ref<HTMLElement | null>(null);

function close() {
  emit("close");
}

useModalFocus(dialog, close);
</script>

<template>
  <Teleport to="body">
    <div class="dialog-backdrop image-preview-backdrop" :class="ui.dialogBackdrop" @mousedown.self="close">
      <section
        ref="dialog"
        class="dialog-window image-preview-dialog" :class="[ui.dialog, ui.dialogImage]"
        role="dialog"
        aria-modal="true"
        :aria-label="tr('composer.imagePreview')"
        tabindex="-1"
      >
        <header :class="ui.dialogHeader">
          <h2 :title="props.image.name">{{ props.image.name }}</h2>
          <button class="icon-button" :class="ui.iconButton" type="button" autofocus :title="tr('common.close')" @click="close">
            <X :size="16" aria-hidden="true" />
          </button>
        </header>
        <div class="image-preview-stage">
          <img :src="props.image.previewUrl" :alt="props.image.name" />
        </div>
      </section>
    </div>
  </Teleport>
</template>
