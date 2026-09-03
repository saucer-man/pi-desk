import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { McpConfigScope } from "../../bindings/pi-desk/internal/domain";
import McpManager from "./McpManager.vue";

const mcpMocks = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), upsert: vi.fn(), delete: vi.fn() }));
vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/modelconfig", () => ({ modelConfigService: { selectable: vi.fn().mockResolvedValue([]) } }));
vi.mock("../services/mcpconfig", () => ({ mcpConfigService: mcpMocks }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

describe("McpManager", () => {
  beforeEach(() => {
    Object.values(mcpMocks).forEach((mock) => mock.mockReset());
  });

  it("edits a Pi-owned MCP server without dropping advanced fields", async () => {
    mcpMocks.list.mockResolvedValue({
      globalPath: "C:\\Users\\dev\\.pi\\agent\\mcp.json", projectEnabled: false,
      servers: [{ scope: McpConfigScope.McpConfigScopeGlobal, name: "docs", transport: "http", endpoint: "https://example.test/mcp", disabled: false }],
    });
    mcpMocks.get.mockResolvedValue({
      scope: McpConfigScope.McpConfigScopeGlobal, name: "docs", transport: "http", endpoint: "https://example.test/mcp", disabled: false,
      definition: '{\n  "url": "https://example.test/mcp",\n  "headers": {"X-Test": "value"}\n}\n',
    });
    mcpMocks.upsert.mockResolvedValue({
      scope: McpConfigScope.McpConfigScopeGlobal, name: "docs", transport: "http", endpoint: "https://example.test/v2/mcp", disabled: false,
      definition: '{\n  "url": "https://example.test/v2/mcp",\n  "headers": {"X-Test": "value"}\n}\n',
    });
    const pinia = createPinia(); setActivePinia(pinia);
    const wrapper = mount(McpManager, { global: { plugins: [pinia] } });
    await flushPromises();

    expect(wrapper.find(".settings-content-header").exists()).toBe(false);
    expect(wrapper.text()).toContain("docs");
    const json = wrapper.get("textarea");
    await json.setValue('{\n  "url": "https://example.test/v2/mcp",\n  "headers": {"X-Test": "value"}\n}\n');
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    expect(mcpMocks.upsert).toHaveBeenCalledWith(expect.objectContaining({ originalName: "docs", name: "docs" }));
    expect(JSON.parse(mcpMocks.upsert.mock.calls[0][0].definition).headers).toEqual({ "X-Test": "value" });
  });

  it("shows project MCP availability for the active workspace", async () => {
    mcpMocks.list.mockResolvedValue({
      globalPath: "C:\\Users\\dev\\.pi\\agent\\mcp.json",
      servers: [],
    });
    const pinia = createPinia(); setActivePinia(pinia);
    const wrapper = mount(McpManager, { global: { plugins: [pinia] } });
    await flushPromises();

    expect(mcpMocks.list).toHaveBeenCalledWith({ workspacePath: "" });
    expect(wrapper.text()).toContain("Global MCP");
    expect(wrapper.text()).toContain("Project MCP servers require a trusted workspace.");
    const projectOption = wrapper.findAll("option").find((option) => option.element.value === McpConfigScope.McpConfigScopeProject);
    expect(projectOption?.attributes("disabled")).toBeDefined();
  });
});
