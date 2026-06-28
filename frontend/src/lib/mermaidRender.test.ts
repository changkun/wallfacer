import { describe, it, expect, vi, beforeEach } from 'vitest';

// Mock the lazy-loaded mermaid module so we can drive parse()/render() outcomes.
vi.mock('mermaid', () => ({
  default: {
    initialize: vi.fn(),
    parse: vi.fn(),
    render: vi.fn(),
  },
}));

import mermaid from 'mermaid';
import { enhanceMermaid } from './mermaidRender';

// makeBlock builds the placeholder the markdown fence renderer emits: a
// .mermaid-block carrying the source in data-mermaid plus the .mermaid-src
// <pre> fallback shown until (and if) rendering succeeds.
function makeBlock(code: string): HTMLElement {
  const wrap = document.createElement('div');
  const inner = document.createElement('div');
  inner.className = 'mermaid-block';
  inner.setAttribute('data-mermaid', code);
  const pre = document.createElement('pre');
  pre.className = 'mermaid-src';
  pre.innerHTML = '<code></code>';
  inner.appendChild(pre);
  wrap.appendChild(inner);
  document.body.appendChild(wrap);
  return wrap;
}

describe('enhanceMermaid', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
  });

  it('never calls render() (no bomb SVG) when the diagram is invalid', async () => {
    // parse() reports the diagram as invalid by returning false.
    (mermaid.parse as ReturnType<typeof vi.fn>).mockResolvedValue(false);

    const wrap = makeBlock('graph TD; A[/unterminated');
    await enhanceMermaid(wrap);

    // render() is the call that injects mermaid's "Syntax error" bomb into the
    // DOM and throws; guarding with parse() means it must not run at all.
    expect(mermaid.render).not.toHaveBeenCalled();

    const block = wrap.querySelector('.mermaid-block')!;
    expect(block.classList.contains('mermaid-rendered')).toBe(false);
    expect(block.classList.contains('mermaid-error')).toBe(true);
    // The source fallback stays visible instead of a bomb tile.
    expect(block.querySelector('.mermaid-src')).toBeTruthy();
    expect(block.querySelector('svg')).toBeNull();
  });

  it('renders valid diagrams into an SVG', async () => {
    (mermaid.parse as ReturnType<typeof vi.fn>).mockResolvedValue({});
    (mermaid.render as ReturnType<typeof vi.fn>).mockResolvedValue({
      svg: '<svg id="ok"></svg>',
    });

    const wrap = makeBlock('graph TD; A-->B');
    await enhanceMermaid(wrap);

    expect(mermaid.render).toHaveBeenCalledTimes(1);
    const block = wrap.querySelector('.mermaid-block')!;
    expect(block.classList.contains('mermaid-rendered')).toBe(true);
    expect(block.classList.contains('mermaid-error')).toBe(false);
    expect(block.querySelector('svg')).toBeTruthy();
  });
});
