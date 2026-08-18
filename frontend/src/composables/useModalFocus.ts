import { nextTick, onBeforeUnmount, onMounted, type Ref } from "vue";

const focusableSelector = [
  "button:not([disabled])",
  "a[href]",
  "input:not([disabled])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  "[tabindex]:not([tabindex='-1'])",
].join(",");

interface ModalFocusOptions {
  canClose?: () => boolean;
}

export function useModalFocus(root: Ref<HTMLElement | null>, close: () => void, options: ModalFocusOptions = {}) {
  let previousFocus: HTMLElement | null = null;

  function focusableElements(): HTMLElement[] {
    return [...(root.value?.querySelectorAll<HTMLElement>(focusableSelector) ?? [])]
      .filter((element) => !element.hidden && element.getAttribute("aria-hidden") !== "true");
  }

  function onKeydown(event: KeyboardEvent) {
    if (event.key === "Escape" && (options.canClose?.() ?? true)) {
      event.preventDefault();
      event.stopPropagation();
      close();
      return;
    }
    if (event.key !== "Tab") return;
    const elements = focusableElements();
    if (!elements.length) {
      event.preventDefault();
      root.value?.focus();
      return;
    }
    const first = elements[0];
    const last = elements[elements.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  onMounted(async () => {
    previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    document.addEventListener("keydown", onKeydown, true);
    await nextTick();
    const preferred = root.value?.querySelector<HTMLElement>("[autofocus]");
    (preferred ?? focusableElements()[0] ?? root.value)?.focus();
  });

  onBeforeUnmount(() => {
    document.removeEventListener("keydown", onKeydown, true);
    if (previousFocus?.isConnected) previousFocus.focus();
  });
}
