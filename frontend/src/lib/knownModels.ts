// Known model identifiers shown in the SettingsTabSandbox datalists.
//
// The legacy UI listed the same names hardcoded; we keep them in one place
// so the Claude and Codex tabs can re-import without duplication. Lists are
// ordered "newest first" so the most likely choice appears at the top of
// the suggestion popover.
//
// `default_for_base_url` returns the list to surface for a given base URL —
// custom (self-hosted) endpoints get an empty list so the user can type any
// model name without the suggestion being misleading.

export const CLAUDE_MODELS = [
  'claude-opus-4-7',
  'claude-sonnet-4-6',
  'claude-haiku-4-5',
  'claude-opus-4-5',
  'claude-sonnet-4-5',
  'claude-opus-4-1',
  'claude-3-7-sonnet-latest',
  'claude-3-5-haiku-latest',
];

export const CODEX_MODELS = [
  'gpt-5-codex',
  'gpt-5',
  'gpt-5-mini',
  'gpt-4.1',
  'gpt-4.1-mini',
  'o4-mini',
  'o3-mini',
];

function isAnthropicDefault(url: string): boolean {
  return !url || url.includes('anthropic.com');
}
function isOpenAIDefault(url: string): boolean {
  return !url || url.includes('openai.com');
}

/** Suggestions to surface for a Claude-style base_url. */
export function claudeModelsFor(baseUrl: string): string[] {
  return isAnthropicDefault(baseUrl) ? CLAUDE_MODELS : [];
}

/** Suggestions to surface for a Codex / OpenAI-style base_url. */
export function codexModelsFor(baseUrl: string): string[] {
  return isOpenAIDefault(baseUrl) ? CODEX_MODELS : [];
}
