import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "./app";
import { repositoryService } from "../services/repository";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/repository", () => ({ repositoryService: { clipboardFiles: vi.fn(), previewFile: vi.fn() } }));

describe("composer clipboard projection", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.resetAllMocks();
    useAppStore().$patch({
      threads: [{ id: "thread", workspaceId: "workspace", workspacePath: "D:\\repo", workspace: "repo", title: "Task", trust: "approve", status: "idle", started: true, generation: 1 }],
      activeThreadId: "thread",
    });
  });

  it("reads original images through the workspace service and keeps mixed copies as references", async () => {
    vi.mocked(repositoryService.clipboardFiles).mockResolvedValue([{ path: "截图.png", name: "截图.png" }]);
    vi.mocked(repositoryService.previewFile).mockResolvedValue({ path: "截图.png", absolutePath: "D:/repo/截图.png", mediaType: "image/png", dataUrl: "data:image/png;base64,aW1hZ2U=", size: 5, binary: true, truncated: false });
    const result = await useAppStore().readComposerClipboard("thread");
    expect(repositoryService.clipboardFiles).toHaveBeenCalledWith({ workspaceId: "workspace" });
    expect(repositoryService.previewFile).toHaveBeenCalledWith({ workspaceId: "workspace" }, "截图.png");
    expect(result.references).toEqual([]);
    expect(result.images[0].name).toBe("截图.png");
    expect(result.images[0].type).toBe("image/png");
    expect(result.images[0].size).toBe(5);
    vi.mocked(repositoryService.previewFile).mockClear();
    vi.mocked(repositoryService.clipboardFiles).mockResolvedValue([{ path: "截图.png", name: "截图.png" }, { path: "a b.pdf", name: "a b.pdf" }]);
    expect(await useAppStore().readComposerClipboard("thread")).toEqual({ references: ["@截图.png", '@"a b.pdf"'], images: [] });
    expect(repositoryService.previewFile).not.toHaveBeenCalled();
  });

  it("falls back to references if an image cannot be read and propagates host boundary rejections", async () => {
    vi.mocked(repositoryService.clipboardFiles).mockResolvedValue([{ path: "image.png", name: "image.png" }]);
    vi.mocked(repositoryService.previewFile).mockRejectedValue(new Error("unavailable"));
    expect(await useAppStore().readComposerClipboard("thread")).toEqual({ references: ["@image.png"], images: [] });
    vi.mocked(repositoryService.clipboardFiles).mockRejectedValue(new Error("outside workspace"));
    await expect(useAppStore().readComposerClipboard("thread")).rejects.toThrow("outside workspace");
  });
});
