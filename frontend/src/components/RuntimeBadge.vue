<script setup lang="ts">
import { ui } from "../ui/classes";
import { CircleAlert, CircleCheck, LoaderCircle } from "lucide-vue-next";
import { computed } from "vue";
import { useAppStore } from "../stores/app";
import { tr } from "../i18n";

const appStore = useAppStore();

const state = computed(() => appStore.bootstrap?.runtime.state ?? "loading");
const checking = computed(() => !appStore.bootstrapError && (appStore.bootstrapLoading || appStore.runtimeCheckLoading || state.value === "checking" || state.value === "loading"));
const label = computed(() => {
  if (checking.value) return tr("runtime.checking");
  if (appStore.bootstrapError) return tr("runtime.offline");
  if (state.value === "ready") {
    const version = appStore.bootstrap?.runtime.version;
    return version ? tr("runtime.currentVersion", { version }) : tr("runtime.ready");
  }
  if (state.value === "missing") return tr("runtime.missing");
  return tr("runtime.unavailable");
});
</script>

<template>
  <div class="runtime-badge" :class="ui.root" :data-state="state" :title="appStore.bootstrap?.runtime.message">
    <LoaderCircle v-if="checking" :size="14" class="is-spinning" />
    <CircleCheck v-else-if="state === 'ready'" :size="14" />
    <CircleAlert v-else :size="14" />
    <span>{{ label }}</span>
  </div>
</template>
