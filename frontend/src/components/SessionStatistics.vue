<script setup lang="ts">
import { BarChart3, RefreshCw } from "lucide-vue-next";
import { computed, onMounted, ref } from "vue";
import type { SessionUsageSummary } from "../../bindings/pi-desk/internal/domain";
import { tr } from "../i18n";
import { catalogService } from "../services/catalog";
import { useAppStore } from "../stores/app";

const appStore = useAppStore();
const scope = ref<"all" | "workspace">("all");
const loading = ref(false);
const error = ref("");
const usage = ref<SessionUsageSummary | null>(null);
const workspacePath = computed(() => appStore.activeThread?.workspacePath || "");
const modelUsage = computed(() => usage.value?.models ?? []);

function formatTokens(value: number) {
  return value.toLocaleString();
}

function formatCost(value: number) {
  return `$${value.toFixed(value >= 100 ? 2 : 4)}`;
}

async function refresh() {
  if (loading.value) return;
  loading.value = true;
  error.value = "";
  try {
    usage.value = await catalogService.getSessionUsage(scope.value === "workspace" ? workspacePath.value : undefined);
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause);
  } finally {
    loading.value = false;
  }
}

async function changeScope(next: "all" | "workspace") {
  if (scope.value === next) return;
  scope.value = next;
  await refresh();
}

onMounted(() => { void refresh(); });
</script>

<template>
  <div class="settings-content runtime-settings-content session-statistics">
    <div class="settings-content-header">
      <div><h3>{{ tr("settings.sessionStatistics") }}</h3><span>{{ tr("settings.sessionStatisticsHelp") }}</span></div>
      <button class="icon-button" type="button" :title="tr('settings.refreshStatistics')" :disabled="loading" @click="void refresh()"><RefreshCw :size="14" :class="{ 'is-spinning': loading }" /></button>
    </div>
    <div class="statistics-scope" role="tablist" :aria-label="tr('settings.statisticsScope')">
      <button type="button" role="tab" :aria-selected="scope === 'all'" @click="void changeScope('all')">{{ tr("settings.allSessions") }}</button>
      <button type="button" role="tab" :aria-selected="scope === 'workspace'" :disabled="!workspacePath" @click="void changeScope('workspace')">{{ tr("settings.currentWorkspace") }}</button>
    </div>
    <p v-if="scope === 'workspace' && workspacePath" class="statistics-path" :title="workspacePath">{{ workspacePath }}</p>
    <div v-if="loading && !usage" class="settings-empty"><RefreshCw :size="18" class="is-spinning" /><span>{{ tr("settings.loadingStatistics") }}</span></div>
    <div v-else-if="usage" class="statistics-content">
      <div class="statistics-grid">
        <section><span>{{ tr("settings.statSessions") }}</span><strong>{{ formatTokens(usage.sessions) }}</strong></section>
        <section><span>{{ tr("settings.statMessages") }}</span><strong>{{ formatTokens(usage.messages) }}</strong><small>{{ usage.userMessages }} / {{ usage.assistantMessages }} / {{ usage.toolResults }}</small></section>
        <section><span>{{ tr("settings.statTokens") }}</span><strong>{{ formatTokens(usage.tokens.total) }}</strong></section>
        <section><span>{{ tr("settings.statCost") }}</span><strong>{{ formatCost(usage.cost) }}</strong></section>
      </div>
      <section class="statistics-breakdown">
        <h4>{{ tr("settings.tokenBreakdown") }}</h4>
        <dl>
          <div><dt>{{ tr("settings.inputTokens") }}</dt><dd>{{ formatTokens(usage.tokens.input) }}</dd></div>
          <div><dt>{{ tr("settings.outputTokens") }}</dt><dd>{{ formatTokens(usage.tokens.output) }}</dd></div>
          <div><dt>{{ tr("settings.cacheReadTokens") }}</dt><dd>{{ formatTokens(usage.tokens.cacheRead) }}</dd></div>
          <div><dt>{{ tr("settings.cacheWriteTokens") }}</dt><dd>{{ formatTokens(usage.tokens.cacheWrite) }}</dd></div>
          <div><dt>{{ tr("settings.reasoningTokens") }}</dt><dd>{{ formatTokens(usage.tokens.reasoning) }}</dd></div>
        </dl>
        <p>{{ tr("settings.reasoningTokenHelp") }}</p>
      </section>
      <section class="statistics-models">
        <h4>{{ tr("settings.usageByModel") }}</h4>
        <div v-if="modelUsage.length" class="statistics-model-list">
          <div v-for="model in modelUsage" :key="`${model.provider}/${model.model}`" class="statistics-model-row">
            <span><strong>{{ model.model || tr("settings.unknownModel") }}</strong><small>{{ model.provider || tr("settings.unknownProvider") }}</small></span>
            <span><strong>{{ formatTokens(model.tokens.total) }}</strong><small>{{ formatCost(model.cost) }}</small></span>
          </div>
        </div>
        <div v-else class="settings-empty compact"><BarChart3 :size="17" /><span>{{ tr("settings.noUsage") }}</span></div>
      </section>
    </div>
    <p v-if="error" class="form-error">{{ error }}</p>
  </div>
</template>
