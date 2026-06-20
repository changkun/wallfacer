// Render contract for the assistant turn: the agent trajectory leads the
// answer. While streaming the activity disclosure is open and labelled
// "Working…"; once the answer lands it collapses into an informative one-liner
// (step + tool counts) that sits ABOVE the prose, not a generic "Agent
// activity" toggle below it. A plain createApp mount exercises the real
// compiled template, so it catches DOM ordering a unit test on the helper
// cannot.
import { describe, it, expect, afterEach } from 'vitest';
import { createApp, ref, h, type App } from 'vue';
import ChatMessageList from './ChatMessageList.vue';
import type { RenderedBubble } from '../../lib/planningBubble';

let app: App | null = null;
let host: HTMLElement;

// A ChatMessageList reads a ChatSession; this stub provides only the refs and
// no-op actions the template touches.
function fakeSession(messages: RenderedBubble[]) {
  return {
    renderedMessages: ref(messages),
    messagesEl: ref<HTMLElement | null>(null),
    interruptedAt: ref(-1),
    latestRound: ref(0),
    currentQueue: ref([]),
    editingQueueId: ref<number | null>(null),
    editQueueDraft: ref(''),
    undoRound: () => {},
    removeFromQueue: () => {},
    startQueueEdit: () => {},
    commitQueueEdit: () => {},
    cancelQueueEdit: () => {},
  };
}

function mount(messages: RenderedBubble[]) {
  host = document.createElement('div');
  document.body.appendChild(host);
  app = createApp({
    render: () => h(ChatMessageList, { session: fakeSession(messages) as never }),
  });
  app.mount(host);
}

afterEach(() => {
  app?.unmount();
  app = null;
  host?.remove();
});

const assistant = (over: Partial<RenderedBubble>): RenderedBubble => ({
  role: 'assistant',
  contentHtml: '<p>final answer</p>',
  rawText: 'final answer',
  planRound: 0,
  reverted: false,
  activity: [
    { kind: 'tool', label: 'Read' },
    { kind: 'tool', label: 'Read' },
    { kind: 'tool', label: 'Bash' },
  ],
  hasActivity: true,
  isStreaming: false,
  ...over,
});

describe('ChatMessageList — trajectory placement', () => {
  it('renders the collapsed trajectory summary above the answer prose', () => {
    mount([assistant({})]);
    const activity = host.querySelector('.pcp-activity');
    const content = host.querySelector('.pcp-bubble-content');
    expect(activity).not.toBeNull();
    expect(content).not.toBeNull();
    // Activity must precede the answer in document order.
    expect(activity!.compareDocumentPosition(content!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('labels the collapsed summary with step + tool counts, not "Agent activity"', () => {
    mount([assistant({})]);
    const title = host.querySelector('.pcp-activity-title')!.textContent;
    expect(title).toBe('3 steps · Read ×2, Bash');
    expect(host.querySelector('details.pcp-activity')!.hasAttribute('open')).toBe(false);
  });

  it('shows a live, open, freeform "Working…" trajectory while streaming', () => {
    mount([assistant({ isStreaming: true, contentHtml: '' })]);
    const details = host.querySelector('details.pcp-activity')!;
    expect(details.hasAttribute('open')).toBe(true);
    // The freeform (boxless) live mode is signalled by the --live modifier.
    expect(details.classList.contains('pcp-activity--live')).toBe(true);
    expect(host.querySelector('.pcp-activity-title')!.textContent).toBe('Working…');
  });

  it('drops the --live modifier once the turn has finished', () => {
    mount([assistant({ isStreaming: false })]);
    const details = host.querySelector('details.pcp-activity')!;
    expect(details.classList.contains('pcp-activity--live')).toBe(false);
  });
});
