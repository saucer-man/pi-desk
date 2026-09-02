<script setup lang="ts">
import { ui } from "../ui/classes";
import { AlertTriangle, GitBranch, GitFork, LoaderCircle, X } from "lucide-vue-next";
import { computed, ref } from "vue";
import { useModalFocus } from "../composables/useModalFocus";
import type { SessionBranchEntry } from "../services/agent";
import { useAppStore } from "../stores/app";
import { tr } from "../i18n";

const appStore = useAppStore();
const maxVisibleNodes = 500;
const dialog = ref<HTMLElement | null>(null);
useModalFocus(dialog, () => appStore.closeBranchPanel(), { canClose: () => !appStore.activeSessionOperation });

interface VisibleNode {
  id: string;
  type: string;
  role: string;
  label: string;
  depth: number;
  active: boolean;
}

function labelFor(entry: SessionBranchEntry): string {
  const text = String(entry.text ?? "").replace(/\s+/g, " ").trim();
  if (text) return text.length > 100 ? `${text.slice(0, 97)}...` : text;
  return entry.label || entry.type.replaceAll("_", " ");
}

const visibleNodes = computed<VisibleNode[]>(() => {
  const response = appStore.activeSessionBranches;
  if (!response) return [];
  const entries = response.entries ?? [];

  const entriesById = new Map(entries.map((entry) => [entry.id, entry]));
  const childrenByParent = new Map<string, SessionBranchEntry[]>();
  const roots: SessionBranchEntry[] = [];
  for (const entry of entries) {
    const parentId = entry.parentId;
    if (!parentId || parentId === entry.id || !entriesById.has(parentId)) {
      roots.push(entry);
      continue;
    }
    const children = childrenByParent.get(parentId) ?? [];
    children.push(entry);
    childrenByParent.set(parentId, children);
  }

  const result: VisibleNode[] = [];
  const visited = new Set<string>();
  const pending = [...roots].reverse().map((entry) => ({ entry, depth: 0 }));
  let fallbackIndex = 0;
  while (result.length < maxVisibleNodes) {
    if (pending.length === 0) {
      while (fallbackIndex < entries.length && visited.has(entries[fallbackIndex].id)) fallbackIndex += 1;
      if (fallbackIndex === entries.length) break;
      pending.push({ entry: entries[fallbackIndex], depth: 0 });
    }
    const { entry, depth } = pending.pop()!;
    if (visited.has(entry.id)) continue;
    visited.add(entry.id);
    result.push({
      id: entry.id,
      type: entry.type,
      role: String(entry.role ?? ""),
      label: labelFor(entry),
      depth,
      active: entry.id === response.leafId,
    });
    const children = childrenByParent.get(entry.id) ?? [];
    for (let index = children.length - 1; index >= 0; index -= 1) {
      pending.push({ entry: children[index], depth: depth + 1 });
    }
  }
  return result;
});

function fork(node: VisibleNode) {
  void appStore.forkActiveSession(node.id, node.label);
}
</script>

<template>
  <div class="dialog-backdrop" :class="ui.dialogBackdrop" @mousedown.self="appStore.closeBranchPanel()">
    <section ref="dialog" class="dialog-window branch-dialog" :class="ui.dialog" :role="appStore.activeSessionBranchesError ? 'alertdialog' : 'dialog'" aria-modal="true" aria-labelledby="branch-dialog-title" tabindex="-1">
      <header :class="ui.dialogHeader">
        <h2 id="branch-dialog-title">
          <AlertTriangle v-if="appStore.activeSessionBranchesError" :size="16" />
          <GitBranch v-else :size="16" />
          {{ appStore.activeSessionBranchesError ? tr("branches.loadFailedTitle") : tr("branches.title") }}
        </h2>
        <button class="icon-button" :class="ui.iconButton" type="button" :title="tr('branches.close')" @click="appStore.closeBranchPanel()"><X :size="17" /></button>
      </header>
      <div class="dialog-body branch-tree" :class="ui.dialogBody">
        <div v-if="appStore.activeSessionBranchesError" class="branch-warning">
          <AlertTriangle :size="24" aria-hidden="true" />
          <div>
            <strong>{{ tr("branches.unavailable") }}</strong>
            <code>{{ appStore.activeSessionBranchesError }}</code>
          </div>
        </div>
        <div v-else-if="!appStore.activeSessionBranches" class="panel-empty" :class="ui.empty">
          <LoaderCircle :size="20" class="is-spinning" />
          <strong>{{ tr("branches.loading") }}</strong>
        </div>
        <div v-else-if="visibleNodes.length === 0" class="panel-empty" :class="ui.empty">
          <GitBranch :size="20" />
          <strong>{{ tr("branches.empty") }}</strong>
        </div>
        <div v-else role="tree" :aria-label="tr('branches.history')">
          <div
            v-for="node in visibleNodes"
            :key="node.id"
            class="branch-node"
            :class="{ 'is-active': node.active }"
            :style="{ marginLeft: `${Math.min(node.depth, 12) * 14}px` }"
            role="treeitem"
            :aria-current="node.active ? 'true' : undefined"
          >
            <span class="branch-line" aria-hidden="true" />
            <span class="branch-role">{{ node.role === "user" ? "U" : node.role === "assistant" ? "P" : "-" }}</span>
            <span class="branch-text" :title="node.label">{{ node.label }}</span>
            <button
              v-if="node.type === 'message' && node.role === 'user'"
              class="icon-button" :class="ui.iconButton"
              type="button"
              :title="tr('branches.fork')"
              :disabled="Boolean(appStore.activeSessionOperation)"
              @click="fork(node)"
            ><GitFork :size="14" /></button>
          </div>
          <p v-if="visibleNodes.length === maxVisibleNodes" class="branch-limit">{{ tr("branches.limit", { count: maxVisibleNodes }) }}</p>
        </div>
      </div>
      <footer :class="ui.dialogFooter">
        <template v-if="appStore.activeSessionBranchesError">
          <button class="text-button primary" :class="ui.buttonPrimary" type="button" @click="appStore.closeBranchPanel()">{{ tr("common.close") }}</button>
        </template>
        <template v-else>
          <span v-if="appStore.activeSessionOperation" class="operation-label"><LoaderCircle :size="13" class="is-spinning" /> {{ appStore.activeSessionOperation }}</span>
          <button class="text-button" :class="ui.button" type="button" :disabled="Boolean(appStore.activeSessionOperation)" @click="void appStore.cloneActiveSession()">{{ tr("branches.clone") }}</button>
        </template>
      </footer>
    </section>
  </div>
</template>
