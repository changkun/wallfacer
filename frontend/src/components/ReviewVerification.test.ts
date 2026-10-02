// The verification panel renders the debate transcript the server read. When
// the server could not read the transcript to its end it marks the response
// truncated, and the panel has to say the rounds shown are not the whole
// debate.
import { afterEach, describe, expect, it } from 'vitest';
import { createApp, h, type App } from 'vue';

import type { ReviewTranscript, Task } from '../api/types';
import ReviewVerification from './ReviewVerification.vue';

const task = { id: 't-1', review_unresolved: 1 } as unknown as Task;

function transcript(overrides: Partial<ReviewTranscript>): ReviewTranscript {
  return {
    session_id: 'sess-01',
    running: false,
    forks: [
      {
        index: 1,
        rounds: [{ round: 1, role: 'critic', body: 'nil deref in foo', ts: '2026-06-27T00:00:01Z' }],
      },
    ],
    ...overrides,
  };
}

let app: App | null = null;
let host: HTMLElement;

function mount(t: ReviewTranscript) {
  host = document.createElement('div');
  document.body.appendChild(host);
  app = createApp({ render: () => h(ReviewVerification, { task, transcript: t }) });
  app.mount(host);
}

afterEach(() => {
  app?.unmount();
  host?.remove();
  app = null;
});

describe('ReviewVerification', () => {
  it('says the transcript is incomplete when the server marks it truncated', () => {
    mount(transcript({ truncated: true }));

    const note = host.querySelector('.review__truncated');
    expect(note?.textContent).toContain('incomplete');
    // The rounds that were read are still shown.
    expect(host.querySelectorAll('.review-msg').length).toBe(1);
  });

  it('shows no such note for a transcript read to its end', () => {
    mount(transcript({}));

    expect(host.querySelector('.review__truncated')).toBeNull();
    expect(host.querySelectorAll('.review-msg').length).toBe(1);
  });
});
