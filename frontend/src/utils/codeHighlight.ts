import { highlightCode, tagHighlighter, tags } from "@lezer/highlight";

export interface CodeSegment {
  text: string;
  classes: string;
}

const highlighter = tagHighlighter([
  { tag: [tags.function(tags.variableName), tags.function(tags.propertyName)], class: "tok-function" },
  { tag: tags.definitionKeyword, class: "tok-definitionKeyword" },
  { tag: [tags.moduleKeyword, tags.controlKeyword], class: "tok-keyword" },
  { tag: tags.operatorKeyword, class: "tok-operatorKeyword" },
  { tag: tags.keyword, class: "tok-keyword" },
  { tag: [tags.regexp, tags.escape, tags.special(tags.string)], class: "tok-string2" },
  { tag: tags.string, class: "tok-string" },
  { tag: [tags.atom, tags.bool, tags.literal], class: "tok-literal" },
  { tag: tags.number, class: "tok-number" },
  { tag: [tags.definition(tags.variableName), tags.definition(tags.propertyName)], class: "tok-definition" },
  { tag: tags.special(tags.variableName), class: "tok-variableName2" },
  { tag: tags.variableName, class: "tok-variableName" },
  { tag: tags.propertyName, class: "tok-propertyName" },
  { tag: [tags.typeName, tags.className, tags.namespace], class: "tok-typeName" },
  { tag: tags.labelName, class: "tok-labelName" },
  { tag: tags.macroName, class: "tok-macroName" },
  { tag: tags.operator, class: "tok-operator" },
  { tag: tags.comment, class: "tok-comment" },
  { tag: tags.meta, class: "tok-meta" },
  { tag: tags.link, class: "tok-link" },
  { tag: tags.url, class: "tok-url" },
  { tag: tags.heading, class: "tok-heading" },
  { tag: tags.strong, class: "tok-strong" },
  { tag: tags.emphasis, class: "tok-emphasis" },
  { tag: tags.inserted, class: "tok-inserted" },
  { tag: tags.deleted, class: "tok-deleted" },
  { tag: tags.invalid, class: "tok-invalid" },
  { tag: tags.punctuation, class: "tok-punctuation" },
]);

export function splitCodeLines(segments: CodeSegment[], trailingNewline = false): CodeSegment[][] {
  const lines: CodeSegment[][] = [[]];
  for (const segment of segments) {
    segment.text.split("\n").forEach((text, index, parts) => {
      if (text) lines[lines.length - 1].push({ text, classes: segment.classes });
      if (index < parts.length - 1) lines.push([]);
    });
  }
  if (trailingNewline && lines.length > 1 && lines.at(-1)?.length === 0) lines.pop();
  return lines;
}

export async function highlightCodeLines(path: string, content: string): Promise<CodeSegment[][] | undefined> {
  if (!content) return [];
  try {
    // Milkdown already ships these parsers; reuse them instead of adding another highlighter stack.
    const { languages } = await import("@codemirror/language-data");
    const fileName = path.split(/[\\/]/).pop() ?? path;
    const extension = fileName.includes(".") ? fileName.split(".").pop()?.toLocaleLowerCase() : "";
    const description = languages.find((language) => language.filename?.test(fileName))
      ?? languages.find((language) => extension && language.extensions.includes(extension));
    if (!description) return undefined;

    const support = await description.load();
    const segments: CodeSegment[] = [];
    highlightCode(
      content,
      support.language.parser.parse(content),
      highlighter,
      (text, classes) => segments.push({ text, classes }),
      () => segments.push({ text: "\n", classes: "" }),
    );
    return splitCodeLines(segments, content.endsWith("\n"));
  } catch {
    return undefined;
  }
}
