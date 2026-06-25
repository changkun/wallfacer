// Token-usage parsing for planning chat turns. Mirrors the backend's
// internal/planner/usage.go ExtractUsage so the UI and server agree on a turn's
// usage. Usage lives only in the terminal stream-json "result" frame; reasoning
// /thinking tokens are not reported separately by the API — they are part of
// output_tokens.

export interface TurnUsage {
  inputTokens: number; // fresh (non-cached) input
  outputTokens: number; // includes reasoning/thinking
  cacheReadTokens: number; // input served from the prompt cache
  cacheCreationTokens: number; // input written to the cache
  costUSD: number;
}

interface ResultLine {
  type?: string;
  stop_reason?: string;
  total_cost_usd?: number;
  usage?: {
    input_tokens?: number;
    output_tokens?: number;
    cache_read_input_tokens?: number;
    cache_creation_input_tokens?: number;
  };
}

function toTurnUsage(r: ResultLine): TurnUsage {
  return {
    inputTokens: r.usage?.input_tokens ?? 0,
    outputTokens: r.usage?.output_tokens ?? 0,
    cacheReadTokens: r.usage?.cache_read_input_tokens ?? 0,
    cacheCreationTokens: r.usage?.cache_creation_input_tokens ?? 0,
    costUSD: r.total_cost_usd ?? 0,
  };
}

function isEmpty(u: TurnUsage): boolean {
  return (
    u.inputTokens === 0 &&
    u.outputTokens === 0 &&
    u.cacheReadTokens === 0 &&
    u.cacheCreationTokens === 0 &&
    u.costUSD === 0
  );
}

/**
 * Usage for one assistant turn from its raw stream-json output, or null when no
 * usable result frame is present. Prefers a frame with a non-empty stop_reason
 * (the terminal result), falling back to the last result-shaped frame — exactly
 * like the Go ExtractUsage.
 */
export function parseTurnUsage(raw: string | undefined): TurnUsage | null {
  if (!raw) return null;
  const lines = raw.trim().split('\n');
  let fallback: ResultLine | null = null;
  for (let i = lines.length - 1; i >= 0; i--) {
    const line = lines[i].trim();
    if (!line || line[0] !== '{') continue;
    let obj: ResultLine;
    try {
      obj = JSON.parse(line) as ResultLine;
    } catch {
      continue;
    }
    // "result" frames, or untyped single-blob outputs, carry usage.
    if (obj.type !== 'result' && obj.type !== undefined && obj.type !== '') continue;
    if (fallback === null) fallback = obj;
    if (obj.stop_reason) {
      const u = toTurnUsage(obj);
      return isEmpty(u) ? null : u;
    }
  }
  if (!fallback) return null;
  const u = toTurnUsage(fallback);
  return isEmpty(u) ? null : u;
}

export interface UsageSummary extends TurnUsage {
  /** Assistant turns that reported usage. */
  rounds: number;
  /** cache_read / (input + cache_read): share of input served from cache. */
  cacheHitRatio: number;
}

/** Sum usage across assistant bubbles and derive the cache-hit insight. */
export function aggregateUsage(bubbles: { role: string; usage?: TurnUsage | null }[]): UsageSummary {
  const s: UsageSummary = {
    inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0,
    costUSD: 0, rounds: 0, cacheHitRatio: 0,
  };
  for (const b of bubbles) {
    if (b.role !== 'assistant' || !b.usage) continue;
    s.inputTokens += b.usage.inputTokens;
    s.outputTokens += b.usage.outputTokens;
    s.cacheReadTokens += b.usage.cacheReadTokens;
    s.cacheCreationTokens += b.usage.cacheCreationTokens;
    s.costUSD += b.usage.costUSD;
    s.rounds += 1;
  }
  const inputTotal = s.inputTokens + s.cacheReadTokens;
  s.cacheHitRatio = inputTotal > 0 ? s.cacheReadTokens / inputTotal : 0;
  return s;
}

/** Compact token count: 1234 → "1.2k", 1200000 → "1.2M". */
export function formatTokens(n: number): string {
  if (n < 1000) return String(n);
  if (n < 1_000_000) return (n / 1000).toFixed(n < 10_000 ? 1 : 0) + 'k';
  return (n / 1_000_000).toFixed(1) + 'M';
}

/** Cost with precision scaled to magnitude. */
export function formatCost(usd: number): string {
  if (usd <= 0) return '$0';
  if (usd < 0.01) return '$' + usd.toFixed(4);
  if (usd < 1) return '$' + usd.toFixed(3);
  return '$' + usd.toFixed(2);
}

/** "84%" for a 0..1 ratio. */
export function formatPercent(ratio: number): string {
  return Math.round(ratio * 100) + '%';
}
