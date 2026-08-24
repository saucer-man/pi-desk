<script setup lang="ts">
import { defaultValueCtx, Editor, editorViewCtx, rootCtx } from "@milkdown/core";
import { listener, listenerCtx } from "@milkdown/plugin-listener";
import { commonmark } from "@milkdown/preset-commonmark";
import { Milkdown, useEditor } from "@milkdown/vue";
import { getMarkdown, replaceAll } from "@milkdown/utils";
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";

const props = defineProps<{
  modelValue: string;
  placeholder: string;
  ariaLabel: string;
}>();

const emit = defineEmits<{ "update:modelValue": [value: string] }>();
const root = ref<HTMLDivElement>();
let lastMarkdown = props.modelValue;

function contentElement(): HTMLElement | undefined {
  return root.value?.querySelector<HTMLElement>("[contenteditable='true']") ?? undefined;
}

function syncContentAttributes() {
  const element = contentElement();
  if (!element) return;
  element.classList.add("markdown-body");
  element.setAttribute("aria-label", props.ariaLabel);
  element.dataset.placeholder = props.placeholder;
}

const { loading, get } = useEditor((editorRoot) => Editor.make()
  .use(listener)
  .use(commonmark)
  .config((ctx) => {
    ctx.set(rootCtx, editorRoot);
    ctx.set(defaultValueCtx, props.modelValue);
    ctx.get(listenerCtx).markdownUpdated((_ctx, markdown) => {
      const normalized = markdown.replace(/\n$/, "");
      lastMarkdown = normalized;
      if (normalized !== props.modelValue) emit("update:modelValue", normalized);
    });
  }));

function focus() {
  get()?.action((ctx) => ctx.get(editorViewCtx).focus());
}

function syncMarkdown() {
  const editor = get();
  if (!editor) return;
  const markdown = editor.action(getMarkdown()).replace(/\n$/, "");
  if (markdown === lastMarkdown) return;
  lastMarkdown = markdown;
  emit("update:modelValue", markdown);
}

function replaceMarkdown(value: string) {
  const editor = get();
  if (!editor) return;
  editor.action(replaceAll(value));
  syncMarkdown();
  focus();
}

function updateElement() {
  syncContentAttributes();
  void nextTick(syncContentAttributes);
}

watch(loading, (isLoading) => {
  if (!isLoading) updateElement();
});

watch(() => props.modelValue, (value) => {
  if (value === lastMarkdown || !get()) return;
  lastMarkdown = value;
  get()?.action(replaceAll(value, true));
  updateElement();
});

watch(() => [props.placeholder, props.ariaLabel], updateElement);

onMounted(updateElement);
onBeforeUnmount(() => { lastMarkdown = ""; });

defineExpose({ focus, replaceMarkdown });
</script>

<template>
  <div ref="root" class="markdown-editor" :class="{ 'is-empty': !modelValue.trim() }" @input="syncMarkdown">
    <Milkdown />
  </div>
</template>
