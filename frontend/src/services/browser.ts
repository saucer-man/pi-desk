import { Events } from "@wailsio/runtime";
import { BrowserService } from "../../bindings/pi-desk/internal/appservice";
import type { BrowserStatus } from "../../bindings/pi-desk/internal/domain";

export interface BrowserEvent {
  tabId: string;
  threadId: string;
  type: "opened" | "state" | "closed";
  status?: BrowserStatus;
}
export const browserService = {
  startTab(tabId: string, threadId: string, url = "") { return BrowserService.StartTab({ tabId, threadId, url }); },
  closeTab(id: string) { return BrowserService.CloseTab(id); },
  openUrl(url: string, tabId: string) { return BrowserService.OpenURL({ url, tabId }); },
  command(id: string, command: string) { return BrowserService.Command(id, command); },
  keepTab(id: string) { return BrowserService.KeepTab(id); },
  bounds(tabId: string, rect: { x: number; y: number; width: number; height: number }, visible: boolean) {
    return BrowserService.SetBounds({ tabId, ...rect, visible });
  },
};
export function onBrowserEvent(callback: (event: BrowserEvent) => void): () => void {
  return Events.On("browser:event", (event) => callback(event.data as BrowserEvent));
}
