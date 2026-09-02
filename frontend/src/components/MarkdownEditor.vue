<script setup lang="ts">
import { MilkdownProvider } from "@milkdown/vue";
import { ref } from "vue";
import { ui } from "../ui/classes";
import MarkdownEditorCore from "./MarkdownEditorCore.vue";

const props = defineProps<{
  modelValue: string;
  placeholder: string;
  ariaLabel: string;
}>();
const emit = defineEmits<{ "update:modelValue": [value: string] }>();

const core = ref<{ focus(): void; replaceMarkdown(value: string): void }>();

defineExpose({
  focus: () => core.value?.focus(),
  replaceMarkdown: (value: string) => core.value?.replaceMarkdown(value),
});
</script>

<template>
  <MilkdownProvider>
    <MarkdownEditorCore ref="core" v-bind="props" :class="ui.root" @update:modelValue="emit('update:modelValue', $event)" />
  </MilkdownProvider>
</template>
