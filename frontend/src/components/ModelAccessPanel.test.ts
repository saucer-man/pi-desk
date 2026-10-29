import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setAppLanguage } from "../i18n";
import type { ModelAuthEvent } from "../services/modelconfig";
import ModelAccessPanel from "./ModelAccessPanel.vue";

const mocks = vi.hoisted(() => ({ catalog: vi.fn(), login: vi.fn(), replyLogin: vi.fn(), saveAPIKey: vi.fn(), logout: vi.fn(), setDefault: vi.fn(), openAuthURL: vi.fn(), onAuth: vi.fn(), dispose: vi.fn() }));
vi.mock("../services/modelconfig", () => ({ modelConfigService: mocks, onModelAuthEvent: mocks.onAuth }));

const fixture = {
  agentDir: "C:/isolated/.pi/agent", defaultProvider: "", defaultModel: "",
  providers: [{ id: "openai", name: "OpenAI", oauth: true, apiKey: true, configured: true, stored: true, authType: "oauth", authSource: "stored" }],
  models: [{ provider: "openai", id: "test-model", name: "Test Model", source: "account" }],
};

describe("ModelAccessPanel", () => {
  let wrapper: VueWrapper | undefined;
  let event: (value: ModelAuthEvent) => void;
  beforeEach(() => {
    vi.resetAllMocks();
    setAppLanguage("en");
    mocks.catalog.mockResolvedValue(structuredClone(fixture));
    mocks.setDefault.mockResolvedValue({ ...structuredClone(fixture), defaultProvider: "openai", defaultModel: "test-model" });
    mocks.saveAPIKey.mockResolvedValue({ ...structuredClone(fixture), providers: [{ ...fixture.providers[0], authType: "api_key" }] });
    mocks.logout.mockResolvedValue({ ...structuredClone(fixture), models: [], providers: [{ ...fixture.providers[0], configured: false, stored: false }] });
    mocks.replyLogin.mockResolvedValue(undefined);
    mocks.openAuthURL.mockResolvedValue(undefined);
    mocks.onAuth.mockImplementation(callback => { event = callback; return mocks.dispose; });
    Object.defineProperty(HTMLDialogElement.prototype, "showModal", { configurable: true, value: function(this: HTMLDialogElement) { this.open = true; } });
    Object.defineProperty(HTMLDialogElement.prototype, "close", { configurable: true, value: function(this: HTMLDialogElement) { this.open = false; } });
  });
  afterEach(() => { wrapper?.unmount(); wrapper = undefined; document.body.innerHTML = ""; });
  const button = (text: string) => wrapper!.findAll("button").find(b => b.text() === text)!;

  it("shows existing Pi authorization and models without custom configuration", async () => {
    wrapper = mount(ModelAccessPanel);
    await flushPromises();
    expect(wrapper.text()).toContain("auth.json");
    expect(wrapper.text()).toContain("Test Model");
    expect(button("Sign in again").exists()).toBe(true);
    expect(wrapper.get('[data-testid="default-model"]').element).toHaveProperty("value", "");
  });

  it("persists both default identifiers and leaves an unavailable default visible", async () => {
    mocks.catalog.mockResolvedValueOnce({ ...fixture, defaultProvider: "openai", defaultModel: "missing-model" });
    wrapper = mount(ModelAccessPanel);
    await flushPromises();
    expect(wrapper.text()).toContain("Default openai/missing-model is unavailable");
    expect(mocks.setDefault).not.toHaveBeenCalled();
    await button("Set as default").trigger("click");
    await flushPromises();
    expect(mocks.setDefault).toHaveBeenCalledWith({ providerId: "openai", modelId: "test-model" });
    expect(wrapper.emitted("changed")).toHaveLength(1);
    expect(wrapper.text()).not.toContain("missing-model is unavailable");
  });

  it("requires confirmation before replacing shared OAuth with a key and clears the input", async () => {
    wrapper = mount(ModelAccessPanel);
    await flushPromises();
    await button("API Key").trigger("click");
    const input = wrapper.get('[data-testid="auth-api-key"]');
    expect(input.attributes("type")).toBe("password");
    await input.setValue("demo-replacement-key");
    await wrapper.get("form").trigger("submit");
    expect(mocks.saveAPIKey).not.toHaveBeenCalled();
    expect(wrapper.get(".model-auth-confirm").text()).toContain("Pi CLI is also affected");
    await wrapper.get(".model-auth-confirm button").trigger("click");
    await flushPromises();
    expect(mocks.saveAPIKey).toHaveBeenCalledWith({ providerId: "openai", apiKey: "demo-replacement-key" });
    expect(input.element).toHaveProperty("value", "");
  });

  it("binds browser and manual callback events to the active login", async () => {
    let finish!: (value: typeof fixture) => void;
    const request = Object.assign(new Promise<typeof fixture>(resolve => { finish = resolve; }), { cancel: vi.fn() });
    mocks.login.mockReturnValue(request);
    wrapper = mount(ModelAccessPanel);
    await flushPromises();
    await button("Sign in again").trigger("click");
    await flushPromises();
    const operationId = mocks.login.mock.calls[0][0].operationId;
    event({ operationId: "stale-operation", type: "auth_url", url: "https://example.test/old" });
    expect(mocks.openAuthURL).not.toHaveBeenCalled();
    event({ operationId, type: "auth_url", url: "https://example.test/authorize" });
    event({ operationId, type: "prompt", promptId: 4, promptType: "manual_code", message: "Callback URL" });
    await flushPromises();
    expect(mocks.openAuthURL).toHaveBeenCalledWith("https://example.test/authorize");
    const dialog = document.body.querySelector<HTMLDialogElement>("dialog")!;
    const input = dialog.querySelector("input")!;
    input.value = "test-callback";
    input.dispatchEvent(new Event("input", { bubbles: true }));
    await flushPromises();
    dialog.querySelector("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await flushPromises();
    expect(mocks.replyLogin).toHaveBeenCalledWith({ operationId, promptId: 4, value: "test-callback" });
    finish(structuredClone(fixture));
    await flushPromises();
    expect(document.body.querySelector("dialog")).toBeNull();
    expect(wrapper.emitted("changed")).toHaveLength(1);
  });

  it("cancels an unfinished native login when leaving settings", async () => {
    const request = Object.assign(new Promise(() => {}), { cancel: vi.fn() });
    mocks.login.mockReturnValue(request);
    wrapper = mount(ModelAccessPanel);
    await flushPromises();
    await button("Sign in again").trigger("click");
    await flushPromises();
    wrapper.unmount(); wrapper = undefined;
    expect(request.cancel).toHaveBeenCalledOnce();
    expect(mocks.dispose).toHaveBeenCalledOnce();
  });
});
