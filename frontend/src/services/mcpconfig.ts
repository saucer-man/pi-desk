import { McpConfigService } from "../../bindings/pi-desk/internal/appservice";
import type {
  ListMcpServersRequest,
  McpConfigSnapshot,
  McpEngineStatus,
  McpEngineStatusRequest,
  McpImportCandidate,
  McpServer,
  McpServerRequest,
  McpServerTestResult,
  TestMcpServerRequest,
  UpsertMcpServerRequest,
} from "../../bindings/pi-desk/internal/domain";

export const mcpConfigService = {
  list(request: ListMcpServersRequest): Promise<McpConfigSnapshot> {
    return McpConfigService.ListMcpServers(request);
  },
  get(request: McpServerRequest): Promise<McpServer> {
    return McpConfigService.GetMcpServer(request);
  },
  upsert(request: UpsertMcpServerRequest): Promise<McpServer> {
    return McpConfigService.UpsertMcpServer(request);
  },
  delete(request: McpServerRequest): Promise<void> {
    return McpConfigService.DeleteMcpServer(request);
  },
  test(request: TestMcpServerRequest): Promise<McpServerTestResult> {
    return McpConfigService.TestMcpServer(request);
  },
  engineStatus(request: McpEngineStatusRequest): Promise<McpEngineStatus> {
    return McpConfigService.GetMcpEngineStatus(request);
  },
  async importCandidates(): Promise<McpImportCandidate[]> {
    return (await McpConfigService.ListImportableMcpServers()) ?? [];
  },
};

export type {
  McpConfigSnapshot,
  McpEngineStatus,
  McpEngineStatusRequest,
  McpImportCandidate,
  McpServer,
  McpServerRequest,
  McpServerTestResult,
  McpServerSummary,
  TestMcpServerRequest,
  UpsertMcpServerRequest,
} from "../../bindings/pi-desk/internal/domain";
