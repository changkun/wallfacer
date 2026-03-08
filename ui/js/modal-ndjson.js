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

function stripCodexShellWrapper(command) {
  const raw = String(command || '').trim();
  const prefix = '/bin/bash -lc ';
  if (!raw.startsWith(prefix)) return raw;
  let rest = raw.slice(prefix.length).trim();
  if ((rest.startsWith("'") && rest.endsWith("'")) || (rest.startsWith('"') && rest.endsWith('"'))) {
    rest = rest.slice(1, -1);
  }
  return rest.replace(/\\'/g, "'").replace(/\\"/g, '"');
}

function renderToolResultBlock(text) {
  if (!text) {
    return `<div class="cc-block cc-tool-result"><span class="cc-result-pipe">&#x23BF;</span> <span class="cc-result-empty">(No output)</span></div>`;
  }
  // Clean Read tool output: "   123→\tcode" → "   123  code"
  text = text.replace(/^(\s*\d+)→\t?/gm, '$1  ');
  const resultLines = text.split('\n');
  if (resultLines.length > 5) {
    const preview = resultLines.slice(0, 3).map(l => escapeHtml(l)).join('\n');
    const rest = resultLines.slice(3).map(l => escapeHtml(l)).join('\n');
    const remaining = resultLines.length - 3;
    return `<div class="cc-block cc-tool-result"><span class="cc-result-pipe">&#x23BF;</span> <pre class="cc-result-text">${preview}</pre><details class="cc-expand"><summary class="cc-expand-toggle">+${remaining} lines</summary><pre class="cc-result-text">${rest}</pre></details></div>`;
  }
  return `<div class="cc-block cc-tool-result"><span class="cc-result-pipe">&#x23BF;</span> <pre class="cc-result-text">${escapeHtml(text)}</pre></div>`;
}

function renderPrettyLogs(rawBuffer) {
  const lines = rawBuffer.split('\n');
  const blocks = [];
  const codexSeenCommandStarts = new Set();

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
        blocks.push(renderToolResultBlock(text));
      }
    } else if ((evt.type === 'item.started' || evt.type === 'item.completed') && evt.item) {
      const item = evt.item;
      if (item.type === 'agent_message' && item.text) {
        blocks.push(`<div class="cc-block cc-text"><span class="cc-marker">&#x23FA;</span> ${escapeHtml(item.text)}</div>`);
        continue;
      }
      if (item.type !== 'command_execution') continue;
      const itemId = item.id || '';
      const command = stripCodexShellWrapper(item.command || '');
      const commandHtml = command ? `(<span class="cc-tool-input">${escapeHtml(command.length > 200 ? command.slice(0, 200) + '\u2026' : command)}</span>)` : '';
      const shouldRenderCall = evt.type === 'item.started' || !itemId || !codexSeenCommandStarts.has(itemId);
      if (shouldRenderCall) {
        blocks.push(`<div class="cc-block cc-tool-call"><span class="cc-marker">&#x23FA;</span> <span class="cc-tool-name">Bash</span>${commandHtml}</div>`);
      }
      if (itemId && evt.type === 'item.started') {
        codexSeenCommandStarts.add(itemId);
      }
      if (evt.type === 'item.completed') {
        let text = '';
        if (typeof item.aggregated_output === 'string' && item.aggregated_output.trim()) {
          text = item.aggregated_output;
        } else if (item.exit_code !== null && item.exit_code !== undefined && Number(item.exit_code) !== 0) {
          text = `Command exited with code ${item.exit_code}`;
        }
        blocks.push(renderToolResultBlock(text));
      }
    } else if (evt.type === 'result') {
      if (evt.result) {
        blocks.push(`<div class="cc-block cc-final-result"><span class="cc-marker cc-marker-result">&#x23FA;</span> <span class="cc-result-label">[Result]</span> ${escapeHtml(evt.result)}</div>`);
      }
    }
  }

  return blocks.join('');
}
