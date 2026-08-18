import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "../stores/app";
import BranchDialog from "./BranchDialog.vue";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

describe("BranchDialog", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
  });

  it("renders the active Pi leaf and forks from a user entry", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      branchPanelOpen: true,
      activeThreadId: "thread-1",
      threads: [{
        id: "thread-1", title: "Audit", workspace: "repo", workspacePath: "D:\\repo", trust: "deny",
        status: "idle", started: true, generation: 1, sessionFile: "one.jsonl",
      }],
      sessionBranchesByThread: {
        "thread-1": {
          leafId: "assistant-1",
          entries: [
            { id: "user-1", parentId: "", type: "message", timestamp: "", role: "user", text: "Inspect runtime", label: "" },
            { id: "assistant-1", parentId: "user-1", type: "message", timestamp: "", role: "assistant", text: "Done", label: "" },
          ],
        },
      },
    });
    store.forkActiveSession = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(BranchDialog, { global: { plugins: [pinia] } });

    expect(wrapper.findAll(".branch-node")).toHaveLength(2);
    expect(wrapper.get(".branch-node.is-active").text()).toContain("Done");
    await wrapper.get('button[title="Fork from this entry"]').trigger("click");
    expect(store.forkActiveSession).toHaveBeenCalledWith("user-1", "Inspect runtime");
  });

  it("renders a deeply nested flat session without recursive stack growth", () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    const entries = Array.from({ length: 2000 }, (_, index) => ({
      id: `entry-${index}`,
      parentId: index === 0 ? "" : `entry-${index - 1}`,
      type: "message",
      timestamp: "",
      role: index % 2 === 0 ? "user" : "assistant",
      text: `Entry ${index}`,
      label: "",
    }));
    store.$patch({
      branchPanelOpen: true,
      activeThreadId: "thread-1",
      threads: [{
        id: "thread-1", title: "Long session", workspace: "repo", workspacePath: "D:\\repo", trust: "deny",
        status: "idle", started: true, generation: 1, sessionFile: "long.jsonl",
      }],
      sessionBranchesByThread: { "thread-1": { entries, leafId: "entry-1999" } },
    });

    const wrapper = mount(BranchDialog, { global: { plugins: [pinia] } });

    expect(wrapper.findAll(".branch-node")).toHaveLength(500);
    expect(wrapper.text()).toContain("Showing the first 500 entries.");
  });

  it("renders branch failures as an accessible warning dialog", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      branchPanelOpen: true,
      activeThreadId: "thread-1",
      threads: [{
        id: "thread-1", title: "Audit", workspace: "repo", workspacePath: "D:\\repo", trust: "deny",
        status: "idle", started: true, generation: 1, sessionFile: "one.jsonl",
      }],
      sessionBranchesErrorByThread: { "thread-1": "Maximum call stack size exceeded" },
    });
    const wrapper = mount(BranchDialog, { global: { plugins: [pinia] } });

    expect(wrapper.get('[role="alertdialog"]').text()).toContain("Unable to load session branches");
    expect(wrapper.get(".branch-warning code").text()).toContain("Maximum call stack size exceeded");
    expect(wrapper.find('button[title="Fork from this entry"]').exists()).toBe(false);
    await wrapper.get("footer button").trigger("click");
    expect(store.branchPanelOpen).toBe(false);
  });
});
