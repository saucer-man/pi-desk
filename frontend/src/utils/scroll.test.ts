import { describe, expect, it } from "vitest";
import { isNearBottom } from "./scroll";

describe("isNearBottom", () => {
  it("sticks within the threshold and releases when reading older content", () => {
    expect(isNearBottom(704, 200, 1000)).toBe(true);
    expect(isNearBottom(650, 200, 1000)).toBe(false);
    expect(isNearBottom(700, 200, 1000, 100)).toBe(true);
  });
});
