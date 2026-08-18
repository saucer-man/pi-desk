import { describe, expect, it } from "vitest";
import { parseImageDataURL } from "./imageAttachments";

describe("parseImageDataURL", () => {
  it("extracts Pi image content without retaining a data URL prefix", () => {
    expect(parseImageDataURL("data:image/png;base64,aW1hZ2U=", "image/jpeg")).toEqual({
      data: "aW1hZ2U=",
      mimeType: "image/png",
      previewUrl: "data:image/png;base64,aW1hZ2U=",
    });
  });

  it("rejects unsupported image types and malformed data URLs", () => {
    expect(() => parseImageDataURL("data:image/svg+xml;base64,PHN2Zz4=", "image/svg+xml")).toThrow("Unsupported image type");
    expect(() => parseImageDataURL("not-a-data-url", "image/png")).toThrow("Unable to read image data");
  });
});
