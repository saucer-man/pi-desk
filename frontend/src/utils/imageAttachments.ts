export const MAX_ATTACHED_IMAGES = 10;
export const MAX_SOURCE_IMAGE_BYTES = 10 * 1024 * 1024;
export const MAX_IMAGE_BASE64_CHARS = 6 * 1024 * 1024;

const SUPPORTED_IMAGE_TYPES = new Set(["image/png", "image/jpeg", "image/gif", "image/webp"]);

export interface PreparedImage {
  id: string;
  name: string;
  data: string;
  mimeType: string;
  previewUrl: string;
}

export function parseImageDataURL(dataURL: string, fallbackMimeType: string): Pick<PreparedImage, "data" | "mimeType" | "previewUrl"> {
  const comma = dataURL.indexOf(",");
  if (comma < 0) throw new Error("Unable to read image data");
  const metadata = dataURL.slice(0, comma);
  const data = dataURL.slice(comma + 1);
  const mimeType = /^data:([^;]+);base64$/i.exec(metadata)?.[1] || fallbackMimeType;
  if (!SUPPORTED_IMAGE_TYPES.has(mimeType) || !data) throw new Error("Unsupported image type");
  return { data, mimeType, previewUrl: dataURL };
}

function readDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error || new Error("Unable to read image"));
    reader.onload = () => resolve(String(reader.result));
    reader.readAsDataURL(file);
  });
}

export async function prepareImage(file: File): Promise<PreparedImage> {
  if (!SUPPORTED_IMAGE_TYPES.has(file.type)) throw new Error(`${file.name || "Image"} uses an unsupported format`);
  if (file.size > MAX_SOURCE_IMAGE_BYTES) throw new Error(`${file.name || "Image"} is larger than 10 MiB`);

  const processed = file.type === "image/gif"
    ? file
    : await (await import("browser-image-compression")).default(file, {
        maxSizeMB: 4,
        maxWidthOrHeight: 2000,
        initialQuality: 0.86,
        useWebWorker: true,
      });
  const parsed = parseImageDataURL(await readDataURL(processed), processed.type || file.type);
  if (parsed.data.length > MAX_IMAGE_BASE64_CHARS) throw new Error(`${file.name || "Image"} is too large after compression`);
  return {
    id: globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random()}`,
    name: file.name || "Pasted image",
    ...parsed,
  };
}
