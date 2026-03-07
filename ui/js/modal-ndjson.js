// --- Pretty NDJSON rendering (Claude Code terminal style) ---

function parseNdjsonLine(line) {
  const t = line.trim();
  if (t.length === 0 || t[0] !== '{') return null;
  try { return JSON.parse(t); } catch { return null; }
}

function extractToolInput(name, inputObj) {
  if (!inputObj || typeof inputObj !== 'object') return '';
  switch (name) {
    case 'Bash': return inputObj.command || '';
    case 'Read': return inputObj.file_path || '';
    case 'Write': return inputObj.file_path || '';
    case 'Edit': return inputObj.file_path || '';
    case 'Glob': return inputObj.pattern || '';
    case 'Grep': return inputObj.pattern || '';
    case 'WebFetch': return inputObj.url || '';
    case 'WebSearch': return inputObj.query || '';
    case 'Task': return inputObj.prompt ? inputObj.prompt.slice(0, 120) : '';
    case 'TodoWrite': return inputObj.todos ? `${inputObj.todos.length} items` : '';
    default: {
      // Try common keys
      for (const key of ['file_path', 'command', 'pattern', 'query', 'path']) {
        if (inputObj[key]) return String(inputObj[key]);
      }
      return '';
    }
  }
}

function renderPrettyLogs(rawBuffer) {
  const lines = rawBuffer.split('\n');
  const blocks = [];

  for (const line of lines) {
    const evt = parseNdjsonLine(line);
    if (!evt) {
      // Non-JSON line (stderr progress output) — render with ANSI colors.
      const trimmed = line.trim();
      if (trimmed) {
        blocks.push(`<div class="cc-block cc-stderr">${ansiToHtml(line)}</div>`);
      }
      continue;
    }

    if (evt.type === 'assistant' && evt.message && evt.message.content) {
      for (const block of evt.message.content) {
        if (block.type === 'text' && block.text) {
          blocks.push(`<div class="cc-block cc-text"><span class="cc-marker">&#x23FA;</span> ${escapeHtml(block.text)}</div>`);
        } else if (block.type === 'tool_use') {
          let input = '';
          if (block.input) {
            const parsed = typeof block.input === 'string' ? (() => { try { return JSON.parse(block.input); } catch { return null; } })() : block.input;
            input = parsed ? extractToolInput(block.name, parsed) : '';
          }
          const inputHtml = input ? `(<span class="cc-tool-input">${escapeHtml(input.length > 200 ? input.slice(0, 200) + '\u2026' : input)}</span>)` : '';
          blocks.push(`<div class="cc-block cc-tool-call"><span class="cc-marker">&#x23FA;</span> <span class="cc-tool-name">${escapeHtml(block.name)}</span>${inputHtml}</div>`);
        }
      }
    } else if (evt.type === 'user' && evt.message && evt.message.content) {
      for (const block of evt.message.content) {
        if (block.type !== 'tool_result') continue;
        let text = '';
        if (Array.isArray(block.content)) {
          for (const c of block.content) {
            if (c.text) text += c.text;
          }
        } else if (typeof block.content === 'string') {
          text = block.content;
        }
        if (!text) {
          blocks.push(`<div class="cc-block cc-tool-result"><span class="cc-result-pipe">&#x23BF;</span> <span class="cc-result-empty">(No output)</span></div>`);
          continue;
        }
        // Clean Read tool output: "   123→\tcode" → "   123  code"
        text = text.replace(/^(\s*\d+)→\t?/gm, '$1  ');
        const resultLines = text.split('\n');
        if (resultLines.length > 5) {
          const preview = resultLines.slice(0, 3).map(l => escapeHtml(l)).join('\n');
          const rest = resultLines.slice(3).map(l => escapeHtml(l)).join('\n');
          const remaining = resultLines.length - 3;
          blocks.push(`<div class="cc-block cc-tool-result"><span class="cc-result-pipe">&#x23BF;</span> <pre class="cc-result-text">${preview}</pre><details class="cc-expand"><summary class="cc-expand-toggle">+${remaining} lines</summary><pre class="cc-result-text">${rest}</pre></details></div>`);
        } else {
          blocks.push(`<div class="cc-block cc-tool-result"><span class="cc-result-pipe">&#x23BF;</span> <pre class="cc-result-text">${escapeHtml(text)}</pre></div>`);
        }
      }
    } else if (evt.type === 'result') {
      if (evt.result) {
        blocks.push(`<div class="cc-block cc-final-result"><span class="cc-marker cc-marker-result">&#x23FA;</span> <span class="cc-result-label">[Result]</span> ${escapeHtml(evt.result)}</div>`);
      }
    }
  }

  return blocks.join('');
}
