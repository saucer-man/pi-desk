import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "../stores/app";
import ModelManager from "./ModelManager.vue";

const mocks = vi.hoisted(() => ({
  selectable: vi.fn(),
  get: vi.fn(),
  upsert: vi.fn(),
  addModels: vi.fn(),
  delete: vi.fn(),
  discover: vi.fn(),
  test: vi.fn(),
  quota: vi.fn(),
}));

vi.mock("../services/modelconfig", () => ({ modelConfigService: mocks }));
vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/desktop", () => ({ getBootstrapState: vi.fn() }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

const configured = {
  path: "C:\\Users\\dev\\.pi\\agent\\models.json",
  providers: [{
    id: "custom-openai",
    baseUrl: "https://gateway.example.com/v1",
    api: "openai-responses",
    apiKey: "sk-visible-local-key",
    headers: { "User-Agent": "existing-agent", "X-Channel": "desktop" },
    compatJson: "",
    models: [{
      id: "gpt-test",
      name: "GPT Test",
      contextWindow: 128000,
      maxTokens: 16384,
      reasoning: true,
      imageInput: true,
      thinkingLevelMapJson: `{
  "xhigh": "xhigh"
}`,
      compatJson: "",
    }],
  }],
};

describe("ModelManager", () => {
  let pinia: ReturnType<typeof createPinia>;

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.selectable.mockResolvedValue([]);
    mocks.get.mockResolvedValue(structuredClone(configured));
    mocks.upsert.mockResolvedValue(structuredClone(configured));
    mocks.addModels.mockResolvedValue(structuredClone(configured));
    mocks.delete.mockResolvedValue({ path: configured.path, providers: [] });
    mocks.discover.mockResolvedValue({
      endpoint: "https://gateway.example.com/v1/models",
      models: [{ id: "gpt-fetched", name: "GPT Fetched" }],
    });
    mocks.test.mockResolvedValue({ ok: true, latencyMs: 420, response: "OK", error: "" });
    mocks.quota.mockResolvedValue({
      ok: true,
      latencyMs: 180,
      status: 200,
      endpoint: "https://gateway.example.com/dashboard/billing/credit_grants",
      summary: "total_available: 12.75",
      detailsJson: '{\n  "total_available": 12.75\n}',
      error: "",
    });
    pinia = createPinia();
    setActivePinia(pinia);
  });

  it("shows the credential and saves editable provider fields", async () => {
    const store = useAppStore();
    store.refreshModels = vi.fn().mockResolvedValue(undefined);
    const wrapper = mount(ModelManager, { global: { plugins: [pinia] } });
    await flushPromises();

    expect(wrapper.find(".settings-content-header").exists()).toBe(false);
    expect(wrapper.text()).toContain("GPT Test");
    expect(wrapper.get('input[placeholder="sk-... / $API_KEY"]').element).toHaveProperty("value", "sk-visible-local-key");
    expect(wrapper.findAll('[data-testid="provider-header-row"]')).toHaveLength(2);
    expect(wrapper.findAll('[data-testid="provider-header-name"]')[0].element).toHaveProperty("value", "User-Agent");
    expect(wrapper.findAll('[data-testid="provider-header-value"]')[0].element).toHaveProperty("value", "existing-agent");
    expect(wrapper.get('[data-testid="thinking-level-map"]').element).toHaveProperty("value", `{
  "xhigh": "xhigh"
}`);

    await wrapper.get('[data-testid="provider-id"]').setValue("renamed-provider");
    await wrapper.get('input[placeholder="GPT 5"]').setValue("GPT Test Updated");
    await wrapper.findAll('[data-testid="provider-header-value"]')[0].setValue("edited-agent");
    await wrapper.get('[data-testid="thinking-level-map"]').setValue('{"xhigh":"xhigh","max":"max"}');
    await wrapper.get(".primary-button").trigger("submit");
    await flushPromises();

    expect(mocks.upsert).toHaveBeenCalledWith(expect.objectContaining({
      originalProviderId: "custom-openai",
      providerId: "renamed-provider",
      apiKey: "sk-visible-local-key",
      headers: { "User-Agent": "edited-agent", "X-Channel": "desktop" },
      modelId: "gpt-test",
      modelName: "GPT Test Updated",
      contextWindow: 128000,
      maxTokens: 16384,
      reasoning: true,
      imageInput: true,
      thinkingLevelMapJson: '{"xhigh":"xhigh","max":"max"}',
    }));
  });

  it("rejects invalid thinking level mappings before saving", async () => {
    const wrapper = mount(ModelManager, { global: { plugins: [pinia] } });
    await flushPromises();

    await wrapper.get('[data-testid="thinking-level-map"]').setValue('{"turbo":"turbo"}');
    await wrapper.get(".primary-button").trigger("submit");
    await flushPromises();

    expect(mocks.upsert).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("Thinking level map must be a JSON object");
  });

  it("opens an editable model test dialog and shows the provider response without saving", async () => {
    const store = useAppStore();
    store.refreshModels = vi.fn().mockResolvedValue(undefined);
    const host = document.createElement("div");
    document.body.appendChild(host);
    const wrapper = mount(ModelManager, { attachTo: host, global: { plugins: [pinia] } });
    await flushPromises();

    await wrapper.get('input[placeholder="sk-... / $API_KEY"]').setValue("sk-edited-key");
    await wrapper.findAll('[data-testid="provider-header-value"]')[0].setValue("test-agent");
    const testButton = wrapper.findAll("button").find((button) => button.text() === "Test");
    expect(testButton).toBeTruthy();
    await testButton!.trigger("click");
    await flushPromises();

    const dialog = document.body.querySelector<HTMLElement>(".model-test-dialog");
    expect(dialog?.getAttribute("role")).toBe("dialog");
    const prompt = dialog?.querySelector<HTMLTextAreaElement>('[data-testid="model-test-prompt"]');
    expect(prompt?.value).toContain("short confirmation");
    if (!prompt) throw new Error("model test prompt was not rendered");
    prompt.value = "Return exactly MODEL_OK";
    prompt.dispatchEvent(new Event("input", { bubbles: true }));
    dialog?.querySelector<HTMLButtonElement>(".model-test-submit")?.click();
    await flushPromises();

    expect(mocks.upsert).not.toHaveBeenCalled();
    expect(mocks.test).toHaveBeenCalledWith({
      baseUrl: "https://gateway.example.com/v1",
      api: "openai-responses",
      apiKey: "sk-edited-key",
      headers: { "User-Agent": "test-agent", "X-Channel": "desktop" },
      modelId: "gpt-test",
      prompt: "Return exactly MODEL_OK",
    });
    expect(dialog?.querySelector(".model-test-response pre")?.textContent).toBe("OK");
    expect(dialog?.textContent).toContain("Connection succeeded in 420 ms");

    dialog?.querySelector<HTMLButtonElement>('button[title="Close"]')?.click();
    await flushPromises();
    expect(document.body.querySelector(".model-test-dialog")).toBeNull();

    const deleteButton = wrapper.get(".danger-button");
    await deleteButton.trigger("click");
    expect(mocks.delete).not.toHaveBeenCalled();
    expect(deleteButton.text()).toContain("Confirm delete");
    await deleteButton.trigger("click");
    await flushPromises();
    expect(mocks.delete).toHaveBeenCalledWith({ providerId: "custom-openai", modelId: "gpt-test" });
    wrapper.unmount();
    host.remove();
  });

  it("queries account quota in a provider-aware result dialog", async () => {
    const host = document.createElement("div");
    document.body.appendChild(host);
    const wrapper = mount(ModelManager, { attachTo: host, global: { plugins: [pinia] } });
    await flushPromises();

    const quotaButton = wrapper.findAll("button").find((button) => button.text() === "Account quota");
    expect(quotaButton).toBeTruthy();
    await quotaButton!.trigger("click");
    await flushPromises();

    expect(mocks.quota).toHaveBeenCalledWith({
      baseUrl: "https://gateway.example.com/v1",
      api: "openai-responses",
      apiKey: "sk-visible-local-key",
      headers: { "User-Agent": "existing-agent", "X-Channel": "desktop" },
    });
    const dialog = document.body.querySelector<HTMLElement>(".model-quota-dialog");
    expect(dialog?.getAttribute("role")).toBe("dialog");
    expect(dialog?.textContent).toContain("total_available: 12.75");
    expect(dialog?.textContent).toContain("HTTP");
    expect(dialog?.textContent).toContain("200 · 180 ms");

    dialog?.querySelector<HTMLButtonElement>('button[title="Close"]')?.click();
    await flushPromises();
    expect(document.body.querySelector(".model-quota-dialog")).toBeNull();
    wrapper.unmount();
    host.remove();
  });

  it("defaults headers for a new provider and saves added custom headers", async () => {
    mocks.get.mockResolvedValue({ path: configured.path, providers: [] });
    const wrapper = mount(ModelManager, { global: { plugins: [pinia] } });
    await flushPromises();

    expect(wrapper.findAll('[data-testid="provider-header-row"]')).toHaveLength(1);
    expect(wrapper.get('[data-testid="provider-header-name"]').element).toHaveProperty("value", "User-Agent");
    expect(wrapper.get('[data-testid="provider-header-value"]').element).toHaveProperty("value", "codex_cli_rs/0.146.0 (Windows 11.0.26100; x86_64) Terminal");
    await wrapper.get(".model-header-add").trigger("click");
    await wrapper.findAll('[data-testid="provider-header-name"]')[1].setValue("X-Channel");
    await wrapper.findAll('[data-testid="provider-header-value"]')[1].setValue("desktop");
    await wrapper.get('[data-testid="provider-id"]').setValue("custom-provider");
    await wrapper.get('input[placeholder="gpt-5"]').setValue("custom-model");
    await wrapper.get(".primary-button").trigger("submit");
    await flushPromises();

    expect(mocks.upsert).toHaveBeenCalledWith(expect.objectContaining({
      providerId: "custom-provider",
      modelId: "custom-model",
      headers: {
        "User-Agent": "codex_cli_rs/0.146.0 (Windows 11.0.26100; x86_64) Terminal",
        "X-Channel": "desktop",
      },
    }));
  });

  it("warns that the openai provider ID merges Pi's built-in and cached catalog", async () => {
    mocks.get.mockResolvedValue({ path: configured.path, providers: [] });
    const wrapper = mount(ModelManager, { global: { plugins: [pinia] } });
    await flushPromises();

    const providerID = wrapper.get('[data-testid="provider-id"]');
    expect(providerID.attributes("placeholder")).toBe("openai-custom");
    await providerID.setValue("openai");
    expect(wrapper.get(".model-field > small.is-warning").text()).toContain("merged with Pi's built-in and cached OpenAI catalog");

    await providerID.setValue("openai-direct");
    expect(wrapper.find(".model-field > small.is-warning").exists()).toBe(false);
  });

  it("disables testing until a new model has the required identifiers", async () => {
    mocks.get.mockResolvedValue({ path: configured.path, providers: [] });
    const wrapper = mount(ModelManager, { global: { plugins: [pinia] } });
    await flushPromises();

    const testButton = wrapper.get(".model-editor-actions .text-button:not(.primary-button)");
    expect(testButton.attributes("disabled")).toBeDefined();
    await testButton.trigger("click");
    expect(mocks.test).not.toHaveBeenCalled();
  });

  it("fetches provider models and applies the selected model", async () => {
    const wrapper = mount(ModelManager, { global: { plugins: [pinia] } });
    await flushPromises();

    const fetchButton = wrapper.findAll(".model-field-heading button").find((button) => button.text() === "Fetch models");
    expect(fetchButton).toBeTruthy();
    await fetchButton!.trigger("click");
    await flushPromises();
    expect(mocks.discover).toHaveBeenCalledWith({
      baseUrl: "https://gateway.example.com/v1",
      api: "openai-responses",
      apiKey: "sk-visible-local-key",
      headers: { "User-Agent": "existing-agent", "X-Channel": "desktop" },
    });

    const fetchedSelect = wrapper.findAll("select").find((select) => select.find('option[value="gpt-fetched"]').exists());
    expect(fetchedSelect).toBeTruthy();
    await fetchedSelect!.setValue("gpt-fetched");
    expect(wrapper.get('input[placeholder="gpt-5"]').element).toHaveProperty("value", "gpt-fetched");
    expect(wrapper.get('input[placeholder="GPT 5"]').element).toHaveProperty("value", "GPT Fetched");

    const addButton = wrapper.findAll("button").find((button) => button.text() === "Add models (1)");
    expect(addButton).toBeTruthy();
    await addButton!.trigger("click");
    await flushPromises();
    expect(mocks.addModels).toHaveBeenCalledWith(expect.objectContaining({
      providerId: "custom-openai",
      api: "openai-responses",
      apiKey: "sk-visible-local-key",
      headers: { "User-Agent": "existing-agent", "X-Channel": "desktop" },
      models: [expect.objectContaining({
        id: "gpt-fetched",
        contextWindow: 128000,
        maxTokens: 16384,
      })],
    }));
  });

  it("keeps manual compatibility fields without an automatic compat button", async () => {
    const wrapper = mount(ModelManager, { global: { plugins: [pinia] } });
    await flushPromises();

    await wrapper.get(".model-advanced summary").trigger("click");
    expect(wrapper.text()).not.toContain("Suggest compat");
    expect(wrapper.findAll(".model-compat-help button")).toHaveLength(0);
    expect(wrapper.findAll(".model-advanced textarea")).toHaveLength(2);
  });

  it("applies a verified model parameter suggestion", async () => {
    const wrapper = mount(ModelManager, { global: { plugins: [pinia] } });
    await flushPromises();

    await wrapper.get('input[placeholder="gpt-5"]').setValue("gpt-5.6-sol");
    const suggestButton = wrapper.findAll("button").find((button) => button.text() === "Suggest parameters");
    expect(suggestButton).toBeTruthy();
    await suggestButton!.trigger("click");

    const numericInputs = wrapper.findAll('input[type="number"]');
    expect(numericInputs[0].element).toHaveProperty("value", "272000");
    expect(numericInputs[1].element).toHaveProperty("value", "128000");
    expect(wrapper.findAll('input[type="checkbox"]')[0].element).toHaveProperty("checked", true);
    expect(wrapper.findAll('input[type="checkbox"]')[1].element).toHaveProperty("checked", true);
  });
});
