import { describe, it, expect } from 'vitest';
import { harnessLabel, modelLabel } from './harness';

describe('harnessLabel', () => {
  it('brand-cases known harness ids', () => {
    expect(harnessLabel('claude')).toBe('Claude');
    expect(harnessLabel('opencode')).toBe('OpenCode');
  });
  it('capitalizes unknown ids', () => {
    expect(harnessLabel('foo')).toBe('Foo');
  });
});

describe('modelLabel', () => {
  it('brand-cases claude model ids and strips the variant suffix', () => {
    expect(modelLabel('claude-opus-4-8[1m]')).toBe('Opus 4.8');
    expect(modelLabel('claude-opus-4-8')).toBe('Opus 4.8');
    expect(modelLabel('claude-sonnet-4-6')).toBe('Sonnet 4.6');
    expect(modelLabel('claude-haiku-4-5')).toBe('Haiku 4.5');
  });
  it('handles single-segment versions', () => {
    expect(modelLabel('claude-fable-5')).toBe('Fable 5');
  });
  it('falls back to the raw id (minus variant) for non-claude models', () => {
    expect(modelLabel('openai/gpt-5')).toBe('openai/gpt-5');
  });
  it('returns empty for empty input', () => {
    expect(modelLabel('')).toBe('');
  });
});
