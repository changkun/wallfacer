import { describe, it, expect } from 'vitest';
import { cardActionsFor, commandPaletteActionsFor, CARD_ACTION_DEFS } from './cardActions';

type T = Parameters<typeof cardActionsFor>[0];
const task = (over: Partial<T>): T => ({ status: 'backlog', archived: false, kind: 'task', session_id: null, ...over });

describe('cardActionsFor', () => {
  it('backlog → Plan, Start', () => {
    expect(cardActionsFor(task({ status: 'backlog' }))).toEqual(['plan', 'start']);
  });

  it('waiting with a session → Resume, Test, Done', () => {
    expect(cardActionsFor(task({ status: 'waiting', session_id: 's1' }))).toEqual(['resume', 'test', 'done']);
  });

  it('waiting without a session → Test, Done (no Resume)', () => {
    expect(cardActionsFor(task({ status: 'waiting', session_id: null }))).toEqual(['test', 'done']);
  });

  it('failed with a session → Resume, Retry', () => {
    expect(cardActionsFor(task({ status: 'failed', session_id: 's1' }))).toEqual(['resume', 'retry']);
  });

  it('failed without a session → Retry only', () => {
    expect(cardActionsFor(task({ status: 'failed', session_id: null }))).toEqual(['retry']);
  });

  it('done and cancelled → Retry', () => {
    expect(cardActionsFor(task({ status: 'done' }))).toEqual(['retry']);
    expect(cardActionsFor(task({ status: 'cancelled' }))).toEqual(['retry']);
  });

  it('in_progress / committing → no quick actions', () => {
    expect(cardActionsFor(task({ status: 'in_progress' }))).toEqual([]);
    expect(cardActionsFor(task({ status: 'committing' }))).toEqual([]);
  });

  it('routine and archived cards have no quick actions', () => {
    expect(cardActionsFor(task({ status: 'waiting', kind: 'routine', session_id: 's1' }))).toEqual([]);
    expect(cardActionsFor(task({ status: 'done', archived: true }))).toEqual([]);
  });

  it('every action id has a render def', () => {
    const ids = new Set<string>();
    for (const s of ['backlog', 'waiting', 'failed', 'done', 'cancelled'] as const) {
      for (const a of cardActionsFor(task({ status: s, session_id: 's1' }))) ids.add(a);
    }
    for (const id of ids) expect(CARD_ACTION_DEFS[id as keyof typeof CARD_ACTION_DEFS]).toBeTruthy();
  });
});

describe('commandPaletteActionsFor', () => {
  it('appends a Sync action for waiting/failed', () => {
    expect(commandPaletteActionsFor(task({ status: 'waiting', session_id: 's1' }))).toEqual(['resume', 'test', 'done', 'sync']);
    expect(commandPaletteActionsFor(task({ status: 'failed', session_id: null }))).toEqual(['retry', 'sync']);
  });
  it('does not add Sync for other statuses', () => {
    expect(commandPaletteActionsFor(task({ status: 'backlog' }))).toEqual(['plan', 'start']);
    expect(commandPaletteActionsFor(task({ status: 'done' }))).toEqual(['retry']);
  });
  it('Sync has a render def', () => {
    expect(CARD_ACTION_DEFS.sync).toBeTruthy();
  });
});
