export const BATCH_ASK_PLACEHOLDER = "__piDeckBatchAsk__";
const BATCH_ASK_ENVELOPE_KEY = "__piDeckBatchAsk";
const MAX_BATCH_QUESTIONS = 32;
const MAX_BATCH_OPTIONS = 50;
const MAX_BATCH_ENVELOPE_BYTES = 1 << 20;
const MAX_QUESTION_TEXT = 8 << 10;
const MAX_OPTION_TEXT = 4 << 10;
const MAX_PREFILL_TEXT = 1 << 20;

export type BatchAskQuestionType = "select" | "confirm" | "input" | "editor";

export interface BatchAskOption {
  label: string;
  value: string;
  description?: string;
}

export interface BatchAskQuestion {
  id: string;
  type: BatchAskQuestionType;
  question: string;
  options?: BatchAskOption[];
  allowOther?: boolean;
  placeholder?: string;
  prefill?: string;
}

export interface BatchAskEnvelope {
  questions: BatchAskQuestion[];
  review: boolean;
}

export interface BatchAskAnswer {
  id: string;
  type: BatchAskQuestionType;
  value: string | boolean;
  label?: string;
  wasCustom?: boolean;
}

function boundedString(value: unknown, limit: number, required = false): string | undefined {
  if (typeof value !== "string" || value.length > limit) return undefined;
  const normalized = required ? value.trim() : value;
  if (required && !normalized) return undefined;
  return normalized;
}

function parseOption(value: unknown): BatchAskOption | undefined {
  if (typeof value === "string") {
    const label = boundedString(value, MAX_OPTION_TEXT, true);
    return label ? { label, value: label } : undefined;
  }
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  const record = value as Record<string, unknown>;
  const label = boundedString(record.label, MAX_OPTION_TEXT, true);
  if (!label) return undefined;
  const optionValue = record.value === undefined ? label : boundedString(record.value, MAX_OPTION_TEXT, true);
  if (optionValue === undefined) return undefined;
  const description = record.description === undefined ? undefined : boundedString(record.description, MAX_OPTION_TEXT);
  if (record.description !== undefined && description === undefined) return undefined;
  return { label, value: optionValue, ...(description ? { description } : {}) };
}

function parseQuestion(value: unknown): BatchAskQuestion | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  const record = value as Record<string, unknown>;
  const id = boundedString(record.id, 128, true);
  const question = boundedString(record.question, MAX_QUESTION_TEXT, true);
  const type = record.type;
  if (!id || !/^[A-Za-z0-9_.:-]+$/.test(id) || !question || typeof type !== "string" || !["select", "confirm", "input", "editor"].includes(type)) return undefined;

  const result: BatchAskQuestion = { id, type: type as BatchAskQuestionType, question };
  if (type === "select") {
    if (!Array.isArray(record.options) || record.options.length === 0 || record.options.length > MAX_BATCH_OPTIONS) return undefined;
    const options = record.options.map(parseOption);
    if (options.some((option) => !option)) return undefined;
    result.options = options as BatchAskOption[];
    if (new Set(result.options.map((option) => option.value)).size !== result.options.length) return undefined;
    result.allowOther = record.allowOther !== false;
  }
  if (record.placeholder !== undefined) {
    const placeholder = boundedString(record.placeholder, MAX_OPTION_TEXT);
    if (placeholder === undefined) return undefined;
    result.placeholder = placeholder;
  }
  if (record.prefill !== undefined) {
    const prefill = boundedString(record.prefill, MAX_PREFILL_TEXT);
    if (prefill === undefined) return undefined;
    result.prefill = prefill;
  }
  return result;
}

export function parseBatchAskEnvelope(value: unknown, markerConfirmed = false): BatchAskEnvelope | undefined {
  let parsed: unknown = value;
  if (typeof value === "string") {
    if (!value.trim().startsWith("{") || value.length > MAX_BATCH_ENVELOPE_BYTES) return undefined;
    try {
      parsed = JSON.parse(value);
    } catch {
      return undefined;
    }
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return undefined;
  const record = parsed as Record<string, unknown>;
  if (record[BATCH_ASK_ENVELOPE_KEY] !== 1 && !markerConfirmed) return undefined;
  if (!Array.isArray(record.questions) || record.questions.length === 0 || record.questions.length > MAX_BATCH_QUESTIONS) return undefined;
  const questions = record.questions.map(parseQuestion);
  if (questions.some((question) => !question)) return undefined;
  const normalized = questions as BatchAskQuestion[];
  if (new Set(normalized.map((question) => question.id)).size !== normalized.length) return undefined;
  return { questions: normalized, review: record.review === true };
}

export function serializeBatchAskAnswers(questions: BatchAskQuestion[], answers: BatchAskAnswer[]): string | undefined {
  if (answers.length !== questions.length) return undefined;
  const byID = new Map(answers.map((answer) => [answer.id, answer]));
  const normalized: BatchAskAnswer[] = [];
  for (const question of questions) {
    const answer = byID.get(question.id);
    if (!answer || answer.type !== question.type) return undefined;
    if (question.type === "confirm") {
      if (typeof answer.value !== "boolean") return undefined;
      normalized.push({
        id: question.id,
        type: question.type,
        value: answer.value,
        ...(answer.label ? { label: answer.label.slice(0, MAX_OPTION_TEXT) } : {}),
      });
      continue;
    }
    if (typeof answer.value !== "string" || !answer.value.trim() || answer.value.length > MAX_PREFILL_TEXT) return undefined;
    if (question.type === "select") {
      const option = question.options?.find((candidate) => candidate.value === answer.value);
      const wasCustom = answer.wasCustom === true;
      if (wasCustom ? question.allowOther === false : !option) return undefined;
      normalized.push({
        id: question.id,
        type: question.type,
        value: answer.value,
        label: (wasCustom ? answer.label || answer.value : option!.label).slice(0, MAX_OPTION_TEXT),
        ...(wasCustom ? { wasCustom: true } : {}),
      });
      continue;
    }
    normalized.push({
      id: question.id,
      type: question.type,
      value: answer.value,
      ...(answer.label ? { label: answer.label.slice(0, MAX_OPTION_TEXT) } : {}),
    });
  }
  const result = JSON.stringify({ answers: normalized });
  return result.length <= MAX_BATCH_ENVELOPE_BYTES ? result : undefined;
}
