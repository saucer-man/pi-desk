import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "../stores/app";
import ExportResultDialog from "./ExportResultDialog.vue";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

describe("ExportResultDialog", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
  });

  it("shows the exported path and closes without adding a conversation message", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.exportDialogOpen = true;
    store.exportResultPath = "D:\\work\\repo\\session.html";
    const wrapper = mount(ExportResultDialog, { global: { plugins: [pinia] } });

    expect(wrapper.text()).toContain("Export complete");
    expect(wrapper.text()).toContain("D:\\work\\repo\\session.html");
    await wrapper.get("footer button").trigger("click");
    expect(store.exportDialogOpen).toBe(false);
  });
});
