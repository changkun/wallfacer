/**
 * Tests for utility alert and layout helpers.
 */
import { describe, it, expect, vi } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';
import vm from 'vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, '..');

function createElement(overrides = {}) {
  return {
    classList: {
      add: vi.fn(),
      remove: vi.fn(),
    },
    style: {},
    textContent: '',
    focus: vi.fn(),
    scrollIntoView: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    ...overrides,
  };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  return vm.createContext({
    console,
    document: {
      getElementById: (id) => elements.get(id) || null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    },
    ...overrides,
  });
}

function loadScript(ctx, filename) {
  const code = readFileSync(join(jsDir, filename), 'utf8');
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

describe('utils alerts', () => {
  it('opens alert modal with message and closes it', () => {
    const alertMessage = createElement();
    const alertModal = createElement();
    const okButton = createElement();
    const ctx = makeContext({
      elements: [
        ['alert-message', alertMessage],
        ['alert-modal', alertModal],
        ['alert-ok-btn', okButton],
      ],
    });
    loadScript(ctx, 'utils.js');

    ctx.showAlert('Need attention');
    expect(alertMessage.textContent).toBe('Need attention');
    expect(alertModal.classList.add).toHaveBeenCalledWith('flex');
    expect(okButton.focus).toHaveBeenCalledTimes(1);
    expect(alertModal.classList.remove).toHaveBeenCalledWith('hidden');

    ctx.closeAlert();
    expect(alertModal.classList.add).toHaveBeenCalledWith('hidden');
    expect(alertModal.classList.remove).toHaveBeenCalledWith('flex');
  });
});

describe('scrollToColumn', () => {
  it('scrolls the column target into view when it exists', () => {
    const target = createElement({
      scrollIntoView: vi.fn(),
    });
    const ctx = makeContext({
      elements: [['col-wrapper-backlog', target]],
    });
    loadScript(ctx, 'utils.js');

    ctx.scrollToColumn('col-wrapper-backlog');
    expect(target.scrollIntoView).toHaveBeenCalledWith({ behavior: 'smooth', block: 'nearest', inline: 'start' });
  });

  it('does nothing when target is missing', () => {
    const ctx = makeContext();
    loadScript(ctx, 'utils.js');
    expect(() => ctx.scrollToColumn('missing')).not.toThrow();
  });
});
