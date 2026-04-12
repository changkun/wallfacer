// Global type declarations for the wallfacer frontend.
//
// Files under ui/js/ run in the browser's global scope (no ES modules).
// As files migrate to TypeScript, their top-level function and variable
// declarations need ambient declarations here so other files that
// reference them as globals still type-check.
//
// Grow this file as migration progresses. When every module has migrated,
// this file is the complete catalog of the frontend's global surface.

export {};

declare global {
  // --- ui/js/lib/clipboard.ts ---
  function copyWithFeedback(
    text: string,
    btn: HTMLElement | null,
    feedback?: string,
    duration?: number,
  ): void;

  // --- ui/js/lib/scheduling.ts ---
  function createRAFScheduler(callback: () => void): () => void;

  // --- ui/js/lib/toggle.ts ---
  function toggleRenderedRaw(
    renderedEl: HTMLElement | null,
    rawEl: HTMLElement | null,
    btn?: HTMLElement | null,
  ): void;

  // --- ui/js/lib/formatting.ts ---
  function escapeHtml(s: string | null | undefined): string;
  function fmtMs(ms: number): string;
  function timeAgo(dateStr: string): string;
  function formatTimeout(minutes: number): string;

  // --- ui/js/lib/tab-switcher.ts ---
  function createTabSwitcher(opts: {
    tabs: string[];
    prefix: string;
    onActivate?: Record<string, (tab: string) => void>;
    onSwitch?: (tab: string) => void;
  }): (tab: string) => void;

  // --- ui/js/lib/modal.ts ---
  function openModalPanel(modal: HTMLElement | null): void;
  function closeModalPanel(modal: HTMLElement | null): void;
  function bindModalDismiss(
    modal: HTMLElement | null,
    onClose: () => void,
  ): () => void;
  function createModalStateController(nodes: {
    loadingEl?: HTMLElement | null;
    errorEl?: HTMLElement | null;
    emptyEl?: HTMLElement | null;
    contentEl?: HTMLElement | null;
    contentState?: string;
  }): (state: string, msg?: string) => void;

  // --- ui/js/lib/modal-controller.ts ---
  function createModalController(
    modalId: string,
    opts?: { onOpen?: () => void; onClose?: () => void },
  ): { open: () => void; close: () => void };

  // --- ui/js/time-map.ts ---
  function buildTimeMap(
    spans: Array<{ startMs: number; endMs: number }> | null | undefined,
    globalStartMs: number,
    globalEndMs: number,
  ): {
    toPercent: (ms: number) => number;
    fromPercent: (pct: number) => number;
    segments: Array<{
      start: number;
      end: number;
      isGap: boolean;
      visualWeight?: number;
      visualStart?: number;
      visualEnd?: number;
      compressed?: boolean;
    }>;
    compressed: boolean;
    totalVisual?: number;
  };

  // --- ui/js/oversight-shared.ts ---
  function buildPhaseListHTML(
    phases:
      | Array<{
          title?: string;
          timestamp?: string;
          summary?: string;
          tools_used?: string[];
          commands?: string[];
          actions?: string[];
        }>
      | null
      | undefined,
  ): string;

  // --- ui/js/tab-leader.ts ---
  // Defined in api.js (still JavaScript); tab-leader's election step
  // checks `typeof restartActiveStreams === "function"` before calling.
  function restartActiveStreams(): void;

  interface Window {
    _sseIsLeader: () => boolean;
    _sseRelay: (
      eventName: string,
      data: unknown,
      lastEventId?: string | null,
    ) => void;
    _sseOnFollowerEvent: (
      eventName: string,
      handler: (data: unknown, lastEventId: string | null) => void,
    ) => void;
  }
}
