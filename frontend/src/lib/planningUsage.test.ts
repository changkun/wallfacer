import { describe, it, expect } from 'vitest';
import { parseTurnUsage, aggregateUsage, formatTokens, formatCost, formatPercent } from './planningUsage';

function ndjson(...frames: unknown[]): string {
  return frames.map((f) => JSON.stringify(f)).join('\n');
}

const resultFrame = (over: Record<string, unknown> = {}) => ({
  type: 'result',
  stop_reason: 'end_turn',
  total_cost_usd: 0.0123,
  usage: {
    input_tokens: 1200,
    output_tokens: 340,
    cache_read_input_tokens: 5000,
    cache_creation_input_tokens: 200,
  },
  ...over,
});

describe('parseTurnUsage', () => {
  it('reads usage and cost from the terminal result frame', () => {
    const raw = ndjson(
      { type: 'assistant', message: { content: [{ type: 'text', text: 'hi' }] } },
      resultFrame(),
    );
    expect(parseTurnUsage(raw)).toEqual({
      inputTokens: 1200,
      outputTokens: 340,
      cacheReadTokens: 5000,
      cacheCreationTokens: 200,
      costUSD: 0.0123,
    });
  });

  it('prefers a frame with a stop_reason over an earlier result-shaped one', () => {
    const raw = ndjson(
      resultFrame({ stop_reason: '', total_cost_usd: 0.001, usage: { input_tokens: 1 } }),
      resultFrame({ total_cost_usd: 0.02 }),
    );
    expect(parseTurnUsage(raw)?.costUSD).toBe(0.02);
  });

  it('returns null when there is no usage', () => {
    expect(parseTurnUsage(ndjson({ type: 'assistant', message: { content: [] } }))).toBeNull();
    expect(parseTurnUsage('')).toBeNull();
    expect(parseTurnUsage(undefined)).toBeNull();
  });
});

describe('aggregateUsage', () => {
  it('sums assistant usage and derives the cache-hit ratio', () => {
    const u = (over = {}) => parseTurnUsage(ndjson(resultFrame(over)));
    const bubbles = [
      { role: 'user' as const, usage: undefined },
      { role: 'assistant' as const, usage: u() },
      { role: 'assistant' as const, usage: u({ usage: { input_tokens: 800, output_tokens: 160, cache_read_input_tokens: 3000, cache_creation_input_tokens: 0 } }) },
    ];
    const s = aggregateUsage(bubbles);
    expect(s.rounds).toBe(2);
    expect(s.inputTokens).toBe(2000);
    expect(s.outputTokens).toBe(500);
    expect(s.cacheReadTokens).toBe(8000);
    expect(s.costUSD).toBeCloseTo(0.0246, 6);
    // cache_read / (input + cache_read) = 8000 / 10000
    expect(s.cacheHitRatio).toBeCloseTo(0.8, 6);
  });

  it('is all-zero with no assistant usage', () => {
    expect(aggregateUsage([{ role: 'user', usage: undefined }])).toMatchObject({ rounds: 0, cacheHitRatio: 0 });
  });
});

describe('formatters', () => {
  it('formats tokens compactly', () => {
    expect(formatTokens(942)).toBe('942');
    expect(formatTokens(1234)).toBe('1.2k');
    expect(formatTokens(48000)).toBe('48k');
    expect(formatTokens(2_300_000)).toBe('2.3M');
  });
  it('scales cost precision to magnitude', () => {
    expect(formatCost(0)).toBe('$0');
    expect(formatCost(0.0012)).toBe('$0.0012');
    expect(formatCost(0.21)).toBe('$0.210');
    expect(formatCost(3.5)).toBe('$3.50');
  });
  it('formats a percent', () => {
    expect(formatPercent(0.84)).toBe('84%');
  });
});
