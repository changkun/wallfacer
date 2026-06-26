<script setup lang="ts">
// One diff line in the Changes-tab review view. Renders the line (highlighted or
// plain), and — only when commentable (waiting task + signed in, on add/del/ctx
// lines) — a gutter affordance that opens an inline editor anchored under the
// line. Comments live in the diffComments store, keyed by (taskId, filename,
// lineIndex). The root is a fragment (the line span, then the optional editor
// block) so the editor flows directly beneath its line inside the <pre>.
import { ref, computed, nextTick } from 'vue';
import type { DiffLine } from '../lib/diff';
import type { HighlightedDiffLine } from '../lib/diffHighlight';
import { useDiffCommentsStore } from '../stores/diffComments';

const props = defineProps<{
  taskId: string;
  filename: string;
  lineIndex: number;
  line: DiffLine;
  hl: HighlightedDiffLine | null;
  commentable: boolean;
}>();

const store = useDiffCommentsStore();
const existing = computed(() => store.forLine(props.taskId, props.filename, props.lineIndex));
const hasComment = computed(() => !!existing.value);

// hunk/header lines keep the raw text even when a highlight variant exists, to
// match the original render.
const useHl = computed(() => !!props.hl && props.line.kind !== 'header' && props.line.kind !== 'hunk');

const editing = ref(false);
const draft = ref('');
const editorEl = ref<HTMLTextAreaElement | null>(null);

function lineClass(kind: string): string {
  return kind === 'ctx' ? '' : 'diff-' + kind;
}

function openEditor() {
  draft.value = existing.value?.body ?? '';
  editing.value = true;
  nextTick(() => editorEl.value?.focus());
}

function save() {
  const body = draft.value.trim();
  if (!body) { cancel(); return; }
  const cur = existing.value;
  if (cur) {
    store.update(cur.id, body);
  } else {
    store.add({
      taskId: props.taskId,
      filename: props.filename,
      lineIndex: props.lineIndex,
      oldLine: props.line.oldLine,
      newLine: props.line.newLine,
      kind: props.line.kind,
      lineText: props.line.text,
      body,
    });
  }
  editing.value = false;
  draft.value = '';
}

function cancel() {
  editing.value = false;
  draft.value = '';
}
</script>

<template>
  <span
    class="diff-line"
    :class="[lineClass(line.kind), { 'dc-line': commentable, 'dc-line--has': hasComment }]"
    :data-dc-anchor="commentable ? `${filename}:${lineIndex}` : undefined"
  ><button
    v-if="commentable"
    type="button"
    class="dc-gutter"
    :class="{ 'dc-gutter--has': hasComment }"
    :title="hasComment ? 'Edit comment' : 'Comment on this line'"
    :aria-label="hasComment ? 'Edit comment' : 'Comment on this line'"
    @click="openEditor"
  >{{ hasComment ? '●' : '+' }}</button><template v-if="useHl">{{ hl!.prefix }}<span v-html="hl!.html"></span></template><template v-else>{{ line.text }}</template></span><div
    v-if="editing"
    class="dc-editor"
  ><textarea
    ref="editorEl"
    v-model="draft"
    class="dc-editor-input"
    rows="2"
    placeholder="Leave a comment on this line… (⌘/Ctrl+Enter to save)"
    @keydown.meta.enter="save"
    @keydown.ctrl.enter="save"
    @keydown.esc="cancel"
  /><div class="dc-editor-actions"><button type="button" class="dc-btn dc-btn--primary" :disabled="!draft.trim()" @click="save">Save</button><button type="button" class="dc-btn" @click="cancel">Cancel</button></div></div>
</template>
