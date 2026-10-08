import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { McpConfigScope } from "../../bindings/pi-desk/internal/domain";
import { useAppStore, type WorkspaceSummary } from "../stores/app";
import McpManager from "./McpManager.vue";

const mcpMocks = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), upsert: vi.fn(), delete: vi.fn(), test: vi.fn(), importCandidates: vi.fn() }));
vi.mock("../services/agent", () => ({ agentService: {}, onPiEvent: () => () => undefined }));
vi.mock("../services/catalog", () => ({ catalogService: {} }));
vi.mock("../services/modelconfig", () => ({ modelConfigService: { selectable: vi.fn().mockResolvedValue([]) } }));
vi.mock("../services/mcpconfig", () => ({ mcpConfigService: mcpMocks }));
vi.mock("../services/repository", () => ({ repositoryService: {} }));

function mountManager(workspaces: WorkspaceSummary[] = []) {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAppStore().workspaces = workspaces;
  return mount(McpManager, { global: { plugins: [pinia] } });
}

describe("McpManager", () => {
  beforeEach(() => {
    Object.values(mcpMocks).forEach((mock) => mock.mockReset());
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
    await wrapper.get(".mcp-server-main").trigger("click");
    await flushPromises();
    await wrapper.get('input[placeholder="https://example.com/mcp"]').setValue("https://example.test/v2/mcp");
    await wrapper.get("[data-testid='mcp-editor']").trigger("submit");
    await flushPromises();
    expect(mcpMocks.upsert).toHaveBeenCalledWith(expect.objectContaining({ originalName: "docs", name: "docs" }));
    expect(JSON.parse(mcpMocks.upsert.mock.calls[0][0].definition).headers).toEqual({ "X-Test": "value" });
  });

  it("uses the selected project path in the new-server scope hint", async () => {
    mcpMocks.list.mockResolvedValue({
      globalPath: "C:\\Users\\dev\\.pi\\agent\\mcp.json",
      projectEnabled: true,
      projectPath: "D:\\repo\\.pi\\mcp.json",
      servers: [], effectiveServers: [], sources: [],
    });
    const wrapper = mountManager([{ id: "repo", name: "repo", path: "D:\\repo", trust: "approve" }]);
    await flushPromises();

    await wrapper.get('[data-testid="mcp-scope-target"]').setValue("D:\\repo");
    await flushPromises();
    await wrapper.get('[data-testid="new-mcp-server"]').trigger("click");
    expect(mcpMocks.list).toHaveBeenLastCalledWith({ workspacePath: "D:\\repo" });
    expect(wrapper.get(".mcp-editor-scope").text()).toContain("D:\\repo\\.pi\\mcp.json");
    expect(wrapper.get(".mcp-editor-scope").text()).not.toContain("~/.pi/agent/mcp.json");
  });

  it("does not render connection-engine management on the MCP page", async () => {
    mcpMocks.list.mockResolvedValue({ globalPath: "C:\\Users\\dev\\.pi\\agent\\mcp.json", servers: [] });
    const wrapper = mountManager();
    await flushPromises();

    expect(wrapper.find("[data-testid='mcp-engine']").exists()).toBe(false);
  });

  it("tests the unsaved definition and shows discovered MCP interfaces", async () => {
    mcpMocks.list.mockResolvedValue({ globalPath: "C:\\Users\\dev\\.pi\\agent\\mcp.json", servers: [] });
    mcpMocks.test.mockResolvedValue({ output: "docs: connected\nsearch: Search documentation", durationMillis: 38 });
    const wrapper = mountManager();
    await flushPromises();

    await wrapper.get('[data-testid="new-mcp-server"]').trigger("click");
    await wrapper.findAll(".mcp-editor-tabs button")[1].trigger("click");
    await wrapper.get(".mcp-complete-json textarea").setValue('{"docs":{"url":"https://example.test/mcp","headers":{"X-Test":"draft"}}}');
    const testButton = wrapper.findAll("button").find((button) => button.text().includes("Test connection"));
    await testButton!.trigger("click");
    await flushPromises();

    expect(mcpMocks.test).toHaveBeenCalledWith(expect.objectContaining({ workspacePath: "" }));
    expect(JSON.parse(mcpMocks.test.mock.calls[0][0].definition)).toEqual({ url: "https://example.test/mcp", headers: { "X-Test": "draft" } });
    const result = wrapper.get("[data-testid='mcp-test-result']");
    expect(result.text()).toContain("Connection successful");
    expect(result.text()).toContain("Search documentation");
    expect(wrapper.find(".mcp-editor-footer").exists()).toBe(true);
    expect(mcpMocks.upsert).not.toHaveBeenCalled();
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

    await wrapper.get('button[title="Import from other tools"]').trigger("click");
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
