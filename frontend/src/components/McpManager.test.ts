import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { McpConfigScope, PiPackageScope } from "../../bindings/pi-desk/internal/domain";
import McpManager from "./McpManager.vue";

const mcpMocks = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), upsert: vi.fn(), delete: vi.fn(), engineStatus: vi.fn(), importCandidates: vi.fn() }));
const extensionMocks = vi.hoisted(() => ({ installPackage: vi.fn(), updatePackage: vi.fn() }));
vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/modelconfig", () => ({ modelConfigService: { selectable: vi.fn().mockResolvedValue([]) } }));
vi.mock("../services/mcpconfig", () => ({ mcpConfigService: mcpMocks }));
vi.mock("../services/extensions", () => ({ piExtensionService: extensionMocks }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

function mountManager() {
  const pinia = createPinia();
  setActivePinia(pinia);
  return mount(McpManager, { global: { plugins: [pinia] } });
}

describe("McpManager", () => {
  beforeEach(() => {
    Object.values(mcpMocks).forEach((mock) => mock.mockReset());
    extensionMocks.installPackage.mockReset();
    extensionMocks.updatePackage.mockReset();
    mcpMocks.engineStatus.mockResolvedValue({ installed: true, enabled: true, source: "npm:@nicobailon/pi-mcp-adapter" });
    mcpMocks.importCandidates.mockResolvedValue([]);
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
    const wrapper = mountManager();
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
    const wrapper = mountManager();
    await flushPromises();

    expect(mcpMocks.list).toHaveBeenCalledWith({ workspacePath: "" });
    expect(wrapper.text()).toContain("Global MCP");
    expect(wrapper.text()).toContain("Project MCP servers require a trusted workspace.");
    const projectOption = wrapper.findAll("option").find((option) => option.element.value === McpConfigScope.McpConfigScopeProject);
    expect(projectOption?.attributes("disabled")).toBeDefined();
  });

  it("offers to install the pi-mcp-adapter engine when missing", async () => {
    mcpMocks.engineStatus.mockResolvedValue({ installed: false, enabled: false });
    mcpMocks.list.mockResolvedValue({ globalPath: "C:\\Users\\dev\\.pi\\agent\\mcp.json", servers: [] });
    extensionMocks.installPackage.mockResolvedValue({ output: "" });
    const wrapper = mountManager();
    await flushPromises();

    const card = wrapper.get("[data-testid='mcp-engine']");
    expect(card.text()).toContain("pi-mcp-adapter is not installed");
    await card.get("button").trigger("click");
    await flushPromises();
    expect(extensionMocks.installPackage).toHaveBeenCalledWith({ scope: PiPackageScope.PiPackageScopeGlobal, source: "npm:@nicobailon/pi-mcp-adapter", workspacePath: "" });
    expect(mcpMocks.engineStatus).toHaveBeenCalledTimes(2);
  });

  it("imports servers from other hosts and flags configured names", async () => {
    mcpMocks.list.mockResolvedValue({
      globalPath: "C:\\Users\\dev\\.pi\\agent\\mcp.json",
      servers: [{ scope: McpConfigScope.McpConfigScopeGlobal, name: "docs", transport: "http", endpoint: "https://example.test/mcp", disabled: false }],
    });
    mcpMocks.importCandidates.mockResolvedValue([
      { host: "Claude Code", path: "C:\\Users\\dev\\.claude.json", name: "fs", definition: '{\n  "command": "npx"\n}\n' },
      { host: "Cursor", path: "C:\\Users\\dev\\.cursor\\mcp.json", name: "docs", definition: '{\n  "command": "uvx"\n}\n' },
    ]);
    mcpMocks.upsert.mockResolvedValue({ scope: McpConfigScope.McpConfigScopeGlobal, name: "fs", transport: "stdio", endpoint: "npx", disabled: false, definition: '{\n  "command": "npx"\n}\n' });
    const wrapper = mountManager();
    await flushPromises();

    const toggle = wrapper.findAll("button").find((button) => button.text().includes("Import from other tools"));
    await toggle!.trigger("click");
    await flushPromises();

    const rows = wrapper.findAll(".mcp-import-row");
    expect(rows).toHaveLength(2);
    expect(rows[1].get("input").attributes("disabled")).toBeDefined();
    expect(rows[1].text()).toContain("already configured");
    await wrapper.get(".mcp-import-panel button.primary").trigger("click");
    await flushPromises();
    expect(mcpMocks.upsert).toHaveBeenCalledTimes(1);
    expect(mcpMocks.upsert).toHaveBeenCalledWith(expect.objectContaining({ scope: McpConfigScope.McpConfigScopeGlobal, name: "fs", definition: '{\n  "command": "npx"\n}\n' }));
    expect(wrapper.text()).toContain("Imported 1 MCP server");
  });
});
