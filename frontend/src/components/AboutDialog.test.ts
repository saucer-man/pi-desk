import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { RuntimeState } from "../../bindings/pi-desk/internal/domain";
import { useAppStore } from "../stores/app";
import AboutDialog from "./AboutDialog.vue";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

describe("AboutDialog", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
  });

  it("describes Pi Desk and reports runtime versions", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      aboutOpen: true,
      bootstrap: {
        productName: "Pi Desk",
        appVersion: "0.1.0",
        wailsVersion: "v3.0.0-beta.6",
        workingDirectory: "D:\\repo",
        runtime: { state: RuntimeState.RuntimeReady, version: "0.83.0", command: "pi.cmd" },
        window: { x: 0, y: 0, width: 1000, height: 700, maximized: false, valid: true },
      },
    });
    const wrapper = mount(AboutDialog, { attachTo: document.body, global: { plugins: [pinia] } });

    expect(wrapper.get("[role='dialog']").attributes("aria-labelledby")).toBe("about-title");
    expect(wrapper.text()).toContain("A desktop interface for Pi");
    expect(wrapper.text()).toContain("0.1.0");
    expect(wrapper.text()).toContain("0.83.0");
    expect(wrapper.text()).toContain("v3.0.0-beta.6");

    await wrapper.get("button[autofocus]").trigger("click");
    expect(store.aboutOpen).toBe(false);
    wrapper.unmount();
  });
});
