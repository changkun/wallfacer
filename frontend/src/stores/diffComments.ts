// Client-side store for inline diff-review comments. Comments anchor to a diff
// line by (taskId, filename, lineIndex) and live only in the browser until the
// user batches them into one feedback message (see lib/diffComments.ts) and
// submits through the existing feedback endpoint. Nothing is persisted across
// reloads — the workflow is open diff → comment → submit.
import { defineStore } from 'pinia';
import { ref } from 'vue';
import type { DiffLineKind } from '../lib/diff';

export interface DiffComment {
  id: string;          // crypto.randomUUID()
  taskId: string;
  filename: string;
  lineIndex: number;   // index into DiffFile.lines (stable for a loaded diff)
  oldLine: number | null;
  newLine: number | null;
  kind: DiffLineKind;  // add | del | ctx (hunk/header are not commentable)
  lineText: string;    // snapshot of the diff line, for the formatted output
  body: string;
}

// The fields the caller supplies; `id` is minted on add.
export type NewDiffComment = Omit<DiffComment, 'id'>;

export const useDiffCommentsStore = defineStore('diffComments', () => {
  // A flat reactive list; the getters slice it by task. Slicing on read keeps
  // the per-task and per-line lookups reactive without a nested Map.
  const comments = ref<DiffComment[]>([]);

  function add(c: NewDiffComment): DiffComment {
    const created: DiffComment = { id: crypto.randomUUID(), ...c };
    comments.value.push(created);
    return created;
  }

  function update(id: string, body: string): void {
    const c = comments.value.find((x) => x.id === id);
    if (c) c.body = body;
  }

  function remove(id: string): void {
    const i = comments.value.findIndex((x) => x.id === id);
    if (i !== -1) comments.value.splice(i, 1);
  }

  function clear(taskId: string): void {
    comments.value = comments.value.filter((c) => c.taskId !== taskId);
  }

  function forTask(taskId: string): DiffComment[] {
    return comments.value.filter((c) => c.taskId === taskId);
  }

  function forLine(taskId: string, filename: string, lineIndex: number): DiffComment | undefined {
    return comments.value.find(
      (c) => c.taskId === taskId && c.filename === filename && c.lineIndex === lineIndex,
    );
  }

  return { comments, add, update, remove, clear, forTask, forLine };
});
