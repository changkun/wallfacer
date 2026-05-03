/* Helpers for the /api/stats response shape used by AnalyticsPage.
   Extracted so the bucket-vs-number rendering can be unit-tested. */

export interface StatusBucket {
  count: number;
  cost_usd?: number;
  input_tokens?: number;
  output_tokens?: number;
}

/* The /api/stats response used to return Record<string, number> for
   by_status; it now returns Record<string, StatusBucket>. Older code
   was rendering the bucket object directly and dumped JSON into the
   bar chart. Normalise both shapes to a count. */
export function statusCount(value: number | StatusBucket | null | undefined): number {
  if (value == null) return 0;
  if (typeof value === 'number') return value;
  return value.count ?? 0;
}
