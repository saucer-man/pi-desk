import { Dialogs } from "@wailsio/runtime";
import { RepositoryService } from "../../bindings/pi-desk/internal/appservice";
import type {
  GitBranchInventory,
  RepositoryFileDiff,
  RepositoryFilePreview,
  RepositorySnapshot,
} from "../../bindings/pi-desk/internal/domain";

export type RepositoryWorkspaceReference = string | { workspaceId: string };

function workspaceRequest(reference: RepositoryWorkspaceReference): { workspaceId?: string; workspacePath?: string } {
  return typeof reference === "string" ? { workspacePath: reference } : reference;
}

export const repositoryService = {
  snapshot(workspace: RepositoryWorkspaceReference): Promise<RepositorySnapshot> {
    return RepositoryService.Snapshot(workspaceRequest(workspace));
  },
  diff(workspace: RepositoryWorkspaceReference, path: string): Promise<RepositoryFileDiff> {
    return RepositoryService.Diff({ ...workspaceRequest(workspace), path });
  },
  previewFile(workspace: RepositoryWorkspaceReference, path: string): Promise<RepositoryFilePreview> {
    return RepositoryService.PreviewFile({ ...workspaceRequest(workspace), path });
  },
  branches(workspace: RepositoryWorkspaceReference): Promise<GitBranchInventory> {
    return RepositoryService.Branches(workspaceRequest(workspace));
  },
  openFile(workspacePath: string, path: string): Promise<void> {
    return RepositoryService.OpenFile({ workspacePath, path });
  },
  openFileWith(workspacePath: string, path: string): Promise<void> {
    return RepositoryService.OpenFileWith({ workspacePath, path });
  },
  revealFile(workspacePath: string, path: string): Promise<void> {
    return RepositoryService.RevealFile({ workspacePath, path });
  },
  async saveFileAs(workspacePath: string, path: string, absolutePath: string): Promise<string | undefined> {
    const filename = absolutePath.split(/[\\/]/).pop() || "file";
    const extension = filename.includes(".") ? filename.slice(filename.lastIndexOf(".")) : "";
    const outputPath = await Dialogs.SaveFile({
      Title: "Save file as",
      Filename: filename,
      Directory: absolutePath.slice(0, Math.max(absolutePath.lastIndexOf("\\"), absolutePath.lastIndexOf("/"))),
      CanCreateDirectories: true,
      AllowsOtherFiletypes: true,
      Filters: extension ? [{ DisplayName: `${extension.slice(1).toUpperCase()} file`, Pattern: `*${extension}` }] : [],
    });
    if (!outputPath) return undefined;
    await RepositoryService.SaveFileAs({ workspacePath, path, outputPath });
    return outputPath;
  },
};

export type { GitBranchInventory, RepositoryFileDiff, RepositoryFilePreview, RepositorySnapshot };
