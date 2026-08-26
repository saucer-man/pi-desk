<script setup lang="ts">
import { AtSign, ChevronDown, ChevronRight, File, Folder, FolderOpen } from "lucide-vue-next";
import { ref } from "vue";
import type { RepositoryTreeNode } from "../utils/fileMentions";

const props = defineProps<{ node: RepositoryTreeNode; depth?: number; changeStatuses?: Record<string, string> }>();
const emit = defineEmits<{
  mention: [path: string, directory: boolean];
  open: [path: string];
  diff: [path: string];
}>();
const open = ref((props.depth ?? 0) === 0);

function forwardMention(path: string, directory: boolean) {
  emit("mention", path, directory);
}

function forwardOpen(path: string) {
  emit("open", path);
}

function forwardDiff(path: string) {
  emit("diff", path);
}
</script>

<template>
  <div class="file-tree-node">
    <div class="file-tree-row" :style="{ '--tree-depth': depth || 0 }">
      <button v-if="node.directory" class="file-tree-toggle" type="button" :title="open ? 'Collapse folder' : 'Expand folder'" @click="open = !open">
        <ChevronDown v-if="open" :size="13" />
        <ChevronRight v-else :size="13" />
      </button>
      <span v-else class="file-tree-spacer" />
      <FolderOpen v-if="node.directory && open" :size="14" />
      <Folder v-else-if="node.directory" :size="14" />
      <File v-else :size="14" />
      <span v-if="node.directory" class="file-tree-name" :title="node.path">{{ node.name }}</span>
      <button
        v-else
        class="file-tree-name file-tree-open"
        :class="{ 'is-changed': changeStatuses?.[node.path] }"
        type="button"
        :data-status="changeStatuses?.[node.path]"
        :title="`Preview ${node.path}`"
        @click="emit('open', node.path)"
      >{{ node.name }}</button>
      <button
        v-if="!node.directory && changeStatuses?.[node.path]"
        class="file-tree-change change-status"
        type="button"
        :data-status="changeStatuses[node.path]"
        :title="`View diff for ${node.path}`"
        @click="emit('diff', node.path)"
      >{{ changeStatuses[node.path] }}</button>
      <span v-else class="file-tree-change-spacer" />
      <button class="file-tree-mention" type="button" :title="node.directory ? 'Mention folder' : 'Mention file'" @click="emit('mention', node.path, node.directory)">
        <AtSign :size="13" />
      </button>
    </div>
    <div v-if="node.directory && open" class="file-tree-children">
      <FileTreeNode
        v-for="child in node.children"
        :key="`${child.directory}-${child.path}`"
        :node="child"
        :depth="(depth || 0) + 1"
        :change-statuses="changeStatuses"
        @mention="forwardMention"
        @open="forwardOpen"
        @diff="forwardDiff"
      />
    </div>
  </div>
</template>
