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

describe('CLAUDE_MODELS', () => {
  it('lists only current-generation IDs in their canonical, undated form', () => {
    // The picker seeds the model field, so a retired alias or a dotted 3.x
    // name here becomes a 404 from the API. IDs are claude-<tier>-<major>
    // with an optional minor, nothing else.
    for (const id of CLAUDE_MODELS) {
      expect(id).toMatch(/^claude-(opus|sonnet|haiku)-\d(-\d)?$/);
    }
  });
});
