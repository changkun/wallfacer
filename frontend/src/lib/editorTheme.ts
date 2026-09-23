// The CodeMirror theme for the file editor, read from the console tokens so
// the pane follows the palette and the theme without a rebuild. Colors are
// CSS variables: the ramp swaps per theme, so one highlight style serves both.
// The token mapping mirrors styles/syntax.css so a keyword reads the same in
// prose, diffs and the editor.
import { EditorView } from '@codemirror/view';
import type { Extension } from '@codemirror/state';
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { tags } from '@lezer/highlight';

export const consoleHighlight = HighlightStyle.define([
  { tag: [tags.keyword, tags.modifier, tags.typeName, tags.self], color: 'var(--err)' },
  { tag: [tags.function(tags.variableName), tags.className, tags.definition(tags.variableName)], color: 'var(--purple)' },
  { tag: [tags.attributeName, tags.number, tags.bool, tags.null, tags.operator, tags.meta, tags.propertyName], color: 'var(--run)' },
  { tag: [tags.string, tags.regexp, tags.special(tags.string)], color: 'color-mix(in srgb, var(--run) 60%, var(--ink))' },
  { tag: [tags.standard(tags.variableName), tags.atom], color: 'var(--warn)' },
  { tag: [tags.comment, tags.lineComment, tags.blockComment], color: 'var(--ink-3)', fontStyle: 'italic' },
  { tag: [tags.tagName, tags.quote], color: 'var(--ok)' },
  { tag: tags.heading, color: 'var(--run)', fontWeight: '700' },
  { tag: tags.list, color: 'var(--warn)' },
  { tag: tags.link, color: 'var(--accent)', textDecoration: 'underline' },
  { tag: tags.invalid, color: 'var(--err)', textDecoration: 'underline' },
]);

// The chrome: page surface, sunk gutters, the accent caret and ring.
function chrome(dark: boolean): Extension {
  return EditorView.theme({
    '&': { backgroundColor: 'var(--bg)', color: 'var(--ink)', height: '100%' },
    '.cm-scroller': { fontFamily: 'var(--font-mono)', fontSize: '13px', lineHeight: '1.55' },
    '.cm-content': { caretColor: 'var(--accent)', padding: '8px 0' },
    '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--accent)' },
    '&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, ::selection':
      { backgroundColor: 'var(--accent-ring)' },
    '.cm-activeLine': { backgroundColor: 'color-mix(in srgb, var(--ink) 4%, transparent)' },
    '.cm-gutters': {
      backgroundColor: 'var(--bg-sunk)',
      color: 'var(--ink-4)',
      borderRight: '1px solid var(--rule)',
    },
    '.cm-activeLineGutter': { backgroundColor: 'color-mix(in srgb, var(--ink) 6%, transparent)', color: 'var(--ink-2)' },
    '.cm-lineNumbers .cm-gutterElement': { padding: '0 10px 0 14px' },
    '.cm-foldPlaceholder': { backgroundColor: 'var(--bg-sunk)', border: '1px solid var(--rule)', color: 'var(--ink-3)' },
    '.cm-matchingBracket, &.cm-focused .cm-matchingBracket': {
      backgroundColor: 'var(--accent-soft)',
      outline: '1px solid var(--accent-line)',
    },
    '.cm-searchMatch': { backgroundColor: 'var(--tint-amber)' },
    '.cm-searchMatch.cm-searchMatch-selected': { backgroundColor: 'var(--accent-soft)' },
    '.cm-tooltip': {
      backgroundColor: 'var(--bg-card)',
      border: '1px solid var(--rule)',
      borderRadius: 'var(--r-md)',
      boxShadow: 'var(--sh-pop)',
      color: 'var(--ink)',
    },
    '.cm-tooltip-autocomplete ul li[aria-selected]': { backgroundColor: 'var(--bg-sunk)', color: 'var(--ink)' },
    '.cm-panels': { backgroundColor: 'var(--bg-sunk)', color: 'var(--ink)' },
    '.cm-panels.cm-panels-bottom': { borderTop: '1px solid var(--rule)' },
  }, { dark });
}

export function consoleTheme(dark: boolean): Extension {
  return [chrome(dark), syntaxHighlighting(consoleHighlight)];
}
