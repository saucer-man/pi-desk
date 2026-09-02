<script setup lang="ts">
import { ui } from "../ui/classes";
import { Check, ChevronRight, CircleCheck, CircleX, Copy, LoaderCircle, SquareTerminal, Wrench } from "lucide-vue-next";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import type { ToolExecution } from "../stores/app";
import { tr } from "../i18n";

const props = defineProps<{ tool: ToolExecution }>();
const copied = ref<"input" | "output" | "">("");
const open = ref(props.tool.status === "running");
let copyResetTimer: ReturnType<typeof setTimeout> | undefined;

const inputText = computed(() => {
  if (props.tool.arguments === undefined) return "";
  if (typeof props.tool.arguments === "string") return props.tool.arguments;
  try {
    return JSON.stringify(props.tool.arguments, null, 2);
  } catch {
    return String(props.tool.arguments);
  }
});

const summary = computed(() => {
  if (!props.tool.arguments || typeof props.tool.arguments !== "object") return props.tool.name;
  const values = props.tool.arguments as Record<string, unknown>;
  const detail = ["command", "cmd", "path", "file", "query", "url"]
    .map((key) => values[key])
    .find((value): value is string => typeof value === "string" && value.trim().length > 0);
  if (!detail) return props.tool.name;
  const compact = detail.replace(/\s+/g, " ").trim();
  return `${props.tool.name} ${compact.length > 96 ? `${compact.slice(0, 93)}...` : compact}`;
});

const statusLabel = computed(() => tr(({ running: "tools.running", complete: "tools.complete", error: "tools.failed" })[props.tool.status]));
const durationLabel = computed(() => {
  const duration = props.tool.durationMs;
  if (duration === undefined) return "";
  if (duration < 1000) return `${duration}ms`;
  return `${(duration / 1000).toFixed(duration < 10_000 ? 1 : 0)}s`;
});

watch(() => props.tool.status, (status, previous) => {
  if (status === "running") open.value = true;
  else if (previous === "running") open.value = false;
});

function syncOpen(event: Event) {
  open.value = (event.currentTarget as HTMLDetailsElement).open;
}

function diffLineClass(line: string): string {
  if (line.startsWith("+") && !line.startsWith("+++")) return "is-added";
  if (line.startsWith("-") && !line.startsWith("---")) return "is-removed";
  if (line.startsWith("@@")) return "is-hunk";
  return "";
}

async function copyText(kind: "input" | "output", text: string) {
  if (!text || !navigator.clipboard?.writeText) return;
  try {
    await navigator.clipboard.writeText(text);
    copied.value = kind;
    if (copyResetTimer) clearTimeout(copyResetTimer);
    copyResetTimer = setTimeout(() => { copied.value = ""; }, 1600);
  } catch {
    copied.value = "";
  }
}

onBeforeUnmount(() => {
  if (copyResetTimer) clearTimeout(copyResetTimer);
});
</script>

<template>
  <details class="tool-call" :data-state="tool.status" :open="open" @toggle="syncOpen">
    <summary :class="ui.root">
      <ChevronRight class="disclosure-icon" :size="13" aria-hidden="true" />
      <SquareTerminal v-if="tool.name === 'bash'" :size="15" aria-hidden="true" />
      <Wrench v-else :size="15" aria-hidden="true" />
      <span class="tool-summary-group">
        <span class="tool-summary">{{ summary }}</span>
        <span v-if="durationLabel" class="tool-duration">{{ durationLabel }}</span>
        <span v-if="tool.diff" class="tool-diff-badge">diff</span>
        <span class="tool-status">
          <LoaderCircle v-if="tool.status === 'running'" :size="12" class="is-spinning" aria-hidden="true" />
          <CircleCheck v-else-if="tool.status === 'complete'" :size="12" aria-hidden="true" />
          <CircleX v-else :size="12" aria-hidden="true" />
          {{ statusLabel }}
        </span>
      </span>
    </summary>
    <div v-if="tool.diff" class="tool-section tool-diff-section">
      <div class="tool-section-header"><span>{{ tool.diff.path }}</span></div>
      <pre class="tool-diff"><code><span v-for="(line, index) in tool.diff.text.split('\n')" :key="index" class="diff-line" :class="diffLineClass(line)">{{ `${line}\n` }}</span></code></pre>
    </div>
    <div v-if="inputText" class="tool-section">
      <div class="tool-section-header">
        <span>{{ tr("tools.input") }}</span>
        <button type="button" :title="tr('tools.copyInput')" :aria-label="tr('tools.copyInput')" @click="void copyText('input', inputText)">
          <Check v-if="copied === 'input'" :size="13" />
          <Copy v-else :size="13" />
        </button>
      </div>
      <pre>{{ inputText }}</pre>
    </div>
    <div v-if="tool.output" class="tool-section">
      <div class="tool-section-header">
        <span>{{ tr("tools.output") }} <small v-if="tool.truncated">{{ tr("tools.truncated") }}</small></span>
        <button type="button" :title="tr('tools.copyOutput')" :aria-label="tr('tools.copyOutput')" @click="void copyText('output', tool.output)">
          <Check v-if="copied === 'output'" :size="13" />
          <Copy v-else :size="13" />
        </button>
      </div>
      <pre>{{ tool.output }}</pre>
    </div>
  </details>
</template>
