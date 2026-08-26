<script setup lang="ts">
import { ref, watch } from "vue";

const props = defineProps<{ path: string; content: string; label: string }>();
const highlighted = ref<{ text: string; classes: string }[]>();
let generation = 0;

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
    const segments: { text: string; classes: string }[] = [];
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
  <pre class="file-preview-content" :aria-label="label"><code v-if="highlighted"><span v-for="(segment, index) in highlighted" :key="index" :class="segment.classes">{{ segment.text }}</span></code><code v-else>{{ content }}</code></pre>
</template>
