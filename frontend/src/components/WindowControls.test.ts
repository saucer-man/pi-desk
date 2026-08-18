import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import WindowControls from "./WindowControls.vue";

const runtime = vi.hoisted(() => ({
  close: vi.fn().mockResolvedValue(undefined),
  isMaximised: vi.fn().mockResolvedValue(false),
  minimise: vi.fn().mockResolvedValue(undefined),
  toggleMaximise: vi.fn().mockResolvedValue(undefined),
  listeners: new Map<string, () => void>(),
}));

vi.mock("@wailsio/runtime", () => ({
  Window: {
    Close: runtime.close,
    IsMaximised: runtime.isMaximised,
    Minimise: runtime.minimise,
    ToggleMaximise: runtime.toggleMaximise,
  },
  Events: {
    Types: { Common: { WindowMaximise: "maximise", WindowUnMaximise: "unmaximise", WindowRestore: "restore" } },
    On: (event: string, callback: () => void) => {
      runtime.listeners.set(event, callback);
      return () => runtime.listeners.delete(event);
    },
  },
}));

describe("WindowControls", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    runtime.listeners.clear();
    runtime.isMaximised.mockResolvedValue(false);
  });

  it("invokes the desktop window actions", async () => {
    const wrapper = mount(WindowControls, { props: { isWindows: true } });
    await flushPromises();

    await wrapper.get('button[aria-label="Minimise window"]').trigger("click");
    await wrapper.get('button[aria-label="Maximise window"]').trigger("click");
    await wrapper.get('button[aria-label="Close window"]').trigger("click");
    await flushPromises();

    expect(runtime.minimise).toHaveBeenCalledOnce();
    expect(runtime.toggleMaximise).toHaveBeenCalledOnce();
    expect(runtime.close).toHaveBeenCalledOnce();
  });

  it("tracks host maximise and restore events", async () => {
    const wrapper = mount(WindowControls, { props: { isWindows: true } });
    await flushPromises();

    runtime.listeners.get("maximise")?.();
    await wrapper.vm.$nextTick();
    expect(wrapper.get('button[aria-label="Restore window"]').attributes("title")).toBe("Restore window");

    runtime.listeners.get("unmaximise")?.();
    await wrapper.vm.$nextTick();
    expect(wrapper.get('button[aria-label="Maximise window"]').attributes("title")).toBe("Maximise window");

    wrapper.unmount();
    expect(runtime.listeners.size).toBe(0);
  });
});
