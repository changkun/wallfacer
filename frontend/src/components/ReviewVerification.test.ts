// The review panel renders the transcript the server read: each round's
// reviewer answer and the task's turn on its findings, the session's outcome,
// a skip reason, a note when the transcript is incomplete, and a note in place
// of rounds for a review an earlier version recorded.
import { afterEach, describe, expect, it } from 'vitest';
import { createApp, h, type App } from 'vue';

import type { ReviewTranscript, Task } from '../api/types';
import ReviewVerification from './ReviewVerification.vue';

function taskWith(overrides: Partial<Task> = {}): Task {
  return { id: 't-1', ...overrides } as unknown as Task;
}

function transcript(overrides: Partial<ReviewTranscript>): ReviewTranscript {
  return {
    session_id: 'sess-01',
    running: false,
    config: { max_rounds: 3, cost_cap: 50000, reviewer_model: 'reviewer-model' },
    rounds: [
      {
        round: 1,
        reviewer: {
          verdict: 'changes_requested',
          findings: [
            { severity: 'high', claim: 'Add writes to a nil map', location: 'store.go:10' },
            { severity: 'low', claim: 'typo in a comment' },
          ],
          model: 'reviewer-model',
          ts: '2026-10-02T00:00:01Z',
        },
        feedback: 'A review of this change on a second model requested changes.',
        reply: 'Fixed the nil map.',
      },
    ],
    ...overrides,
  };
}

let app: App | null = null;
let host: HTMLElement;

function mount(t: ReviewTranscript | null, task: Task = taskWith()) {
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
  it('renders each round: findings, the feedback sent and the task reply', () => {
    mount(transcript({}));

    expect(host.querySelector('.review__config')?.textContent).toContain('reviewer-model');
    expect(host.querySelectorAll('.review-round').length).toBe(1);
    const findings = host.querySelectorAll('.review-finding');
    expect(findings.length).toBe(2);
    expect(findings[0].textContent).toContain('Add writes to a nil map');
    expect(findings[0].textContent).toContain('store.go:10');
    expect(host.querySelector('.review-verdict')?.textContent).toContain('Changes requested');
    expect(host.querySelector('.review-msg--feedback')?.textContent).toContain('requested changes');
    expect(host.querySelector('.review-msg--implementer')?.textContent).toContain('Fixed the nil map.');
    // Between rounds, with no verdict yet, the review is in progress.
    expect(host.querySelector('.review__status')?.textContent).toContain('In progress');
  });

  it('shows the outcome and the open findings when the session ends', () => {
    mount(
      transcript({ outcome: { termination: 'max_rounds', rounds: 3, unresolved: 2, headline: 'Add writes to a nil map', tokens: 4200, usd: 0.1 } }),
      taskWith({ review_unresolved: 2, review_headline: 'Add writes to a nil map' }),
    );

    expect(host.querySelector('.review__status')?.textContent).toContain('2 open');
    expect(host.querySelector('.review__verdict')?.textContent).toContain('2 open findings after the last round');
    expect(host.querySelector('.review__headline')?.textContent).toContain('Add writes to a nil map');
  });

  it('shows the reason a review did not run', () => {
    mount(
      transcript({
        rounds: [],
        outcome: {
          termination: 'skipped', rounds: 0, unresolved: 0, tokens: 0, usd: 0,
          skip: { code: 'review_model_unset', message: 'Review did not run: no reviewer model is set.' },
        },
      }),
    );

    expect(host.querySelector('.review__status')?.textContent).toContain('Did not run');
    expect(host.querySelector('.review__verdict')?.textContent).toContain('no reviewer model is set');
  });

  it('shows a failed attempt with the reviewer output', () => {
    mount(
      transcript({
        rounds: [{ round: 1, failed_attempts: [{ code: 'review_output_invalid', message: "Review: the reviewer's answer could not be read.", raw: 'it looks fine', ts: '' }] }],
      }),
    );

    const failed = host.querySelector('.review-round__failed');
    expect(failed?.textContent).toContain('could not be read');
    expect(failed?.querySelector('pre')?.textContent).toBe('it looks fine');
  });

  it('says the transcript is incomplete when the server marks it truncated', () => {
    mount(transcript({ truncated: true }));

    expect(host.querySelector('.review__truncated')?.textContent).toContain('incomplete');
    // The rounds that were read are still shown.
    expect(host.querySelectorAll('.review-round').length).toBe(1);
  });

  it('shows no such note for a transcript read to its end', () => {
    mount(transcript({}));

    expect(host.querySelector('.review__truncated')).toBeNull();
  });

  it('degrades a review recorded by an earlier version to a note and the task verdict', () => {
    mount(transcript({ legacy: true, rounds: [] }), taskWith({ review_unresolved: 1, review_headline: 'nil deref in foo' }));

    expect(host.querySelector('.review__legacy')?.textContent).toContain('earlier version');
    expect(host.querySelectorAll('.review-round').length).toBe(0);
    expect(host.querySelector('.review__headline')?.textContent).toContain('nil deref in foo');
  });

  it('shows the empty state when no review has run', () => {
    mount(null);

    expect(host.textContent).toContain('No review has run');
  });
});
