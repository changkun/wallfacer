// --- ANSI terminal rendering ---

// ANSI foreground colors tuned for the dark (#0d1117) terminal background.
const ANSI_FG = ['#484f58','#ff7b72','#3fb950','#e3b341','#79c0ff','#ff79c6','#39c5cf','#b1bac4'];
const ANSI_FG_BRIGHT = ['#6e7681','#ffa198','#56d364','#f8e3ad','#cae8ff','#fecfe8','#b3f0ff','#ffffff'];

// Convert ANSI escape codes to HTML <span> tags.
// Carriage returns are collapsed so only the last overwrite per line is shown,
// matching how a real terminal renders spinner animations.
function ansiToHtml(rawText) {
  const lines = rawText.split('\n');
  const text = lines.map(line => {
    const parts = line.split('\r');
    return parts[parts.length - 1];
  }).join('\n');

  const seqRegex = /\x1b\[([0-9;]*)([A-Za-z])/g;
  let result = '';
  let lastIndex = 0;
  let openSpans = 0;
  let match;

  function esc(s) {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }

  while ((match = seqRegex.exec(text)) !== null) {
    if (match.index > lastIndex) result += esc(text.slice(lastIndex, match.index));
    lastIndex = seqRegex.lastIndex;

    if (match[2] === 'm') {
      while (openSpans > 0) { result += '</span>'; openSpans--; }
      const codes = match[1] ? match[1].split(';').map(Number) : [0];
      let style = '';
      let i = 0;
      while (i < codes.length) {
        const c = codes[i];
        if (c === 1) style += 'font-weight:bold;';
        else if (c === 2) style += 'opacity:0.6;';
        else if (c === 3) style += 'font-style:italic;';
        else if (c === 4) style += 'text-decoration:underline;';
        else if (c >= 30 && c <= 37) style += `color:${ANSI_FG[c - 30]};`;
        else if (c >= 90 && c <= 97) style += `color:${ANSI_FG_BRIGHT[c - 90]};`;
        else if (c === 38 && codes[i + 1] === 2 && i + 4 < codes.length) {
          style += `color:rgb(${codes[i + 2]},${codes[i + 3]},${codes[i + 4]});`;
          i += 4;
        }
        i++;
      }
      if (style) { result += `<span style="${style}">`; openSpans++; }
    }
    // Other ANSI commands (cursor movement, erase-line, etc.) are intentionally ignored.
  }

  if (lastIndex < text.length) result += esc(text.slice(lastIndex));
  while (openSpans > 0) { result += '</span>'; openSpans--; }
  return result;
}
