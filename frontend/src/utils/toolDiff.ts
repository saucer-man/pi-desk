import type { ToolDiff } from "../stores/app";

const MAX_TOOL_DIFF_LENGTH = 256 << 10;

function recordValue(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : undefined;
}

function toolPath(args: unknown): string {
  const values = recordValue(args);
  const path = values?.path ?? values?.file_path ?? values?.filePath ?? values?.file;
  return typeof path === "string" ? path : "";
}

function textLines(value: string): string[] {
  if (!value) return [];
  const lines = value.split("\n");
  if (lines.at(-1) === "") lines.pop();
  return lines;
}

function textEdits(args: unknown): Array<{ oldText: string; newText: string }> {
  const values = recordValue(args);
  if (!values) return [];
  const edits = Array.isArray(values.edits)
    ? values.edits.map(recordValue).filter((edit): edit is Record<string, unknown> => Boolean(edit))
    : [values];
  return edits.flatMap((edit) => typeof edit.oldText === "string" && typeof edit.newText === "string"
    ? [{ oldText: edit.oldText, newText: edit.newText }]
    : []);
}

function editDiff(edits: Array<{ oldText: string; newText: string }>): string {
  const lines: string[] = [];
  for (const edit of edits) {
    lines.push(...textLines(edit.oldText).map((line) => `- ${line}`));
    lines.push(...textLines(edit.newText).map((line) => `+ ${line}`));
  }
  return lines.join("\n");
}

function replaceUnique(value: string, search: string, replacement: string): string | undefined {
  if (!search) return undefined;
  const index = value.indexOf(search);
  if (index < 0 || value.indexOf(search, index + search.length) >= 0) return undefined;
  return `${value.slice(0, index)}${replacement}${value.slice(index + search.length)}`;
}

function mergeEdits(edits: NonNullable<ToolDiff["edits"]>): NonNullable<ToolDiff["edits"]> {
  const merged: NonNullable<ToolDiff["edits"]> = [];
  for (const source of edits) {
    if (source.oldText === source.newText) continue;
    const current = { ...source };
    let absorbed = false;
    for (let index = merged.length - 1; index >= 0; index--) {
      const previous = merged[index];
      const updated = replaceUnique(previous.newText, current.oldText, current.newText);
      if (updated !== undefined) {
        previous.newText = updated;
        previous.firstChangedLine ??= current.firstChangedLine;
        if (previous.oldText === previous.newText) merged.splice(index, 1);
        absorbed = true;
        break;
      }
      const original = replaceUnique(current.oldText, previous.newText, previous.oldText);
      if (original !== undefined) {
        current.oldText = original;
        current.firstChangedLine = Math.min(
          current.firstChangedLine ?? Number.MAX_SAFE_INTEGER,
          previous.firstChangedLine ?? Number.MAX_SAFE_INTEGER,
        );
        if (current.firstChangedLine === Number.MAX_SAFE_INTEGER) delete current.firstChangedLine;
        merged.splice(index, 1);
      }
    }
    if (!absorbed && current.oldText !== current.newText) merged.push(current);
  }
  return merged;
}

function lineDiff(oldText: string, newText: string): string[] {
  const oldLines = textLines(oldText);
  const newLines = textLines(newText);
  // ponytail: keep the dependency-free LCS for edit-sized blocks; use Myers only if huge replacements become common.
  if (oldLines.length * newLines.length > 2_000_000) {
    return [...oldLines.map((line) => `-${line}`), ...newLines.map((line) => `+${line}`)];
  }
  const table = Array.from({ length: oldLines.length + 1 }, () => new Uint32Array(newLines.length + 1));
  for (let oldIndex = oldLines.length - 1; oldIndex >= 0; oldIndex--) {
    for (let newIndex = newLines.length - 1; newIndex >= 0; newIndex--) {
      table[oldIndex][newIndex] = oldLines[oldIndex] === newLines[newIndex]
        ? table[oldIndex + 1][newIndex + 1] + 1
        : Math.max(table[oldIndex + 1][newIndex], table[oldIndex][newIndex + 1]);
    }
  }
  const lines: string[] = [];
  let oldIndex = 0;
  let newIndex = 0;
  while (oldIndex < oldLines.length && newIndex < newLines.length) {
    if (oldLines[oldIndex] === newLines[newIndex]) {
      lines.push(` ${oldLines[oldIndex]}`);
      oldIndex++;
      newIndex++;
    } else if (table[oldIndex + 1][newIndex] >= table[oldIndex][newIndex + 1]) {
      lines.push(`-${oldLines[oldIndex++]}`);
    } else {
      lines.push(`+${newLines[newIndex++]}`);
    }
  }
  lines.push(...oldLines.slice(oldIndex).map((line) => `-${line}`));
  lines.push(...newLines.slice(newIndex).map((line) => `+${line}`));
  return lines;
}

function formatEdit(edit: NonNullable<ToolDiff["edits"]>[number]): string {
  const oldLines = textLines(edit.oldText);
  const newLines = textLines(edit.newText);
  let prefix = 0;
  while (prefix < oldLines.length && prefix < newLines.length && oldLines[prefix] === newLines[prefix]) prefix++;
  let suffix = 0;
  while (
    suffix < oldLines.length - prefix
    && suffix < newLines.length - prefix
    && oldLines[oldLines.length - suffix - 1] === newLines[newLines.length - suffix - 1]
  ) suffix++;
  const oldChanged = oldLines.slice(prefix, oldLines.length - suffix);
  const newChanged = newLines.slice(prefix, newLines.length - suffix);
  const body = lineDiff(oldChanged.join("\n"), newChanged.join("\n"));
  const start = edit.firstChangedLine ?? 1;
  return `@@ -${start},${oldChanged.length} +${start},${newChanged.length} @@\n${body.join("\n")}`;
}

export function mergeToolDiffs(diffs: ToolDiff[]): string {
  if (diffs.length < 2 || diffs.some((diff) => !diff.edits?.length)) return diffs.map((diff) => diff.text).join("\n");
  return mergeEdits(diffs.flatMap((diff) => diff.edits ?? [])).map(formatEdit).filter(Boolean).join("\n");
}

function editStartLines(edits: Array<{ oldText: string; newText: string }>, displayDiff: string, fallback?: number): Array<number | undefined> {
  const displayLines = displayDiff.split("\n");
  let cursor = 0;
  return edits.map((edit, index) => {
    const change = lineDiff(edit.oldText, edit.newText).find((line) => line.startsWith("+") || line.startsWith("-"));
    if (change) {
      for (let lineIndex = cursor; lineIndex < displayLines.length; lineIndex++) {
        const match = displayLines[lineIndex].match(/^([+-])\s*(\d+)\s(.*)$/);
        if (match?.[1] === change[0] && match[3] === change.slice(1)) {
          cursor = lineIndex + 1;
          return Number(match[2]);
        }
      }
    }
    return index === 0 ? fallback : undefined;
  });
}

export function buildToolDiff(name: string, args: unknown, details?: unknown): ToolDiff | undefined {
  const normalizedName = name.toLowerCase();
  const path = toolPath(args);
  if (!path || (normalizedName !== "edit" && normalizedName !== "write")) return undefined;

  const detailValues = recordValue(details);
  const edits = normalizedName === "edit" ? textEdits(args) : [];
  let text = typeof detailValues?.diff === "string" ? detailValues.diff : "";
  if (!text && normalizedName === "edit") text = editDiff(edits);
  if (!text && normalizedName === "write") {
    const content = recordValue(args)?.content;
    if (typeof content === "string") {
      text = content.split("\n").map((line, index) => `+${String(index + 1).padStart(4)} ${line}`).join("\n");
    }
  }
  if (!text) return undefined;
  const firstChangedLine = typeof detailValues?.firstChangedLine === "number" ? detailValues.firstChangedLine : undefined;
  const startLines = editStartLines(edits, text, firstChangedLine);
  return {
    path,
    text: text.slice(0, MAX_TOOL_DIFF_LENGTH),
    ...(edits.length ? { edits: edits.map((edit, index) => startLines[index] === undefined ? edit : { ...edit, firstChangedLine: startLines[index] }) } : {}),
  };
}
