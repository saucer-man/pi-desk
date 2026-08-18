const MAX_RUNTIME_ERROR_LENGTH = 2048;

const urlCredentialsPattern = /\b([a-z][a-z0-9+.-]*:\/\/)[^\s/@]+@/gi;
const bearerCredentialPattern = /\bbearer\s+[^\s,;]+/gi;
const namedCredentialPattern = /\b(api[-_ ]?key|authorization|x-api-key|x-goog-api-key|access[-_ ]?token|refresh[-_ ]?token|password|secret)\b(\s*[:=]\s*)(?:(?:bearer)\s+)?(?:"[^"]*"|'[^']*'|[^\s,;&]+)/gi;
const commonSecretPattern = /\b(?:sk|pk|ghp|github_pat)-?[a-z0-9_-]{16,}\b/gi;

export function runtimeErrorText(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  let text = value.trim();
  if (!text) return undefined;
  text = text
    .replace(urlCredentialsPattern, "$1[redacted]@")
    .replace(namedCredentialPattern, (_match, name: string, separator: string) => `${name}${separator}[redacted]`)
    .replace(bearerCredentialPattern, "Bearer [redacted]")
    .replace(commonSecretPattern, "[redacted]");
  if (text.length > MAX_RUNTIME_ERROR_LENGTH) text = `${text.slice(0, MAX_RUNTIME_ERROR_LENGTH - 3)}...`;
  return text;
}
