import { describe, it, expect } from 'vitest';
import { matchesTaskFilter, type FilterableTask } from './taskFilter';

const t: FilterableTask = {
  id: 'abc12345-0000',
  title: 'Fix login timeout',
  prompt: 'The login flow times out after 30s under load',
  tags: ['bug', 'priority:high'],
};

describe('matchesTaskFilter', () => {
  it('empty query matches everything', () => {
    expect(matchesTaskFilter(t, '')).toBe(true);
    expect(matchesTaskFilter(t, '   ')).toBe(true);
  });

  it('single text token substring-matches title/prompt/tags', () => {
    expect(matchesTaskFilter(t, 'login')).toBe(true);   // title + prompt
    expect(matchesTaskFilter(t, 'load')).toBe(true);    // prompt only
    expect(matchesTaskFilter(t, 'bug')).toBe(true);     // tag text
    expect(matchesTaskFilter(t, 'nope')).toBe(false);
  });

  it('multiple text tokens are AND-ed', () => {
    expect(matchesTaskFilter(t, 'login timeout')).toBe(true);
    expect(matchesTaskFilter(t, 'login missing')).toBe(false); // one token fails
  });

  it('#tag token requires exact tag membership', () => {
    expect(matchesTaskFilter(t, '#bug')).toBe(true);
    expect(matchesTaskFilter(t, '#priority:high')).toBe(true);
    expect(matchesTaskFilter(t, '#feature')).toBe(false);
    expect(matchesTaskFilter(t, '#bu')).toBe(false); // not a prefix match, exact only
  });

  it('multiple #tag tokens all required (AND)', () => {
    expect(matchesTaskFilter(t, '#bug #priority:high')).toBe(true);
    expect(matchesTaskFilter(t, '#bug #wontfix')).toBe(false);
  });

  it('mixes tag and text tokens', () => {
    expect(matchesTaskFilter(t, '#bug login')).toBe(true);
    expect(matchesTaskFilter(t, '#bug login zzz')).toBe(false);  // text token fails
    expect(matchesTaskFilter(t, '#feature login')).toBe(false);  // tag fails
  });

  it('matches an id prefix', () => {
    expect(matchesTaskFilter(t, 'abc123')).toBe(true);
    expect(matchesTaskFilter(t, '12345')).toBe(false); // prefix only, not mid-id
  });

  it('is case-insensitive', () => {
    expect(matchesTaskFilter(t, 'LOGIN')).toBe(true);
    expect(matchesTaskFilter(t, '#BUG')).toBe(true);
  });
});
