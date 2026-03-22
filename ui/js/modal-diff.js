// --- Diff helpers ---

function parseDiffByFile(diff) {
  const files = [];
  // Track current workspace section from "=== name ===" separators.
  let currentWorkspace = "";
  const blocks = diff.split(/(?=^diff --git )/m);
  for (const block of blocks) {
    if (!block.trim()) continue;
    const lines = block.split("\n");
    // Extract workspace separator if present (before or after diff content).
    for (const line of lines) {
      const wsMatch = line.match(/^=== (.+) ===$/);
      if (wsMatch) currentWorkspace = wsMatch[1];
    }
    const match = lines[0].match(/^diff --git a\/.+ b\/(.+)$/);
    if (!match) continue; // skip blocks without diff header (e.g. bare separators)
    const filename = match[1];
    let adds = 0,
      dels = 0;
    for (const line of lines.slice(1)) {
      if (line.startsWith("+") && !line.startsWith("+++")) adds++;
      if (line.startsWith("-") && !line.startsWith("---")) dels++;
    }
    files.push({
      filename,
      content: block,
      adds,
      dels,
      workspace: currentWorkspace,
    });
  }
  return files;
}

function renderDiffLine(line) {
  const escaped = escapeHtml(line);
  if (line.startsWith("+") && !line.startsWith("+++"))
    return `<span class="diff-line diff-add">${escaped}</span>`;
  if (line.startsWith("-") && !line.startsWith("---"))
    return `<span class="diff-line diff-del">${escaped}</span>`;
  if (line.startsWith("@@"))
    return `<span class="diff-line diff-hunk">${escaped}</span>`;
  if (/^(diff |--- |\+{3} |index |Binary )/.test(line))
    return `<span class="diff-line diff-header">${escaped}</span>`;
  return `<span class="diff-line">${escaped}</span>`;
}

// Map file extension (or full basename) to a highlight.js language name.
function extToLang(filename) {
  const basename = filename.split("/").pop().toLowerCase();
  if (basename === "dockerfile") return "dockerfile";
  if (basename === "makefile") return "makefile";
  const ext = basename.includes(".") ? basename.split(".").pop() : "";
  const map = {
    js: "javascript",
    mjs: "javascript",
    cjs: "javascript",
    jsx: "javascript",
    ts: "typescript",
    tsx: "typescript",
    py: "python",
    pyi: "python",
    go: "go",
    rs: "rust",
    java: "java",
    kt: "kotlin",
    kts: "kotlin",
    swift: "swift",
    c: "c",
    h: "c",
    cpp: "cpp",
    cc: "cpp",
    cxx: "cpp",
    hpp: "cpp",
    cs: "csharp",
    rb: "ruby",
    php: "php",
    css: "css",
    scss: "scss",
    sass: "scss",
    html: "xml",
    htm: "xml",
    svg: "xml",
    xml: "xml",
    json: "json",
    yaml: "yaml",
    yml: "yaml",
    toml: "ini",
    sh: "bash",
    bash: "bash",
    zsh: "bash",
    md: "markdown",
    mdx: "markdown",
    sql: "sql",
    r: "r",
    lua: "lua",
    pl: "perl",
    proto: "protobuf",
  };
  return map[ext] || null;
}

// Split a highlight.js HTML output string into an array of per-line HTML
// strings, properly closing and reopening <span> tags at line boundaries.
function splitHighlightedLines(html) {
  const lines = [];
  let current = "";
  const openSpans = [];
  let i = 0;
  while (i < html.length) {
    if (html[i] === "<") {
      const end = html.indexOf(">", i);
      if (end === -1) {
        current += html.slice(i);
        break;
      }
      const tag = html.slice(i, end + 1);
      if (tag.startsWith("<span")) {
        openSpans.push(tag);
        current += tag;
      } else if (tag === "</span>") {
        openSpans.pop();
        current += tag;
      } else {
        current += tag;
      }
      i = end + 1;
    } else if (html[i] === "\n") {
      // Close all open spans at line boundary, then reopen them on the next line.
      current += openSpans.map(() => "</span>").join("");
      lines.push(current);
      current = openSpans.join("");
      i++;
    } else {
      current += html[i];
      i++;
    }
  }
  if (current) lines.push(current);
  return lines;
}

function renderDiffFiles(container, diff) {
  if (!diff) {
    container.innerHTML =
      '<span class="text-xs text-v-muted">No changes</span>';
    return;
  }
  const files = parseDiffByFile(diff);
  if (files.length === 0) {
    container.innerHTML =
      '<span class="text-xs text-v-muted">No changes</span>';
    return;
  }
  let lastWorkspace = "";
  container.innerHTML = files
    .map((f) => {
      let wsHeader = "";
      if (f.workspace && f.workspace !== lastWorkspace) {
        lastWorkspace = f.workspace;
        wsHeader = `<div class="diff-workspace-header">${escapeHtml(f.workspace)}</div>`;
      }
      const statsHtml = [
        f.adds > 0 ? `<span class="diff-add">+${f.adds}</span>` : "",
        f.dels > 0 ? `<span class="diff-del">&minus;${f.dels}</span>` : "",
      ]
        .filter(Boolean)
        .join(" ");

      const codeLines = f.content
        .split("\n")
        .filter((l) => !/^=== .+ ===$/.test(l));
      const lang = extToLang(f.filename);
      let diffHtml;

      if (lang && typeof hljs !== "undefined") {
        // Classify each line and build the code-only array for the highlighter.
        // Header/hunk lines get an empty string placeholder so indices stay aligned.
        const lineMap = codeLines.map((line) => {
          if (/^(diff |--- |\+{3} |index |Binary )/.test(line))
            return { type: "header", code: line };
          if (line.startsWith("@@")) return { type: "hunk", code: line };
          if (line.startsWith("+") && !line.startsWith("+++"))
            return { type: "add", code: line.slice(1) };
          if (line.startsWith("-") && !line.startsWith("---"))
            return { type: "del", code: line.slice(1) };
          return { type: "ctx", code: line.length > 0 ? line.slice(1) : "" };
        });

        let hlLines = null;
        try {
          const codeOnly = lineMap.map((l) =>
            l.type === "header" || l.type === "hunk" ? "" : l.code,
          );
          const highlighted = hljs.highlight(codeOnly.join("\n"), {
            language: lang,
          }).value;
          hlLines = splitHighlightedLines(highlighted);
        } catch (_) {
          // fall through to plain rendering
        }

        if (hlLines) {
          diffHtml = lineMap
            .map((item, i) => {
              if (item.type === "header")
                return `<span class="diff-line diff-header">${escapeHtml(item.code)}</span>`;
              if (item.type === "hunk")
                return `<span class="diff-line diff-hunk">${escapeHtml(item.code)}</span>`;
              const prefix =
                item.type === "add" ? "+" : item.type === "del" ? "-" : " ";
              const cls =
                item.type === "add"
                  ? " diff-add"
                  : item.type === "del"
                    ? " diff-del"
                    : "";
              return `<span class="diff-line${cls}">${escapeHtml(prefix)}${hlLines[i] || ""}</span>`;
            })
            .join("\n");
        } else {
          diffHtml = codeLines.map(renderDiffLine).join("\n");
        }
      } else {
        diffHtml = codeLines.map(renderDiffLine).join("\n");
      }

      return (
        wsHeader +
        `<details class="diff-file">
      <summary class="diff-file-summary">
        <span class="diff-filename">${escapeHtml(f.filename)}</span>
        <span class="diff-stats">${statsHtml}</span>
      </summary>
      <pre class="diff-block diff-block-modal">${diffHtml}</pre>
    </details>`
      );
    })
    .join("");
}
