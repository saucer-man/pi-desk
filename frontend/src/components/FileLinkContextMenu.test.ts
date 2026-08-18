import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import FileLinkContextMenu from "./FileLinkContextMenu.vue";

const mocks = vi.hoisted(() => ({
  clipboard: vi.fn(),
  openFile: vi.fn(),
  openFileWith: vi.fn(),
  saveFileAs: vi.fn(),
  revealFile: vi.fn(),
}));

vi.mock("@wailsio/runtime", () => ({ Clipboard: { SetText: mocks.clipboard } }));
vi.mock("../services/repository", () => ({
  repositoryService: {
    openFile: mocks.openFile,
    openFileWith: mocks.openFileWith,
    saveFileAs: mocks.saveFileAs,
    revealFile: mocks.revealFile,
  },
}));

describe("FileLinkContextMenu", () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => { document.body.innerHTML = ""; });

  it("offers and executes every requested file action", async () => {
    const wrapper = mount(FileLinkContextMenu, {
      attachTo: document.body,
      global: { stubs: { Teleport: true } },
      props: {
        workspacePath: "D:\\repo",
        file: {
          relativePath: "reports/result.csv",
          absolutePath: "D:\\repo\\reports\\result.csv",
          name: "result.csv",
        },
        x: 120,
        y: 150,
      },
    });
    await flushPromises();
    const buttons = wrapper.findAll('[role="menuitem"]');
    expect(buttons.map((button) => button.text())).toEqual([
      "Open file", "Open with...", "Save as...", "Copy path", "Show in file manager",
    ]);

    await buttons[0].trigger("click");
    await buttons[1].trigger("click");
    await buttons[2].trigger("click");
    await buttons[3].trigger("click");
    await buttons[4].trigger("click");
    await flushPromises();

    expect(mocks.openFile).toHaveBeenCalledWith("D:\\repo", "reports/result.csv");
    expect(mocks.openFileWith).toHaveBeenCalledWith("D:\\repo", "reports/result.csv");
    expect(mocks.saveFileAs).toHaveBeenCalledWith("D:\\repo", "reports/result.csv", "D:\\repo\\reports\\result.csv");
    expect(mocks.clipboard).toHaveBeenCalledWith("D:\\repo\\reports\\result.csv");
    expect(mocks.revealFile).toHaveBeenCalledWith("D:\\repo", "reports/result.csv");
    wrapper.unmount();
  });

  it("keeps the menu open and reports native action failures", async () => {
    mocks.openFileWith.mockRejectedValueOnce(new Error("chooser unavailable"));
    const wrapper = mount(FileLinkContextMenu, {
      attachTo: document.body,
      global: { stubs: { Teleport: true } },
      props: {
        workspacePath: "D:\\repo",
        file: { relativePath: "main.go", absolutePath: "D:\\repo\\main.go", name: "main.go" },
        x: 12,
        y: 12,
      },
    });
    await flushPromises();
    await wrapper.findAll('[role="menuitem"]')[1].trigger("click");
    await flushPromises();
    expect(wrapper.get('[role="alert"]').text()).toBe("chooser unavailable");
    expect(wrapper.emitted("close")).toBeUndefined();
    wrapper.unmount();
  });
});
