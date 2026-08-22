import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { describe, expect, it, vi } from "vitest";
import { useAppStore } from "../stores/app";
import RemoteReconnectDialog from "./RemoteReconnectDialog.vue";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));
vi.mock("../services/remoteWorkspaces", () => ({ remoteWorkspaceService: { resume: vi.fn() } }));

describe("RemoteReconnectDialog", () => {
  it("shows the persisted remote root and cancellation stays offline", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.workspaces = [{
      id: "workspace-remote", name: "remote", path: "", kind: "ssh", targetId: "target-remote",
      remoteRoot: "/srv/repo", trust: "approve",
    }];
    store.threads = [{
      id: "thread-remote", title: "Remote task", workspace: "remote", workspaceId: "workspace-remote",
      workspacePath: "", trust: "approve", status: "idle", started: false, generation: 0,
    }];
    store.remoteReconnectThreadId = "thread-remote";
    store.remoteReconnectOpen = true;
    store.remoteReconnectProgress = [
      { id: "stop", label: "remoteReconnect.stepStop", status: "complete" },
      { id: "connect", label: "remoteReconnect.stepConnect", status: "active" },
      { id: "restore", label: "remoteReconnect.stepRestore", status: "pending" },
    ];
    store.confirmRemoteReconnect = vi.fn();
    const wrapper = mount(RemoteReconnectDialog, { global: { plugins: [pinia] } });

    expect(wrapper.text()).toContain("/srv/repo");
    expect(wrapper.findAll(".remote-progress-step")).toHaveLength(3);
    expect(wrapper.get(".remote-setup-progress").attributes("role")).toBe("status");
    await wrapper.find("footer .text-button").trigger("click");
    expect(store.remoteReconnectOpen).toBe(false);
    expect(store.confirmRemoteReconnect).not.toHaveBeenCalled();
  });
});
