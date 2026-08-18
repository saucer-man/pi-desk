import { McpConfigService } from "../../bindings/pi-desk/internal/appservice";
import type {
  ListMcpServersRequest,
  McpConfigSnapshot,
  McpServer,
  McpServerRequest,
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
};

export type {
  McpConfigSnapshot,
  McpServer,
  McpServerRequest,
  McpServerSummary,
  UpsertMcpServerRequest,
} from "../../bindings/pi-desk/internal/domain";
