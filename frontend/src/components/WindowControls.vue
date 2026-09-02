<script setup lang="ts">
import { ui } from "../ui/classes";
import { Copy, Minus, Square, X } from "lucide-vue-next";
import { Events, Window } from "@wailsio/runtime";
import { onBeforeUnmount, onMounted, ref } from "vue";
import { tr } from "../i18n";

const props = defineProps<{ isWindows: boolean }>();
const isMaximised = ref(false);
const unsubscribe: Array<() => void> = [];

async function refreshMaximisedState() {
  if (!props.isWindows) return;
  try {
    isMaximised.value = await Window.IsMaximised();
  } catch {
    isMaximised.value = false;
  }
}

function minimise() {
  void Window.Minimise();
}

async function toggleMaximise() {
  try {
    await Window.ToggleMaximise();
    await refreshMaximisedState();
  } catch {
    // The host owns window state; a failed request leaves the current state intact.
  }
}

function close() {
  void Window.Close();
}

onMounted(() => {
  if (!props.isWindows) return;
  void refreshMaximisedState();
  unsubscribe.push(
    Events.On(Events.Types.Common.WindowMaximise, () => { isMaximised.value = true; }),
    Events.On(Events.Types.Common.WindowUnMaximise, () => { isMaximised.value = false; }),
    Events.On(Events.Types.Common.WindowRestore, () => { void refreshMaximisedState(); }),
  );
});

onBeforeUnmount(() => {
  for (const off of unsubscribe) off();
});
</script>

<template>
  <div v-if="props.isWindows" class="window-controls" :class="ui.root" :aria-label="tr('window.controls')">
    <button class="window-control window-control-minimise" type="button" :title="tr('window.minimise')" :aria-label="tr('window.minimise')" @click="minimise">
      <Minus :size="16" :stroke-width="1.6" />
    </button>
    <button
      class="window-control window-control-maximise"
      type="button"
      :title="isMaximised ? tr('window.restore') : tr('window.maximise')"
      :aria-label="isMaximised ? tr('window.restore') : tr('window.maximise')"
      @click="toggleMaximise"
    >
      <Copy v-if="isMaximised" :size="13" :stroke-width="1.45" />
      <Square v-else :size="13" :stroke-width="1.45" />
    </button>
    <button class="window-control window-control-close" type="button" :title="tr('window.close')" :aria-label="tr('window.close')" @click="close">
      <X :size="17" :stroke-width="1.5" />
    </button>
  </div>
</template>
