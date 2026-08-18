import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { toggleDebugMode } from "../services/desktop";
import { useAppStore } from "../stores/app";
import AppMenuBar from "./AppMenuBar.vue";

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn(), toggleDebugMode: vi.fn() }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

describe("AppMenuBar", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
    vi.mocked(toggleDebugMode).mockResolvedValue(true);
  });

  it("renders the product name without a duplicate mark or edit menu", () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const wrapper = mount(AppMenuBar, { global: { plugins: [pinia] } });

    expect(wrapper.get(".app-menu-identity").text()).toBe("Pi Desk");
    expect(wrapper.find(".brand-mark").exists()).toBe(false);
    expect(wrapper.find('button[aria-label="Collapse sidebar"]').exists()).toBe(false);
    expect(wrapper.findAll("button[aria-haspopup='menu']")).toHaveLength(1);
    expect(wrapper.get("button[aria-haspopup='menu']").text()).toBe("Help");
  });

  it("toggles debug mode and opens About Pi Desk from the help menu", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    const wrapper = mount(AppMenuBar, { attachTo: document.body, global: { plugins: [pinia] } });
    const help = wrapper.get("button[aria-haspopup='menu']");

    await help.trigger("click");
    let items = wrapper.findAll("[role='menuitem']");
    expect(items).toHaveLength(2);
    expect(items[0].text()).toBe("Open debug mode");
    expect(items[1].text()).toBe("About Pi Desk");

    await items[0].trigger("click");
    expect(toggleDebugMode).toHaveBeenCalledOnce();
    await help.trigger("click");
    items = wrapper.findAll("[role='menuitem']");
    expect(items[0].text()).toBe("Close debug mode");

    await items[1].trigger("click");
    expect(store.aboutOpen).toBe(true);
    wrapper.unmount();
  });
});
