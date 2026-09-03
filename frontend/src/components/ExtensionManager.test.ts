import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia } from "pinia";
import { PiExtensionOrigin, PiPackageScope } from "../../bindings/pi-desk/internal/domain";
import ExtensionManager from "./ExtensionManager.vue";

const extensionMocks = vi.hoisted(() => ({
  list: vi.fn(), installTodo: vi.fn(), removeTodo: vi.fn(), listPackages: vi.fn(),
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
    expect(wrapper.text()).toContain("local");
    expect(wrapper.text()).toContain("context-mode");
    expect(wrapper.text()).toContain("Global file");
    expect(wrapper.text()).toContain("Pi package");

    await wrapper.get('[data-testid="install-todo-extension"]').trigger("click");
    await flushPromises();

    expect(extensionMocks.installTodo).toHaveBeenCalledOnce();
    expect(wrapper.text()).toContain("legacy PiDeck Todo extension was disabled");
    expect(wrapper.get('[data-testid="remove-todo-extension"]').text()).toContain("Remove");
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

  it("installs, toggles, updates, and removes Pi packages", async () => {
    extensionMocks.list.mockResolvedValue(baseSnapshot);
    extensionMocks.listPackages.mockResolvedValue(packageSnapshot);
    extensionMocks.installPackage.mockResolvedValue({ output: "installed" });
    extensionMocks.setPackageEnabled.mockResolvedValue(undefined);
    extensionMocks.updatePackage.mockResolvedValue({ output: "updated" });
    extensionMocks.removePackage.mockResolvedValue({ output: "removed" });
    Object.defineProperty(window, "confirm", { configurable: true, value: vi.fn(() => true) });
    const wrapper = mount(ExtensionManager, { global: { plugins: [createPinia()] } });
    await flushPromises();

    await wrapper.get('input[placeholder="npm:package, git URL, or local path"]').setValue("npm:new-package");
    await wrapper.get(".extension-package-install button").trigger("click");
    await flushPromises();
    expect(extensionMocks.installPackage).toHaveBeenCalledWith(expect.objectContaining({ source: "npm:new-package", scope: PiPackageScope.PiPackageScopeGlobal }));

    const rowButtons = wrapper.findAll(".package-row button");
    await rowButtons[0].trigger("click");
    await flushPromises();
    expect(extensionMocks.setPackageEnabled).toHaveBeenCalledWith(expect.objectContaining({ source: "npm:context-mode", enabled: false }));
    await rowButtons[1].trigger("click");
    await flushPromises();
    expect(extensionMocks.updatePackage).toHaveBeenCalled();
    await rowButtons[2].trigger("click");
    await flushPromises();
    expect(extensionMocks.removePackage).toHaveBeenCalled();
  });
});
