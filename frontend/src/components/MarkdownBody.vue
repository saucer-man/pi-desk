<script setup lang="ts">
import MarkdownIt from "markdown-it";
import { computed, ref } from "vue";
import { useAppStore } from "../stores/app";
import { resolveWorkspaceFileLink, type WorkspaceFileLink } from "../utils/fileLinks";
import FileLinkContextMenu from "./FileLinkContextMenu.vue";

const props = defineProps<{ text: string; streaming?: boolean }>();
const appStore = useAppStore();
const MAX_MARKDOWN_CHARS = 100_000;
const workspacePath = computed(() => appStore.activeThread?.workspacePath || "");
const contextMenu = ref<{ file: WorkspaceFileLink; x: number; y: number }>();
const markdown = new MarkdownIt({ html: false, breaks: true, linkify: true, typographer: false });
const defaultValidateLink = markdown.validateLink.bind(markdown);
const originalLinkOpen = markdown.renderer.rules.link_open;

markdown.validateLink = (url) => /^file:/i.test(url) || defaultValidateLink(url);
markdown.renderer.rules.link_open = (tokens, index, options, environment, renderer) => {
  const rawHref = tokens[index].attrGet("href");
  const href = typeof rawHref === "string" ? rawHref : String(rawHref ?? "");
  const linkEnvironment = (environment ?? {}) as { workspacePath?: string };
  const file = resolveWorkspaceFileLink(href, String(linkEnvironment.workspacePath ?? ""));
  if (file) {
    tokens[index].attrSet("href", "#");
    tokens[index].attrSet("class", "markdown-file-link");
    tokens[index].attrSet("title", file.absolutePath);
    tokens[index].attrSet("data-file-path", file.relativePath);
    tokens[index].attrSet("data-file-absolute", file.absolutePath);
    tokens[index].attrSet("data-file-name", file.name);
    if (file.line) tokens[index].attrSet("data-file-line", String(file.line));
  } else if (/^(https?:|mailto:|tel:)/i.test(href)) {
    tokens[index].attrSet("target", "_blank");
    tokens[index].attrSet("rel", "noopener noreferrer");
  } else if (!href.startsWith("#")) {
    tokens[index].attrSet("href", "#");
  }
  return originalLinkOpen
    ? originalLinkOpen(tokens, index, options, environment, renderer)
    : renderer.renderToken(tokens, index, options);
};

const renderMarkdown = computed(() => props.text.length <= MAX_MARKDOWN_CHARS);
const rendered = computed(() => renderMarkdown.value ? markdown.render(props.text, { workspacePath: workspacePath.value }) : "");

function fileLinkFromEvent(event: MouseEvent): WorkspaceFileLink | undefined {
  const target = event.target instanceof Element ? event.target.closest<HTMLAnchorElement>("a.markdown-file-link") : null;
  const relativePath = target?.dataset.filePath;
  const absolutePath = target?.dataset.fileAbsolute;
  if (!target || !relativePath || !absolutePath) return undefined;
  const line = Number(target.dataset.fileLine);
  return {
    relativePath,
    absolutePath,
    name: target.dataset.fileName || relativePath.split("/").pop() || relativePath,
    line: Number.isFinite(line) && line > 0 ? line : undefined,
  };
}

function openPreview(event: MouseEvent) {
  const file = fileLinkFromEvent(event);
  if (!file) return;
  event.preventDefault();
  contextMenu.value = undefined;
  void appStore.openRepositoryFilePreview(file.relativePath, file.line);
}

function openContextMenu(event: MouseEvent) {
  const file = fileLinkFromEvent(event);
  if (!file) return;
  event.preventDefault();
  contextMenu.value = { file, x: event.clientX, y: event.clientY };
}
</script>

<template>
  <div
    v-if="renderMarkdown"
    class="markdown-body"
    :class="{ streaming }"
    v-html="rendered"
    @click="openPreview"
    @contextmenu="openContextMenu"
  />
  <pre v-else class="oversized-message">{{ text }}</pre>
  <FileLinkContextMenu
    v-if="contextMenu && workspacePath"
    :file="contextMenu.file"
    :workspace-path="workspacePath"
    :x="contextMenu.x"
    :y="contextMenu.y"
    @close="contextMenu = undefined"
  />
</template>
