<script setup lang="ts">
import { ui } from "../ui/classes";
import { computed, ref, watch } from "vue";

const props = defineProps<{ path: string; content: string; label: string; flush?: boolean }>();
type Segment = { text: string; classes: string };
const highlighted = ref<Segment[]>();
let generation = 0;

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
    const [{ languages }, { classHighlighter, highlightCode }] = await Promise.all([
      import("@codemirror/language-data"),
      import("@lezer/highlight"),
    ]);
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
      classHighlighter,
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
