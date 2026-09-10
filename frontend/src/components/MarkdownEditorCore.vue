<script setup lang="ts">
import { ui } from "../ui/classes";
import { defaultValueCtx, Editor, editorViewCtx, editorViewOptionsCtx, parserCtx, rootCtx, serializerCtx } from "@milkdown/core";
import { codeBlockSchema, commonmark, createCodeBlockInputRule, hardbreakSchema, strongInputRule, strongSchema } from "@milkdown/preset-commonmark";
import { markRule } from "@milkdown/prose";
import { exitCode, newlineInCode } from "@milkdown/prose/commands";
import { textblockTypeInputRule } from "@milkdown/prose/inputrules";
import { Fragment, Slice } from "@milkdown/prose/model";
import { TextSelection } from "@milkdown/prose/state";
import { gfm } from "@milkdown/preset-gfm";
import { Milkdown, useEditor } from "@milkdown/vue";
import { $inputRule, replaceAll } from "@milkdown/utils";
import { nextTick, onMounted, ref, watch } from "vue";
import { normalizeMarkdownBreakTags } from "../utils/markdown";

const props = defineProps<{
  modelValue: string;
  placeholder: string;
  ariaLabel: string;
}>();

const emit = defineEmits<{ "update:modelValue": [value: string] }>();
const root = ref<HTMLDivElement>();
let lastMarkdown = props.modelValue;
let replacingMarkdown = false;
const codeFence = /^ {0,3}(?:`{3,}|~{3,})([^\s`~]*)[ \t\n]$/;

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
  .use(commonmark.filter((plugin) => plugin !== createCodeBlockInputRule && plugin !== strongInputRule))
  .use($inputRule((ctx) => textblockTypeInputRule(codeFence, codeBlockSchema.type(ctx), (match) => ({ language: match[1] }))))
  .use($inputRule((ctx) => markRule(
    /(?<![\w:/])(\*\*|__)(\S(?:.*?\S)?)\1(?![\w/])$/,
    strongSchema.type(ctx),
    { getAttr: (match) => ({ marker: match[1]?.[0] }) },
  )))
  .use(gfm)
  .config((ctx) => {
    ctx.set(rootCtx, editorRoot);
    ctx.set(defaultValueCtx, props.modelValue);
    // Match the message renderer's breaks:true when a saved draft is parsed again.
    ctx.update(hardbreakSchema.key, (schema) => (ctx) => ({ ...schema(ctx), toDOM: () => ["br"] }));
    ctx.update(editorViewOptionsCtx, (options) => ({
      ...options,
      handleKeyDown(view, event) {
        if (event.key === "Enter" && event.shiftKey && !event.isComposing && event.keyCode !== 229) {
          return newlineInCode(view.state, view.dispatch);
        }
        return false;
      },
      dispatchTransaction(transaction) {
        const view = ctx.get(editorViewCtx);
        const state = view.state.apply(transaction);
        view.updateState(state);
        if (!transaction.docChanged || replacingMarkdown) return;

        const serialized = normalizeMarkdownBreakTags(ctx.get(serializerCtx)(state.doc));
        const trailingNode = state.doc.lastChild?.lastChild;
        const markdown = trailingNode?.type.name === "hardbreak" ? serialized : serialized.replace(/\n$/, "");
        lastMarkdown = markdown;
        if (markdown !== props.modelValue) emit("update:modelValue", markdown);
      },
    }));
  }));

function focus() {
  get()?.action((ctx) => ctx.get(editorViewCtx).focus());
}

// Clipboard IPC is asynchronous: keep the original selection, and never replace
// a newer draft if the document changed while the host was reading the files.
function captureTextInsertion(): (text: string, separate?: boolean) => boolean {
  const editor = get();
  const view = editor?.action((ctx) => ctx.get(editorViewCtx));
  const state = view?.state;
  return (text: string, separate = false) => {
    if (!view || !state || view.isDestroyed || view.state.doc !== state.doc) return false;
    const previous = state.doc.textBetween(Math.max(0, state.selection.from - 1), state.selection.from);
    if (separate && previous && !/\s/.test(previous)) text = ` ${text}`;
    const inCode = state.selection.$from.parent.type.spec.code
      || (state.storedMarks ?? state.selection.$from.marks()).some((mark) => mark.type.spec.code);
    if (!separate && !inCode) {
      const doc = editor?.action((ctx) => ctx.get(parserCtx)(text));
      if (!doc) return false;
      let formatted = false;
      doc.descendants((node) => {
        if (node.marks.length || !["paragraph", "text", "hardbreak"].includes(node.type.name)) formatted = true;
      });
      if (formatted) {
        let content = doc.content;
        const paragraph = doc.firstChild;
        if (doc.childCount === 1 && paragraph?.type.name === "paragraph") {
          const leading = text.match(/^[ \t]+/)?.[0] ?? "";
          const trailing = text.match(/[ \t]+$/)?.[0] ?? "";
          const edge = (value: string) => value ? Fragment.from(state.schema.text(value)) : Fragment.empty;
          content = Fragment.from(paragraph.copy(edge(leading).append(paragraph.content).append(edge(trailing))));
        }
        view.dispatch(view.state.tr.setSelection(state.selection).replaceSelection(Slice.maxOpen(content)).scrollIntoView());
      } else {
        // Plain text and file references must retain literal whitespace and active marks.
        view.dispatch(view.state.tr.insertText(text, state.selection.from, state.selection.to).scrollIntoView());
      }
    } else {
      view.dispatch(view.state.tr.insertText(text, state.selection.from, state.selection.to).scrollIntoView());
    }
    view.focus();
    return true;
  };
}

function handleEnter(event: KeyboardEvent): boolean {
  return get()?.action((ctx) => {
    const view = ctx.get(editorViewCtx);
    const { selection } = view.state;
    const { $from, empty } = selection;
    if ($from.parent.type.spec.code) {
      const line = $from.parent.textBetween(0, $from.parentOffset).split("\n").at(-1);
      if (/^(?:```|~~~)$/.test(line ?? "")) {
        view.dispatch(view.state.tr.delete(selection.from - 3, selection.from));
        exitCode(view.state, view.dispatch);
      } else {
        view.someProp("handleKeyDown", (handler) => handler(view, event));
      }
      return true;
    }
    const text = $from.parent.textBetween(0, $from.parentOffset, undefined, "\uFFFC");
    const lineStart = text.lastIndexOf("\uFFFC") + 1;
    const match = empty && codeFence.exec(`${text.slice(lineStart)}\n`);
    if (match) {
      if (lineStart) {
        const splitAt = $from.start() + lineStart - 1;
        const tr = view.state.tr.delete(splitAt, selection.from).split(splitAt);
        const codePos = splitAt + 2;
        tr.setBlockType(codePos, codePos, codeBlockSchema.type(ctx), { language: match[1] })
          .setSelection(TextSelection.create(tr.doc, codePos));
        view.dispatch(tr.scrollIntoView());
      } else {
        view.someProp("handleKeyDown", (handler) => handler(view, event));
      }
      return true;
    }
    for (let depth = $from.depth; depth > 0; depth -= 1) {
      if ($from.node(depth).type.name === "list_item") {
        view.someProp("handleKeyDown", (handler) => handler(view, event));
        return true;
      }
    }
    return false;
  }) ?? false;
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

defineExpose({ focus, replaceMarkdown, handleEnter, captureTextInsertion });
</script>

<template>
  <div ref="root" class="markdown-editor" :class="[ui.root, { 'is-empty': !modelValue.trim() }]">
    <Milkdown />
  </div>
</template>
