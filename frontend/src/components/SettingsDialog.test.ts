import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { describe, expect, it, vi } from "vitest";
import { RuntimeState } from "../../bindings/pi-desk/internal/domain";
import { useAppStore } from "../stores/app";
import SettingsDialog from "./SettingsDialog.vue";

const modelConfigMocks = vi.hoisted(() => ({ selectable: vi.fn() }));
const desktopMocks = vi.hoisted(() => ({ getBootstrapState: vi.fn(), maintainPi: vi.fn() }));

vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => desktopMocks);
vi.mock("../services/modelconfig", () => ({ modelConfigService: { selectable: modelConfigMocks.selectable } }));
vi.mock("../services/prompts", () => ({ promptTemplateService: { list: vi.fn(), get: vi.fn(), upsert: vi.fn(), delete: vi.fn() } }));
vi.mock("../services/skills", () => ({ managedSkillService: { list: vi.fn(), get: vi.fn(), create: vi.fn(), update: vi.fn(), delete: vi.fn() } }));
vi.mock("../services/extensions", () => ({ piExtensionService: { list: vi.fn().mockResolvedValue({ extensions: [], todo: {} }), installTodo: vi.fn(), removeTodo: vi.fn() } }));
vi.mock("../services/mcpconfig", () => ({ mcpConfigService: { list: vi.fn(), get: vi.fn(), upsert: vi.fn(), delete: vi.fn(), engineStatus: vi.fn(), importCandidates: vi.fn() } }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

describe("SettingsDialog", () => {
  modelConfigMocks.selectable.mockResolvedValue([]);

  it("updates persisted network and task defaults", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.settingsOpen = true;
    store.preferencesChanged = vi.fn();
    store.appearanceChanged = vi.fn();
    store.closeSettings = vi.fn();
    store.syncAndRestoreSessions = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(SettingsDialog, { global: { plugins: [pinia] } });

    expect(wrapper.find(".dialog-backdrop").exists()).toBe(false);
    expect(wrapper.get(".settings-page").attributes("role")).toBeUndefined();
    expect(wrapper.get("h1").text()).toBe("General");
    expect(wrapper.find(".settings-page-header").exists()).toBe(false);
    expect(wrapper.find(".settings-project-path").exists()).toBe(false);
    expect(wrapper.text()).toContain("Basic settings");
    expect(wrapper.text()).toContain("Agent capabilities");
    expect(wrapper.text()).toContain("Data and statistics");
    expect(wrapper.get(".settings-layout").classes()).not.toContain("px-5");
    expect(wrapper.get(".settings-page").classes()).toEqual(expect.arrayContaining([
      "[&_.text-button]:!h-[34px]",
      "[&_.text-button]:!min-h-[34px]",
      "[&_.text-button]:!text-sm",
      "[&_.icon-button]:!size-7",
      "[&_.text-button_svg]:!size-3.5",
      "[&_select]:!h-[34px]",
      "[&_select]:!text-sm",
      "[&_input:not([type=checkbox]):not([type=radio])]:!h-[34px]",
      "[&_input[type=checkbox]]:!size-3.5",
      "[&_.setting-row>input[type=checkbox]]:!h-[22px]",
      "[&_.setting-row>input[type=checkbox]]:!w-[38px]",
      "[&_textarea]:!text-sm",
    ]));
    expect(wrapper.get(".settings-sections").classes()).not.toContain("[&>section]:px-1");
    expect(wrapper.findAll(".settings-section-title")).toHaveLength(4);
    expect(wrapper.findAll(".settings-card")).toHaveLength(4);
    expect(wrapper.find(".settings-card .settings-section-title").exists()).toBe(false);
    expect(wrapper.text()).not.toContain("Open a task to start Pi and change runtime behavior.");
    const updateRow = wrapper.get('[data-testid="update-check-row"]');
    expect(updateRow.find('input[type="checkbox"]').exists()).toBe(false);
    expect(updateRow.get('[data-testid="check-updates-now"]').text()).toContain("Check now");
    expect(updateRow.text()).toContain("Not checked");
    const syncRow = wrapper.get('[data-testid="sync-local-sessions-row"]');
    expect(syncRow.text()).toContain("Sync local sessions");
    await syncRow.get("button").trigger("click");
    expect(store.syncAndRestoreSessions).toHaveBeenCalledOnce();

    const checkboxes = wrapper.findAll('input[type="checkbox"]');
    await checkboxes[0].setValue(false);
    await checkboxes[1].setValue(true);

    await wrapper.findAll(".settings-nav button").find((button) => button.text() === "Appearance")!.trigger("click");
    expect(wrapper.get("h1").text()).toBe("Appearance");
    expect(wrapper.get(".settings-section-title").text()).toBe("Appearance");
    expect(wrapper.find(".settings-card .settings-section-title").exists()).toBe(false);
    expect(wrapper.findAll(".appearance-settings .settings-card .setting-row")).toHaveLength(9);
    expect(wrapper.findAll(".appearance-select")).toHaveLength(7);
    expect(wrapper.findAll('select[aria-label="Light code theme"] option')).toHaveLength(10);
    await wrapper.get('select[aria-label="Theme"]').setValue("light");
    await wrapper.get('select[aria-label="Font"]').setValue("mono");
    await wrapper.get('select[aria-label="Font size"]').setValue("16");
    await wrapper.get('select[aria-label="Light code theme"]').setValue("catppuccin-latte");
    await wrapper.get('select[aria-label="Dark code theme"]').setValue("catppuccin-mocha");
    await wrapper.get('select[aria-label="Code font size"]').setValue("14");
    const codeRows = wrapper.findAll(".appearance-settings .setting-row");
    await codeRows.find((row) => row.text().includes("Show line numbers"))!.get('input[type="checkbox"]').setValue(false);
    await codeRows.find((row) => row.text().includes("Wrap long lines"))!.get('input[type="checkbox"]').setValue(true);

    expect(store.offlineMode).toBe(false);
    expect(store.proxyEnabled).toBe(true);
    expect(store.appearance).toBe("light");
    expect(store.interfaceFont).toBe("mono");
    expect(store.interfaceFontSize).toBe(16);
    expect(store.lightCodeTheme).toBe("catppuccin-latte");
    expect(store.darkCodeTheme).toBe("catppuccin-mocha");
    expect(store.codeFontSize).toBe(14);
    expect(store.showCodeLineNumbers).toBe(false);
    expect(store.wrapCodeLines).toBe(true);
    expect(store.preferencesChanged).toHaveBeenCalledTimes(9);
    expect(store.appearanceChanged).toHaveBeenCalledOnce();
    await wrapper.get('[data-testid="settings-back"]').trigger("click");
    expect(store.closeSettings).toHaveBeenCalledOnce();
  });

  it("hides an expected unregistered-workspace persistence error", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.settingsError = "workspace is not registered";
    const wrapper = mount(SettingsDialog, { global: { plugins: [pinia] } });

    expect(wrapper.text()).not.toContain("workspace is not registered");
    store.settingsError = "state file is locked";
    await flushPromises();
    expect(wrapper.text()).toContain("state file is locked");
  });

  it("shows loaded Pi resources as a separate read-only view", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{
        id: "thread-1", title: "Runtime audit", workspace: "repo", workspacePath: "D:\\repo", trust: "approve",
        status: "idle", started: true, generation: 1,
      }],
      activeThreadId: "thread-1",
      commandsByThread: { "thread-1": [
        { name: "skill:review", description: "Review code", source: "skill", location: "user", path: "C:\\Users\\dev\\.pi\\agent\\skills\\review\\SKILL.md" },
        { name: "deploy", description: "Deploy project", source: "extension", path: "D:\\repo\\.pi\\extensions\\deploy.ts" },
        { name: "fix-tests", description: "Fix tests", source: "prompt", location: "project", path: "D:\\repo\\.pi\\prompts\\fix-tests.md" },
      ] },
    });
    store.chooseModel = vi.fn().mockResolvedValue(undefined);
    store.refreshModels = vi.fn().mockResolvedValue(undefined);
    store.refreshThinkingLevels = vi.fn().mockResolvedValue(undefined);
    store.refreshCommands = vi.fn().mockResolvedValue(undefined);
    store.setSteeringMode = vi.fn().mockResolvedValue(undefined);
    store.setAutoCompaction = vi.fn().mockResolvedValue(undefined);
    store.setAutoRetry = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(SettingsDialog, { global: { plugins: [pinia] } });

    expect(wrapper.find(".settings-project-path").exists()).toBe(false);

    await wrapper.get('select[aria-label="Steering queue processing"]').setValue("all");
    await flushPromises();
    expect(store.setSteeringMode).toHaveBeenCalledWith("all");

    expect(wrapper.findAll(".settings-nav button").some((button) => button.text() === "Models")).toBe(false);
    expect(wrapper.findAll(".settings-nav button").filter((button) => button.text() === "Model management")).toHaveLength(1);
    expect(wrapper.findAll(".settings-nav button").filter((button) => button.text() === "Extensions")).toHaveLength(1);

    await wrapper.findAll(".settings-nav button").find((button) => button.text() === "Extensions")!.trigger("click");
    expect(wrapper.find(".settings-content-header").exists()).toBe(false);
    expect(wrapper.find(".settings-fill-body").exists()).toBe(true);

    await wrapper.findAll(".settings-nav button").find((button) => button.text() === "Runtime resources")!.trigger("click");
    expect(wrapper.find(".settings-content-header").exists()).toBe(false);
    expect(wrapper.find(".runtime-resources-body").exists()).toBe(true);
    expect(wrapper.findAll(".resource-row")).toHaveLength(3);
    expect(wrapper.text()).toContain("SKILL.md");
    await wrapper.findAll(".resource-filters button")[1].trigger("click");
    expect(wrapper.findAll(".resource-row")).toHaveLength(1);
    expect(wrapper.text()).toContain("/skill:review");

    expect(wrapper.find('button[title="Refresh resources"]').exists()).toBe(false);
  });

  it("keeps only Pi self-update in Runtime and confirms before invoking the backend", async () => {
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      bootstrap: {
        productName: "Pi Desk", appVersion: "0.1.0", wailsVersion: "v3", workingDirectory: "D:\\repo",
        runtime: { state: RuntimeState.RuntimeReady, command: "C:\\tools\\pi.cmd", version: "0.84.0" },
        window: { x: 0, y: 0, width: 1000, height: 700, maximized: false, valid: true },
      },
    });
    desktopMocks.maintainPi.mockResolvedValue({
      action: "update-self", command: "C:\\tools\\pi.cmd", output: "updated",
      runtime: { state: RuntimeState.RuntimeReady, command: "C:\\tools\\pi.cmd", version: "0.85.0" },
    });
    const wrapper = mount(SettingsDialog, { global: { plugins: [pinia] } });

    const runtime = wrapper.get(".runtime-settings");
    expect(runtime.get('[data-testid="update-pi"]').text()).toContain("Update Pi");
    expect(wrapper.find('[data-testid="update-pi-all"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="update-pi-extensions"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="update-pi-models"]').exists()).toBe(false);
    expect(wrapper.text()).not.toContain("Pi maintenance");

    await wrapper.get('[data-testid="update-pi"]').trigger("click");
    expect(desktopMocks.maintainPi).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("Update Pi?");

    await wrapper.get('[data-testid="confirm-pi-maintenance"]').trigger("click");
    await flushPromises();
    expect(desktopMocks.maintainPi).toHaveBeenCalledWith("update-self");
    expect(store.bootstrap?.runtime.version).toBe("0.85.0");
    expect(wrapper.text()).toContain("updated");
  });

  it("does not update Pi while a task is actively running", async () => {
    desktopMocks.maintainPi.mockClear();
    const pinia = createPinia();
    setActivePinia(pinia);
    const store = useAppStore();
    store.$patch({
      threads: [{ id: "running-thread", title: "Running", workspace: "repo", workspacePath: "D:\\repo", trust: "approve", status: "running", started: true, generation: 1 }],
      activeThreadId: "running-thread",
      bootstrap: {
        productName: "Pi Desk", appVersion: "0.1.0", wailsVersion: "v3", workingDirectory: "D:\\repo",
        runtime: { state: RuntimeState.RuntimeReady, command: "C:\\tools\\pi.cmd", version: "0.84.0" },
        window: { x: 0, y: 0, width: 1000, height: 700, maximized: false, valid: true },
      },
    });
    const wrapper = mount(SettingsDialog, { global: { plugins: [pinia] } });

    await wrapper.get('[data-testid="update-pi"]').trigger("click");
    await wrapper.get('[data-testid="confirm-pi-maintenance"]').trigger("click");
    await flushPromises();

    expect(desktopMocks.maintainPi).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("A Pi task is still running");
  });
});
