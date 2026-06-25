// ANSI escape-code → HTML renderer for log text. Ported from
// ui/js/modal-ansi.js so the Activity tab and any future raw-stderr view
// can colour CLI output without a heavyweight terminal emulator. Handles:
//
//   - Standard / bright 16-colour foregrounds (codes 30–37, 90–97)
//   - Bold / dim / italic / underline (1 / 2 / 3 / 4)
//   - 24-bit colour escapes (38;2;r;g;b)
//   - Carriage returns collapsed per line so spinner animations render
//     as their last overwrite, matching a real terminal.
//
// Other CSI sequences (cursor movement, erase-line, etc.) are dropped.

const ANSI_FG = [
  '#484f58', '#ff7b72', '#3fb950', '#e3b341',
  '#79c0ff', '#ff79c6', '#39c5cf', '#b1bac4',
];
const ANSI_FG_BRIGHT = [
  '#6e7681', '#ffa198', '#56d364', '#f8e3ad',
  '#cae8ff', '#fecfe8', '#b3f0ff', '#ffffff',
];

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

/** Drop everything before the last \r on each line. */
export function collapseCarriageReturns(raw: string): string {
  return raw.split('\n').map((line) => {
    const parts = line.split('\r');
    return parts[parts.length - 1];
  }).join('\n');
}

/** Convert ANSI-escaped text to HTML with span colouring. Output is safe
 *  to drop into v-html (input HTML metacharacters are escaped first). */
export function ansiToHtml(rawText: string): string {
  const text = collapseCarriageReturns(rawText);
  // eslint-disable-next-line no-control-regex
  const seqRegex = /\x1b\[([0-9;]*)([A-Za-z])/g;
  let result = '';
  let lastIndex = 0;
  let openSpans = 0;
  let match: RegExpExecArray | null;

  while ((match = seqRegex.exec(text)) !== null) {
    if (match.index > lastIndex) result += escapeHtml(text.slice(lastIndex, match.index));
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
        else if (c === 38 || c === 48) {
          // Extended-colour operands: 38/48;5;n (256-colour) or 38/48;2;r;g;b
          // (24-bit). Consume the operand run so it is not reparsed as
          // standalone SGR codes. Only 24-bit foreground is rendered; the
          // rest are consumed and dropped.
          if (codes[i + 1] === 2) {
            if (c === 38 && i + 4 < codes.length) {
              style += `color:rgb(${codes[i + 2]},${codes[i + 3]},${codes[i + 4]});`;
            }
            i += 4;
          } else if (codes[i + 1] === 5) {
            i += 2;
          }
        }
        i++;
      }
      if (style) {
        result += `<span style="${style}">`;
        openSpans++;
      }
    }
    // Non-SGR sequences are intentionally ignored.
  }

  if (lastIndex < text.length) result += escapeHtml(text.slice(lastIndex));
  while (openSpans > 0) { result += '</span>'; openSpans--; }
  return result;
}
