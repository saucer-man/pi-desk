import { flushPromises, mount } from "@vue/test-utils";
import { defineComponent, ref } from "vue";
import { describe, expect, it } from "vitest";
import { useModalFocus } from "./useModalFocus";

const TestModal = defineComponent({
  props: { canClose: { type: Boolean, default: true } },
  emits: ["close"],
  setup(props, { emit }) {
    const root = ref<HTMLElement | null>(null);
    useModalFocus(root, () => emit("close"), { canClose: () => props.canClose });
    return { root };
  },
  template: `<section ref="root" tabindex="-1"><button data-first autofocus>First</button><button data-last>Last</button></section>`,
});

describe("useModalFocus", () => {
  it("focuses the preferred control, traps tab, closes on escape, and restores focus", async () => {
    const trigger = document.createElement("button");
    document.body.appendChild(trigger);
    trigger.focus();
    const wrapper = mount(TestModal, { attachTo: document.body });
    await flushPromises();
    expect(document.activeElement).toBe(wrapper.get("[data-first]").element);

    (wrapper.get("[data-last]").element as HTMLElement).focus();
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Tab", bubbles: true, cancelable: true }));
    expect(document.activeElement).toBe(wrapper.get("[data-first]").element);

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
    expect(wrapper.emitted("close")).toHaveLength(1);
    wrapper.unmount();
    expect(document.activeElement).toBe(trigger);
    trigger.remove();
  });

  it("does not close while the modal operation is locked", async () => {
    const wrapper = mount(TestModal, { props: { canClose: false } });
    await flushPromises();
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
    expect(wrapper.emitted("close")).toBeUndefined();
    wrapper.unmount();
  });
});
