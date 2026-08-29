export function fuzzyScore(value: string, query: string): number {
  const text = value.toLocaleLowerCase();
  const needle = query.trim().toLocaleLowerCase();
  if (!needle) return 0;

  let score = 0;
  let position = -1;
  let streak = 0;
  for (const character of needle) {
    const next = text.indexOf(character, position + 1);
    if (next < 0) return -1;
    streak = next === position + 1 ? streak + 1 : 0;
    score += streak * 4 - Math.max(0, next - position - 1);
    if (next === 0 || "/\\._- ".includes(text[next - 1])) score += 12;
    position = next;
  }
  return score - Math.max(0, text.length - needle.length) / 100;
}

export function rankFuzzy<T>(items: T[], query: string, targets: (item: T) => string[]): T[] {
  const needle = query.trim();
  if (!needle) return items;
  return items
    .map((item, index) => ({
      item,
      index,
      score: Math.max(...targets(item).map((target, targetIndex) => fuzzyScore(target, needle) + (targetIndex === 0 ? 24 : 0))),
    }))
    .filter((result) => result.score >= 0)
    .sort((left, right) => right.score - left.score || left.index - right.index)
    .map((result) => result.item);
}
