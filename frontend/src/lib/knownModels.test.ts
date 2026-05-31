import { describe, it, expect } from 'vitest';
import { claudeModelsFor, codexModelsFor, CLAUDE_MODELS, CODEX_MODELS } from './knownModels';

describe('claudeModelsFor', () => {
  it('returns the full Claude list for an empty / Anthropic URL', () => {
    expect(claudeModelsFor('')).toEqual(CLAUDE_MODELS);
    expect(claudeModelsFor('https://api.anthropic.com')).toEqual(CLAUDE_MODELS);
  });
  it('returns empty for a self-hosted base URL', () => {
    expect(claudeModelsFor('http://localhost:11434')).toEqual([]);
    expect(claudeModelsFor('https://openrouter.ai/api/v1')).toEqual([]);
  });
});

describe('codexModelsFor', () => {
  it('returns the full Codex list for an empty / OpenAI URL', () => {
    expect(codexModelsFor('')).toEqual(CODEX_MODELS);
    expect(codexModelsFor('https://api.openai.com/v1')).toEqual(CODEX_MODELS);
  });
  it('returns empty for a self-hosted base URL', () => {
    expect(codexModelsFor('http://localhost:8000')).toEqual([]);
  });
});
