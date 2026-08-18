import { PromptTemplateService } from "../../bindings/pi-desk/internal/appservice";
import type {
  ListPromptTemplatesRequest,
  PromptTemplate,
  PromptTemplateRequest,
  PromptTemplateSnapshot,
  UpsertPromptTemplateRequest,
} from "../../bindings/pi-desk/internal/domain";

export const promptTemplateService = {
  list(request: ListPromptTemplatesRequest): Promise<PromptTemplateSnapshot> {
    return PromptTemplateService.ListPromptTemplates(request);
  },
  get(request: PromptTemplateRequest): Promise<PromptTemplate> {
    return PromptTemplateService.GetPromptTemplate(request);
  },
  upsert(request: UpsertPromptTemplateRequest): Promise<PromptTemplate> {
    return PromptTemplateService.UpsertPromptTemplate(request);
  },
  delete(request: PromptTemplateRequest): Promise<void> {
    return PromptTemplateService.DeletePromptTemplate(request);
  },
};

export type {
  PromptTemplate,
  PromptTemplateRequest,
  PromptTemplateSnapshot,
  PromptTemplateSummary,
  UpsertPromptTemplateRequest,
} from "../../bindings/pi-desk/internal/domain";
