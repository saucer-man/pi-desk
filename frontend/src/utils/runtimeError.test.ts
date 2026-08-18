import { describe, expect, it } from "vitest";
import { runtimeErrorText } from "./runtimeError";

describe("runtimeErrorText", () => {
  it("trims and bounds provider errors", () => {
    expect(runtimeErrorText("  Request timed out.  ")).toBe("Request timed out.");
    expect(runtimeErrorText("x".repeat(3000))).toHaveLength(2048);
    expect(runtimeErrorText("x".repeat(3000))).toMatch(/\.\.\.$/);
  });

  it("redacts credentials while retaining useful provider context", () => {
    const result = runtimeErrorText(
      "POST https://user:password@example.test/v1 failed; Authorization: Bearer secret-token; apiKey=sk-1234567890abcdefghijkl",
    );
    expect(result).toContain("https://[redacted]@example.test/v1");
    expect(result).toContain("Authorization: [redacted]");
    expect(result).toContain("apiKey=[redacted]");
    expect(result).not.toContain("password");
    expect(result).not.toContain("secret-token");
    expect(result).not.toContain("sk-1234567890abcdefghijkl");
  });

  it("rejects empty and non-string values", () => {
    expect(runtimeErrorText("   ")).toBeUndefined();
    expect(runtimeErrorText(undefined)).toBeUndefined();
  });
});
