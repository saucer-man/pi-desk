<script setup lang="ts">
import { ui } from "../ui/classes";
import { ChevronDown, CirclePause, CirclePlay, Goal, LoaderCircle, Trash2 } from "lucide-vue-next";
import { computed, ref } from "vue";
import { tr } from "../i18n";
import type { GoalWidgetProjection } from "../utils/goalWidget";

const props = defineProps<{
  goal: GoalWidgetProjection;
  running?: boolean;
}>();

const emit = defineEmits<{ command: [command: string] }>();

const collapsed = ref(false);
const clearArmed = ref(false);

const resumable = computed(() => ["paused", "blocked", "usage_limited", "budget_limited"].includes(props.goal.status));
const complete = computed(() => props.goal.status === "complete");
const budgetProgress = computed(() => (props.goal.tokenBudget ? Math.min(100, (props.goal.tokensUsed / props.goal.tokenBudget) * 100) : 0));
const statusLabel = computed(() => tr(`composer.goalStatus.${props.goal.status}`));
const tokensLabel = computed(() => (props.goal.tokenBudget
  ? tr("composer.goalTokensBudget", { used: props.goal.tokensUsed, budget: props.goal.tokenBudget })
  : tr("composer.goalTokensUsed", { count: props.goal.tokensUsed })));

function armClear() {
  if (!clearArmed.value) {
    clearArmed.value = true;
    window.setTimeout(() => { clearArmed.value = false; }, 5000);
    return;
  }
  clearArmed.value = false;
  emit("command", "/goal clear");
}
</script>

<template>
  <section
    class="pi-desk-goal-panel composer-stack-panel"
    :class="[ui.panel, `is-${goal.status}`, { 'is-collapsed': collapsed, 'is-complete': complete }]"
    :aria-label="statusLabel"
  >
    <header class="pi-desk-goal-header">
      <div class="pi-desk-goal-heading">
        <Goal :size="15" aria-hidden="true" />
        <strong>{{ tr("composer.goalTitle") }}</strong>
        <em class="pi-desk-goal-status">{{ statusLabel }}</em>
        <span>{{ tr("composer.goalIteration", { iteration: goal.iteration }) }}</span>
        <LoaderCircle v-if="running && goal.status === 'active'" :size="12" class="is-spinning" aria-hidden="true" />
      </div>
      <div class="pi-desk-goal-actions">
        <button
          v-if="goal.status === 'active'"
          type="button"
          class="text-button"
          :class="ui.button"
          :title="tr('composer.goalPause')"
          @click="emit('command', '/goal pause')"
        >
          <CirclePause :size="13" />{{ tr("composer.goalPause") }}
        </button>        <button
          v-else-if="resumable"
          type="button"
          class="text-button primary"
          :class="ui.buttonPrimary"
          :title="tr('composer.goalResume')"
          @click="emit('command', '/goal resume')"
        >
          <CirclePlay :size="13" />{{ tr("composer.goalResume") }}
        </button>
        <button
          v-if="!complete"
          type="button"
          class="icon-button"
          :class="[ui.iconButton, { danger: clearArmed }]"
          :title="clearArmed ? tr('composer.goalClearConfirm') : tr('composer.goalClear')"
          :aria-label="clearArmed ? tr('composer.goalClearConfirm') : tr('composer.goalClear')"
          @click="armClear"
        >
          <Trash2 :size="13" />
        </button>
        <button
          type="button"
          class="pi-desk-goal-collapse"
          :title="tr(collapsed ? 'composer.expandGoal' : 'composer.collapseGoal')"
          :aria-label="tr(collapsed ? 'composer.expandGoal' : 'composer.collapseGoal')"
          :aria-expanded="!collapsed"
          @click="collapsed = !collapsed"
        >
          <ChevronDown :size="15" aria-hidden="true" />
        </button>
      </div>
    </header>
    <div v-if="goal.tokenBudget" class="pi-desk-goal-budget" role="progressbar" :aria-valuenow="goal.tokensUsed" aria-valuemin="0" :aria-valuemax="goal.tokenBudget">
      <span :style="{ width: `${budgetProgress}%` }" />
    </div>
    <div class="pi-desk-goal-content" :aria-hidden="collapsed">
      <div class="pi-desk-goal-content-inner">
        <p class="pi-desk-goal-text" :title="goal.text">{{ goal.text }}</p>
        <footer class="pi-desk-goal-meta">
          <span>{{ tokensLabel }}</span>
        </footer>
      </div>
    </div>
  </section>
</template>
