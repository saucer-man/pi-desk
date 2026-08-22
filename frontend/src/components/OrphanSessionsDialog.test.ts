import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "../stores/app";
import OrphanSessionsDialog from "./OrphanSessionsDialog.vue";

const orphanSessionService = vi.hoisted(() => ({
  list: vi.fn(), snapshot: vi.fn(), remove: vi.fn(), exportHTML: vi.fn(),
}));
vi.mock("../services/orphanSessions", () => ({ orphanSessionService }));
vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

const session = {
  id: "session-1", path: "D:\\sessions\\orphan.jsonl", anchorWorkspaceId: "workspace-old",
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
      before: "", hasMore: false, messageCount: 1,
    });
    orphanSessionService.exportHTML.mockResolvedValue("D:\\exports\\remote.html");
    orphanSessionService.remove.mockResolvedValue({ recoveryPath: "D:\\sessions\\orphan.deleted-1.jsonl" });
  });

  it("loads a local-only orphan transcript and exports it", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    useAppStore().orphanSessionsOpen = true;
    const wrapper = mount(OrphanSessionsDialog, { global: { plugins: [pinia] } });

    await vi.waitFor(() => expect(wrapper.text()).toContain("Inspect remote"));
    expect(orphanSessionService.snapshot).toHaveBeenCalledWith(session.path);
    await wrapper.findAll(".orphan-actions button")[0].trigger("click");
    await vi.waitFor(() => expect(orphanSessionService.exportHTML).toHaveBeenCalledWith(session.path, "Remote review"));
    expect(wrapper.text()).toContain("remote.html");
  });

  it("requires a second delete click and shows the recovery path", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    useAppStore().orphanSessionsOpen = true;
    const wrapper = mount(OrphanSessionsDialog, { global: { plugins: [pinia] } });
    await vi.waitFor(() => expect(wrapper.text()).toContain("Inspect remote"));
    const remove = wrapper.findAll(".orphan-actions button")[1];

    await remove.trigger("click");
    expect(orphanSessionService.remove).not.toHaveBeenCalled();
    expect(remove.text()).toContain("Confirm delete");
    await remove.trigger("click");

    await vi.waitFor(() => expect(orphanSessionService.remove).toHaveBeenCalledWith(session.path));
    expect(wrapper.text()).toContain("orphan.deleted-1.jsonl");
  });
});
