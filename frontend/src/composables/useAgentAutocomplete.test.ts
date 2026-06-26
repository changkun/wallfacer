// The agent composer's @-mention autocomplete now delegates query detection
// and ranking to the shared lib/mentions helpers, so it ranks identically to the
// other mention surface (basename-prefix first) instead of an unranked substring
// match. This test pins that ranking.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { ref } from 'vue';
import { useAgentAutocomplete } from './useAgentAutocomplete';

let originalFetch: typeof globalThis.fetch;
let files: string[] = [];

beforeEach(() => {
  originalFetch = globalThis.fetch;
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
    const url = typeof input === 'string' ? input : input.toString();
    if (url.includes('/api/files')) {
      return new Response(JSON.stringify({ files }), { status: 200 });
    }
    return new Response('{}', { status: 200 });
  }) as unknown as typeof globalThis.fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  files = [];
});

describe('useAgentAutocomplete @-mention', () => {
  it('ranks basename-prefix matches above substring matches', async () => {
    // Original array order puts the weaker (substring) match first; ranking must
    // float the basename-prefix match to the top. The old unranked includes()
    // preserved array order and would leave specalpha.md first.
    files = ['x/specalpha.md', 'y/alpha.md'];

    const el = document.createElement('textarea');
    el.value = '@alpha';
    el.setSelectionRange(el.value.length, el.value.length);
    const inputEl = ref<HTMLTextAreaElement | null>(el);
    const inputText = ref('@alpha');

    const ac = useAgentAutocomplete({ inputEl, inputText });
    await ac.onInput();

    expect(ac.mentionOpen.value).toBe(true);
    expect(ac.mentionFiltered.value[0]).toBe('y/alpha.md');
    expect(ac.mentionFiltered.value).toEqual(['y/alpha.md', 'x/specalpha.md']);
  });

  it('opens the mention dropdown for queries with non-word characters', async () => {
    // mentionQueryAt accepts any non-whitespace after '@', unlike the old
    // [\w./-] regex, so an input like "@foo:bar" now triggers the surface.
    files = ['pkg/foo:bar.md'];

    const el = document.createElement('textarea');
    el.value = '@foo:bar';
    el.setSelectionRange(el.value.length, el.value.length);
    const inputEl = ref<HTMLTextAreaElement | null>(el);
    const inputText = ref('@foo:bar');

    const ac = useAgentAutocomplete({ inputEl, inputText });
    await ac.onInput();

    expect(ac.mentionOpen.value).toBe(true);
    expect(ac.mentionFiltered.value).toContain('pkg/foo:bar.md');
  });
});
