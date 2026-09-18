import { Events } from "@wailsio/runtime";
import { BrowserService } from "../../bindings/pi-desk/internal/appservice";
import type { BrowserStatus } from "../../bindings/pi-desk/internal/domain";

export interface BrowserEvent {
  type: "frame" | "attached" | "detached" | "navigated";
  sequence: number;
  dataB64?: string;
  cssWidth?: number;
  cssHeight?: number;
  url?: string;
  title?: string;
  error?: string;
}

export const browserService = {
  start(): Promise<BrowserStatus> {
    return BrowserService.Start();
  },
  openUrl(url: string): Promise<BrowserStatus> {
    return BrowserService.OpenURL({ url });
  },
  stop(): Promise<void> {
    return BrowserService.Stop();
  },
  click(x: number, y: number, button?: string, clickCount?: number): Promise<void> {
    return BrowserService.Click({ x, y, button, clickCount });
  },
  wheel(x: number, y: number, deltaX: number, deltaY: number): Promise<void> {
    return BrowserService.Scroll({ x, y, deltaX, deltaY });
  },
  key(key: string, modifiers: number): Promise<void> {
    return BrowserService.Key({ key, modifiers });
  },
  type(text: string): Promise<void> {
    return BrowserService.Type({ text });
  },
};

export function onBrowserEvent(callback: (event: BrowserEvent) => void): () => void {
  return Events.On("browser:event", (event) => callback(event.data as BrowserEvent));
}
