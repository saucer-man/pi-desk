import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia } from "pinia";
import { PiExtensionOrigin, PiPackageScope } from "../../bindings/pi-desk/internal/domain";
import ExtensionManager from "./ExtensionManager.vue";

const extensionMocks = vi.hoisted(() => ({
  list: vi.fn(), installTodo: vi.fn(), removeTodo: vi.fn(), installGoal: vi.fn(), removeGoal: vi.fn(),
  installComputerUse: vi.fn(), removeComputerUse: vi.fn(), installSubagents: vi.fn(), removeSubagents: vi.fn(),
  installBrowser: vi.fn(), removeBrowser: vi.fn(), listPackages: vi.fn(),
  installPackage: vi.fn(), updatePackage: vi.fn(), removePackage: vi.fn(), setPackageEnabled: vi.fn(),
}));
vi.mock("../services/extensions", () => ({ piExtensionService: extensionMocks }));

const baseSnapshot = {
  globalDirectory: "C:\\Users\\dev\\.pi\\agent\\extensions",
  settingsPath: "C:\\Users\\dev\\.pi\\agent\\settings.json",
  extensions: [
    { name: "local", source: "local.ts", path: "C:\\Users\\dev\\.pi\\agent\\extensions\\local.ts", origin: PiExtensionOrigin.PiExtensionOriginGlobal },
    { name: "context-mode", source: "npm:context-mode", origin: PiExtensionOrigin.PiExtensionOriginPackage },
  ],
  todo: {
    path: "C:\\Users\\dev\\.pi\\agent\\extensions\\pi-desk-todo.ts",
    installed: false,
    updateAvailable: false,
    legacyPath: "C:\\Users\\dev\\.pi\\agent\\extensions\\pi-deck-todo.ts",
    legacyInstalled: true,
  },
  goal: {
    path: "C:\\Users\\dev\\.pi\\agent\\extensions\\pi-desk-goal.ts",
    installed: false,
    updateAvailable: false,
  },
  computerUse: {
    path: "C:\\Users\\dev\\.pi\\agent\\extensions\\pi-desk-computer-use.ts",
    installed: false,
    updateAvailable: false,
  },
  subagents: {
    path: "C:\\Users\\dev\\.pi\\agent\\extensions\\pi-desk-subagents.ts",
    installed: false,
    updateAvailable: false,
  },
  browser: {
    path: "C:\\Users\\dev\\.pi\\agent\\extensions\\pi-desk-browser.ts",
    installed: false,
    updateAvailable: false,
  },
};
const packageSnapshot = {
  globalSettingsPath: "C:\\Users\\dev\\.pi\\agent\\settings.json",
  projectEnabled: false,
  projectNotice: "select a workspace to manage project packages",
  packages: [{ source: "npm:context-mode", scope: PiPackageScope.PiPackageScopeGlobal, enabled: true }],
};

describe("ExtensionManager", () => {
  beforeEach(() => {
    Object.values(extensionMocks).forEach((mock) => mock.mockReset());
    extensionMocks.listPackages.mockResolvedValue(packageSnapshot);
  });

  it("lists Pi extension sources and migrates the legacy Todo extension", async () => {
    extensionMocks.list
      .mockResolvedValueOnce(baseSnapshot)
      .mockResolvedValueOnce({
        ...baseSnapshot,
        extensions: [...baseSnapshot.extensions, {
          name: "pi-desk-todo", source: "pi-desk-todo.ts",
          path: baseSnapshot.todo.path, origin: PiExtensionOrigin.PiExtensionOriginGlobal,
        }],
        todo: { ...baseSnapshot.todo, installed: true, legacyInstalled: false },
      });
    extensionMocks.installTodo.mockResolvedValue({
      todo: { ...baseSnapshot.todo, installed: true, legacyInstalled: false },
      replacedLegacy: true,
    });

    const wrapper = mount(ExtensionManager, { global: { plugins: [createPinia()] } });
    await flushPromises();

    expect(wrapper.find(".settings-content-header").exists()).toBe(false);
    expect(wrapper.find(".settings-fill-body").exists()).toBe(true);
    expect(wrapper.text()).toContain("Pi Desk Todo");
    expect(wrapper.text()).toContain("Legacy PiDeck Todo");
    expect(wrapper.findAll(".extension-recommended")).toHaveLength(1);
    expect(wrapper.findAll(".extension-feature-row")).toHaveLength(6);
    expect(wrapper.find(".installed-extensions").exists()).toBe(false);
    expect(wrapper.text()).not.toContain("Configured extensions and packages");
    expect(wrapper.get('[data-testid="install-todo-extension"]').attributes("disabled")).toBeUndefined();
    expect(wrapper.get('[data-testid="remove-todo-extension"]').attributes("disabled")).toBeDefined();

    await wrapper.get('[data-testid="install-todo-extension"]').trigger("click");
    await flushPromises();

    expect(extensionMocks.installTodo).toHaveBeenCalledOnce();
    expect(wrapper.text()).toContain("legacy PiDeck Todo extension was disabled");
    expect(wrapper.get('[data-testid="install-todo-extension"]').text()).toContain("Installed");
    expect(wrapper.get('[data-testid="install-todo-extension"]').attributes("disabled")).toBeDefined();
    expect(wrapper.get('[data-testid="remove-todo-extension"]').text()).toContain("Remove");
    expect(wrapper.get('[data-testid="remove-todo-extension"]').attributes("disabled")).toBeUndefined();
  });

  it("requires confirmation before removing Pi Desk Todo", async () => {
    extensionMocks.list.mockResolvedValue({
      ...baseSnapshot,
      todo: { ...baseSnapshot.todo, installed: true, legacyInstalled: false },
    });
    extensionMocks.removeTodo.mockResolvedValue(undefined);
    const wrapper = mount(ExtensionManager, { global: { plugins: [createPinia()] } });
    await flushPromises();

    const remove = wrapper.get('[data-testid="remove-todo-extension"]');
    await remove.trigger("click");
    expect(extensionMocks.removeTodo).not.toHaveBeenCalled();
    expect(remove.text()).toContain("Confirm remove");

    await remove.trigger("click");
    await flushPromises();
    expect(extensionMocks.removeTodo).toHaveBeenCalledOnce();
  });

  it("requires confirmation before removing Pi Desk Goal", async () => {
    extensionMocks.list.mockResolvedValue({
      ...baseSnapshot,
      goal: { ...baseSnapshot.goal, installed: true },
    });
    extensionMocks.removeGoal.mockResolvedValue(undefined);
    const wrapper = mount(ExtensionManager, { global: { plugins: [createPinia()] } });
    await flushPromises();

    expect(wrapper.text()).toContain("Pi Desk Goal");
    const remove = wrapper.get('[data-testid="remove-goal-extension"]');
    await remove.trigger("click");
    expect(extensionMocks.removeGoal).not.toHaveBeenCalled();
    expect(remove.text()).toContain("Confirm remove");

    await remove.trigger("click");
    await flushPromises();
    expect(extensionMocks.removeGoal).toHaveBeenCalledOnce();
  });

  it("requires confirmation before removing Pi Desk Computer Use", async () => {
    extensionMocks.list.mockResolvedValue({
      ...baseSnapshot,
      computerUse: { ...baseSnapshot.computerUse, installed: true },
    });
    extensionMocks.removeComputerUse.mockResolvedValue(undefined);
    const wrapper = mount(ExtensionManager, { global: { plugins: [createPinia()] } });
    await flushPromises();

    expect(wrapper.text()).toContain("Pi Desk Computer Use");
    const remove = wrapper.get('[data-testid="remove-computer-use-extension"]');
    await remove.trigger("click");
    expect(extensionMocks.removeComputerUse).not.toHaveBeenCalled();
    expect(remove.text()).toContain("Confirm remove");

    await remove.trigger("click");
    await flushPromises();
    expect(extensionMocks.removeComputerUse).toHaveBeenCalledOnce();
  });

  it("requires confirmation before removing Pi Desk Subagents", async () => {
    extensionMocks.list.mockResolvedValue({
      ...baseSnapshot,
      subagents: { ...baseSnapshot.subagents, installed: true },
    });
    extensionMocks.removeSubagents.mockResolvedValue(undefined);
    const wrapper = mount(ExtensionManager, { global: { plugins: [createPinia()] } });
    await flushPromises();

    expect(wrapper.text()).toContain("Pi Desk Subagents");
    const remove = wrapper.get('[data-testid="remove-subagents-extension"]');
    await remove.trigger("click");
    expect(extensionMocks.removeSubagents).not.toHaveBeenCalled();
    expect(remove.text()).toContain("Confirm remove");

    await remove.trigger("click");
    await flushPromises();
    expect(extensionMocks.removeSubagents).toHaveBeenCalledOnce();
  });

  it("installs Pi Desk Computer Use from the extension card", async () => {
    extensionMocks.list
      .mockResolvedValueOnce(baseSnapshot)
      .mockResolvedValueOnce({
        ...baseSnapshot,
        computerUse: { ...baseSnapshot.computerUse, installed: true },
      });
    extensionMocks.installComputerUse.mockResolvedValue({ ...baseSnapshot.computerUse, installed: true });
    const wrapper = mount(ExtensionManager, { global: { plugins: [createPinia()] } });
    await flushPromises();

    await wrapper.get('[data-testid="install-computer-use-extension"]').trigger("click");
    await flushPromises();

    expect(extensionMocks.installComputerUse).toHaveBeenCalledOnce();
    expect(wrapper.get('[data-testid="remove-computer-use-extension"]').text()).toContain("Remove");
  });

  it("offers to install the pi-mcp-adapter engine from the plugin list", async () => {
    extensionMocks.list.mockResolvedValue(baseSnapshot);
    extensionMocks.installPackage.mockResolvedValue({ output: "installed" });
    const wrapper = mount(ExtensionManager, { global: { plugins: [createPinia()] } });
    await flushPromises();

    const row = wrapper.get('[data-testid="mcp-adapter-extension-row"]');
    expect(row.get('[data-testid="install-mcp-adapter-extension"]').text()).toContain("Install");
    expect(row.get('[data-testid="remove-mcp-adapter-extension"]').attributes("disabled")).toBeDefined();
    await row.get('[data-testid="install-mcp-adapter-extension"]').trigger("click");
    await flushPromises();
    expect(extensionMocks.installPackage).toHaveBeenCalledWith({ source: "npm:pi-mcp-adapter", scope: PiPackageScope.PiPackageScopeGlobal, workspacePath: "" });
  });

  it("marks the pi-mcp-adapter engine as removable once installed", async () => {
    extensionMocks.list.mockResolvedValue(baseSnapshot);
    extensionMocks.listPackages.mockResolvedValue({
      ...packageSnapshot,
      packages: [...packageSnapshot.packages, { source: "npm:pi-mcp-adapter@1.0.0", scope: PiPackageScope.PiPackageScopeGlobal, enabled: true }],
    });
    const wrapper = mount(ExtensionManager, { global: { plugins: [createPinia()] } });
    await flushPromises();

    const row = wrapper.get('[data-testid="mcp-adapter-extension-row"]');
    expect(row.get('[data-testid="install-mcp-adapter-extension"]').text()).toContain("Installed");
    expect(row.get('[data-testid="install-mcp-adapter-extension"]').attributes("disabled")).toBeDefined();
    expect(row.get('[data-testid="remove-mcp-adapter-extension"]').text()).toContain("Remove");
    expect(row.get('[data-testid="remove-mcp-adapter-extension"]').attributes("disabled")).toBeUndefined();
  });
});
