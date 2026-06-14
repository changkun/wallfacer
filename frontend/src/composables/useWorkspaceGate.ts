import { computed, type ComputedRef } from 'vue';
import { useRoute } from 'vue-router';
import { useTaskStore } from '../stores/tasks';

// useWorkspaceGate reports whether the current route renders workspace-scoped
// content (meta.needsWorkspace) but no workspace is visible to this session.
// App.vue swaps in the WorkspaceRequired prompt when this is true, keeping the
// board, plan/chat, file tree, etc. consistent with /api/config's "no
// workspace" state. Returns false until config has loaded so first paint does
// not flash the prompt.
export function useWorkspaceGate(): ComputedRef<boolean> {
  const route = useRoute();
  const store = useTaskStore();
  return computed(
    () =>
      route.meta?.needsWorkspace === true &&
      store.config != null &&
      (store.config.workspaces?.length ?? 0) === 0,
  );
}
