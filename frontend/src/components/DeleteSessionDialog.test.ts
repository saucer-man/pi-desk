import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "../stores/app";
import DeleteSessionDialog from "./DeleteSessionDialog.vue";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

describe("DeleteSessionDialog", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
  });

  it("requires confirmation and displays the recovery path after deletion", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      deleteDialogOpen: true,
      deleteThreadId: "thread-1",
      deleteSessionTitle: "Runtime audit",
      deleteHasSession: true,
      activeThreadId: "thread-1",
      threads: [{
        id: "thread-1", title: "Runtime audit", workspace: "repo", workspacePath: "D:\\repo", trust: "deny",
        status: "idle", started: false, generation: 0, sessionFile: "one.jsonl",
      }],
    });
    store.confirmDeleteSession = vi.fn().mockImplementation(async () => {
      store.deletedRecoveryPath = "one.jsonl.deleted-test";
    });
    const wrapper = mount(DeleteSessionDialog, { global: { plugins: [pinia] } });

    expect(wrapper.text()).toContain("Delete Pi session?");
    await wrapper.get(".danger-button").trigger("click");
    expect(store.confirmDeleteSession).toHaveBeenCalledOnce();
    await wrapper.vm.$nextTick();
    expect(wrapper.text()).toContain("one.jsonl.deleted-test");
    expect(wrapper.text()).toContain("Session removed");
  });
});
