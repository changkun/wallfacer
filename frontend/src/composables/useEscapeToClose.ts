// Close a modal on the Escape key while it is open. Several modals
// (ContainerMonitor, WorkspacePicker) hand-rolled this; this composable
// is the shared version so SystemPromptsManager / TemplatesManager get
// consistent keyboard-dismiss without duplicating
// the listener-lifecycle boilerplate.
//
// The listener is only attached while `isOpen` is true, and is always
// removed on unmount. Uses the capture phase so it fires before any
// inner element's own Escape handling (e.g. clearing a search box) when
// the host wants the modal close to win — callers that need the inner
// handler to win should not use this.
import { watch, onUnmounted, type WatchSource } from 'vue';

export function useEscapeToClose(isOpen: WatchSource<boolean>, close: () => void) {
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault();
      close();
    }
  }

  let attached = false;
  function attach() {
    if (attached) return;
    document.addEventListener('keydown', onKey);
    attached = true;
  }
  function detach() {
    if (!attached) return;
    document.removeEventListener('keydown', onKey);
    attached = false;
  }

  watch(isOpen, (open) => {
    if (open) attach();
    else detach();
  }, { immediate: true });

  onUnmounted(detach);
}
