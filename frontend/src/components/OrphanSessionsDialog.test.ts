import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "../stores/app";
import OrphanSessionsDialog from "./OrphanSessionsDialog.vue";

const orphanSessionService = vi.hoisted(() => ({
  list: vi.fn(), snapshot: vi.fn(), restore: vi.fn(),
}));
vi.mock("../services/orphanSessions", () => ({ orphanSessionService }));
vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

const session = {
  id: "session-1", path: "D:\\sessions\\orphan.jsonl", anchorWorkspaceId: "workspace-old",
  targetId: "target-1", remoteRoot: "/srv/repository",
  name: "", title: "Remote review", firstMessage: "Inspect remote", createdAt: "2026-08-10T08:00:00Z",
  modifiedAt: "2026-08-10T08:01:00Z", messageCount: 1,
};

describe("OrphanSessionsDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setActivePinia(createPinia());
    orphanSessionService.list.mockResolvedValue([session]);
    orphanSessionService.snapshot.mockResolvedValue({
      messages: [{ type: "message", id: "user-1", timestamp: "2026-08-10T08:01:00Z", message: { role: "user", content: "Inspect remote" } }],
      messageCount: 1,
    });
    orphanSessionService.restore.mockResolvedValue(undefined);
  });

  it("restores an orphan transcript only to a matching SSH workspace", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.orphanSessionsOpen = true;
    store.workspaces = [{ id: "workspace-new", name: "Remote", path: "", kind: "ssh", targetId: "target-1", remoteRoot: "/srv/repository", trust: "approve" }];
    const wrapper = mount(OrphanSessionsDialog, { global: { plugins: [pinia] } });

    await vi.waitFor(() => expect(wrapper.text()).toContain("Inspect remote"));
    expect(orphanSessionService.snapshot).toHaveBeenCalledWith(session.path);
    await wrapper.findAll(".orphan-actions button")[0].trigger("click");
    await vi.waitFor(() => expect(orphanSessionService.restore).toHaveBeenCalledWith(session.path, "workspace-new"));
  });
});
