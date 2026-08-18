<script setup lang="ts">
import { onBeforeUnmount } from "vue";

const props = defineProps<{
  side: "left" | "right";
  value: number;
  min: number;
  max: number;
  label: string;
}>();

const emit = defineEmits<{
  resize: [width: number];
  commit: [width: number];
}>();

let startX = 0;
let startWidth = 0;
let currentWidth = 0;

function bounded(width: number): number {
  return Math.min(props.max, Math.max(props.min, Math.round(width)));
}

function move(event: PointerEvent) {
  const delta = props.side === "left" ? event.clientX - startX : startX - event.clientX;
  currentWidth = bounded(startWidth + delta);
  emit("resize", currentWidth);
}

function stop() {
  document.documentElement.classList.remove("is-resizing-pane");
  window.removeEventListener("pointermove", move);
  window.removeEventListener("pointerup", stop);
  window.removeEventListener("pointercancel", stop);
  emit("commit", currentWidth);
}

function start(event: PointerEvent) {
  if (event.button !== 0) return;
  event.preventDefault();
  startX = event.clientX;
  startWidth = props.value;
  currentWidth = props.value;
  document.documentElement.classList.add("is-resizing-pane");
  window.addEventListener("pointermove", move);
  window.addEventListener("pointerup", stop, { once: true });
  window.addEventListener("pointercancel", stop, { once: true });
}

function onKeydown(event: KeyboardEvent) {
  if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
  event.preventDefault();
  const visualDelta = event.key === "ArrowRight" ? 12 : -12;
  const width = bounded(props.value + (props.side === "left" ? visualDelta : -visualDelta));
  emit("resize", width);
  emit("commit", width);
}

onBeforeUnmount(() => {
  document.documentElement.classList.remove("is-resizing-pane");
  window.removeEventListener("pointermove", move);
  window.removeEventListener("pointerup", stop);
  window.removeEventListener("pointercancel", stop);
});
</script>

<template>
  <div
    class="pane-resizer"
    :class="`is-${side}`"
    role="separator"
    aria-orientation="vertical"
    :aria-label="label"
    :aria-valuemin="min"
    :aria-valuemax="max"
    :aria-valuenow="value"
    tabindex="0"
    @pointerdown="start"
    @keydown="onKeydown"
  />
</template>
