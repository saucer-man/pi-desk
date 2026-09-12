<script setup lang="ts">
import { highlightCode, tagHighlighter, tags } from "@lezer/highlight";
import { ui } from "../ui/classes";
import { computed, ref, watch } from "vue";

const props = defineProps<{ path: string; content: string; label: string; flush?: boolean }>();
type Segment = { text: string; classes: string };
const highlighted = ref<Segment[]>();
let generation = 0;

const codexHighlighter = tagHighlighter([
  { tag: [tags.function(tags.variableName), tags.function(tags.propertyName)], class: "tok-function" },
  { tag: tags.definitionKeyword, class: "tok-definitionKeyword" },
  { tag: [tags.moduleKeyword, tags.controlKeyword], class: "tok-keyword" },
  { tag: tags.operatorKeyword, class: "tok-operatorKeyword" },
  { tag: tags.keyword, class: "tok-keyword" },
  { tag: [tags.regexp, tags.escape, tags.special(tags.string)], class: "tok-string2" },
  { tag: tags.string, class: "tok-string" },
  { tag: [tags.atom, tags.bool, tags.literal], class: "tok-literal" },
  { tag: tags.number, class: "tok-number" },
  { tag: tags.definition(tags.variableName), class: "tok-definition" },
  { tag: tags.definition(tags.propertyName), class: "tok-definition" },
  { tag: tags.special(tags.variableName), class: "tok-variableName2" },
  { tag: tags.variableName, class: "tok-variableName" },
  { tag: tags.propertyName, class: "tok-propertyName" },
  { tag: [tags.typeName, tags.className, tags.namespace], class: "tok-typeName" },
  { tag: tags.labelName, class: "tok-labelName" },
  { tag: tags.macroName, class: "tok-macroName" },
  { tag: tags.operator, class: "tok-operator" },
  { tag: tags.comment, class: "tok-comment" },
  { tag: tags.meta, class: "tok-meta" },
  { tag: tags.link, class: "tok-link" },
  { tag: tags.url, class: "tok-url" },
  { tag: tags.heading, class: "tok-heading" },
  { tag: tags.strong, class: "tok-strong" },
  { tag: tags.emphasis, class: "tok-emphasis" },
  { tag: tags.inserted, class: "tok-inserted" },
  { tag: tags.deleted, class: "tok-deleted" },
  { tag: tags.invalid, class: "tok-invalid" },
  { tag: tags.punctuation, class: "tok-punctuation" },
]);

const previewLines = computed(() => {
  const lines: Segment[][] = [[]];
  for (const segment of highlighted.value ?? [{ text: props.content, classes: "" }]) {
    segment.text.split("\n").forEach((text, index, parts) => {
      if (text) lines[lines.length - 1].push({ text, classes: segment.classes });
      if (index < parts.length - 1) lines.push([]);
    });
  }
  if (props.content.endsWith("\n") && lines.length > 1 && lines.at(-1)?.length === 0) lines.pop();
  return lines.map((segments, index) => ({ number: index + 1, segments }));
});

watch(() => [props.path, props.content] as const, async ([path, content]) => {
  const currentGeneration = ++generation;
  highlighted.value = undefined;
  if (!content) return;

  try {
    // Milkdown already ships these parsers; reuse them instead of adding a second highlighter stack.
    const { languages } = await import("@codemirror/language-data");
    const fileName = path.split(/[\\/]/).pop() ?? path;
    const extension = fileName.includes(".") ? fileName.split(".").pop()?.toLocaleLowerCase() : "";
    const description = languages.find((language) => language.filename?.test(fileName))
      ?? languages.find((language) => extension && language.extensions.includes(extension));
    if (!description) return;

    const support = await description.load();
    const segments: Segment[] = [];
    highlightCode(
      content,
      support.language.parser.parse(content),
      codexHighlighter,
      (text, classes) => segments.push({ text, classes }),
      () => segments.push({ text: "\n", classes: "" }),
    );
    if (currentGeneration === generation) highlighted.value = segments;
  } catch {
    // A missing or failed language parser should never prevent reading the file.
  }
}, { immediate: true });
</script>

<template>
  <pre class="file-preview-content" :class="[ui.code, { 'rounded-none! border-0! bg-[var(--preview-bg)]! p-0! text-[var(--preview-fg)]!': flush }]" :aria-label="label"><code><span v-for="line in previewLines" :key="line.number" class="file-preview-row"><span class="file-preview-line-number" aria-hidden="true">{{ line.number }}</span><span class="file-preview-line-text"><span v-for="(segment, index) in line.segments" :key="index" :class="segment.classes">{{ segment.text }}</span></span></span></code></pre>
</template>
