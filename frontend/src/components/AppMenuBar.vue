<script setup lang="ts">
import { ui } from "../ui/classes";
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { toggleDebugMode } from "../services/desktop";
import { tr } from "../i18n";
import { useAppStore } from "../stores/app";

const appStore = useAppStore();
const helpOpen = ref(false);
const helpButton = ref<HTMLButtonElement>();
const debugEnabled = ref(false);
const debugBusy = ref(false);
const debugLabel = computed(() => tr(debugEnabled.value ? "appMenu.closeDebugMode" : "appMenu.openDebugMode"));

function closeHelp(restoreFocus = false) {
  helpOpen.value = false;
  if (restoreFocus) helpButton.value?.focus();
}

function openAbout() {
  closeHelp();
  appStore.openAbout();
}

async function toggleDebug() {
  if (debugBusy.value) return;
  debugBusy.value = true;
  try {
    debugEnabled.value = await toggleDebugMode();
    closeHelp();
  } finally {
    debugBusy.value = false;
  }
}

function onDocumentPointerDown(event: PointerEvent) {
  const target = event.target;
  if (target instanceof Element && target.closest(".app-menu-anchor")) return;
  closeHelp();
}

function onDocumentKeydown(event: KeyboardEvent) {
  if (event.key === "Escape" && helpOpen.value) {
    event.preventDefault();
    closeHelp(true);
  }
}

onMounted(() => {
  document.addEventListener("pointerdown", onDocumentPointerDown);
  document.addEventListener("keydown", onDocumentKeydown);
});

onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", onDocumentPointerDown);
  document.removeEventListener("keydown", onDocumentKeydown);
});
</script>

<template>
  <header class="app-menubar" :class="ui.root">
    <div class="app-menu-drag-region" aria-hidden="true" />
    <nav class="app-menu-list" :aria-label="tr('appMenu.navigation')">
      <div class="app-menu-identity" aria-label="Pi Desk">
        <span class="brand-name">Pi Desk</span>
      </div>
      <div class="app-menu-anchor">
        <button ref="helpButton" class="app-menu-trigger" type="button" aria-haspopup="menu" :aria-expanded="helpOpen" @click="helpOpen = !helpOpen">
          {{ tr("appMenu.help") }}
        </button>
        <div v-if="helpOpen" class="app-menu-popover help-menu" :class="ui.menuSurface" role="menu" @keydown.esc.stop.prevent="closeHelp(true)">
          <button type="button" role="menuitem" :disabled="debugBusy" @click="void toggleDebug()"><span>{{ debugLabel }}</span></button>
          <button type="button" role="menuitem" @click="openAbout"><span>{{ tr("appMenu.about") }}</span></button>
        </div>
      </div>
    </nav>
  </header>
</template>
