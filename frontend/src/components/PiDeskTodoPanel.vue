<script setup lang="ts">
import { Check, ChevronDown, Circle, ListTodo } from "lucide-vue-next";
import { computed, ref } from "vue";
import { tr } from "../i18n";
import type { TodoWidgetProjection } from "../utils/todoWidget";

const props = defineProps<{
  todo: TodoWidgetProjection;
}>();

const collapsed = ref(props.todo.items.length === 0);
const complete = computed(() => props.todo.total > 0 && props.todo.completed >= props.todo.total);
const progress = computed(() => props.todo.total > 0
  ? Math.min(100, Math.max(0, (props.todo.completed / props.todo.total) * 100))
  : 0);
const progressLabel = computed(() => tr("composer.todoProgress", {
  done: props.todo.completed,
  total: props.todo.total,
}));
</script>

<template>
  <section
    class="pi-desk-todo-panel composer-stack-panel"
    :class="{ 'is-collapsed': collapsed, 'is-complete': complete }"
    :aria-label="progressLabel"
  >
    <header class="pi-desk-todo-header">
      <div class="pi-desk-todo-heading">
        <ListTodo :size="15" aria-hidden="true" />
        <strong>{{ tr("composer.todoTitle") }}</strong>
        <span>{{ todo.completed }}/{{ todo.total }}</span>
      </div>
      <button
        type="button"
        :title="tr(collapsed ? 'composer.expandTodo' : 'composer.collapseTodo')"
        :aria-label="tr(collapsed ? 'composer.expandTodo' : 'composer.collapseTodo')"
        :aria-expanded="!collapsed"
        @click="collapsed = !collapsed"
      >
        <ChevronDown :size="15" aria-hidden="true" />
      </button>
    </header>
    <div class="pi-desk-todo-progress" role="progressbar" :aria-label="progressLabel" :aria-valuenow="todo.completed" aria-valuemin="0" :aria-valuemax="todo.total">
      <span :style="{ width: `${progress}%` }" />
    </div>
    <div class="pi-desk-todo-content" :aria-hidden="collapsed">
      <div class="pi-desk-todo-content-inner">
        <div class="pi-desk-todo-list" role="list">
          <div v-for="item in todo.items" :key="item.id" class="pi-desk-todo-row" :class="{ 'is-done': item.done }" role="listitem" :title="item.text">
            <Check v-if="item.done" :size="14" aria-hidden="true" />
            <Circle v-else :size="13" aria-hidden="true" />
            <span class="pi-desk-todo-id">#{{ item.id }}</span>
            <span class="pi-desk-todo-text">{{ item.text }}</span>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>
