<script setup lang="ts">
import { defaultValueCtx, Editor, editorViewCtx, editorViewOptionsCtx, rootCtx, serializerCtx } from "@milkdown/core";
import { commonmark } from "@milkdown/preset-commonmark";
import { gfm } from "@milkdown/preset-gfm";
import { Milkdown, useEditor } from "@milkdown/vue";
import { replaceAll } from "@milkdown/utils";
import { nextTick, onMounted, ref, watch } from "vue";

const props = defineProps<{
  modelValue: string;
  placeholder: string;
  ariaLabel: string;
}>();

const emit = defineEmits<{ "update:modelValue": [value: string] }>();
const root = ref<HTMLDivElement>();
let lastMarkdown = props.modelValue;
let replacingMarkdown = false;

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
  .use(commonmark)
  .use(gfm)
  .config((ctx) => {
    ctx.set(rootCtx, editorRoot);
    ctx.set(defaultValueCtx, props.modelValue);
    ctx.update(editorViewOptionsCtx, (options) => ({
      ...options,
      dispatchTransaction(transaction) {
        const view = ctx.get(editorViewCtx);
        const state = view.state.apply(transaction);
        view.updateState(state);
        if (!transaction.docChanged || replacingMarkdown) return;

        const markdown = ctx.get(serializerCtx)(state.doc).replace(/\n$/, "");
        lastMarkdown = markdown;
        if (markdown !== props.modelValue) emit("update:modelValue", markdown);
      },
    }));
  }));

function focus() {
  get()?.action((ctx) => ctx.get(editorViewCtx).focus());
}

function applyMarkdown(value: string, flush = false): boolean {
  const editor = get();
  if (!editor) return false;
  const trailingWhitespace = value.match(/[ \t]+$/)?.[0] ?? "";
  const parsedValue = trailingWhitespace ? value.slice(0, -trailingWhitespace.length) : value;
  lastMarkdown = value;
  replacingMarkdown = true;
  try {
    editor.action(replaceAll(parsedValue, flush));
    if (trailingWhitespace) {
      editor.action((ctx) => {
        const view = ctx.get(editorViewCtx);
        view.dispatch(view.state.tr.insertText(trailingWhitespace, view.state.doc.content.size - 1));
      });
    }
  } finally {
    replacingMarkdown = false;
  }
  return true;
}

function replaceMarkdown(value: string) {
  if (!applyMarkdown(value)) return;
  if (value !== props.modelValue) emit("update:modelValue", value);
  focus();
}

function updateElement() {
  syncContentAttributes();
  void nextTick(syncContentAttributes);
}

watch(loading, (isLoading) => {
  if (isLoading) return;
  if (props.modelValue !== lastMarkdown || /[ \t]+$/.test(props.modelValue)) applyMarkdown(props.modelValue, true);
  updateElement();
});

watch(() => props.modelValue, (value) => {
  if (value === lastMarkdown || !applyMarkdown(value, true)) return;
  updateElement();
});

watch(() => [props.placeholder, props.ariaLabel], updateElement);

onMounted(updateElement);

defineExpose({ focus, replaceMarkdown });
</script>

<template>
  <div ref="root" class="markdown-editor" :class="{ 'is-empty': !modelValue.trim() }">
    <Milkdown />
  </div>
</template>
