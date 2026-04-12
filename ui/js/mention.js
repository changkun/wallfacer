// --- @-mention file autocomplete ---
//
// Typing "@" in a prompt textarea opens a dropdown that filters workspace
// files as you type.  Select with Enter/Tab or click; dismiss with Escape.
//
// The dropdown UI, keyboard nav, and lifecycle live in the shared
// `attachAutocomplete` widget (ui/js/lib/autocomplete.ts). This file
// supplies only the @-specific bits: query detection, file loading,
// ranking, and splice-on-select.

/* global attachAutocomplete */

const _mentionFiles = { list: null, loading: false };

async function _mentionLoadFiles() {
  if (_mentionFiles.list !== null) return _mentionFiles.list;
  if (_mentionFiles.loading) return [];
  _mentionFiles.loading = true;
  try {
    const res = await api("/api/files");
    _mentionFiles.list = res.files || [];
  } catch (_e) {
    _mentionFiles.list = [];
  }
  _mentionFiles.loading = false;
  return _mentionFiles.list;
}

// Returns { query, atIdx } when the cursor sits inside an active
// "@mention", or null when it doesn't.
function _mentionGetQuery(textarea) {
  const pos = textarea.selectionStart;
  const text = textarea.value.substring(0, pos);
  const atIdx = text.lastIndexOf("@");
  if (atIdx === -1) return null;
  // The "@" must be at the start of the text or preceded by whitespace.
  if (atIdx > 0 && !/\s/.test(text[atIdx - 1])) return null;
  const query = text.substring(atIdx + 1);
  // Spaces or newlines inside the query mean the mention is over.
  if (/[\s]/.test(query)) return null;
  return { query, atIdx };
}

// priorityPrefix: optional path prefix (e.g. "specs/") to boost in ranking.
function _mentionFilter(files, query, priorityPrefix) {
  const lower = (query || "").toLowerCase();
  const scored = [];
  for (let i = 0; i < files.length; i++) {
    const f = files[i];
    const fl = f.toLowerCase();
    if (lower && !fl.includes(lower)) continue;
    const base = fl.split("/").pop();
    // Lower score = higher rank.
    // Priority prefix files get a -2 bonus; basename match gets -1.
    let score = 2;
    // Check if the path contains the priority prefix anywhere (not just at start),
    // because file paths are prefixed with the workspace basename (e.g. "repo/specs/...").
    if (
      priorityPrefix &&
      (fl.startsWith(priorityPrefix.toLowerCase()) ||
        fl.includes("/" + priorityPrefix.toLowerCase()))
    )
      score -= 2;
    if (!lower || base.includes(lower)) score -= 1;
    scored.push({ f, score, idx: i });
  }
  // Stable sort: same score preserves original order.
  scored.sort((a, b) => a.score - b.score || a.idx - b.idx);
  return scored.slice(0, 20).map((s) => s.f);
}

// Attach @-mention autocomplete to a single textarea element.
// Options:
//   position: "below" (default) — dropdown appears below the textarea
//   position: "above" — dropdown appears above the textarea
//   priorityPrefix: path prefix to boost in ranking (e.g. "specs/")
function attachMentionAutocomplete(textarea, opts) {
  if (!textarea) return;
  const position = (opts && opts.position) || "below";
  const priorityPrefix = (opts && opts.priorityPrefix) || "";

  return attachAutocomplete(textarea, {
    position,
    emptyMessage: "No matching files",
    shouldActivate: (ta) => {
      const q = _mentionGetQuery(ta);
      return q ? { query: q.query, startIdx: q.atIdx } : null;
    },
    fetchItems: async (match) => {
      const files = await _mentionLoadFiles();
      return _mentionFilter(files, match.query, priorityPrefix);
    },
    renderItem: (file) => {
      const item = document.createElement("div");
      item.className = "mention-item";
      const parts = file.split("/");
      const basename = parts.pop();
      const dir = parts.join("/");
      const pathEl = document.createElement("span");
      pathEl.className = "mention-path";
      pathEl.textContent = dir ? dir + "/" : "";
      const nameEl = document.createElement("span");
      nameEl.className = "mention-filename";
      nameEl.textContent = basename;
      item.appendChild(pathEl);
      item.appendChild(nameEl);
      return item;
    },
    onSelect: (file, ta, match) => {
      const cursorPos = ta.selectionStart;
      const before = ta.value.substring(0, match.startIdx);
      const after = ta.value.substring(cursorPos);
      const insert = "@" + file + " ";
      ta.value = before + insert + after;
      const newPos = before.length + insert.length;
      ta.setSelectionRange(newPos, newPos);
      // Notify listeners (e.g. auto-save in tasks.js).
      ta.dispatchEvent(new Event("input", { bubbles: true }));
      ta.focus();
    },
  });
}

// Attach to all prompt textareas that exist at load time.
attachMentionAutocomplete(document.getElementById("new-prompt"));
attachMentionAutocomplete(document.getElementById("modal-edit-prompt"));
attachMentionAutocomplete(document.getElementById("modal-retry-prompt"));
