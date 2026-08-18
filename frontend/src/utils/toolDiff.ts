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

function editDiff(args: unknown): string {
  const values = recordValue(args);
  if (!values) return "";
  const edits = Array.isArray(values.edits)
    ? values.edits.map(recordValue).filter((edit): edit is Record<string, unknown> => Boolean(edit))
    : [values];
  const lines: string[] = [];
  for (const edit of edits) {
    if (typeof edit.oldText !== "string" || typeof edit.newText !== "string") continue;
    lines.push(...edit.oldText.split("\n").map((line) => `- ${line}`));
    lines.push(...edit.newText.split("\n").map((line) => `+ ${line}`));
  }
  return lines.join("\n");
}

export function buildToolDiff(name: string, args: unknown, details?: unknown): ToolDiff | undefined {
  const normalizedName = name.toLowerCase();
  const path = toolPath(args);
  if (!path || (normalizedName !== "edit" && normalizedName !== "write")) return undefined;

  const detailValues = recordValue(details);
  let text = typeof detailValues?.diff === "string" ? detailValues.diff : "";
  if (!text && normalizedName === "edit") text = editDiff(args);
  if (!text && normalizedName === "write") {
    const content = recordValue(args)?.content;
    if (typeof content === "string") {
      text = content.split("\n").map((line, index) => `+${String(index + 1).padStart(4)} ${line}`).join("\n");
    }
  }
  if (!text) return undefined;
  return { path, text: text.slice(0, MAX_TOOL_DIFF_LENGTH) };
}
