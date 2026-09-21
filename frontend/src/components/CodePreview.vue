<script setup lang="ts">
import { ui } from "../ui/classes";
import { computed, ref, watch } from "vue";
import { highlightCodeLines, splitCodeLines, type CodeSegment } from "../utils/codeHighlight";

const props = defineProps<{ path: string; content: string; label: string; flush?: boolean }>();
const highlighted = ref<CodeSegment[][]>();
let generation = 0;

const previewLines = computed(() => {
  const lines = highlighted.value ?? splitCodeLines([{ text: props.content, classes: "" }], props.content.endsWith("\n"));
  return lines.map((segments, index) => ({ number: index + 1, segments }));
});

watch(() => [props.path, props.content] as const, async ([path, content]) => {
  const currentGeneration = ++generation;
  highlighted.value = undefined;
  if (!content) return;

  const lines = await highlightCodeLines(path, content);
  if (currentGeneration === generation) highlighted.value = lines;
}, { immediate: true });
</script>

<template>
  <pre class="file-preview-content" :class="[!flush && ui.code, { 'rounded-none! border-0!': flush }]" :aria-label="label"><code><span v-for="line in previewLines" :key="line.number" class="file-preview-row"><span class="file-preview-line-number" aria-hidden="true">{{ line.number }}</span><span class="file-preview-line-text"><span v-for="(segment, index) in line.segments" :key="index" :class="segment.classes">{{ segment.text }}</span></span></span></code></pre>
</template>
