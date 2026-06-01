// Formats a 409 workspace-mutation conflict (push/sync/rebase blocked by
// in-flight tasks) into a readable multi-line message listing the blocking
// tasks. Mirrors ui/js/git.js's formatGitWorkspaceConflict.

export interface BlockingTask {
  id: string;
  title?: string;
  status?: string;
}

interface ConflictBody {
  error?: string;
  blocking_tasks?: BlockingTask[];
}

export function formatGitConflict(body: unknown, fallbackAction: string): string {
  const b = (body && typeof body === 'object' ? body : {}) as ConflictBody;
  const tasks = Array.isArray(b.blocking_tasks) ? b.blocking_tasks : [];
  if (tasks.length === 0) {
    return b.error || `${fallbackAction} failed`;
  }
  const lines = tasks.map((task) => {
    const title = task.title || '(untitled task)';
    const status = String(task.status || 'unknown').replace(/_/g, ' ');
    return `- [${status}] ${title} (${task.id})`;
  });
  return `${b.error || fallbackAction + ' blocked'}\n\nBlocking tasks:\n${lines.join('\n')}`;
}
