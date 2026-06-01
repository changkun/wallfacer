import { describe, it, expect } from 'vitest';
import { detectResultType } from './resultType';

describe('detectResultType', () => {
  it('returns result for empty / undefined', () => {
    expect(detectResultType('')).toBe('result');
  });
  it.each([
    '# The Plan',
    '## Implementation Plan',
    '### Phase 1',
    '### Step 2',
    'Here is the implementation plan for foo',
    '## Approach',
    '## Architecture',
    '## Strategy',
  ])('detects plan in %s', (text) => {
    expect(detectResultType(text)).toBe('plan');
  });
  it.each([
    'Done. Files changed: a.go, b.go.',
    '## Summary\nFixed the bug.',
    'A short result without plan markers.',
  ])('detects result in %s', (text) => {
    expect(detectResultType(text)).toBe('result');
  });
});
