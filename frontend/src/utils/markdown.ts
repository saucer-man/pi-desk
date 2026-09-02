export function normalizeMarkdownBreakTags(value: string): string {
  let normalized = "";
  let cursor = 0;
  let fence: { marker: "`" | "~"; length: number } | undefined;

  while (cursor < value.length) {
    const atLineStart = cursor === 0 || value[cursor - 1] === "\n";
    if (atLineStart) {
      const lineEnd = value.indexOf("\n", cursor);
      const end = lineEnd === -1 ? value.length : lineEnd + 1;
      const line = value.slice(cursor, lineEnd === -1 ? value.length : lineEnd);

      if (fence) {
        const closing = /^ {0,3}([`~]+)\s*$/.exec(line);
        normalized += value.slice(cursor, end);
        cursor = end;
        if (closing?.[1]?.[0] === fence.marker && closing[1].length >= fence.length) fence = undefined;
        continue;
      }

      const opening = /^ {0,3}(`{3,}|~{3,})/.exec(line)?.[1];
      if (opening) {
        fence = { marker: opening[0] as "`" | "~", length: opening.length };
        normalized += value.slice(cursor, end);
        cursor = end;
        continue;
      }
    }

    if (value[cursor] === "`") {
      let runEnd = cursor + 1;
      while (value[runEnd] === "`") runEnd += 1;
      const delimiter = value.slice(cursor, runEnd);
      let closing = value.indexOf(delimiter, runEnd);
      while (closing !== -1 && (value[closing - 1] === "`" || value[closing + delimiter.length] === "`")) {
        closing = value.indexOf(delimiter, closing + delimiter.length);
      }
      if (closing !== -1) {
        const end = closing + delimiter.length;
        normalized += value.slice(cursor, end);
        cursor = end;
        continue;
      }
    }

    const breakTag = /^<\/?br\s*\/?>/i.exec(value.slice(cursor))?.[0];
    if (breakTag) {
      normalized += "\n";
      cursor += breakTag.length;
      continue;
    }

    normalized += value[cursor];
    cursor += 1;
  }

  return normalized;
}
