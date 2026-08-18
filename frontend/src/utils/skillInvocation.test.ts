import { describe, expect, it } from "vitest";
import {
  parseSkillInvocation,
  replaceSkillInvocationUserMessage,
  skillInvocationCommandText,
  skillInvocationDisplayText,
  skillInvocationTitleText,
} from "./skillInvocation";

const expanded = `<skill name="grill-me" location="C:\\Users\\yanq\\.agents\\skills\\grill-me\\SKILL.md">
References are relative to C:\\Users\\yanq\\.agents\\skills\\grill-me.

Run a \`/grilling\` session.
</skill>

Review the image generation plan.`;

describe("skill invocation projection", () => {
  it("parses Pi's expanded skill block without exposing it as the user message", () => {
    expect(parseSkillInvocation(expanded)).toMatchObject({
      kind: "expanded",
      name: "grill-me",
      location: "C:\\Users\\yanq\\.agents\\skills\\grill-me\\SKILL.md",
      userMessage: "Review the image generation plan.",
    });
    expect(skillInvocationDisplayText(expanded)).toBe("Review the image generation plan.");
    expect(skillInvocationCommandText(expanded)).toBe("/skill:grill-me Review the image generation plan.");
    expect(skillInvocationTitleText(expanded)).toBe("Review the image generation plan.");
  });

  it("keeps the expanded skill context while editing only the user message", () => {
    const edited = replaceSkillInvocationUserMessage(expanded, "Audit the revised plan.");
    expect(edited).toContain("Run a `/grilling` session.\n</skill>");
    expect(edited).toContain("location=\"C:\\Users\\yanq\\.agents\\skills\\grill-me\\SKILL.md\"");
    expect(edited.endsWith("</skill>\n\nAudit the revised plan.")).toBe(true);
  });

  it("projects an unexpanded slash command immediately and leaves ordinary text unchanged", () => {
    expect(parseSkillInvocation("/skill:grill-me Check this decision")).toMatchObject({
      kind: "command",
      name: "grill-me",
      userMessage: "Check this decision",
    });
    expect(skillInvocationDisplayText("Ordinary request")).toBe("Ordinary request");
    expect(skillInvocationCommandText("Ordinary request")).toBe("Ordinary request");
    expect(skillInvocationTitleText("/skill:grill-me Check this decision")).toBe("Check this decision");
    expect(skillInvocationTitleText("/skill:grill-me")).toBe("/skill:grill-me");
    expect(skillInvocationTitleText("Ordinary request")).toBe("Ordinary request");
    expect(replaceSkillInvocationUserMessage("Ordinary request", "  Keep spacing  ")).toBe("  Keep spacing  ");
  });
});
