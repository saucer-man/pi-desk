export interface SkillInvocation {
  kind: "command" | "expanded";
  name: string;
  location?: string;
  content?: string;
  userMessage: string;
  rawBlock?: string;
}

const EXPANDED_SKILL_PATTERN = /^(<skill name="([^"]+)" location="([^"]+)">\n([\s\S]*?)\n<\/skill>)(?:\n\n([\s\S]+))?$/;
const SKILL_COMMAND_PATTERN = /^\/skill:([a-z0-9-]+)(?:\s+([\s\S]*))?$/;

export function parseSkillInvocation(text: string): SkillInvocation | undefined {
  const expanded = text.match(EXPANDED_SKILL_PATTERN);
  if (expanded) {
    return {
      kind: "expanded",
      name: expanded[2],
      location: expanded[3],
      content: expanded[4],
      userMessage: expanded[5]?.trim() ?? "",
      rawBlock: expanded[1],
    };
  }

  const command = text.match(SKILL_COMMAND_PATTERN);
  if (!command) return undefined;
  return {
    kind: "command",
    name: command[1],
    userMessage: command[2]?.trim() ?? "",
  };
}

export function skillInvocationDisplayText(text: string): string {
  return parseSkillInvocation(text)?.userMessage ?? text;
}

export function skillInvocationCommandText(text: string): string {
  const invocation = parseSkillInvocation(text);
  if (!invocation) return text;
  const args = invocation.userMessage ? ` ${invocation.userMessage}` : "";
  return `/skill:${invocation.name}${args}`;
}

export function skillInvocationTitleText(text: string): string {
  const invocation = parseSkillInvocation(text);
  if (!invocation) return text;
  return invocation.userMessage || `/skill:${invocation.name}`;
}

export function replaceSkillInvocationUserMessage(text: string, userMessage: string): string {
  const invocation = parseSkillInvocation(text);
  if (invocation?.kind !== "expanded" || !invocation.rawBlock) return userMessage;
  return `${invocation.rawBlock}\n\n${userMessage}`;
}
