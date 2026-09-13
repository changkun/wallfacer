import { afterEach, describe, expect, it, vi } from 'vitest';
import { createApp, h, reactive } from 'vue';
import TaskCommitHistory from './TaskCommitHistory.vue';
import type { TaskCommit } from '../api/types';

const mock = vi.hoisted(() => ({ api: vi.fn() }));
vi.mock('../api/client', () => ({ api: mock.api }));
const commit: TaskCommit = { repository: '/repo', hash: '1234567890', subject: 'First feedback change', author: 'Test', authored_at: '2026-09-13T10:00:00Z', attempt: 1, turn: 2, patch: '+hello' };
let cleanup = () => {};
afterEach(() => { cleanup(); vi.clearAllMocks(); });
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));
async function mount() {
  const props = reactive({ taskId: 'a', active: true, refreshKey: 0 });
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp({ render: () => h(TaskCommitHistory, props) });
  app.mount(host);
  cleanup = () => { app.unmount(); host.remove(); };
  await settle();
  return { props, host };
}

describe('task commit history', () => {
  it('lists commit attribution and a bounded patch that expands', async () => {
    mock.api.mockResolvedValue([{ ...commit, patch_truncated: true }]);
    const { host } = await mount();
    expect(host.textContent).toContain('Attempt 1, turn 2');
    expect(host.querySelector('summary')?.textContent).toContain('12345678');
    const details = host.querySelector('details')!;
    expect(details.open).toBe(false);
    host.querySelector('summary')!.click();
    expect(details.open).toBe(true);
    expect(host.querySelector('pre')?.textContent).toBe('+hello');
    expect(host.textContent).toContain('limited to 2 MiB');
  });

  it('refreshes in place and gives empty and failure states', async () => {
    mock.api.mockResolvedValue([]);
    const { props, host } = await mount();
    expect(host.textContent).toContain('No recorded commits');
    mock.api.mockResolvedValue([commit]);
    props.refreshKey++;
    await settle();
    expect(host.textContent).toContain(commit.subject);
    mock.api.mockRejectedValue(new Error('Unavailable'));
    props.refreshKey++;
    await settle();
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('Unavailable');
  });

  it('ignores an old task response after navigation', async () => {
    let finish!: (value: TaskCommit[]) => void;
    mock.api.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
    const { props, host } = await mount();
    mock.api.mockResolvedValue([{ ...commit, subject: 'Other task' }]);
    props.taskId = 'b';
    await settle();
    finish([commit]);
    await settle();
    expect(host.textContent).toContain('Other task');
    expect(host.textContent).not.toContain(commit.subject);
  });
});
