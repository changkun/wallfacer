// Date bucketing for the chat session list. Sessions are grouped by how long
// ago they were last active (the server's `updated` timestamp), so the most
// recently touched threads sort to the top under coarse, scannable headers.

export type SessionBucketKey = 'today' | 'last7' | 'last30' | 'older';

const DAY_MS = 86_400_000;

// Bucket order also drives render order (most recent first).
export const SESSION_BUCKETS: { key: SessionBucketKey; label: string }[] = [
  { key: 'today', label: 'Today' },
  { key: 'last7', label: 'Previous 7 days' },
  { key: 'last30', label: 'Previous 30 days' },
  { key: 'older', label: 'Older' },
];

// bucketForUpdated places a thread by the age of its last activity. Rolling
// windows (not calendar days) keep the boundaries simple and testable:
//   today  : < 1 day      last30 : 7–30 days
//   last7  : 1–7 days     older  : > 30 days
// A missing/zero timestamp is treated as very old (falls into "older"). A
// future timestamp (clock skew) lands in "today".
export function bucketForUpdated(nowMs: number, updatedMs: number): SessionBucketKey {
  const age = nowMs - updatedMs;
  if (age < DAY_MS) return 'today';
  if (age < 7 * DAY_MS) return 'last7';
  if (age < 30 * DAY_MS) return 'last30';
  return 'older';
}
