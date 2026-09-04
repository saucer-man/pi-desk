export function splitTaggedThinking(text: string): { text: string; thinking: string; open: boolean } {
  const thoughts: string[] = [];
  let visible = "";
  let cursor = 0;
  const opening = /<(think(?:ing)?)(?:\s[^>]*)?>/gi;
  while (true) {
    opening.lastIndex = cursor;
    const start = opening.exec(text);
    if (!start) {
      visible += text.slice(cursor);
      return { text: visible, thinking: thoughts.filter(Boolean).join("\n\n"), open: false };
    }
    visible += text.slice(cursor, start.index);
    const contentStart = start.index + start[0].length;
    const closing = new RegExp(`</${start[1]}\\s*>`, "gi");
    closing.lastIndex = contentStart;
    const end = closing.exec(text);
    if (!end) {
      thoughts.push(text.slice(contentStart).trim());
      return { text: visible, thinking: thoughts.filter(Boolean).join("\n\n"), open: true };
    }
    thoughts.push(text.slice(contentStart, end.index).trim());
    cursor = end.index + end[0].length;
  }
}
