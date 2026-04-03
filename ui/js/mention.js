// --- @-mention file autocomplete ---
//
// Typing "@" in a prompt textarea opens a dropdown that filters workspace
// files as you type.  Select with Enter/Tab or click; dismiss with Escape.

const _mentionFiles = { list: null, loading: false };

async function _mentionLoadFiles() {
  if (_mentionFiles.list !== null) return _mentionFiles.list;
  if (_mentionFiles.loading) return [];
  _mentionFiles.loading = true;
  try {
    const res = await api("/api/files");
    _mentionFiles.list = res.files || [];
  } catch (e) {
    _mentionFiles.list = [];
  }
  _mentionFiles.loading = false;
  return _mentionFiles.list;
}

// Returns { query, atIdx } when the cursor is right after an active "@mention",
// or null when the cursor is not inside one.
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

function _scrollSelectedIntoView(dropdown, index) {
  if (!dropdown || index < 0) return;
  var item = dropdown.children && dropdown.children[index];
  if (item && typeof item.scrollIntoView === "function") {
    item.scrollIntoView({ block: "nearest" });
  }
}

// Attach @-mention autocomplete to a single textarea element.
// Options:
//   position: "below" (default) — dropdown appears below the textarea
//   position: "above" — dropdown appears above the textarea
//   priorityPrefix: path prefix to boost in ranking (e.g. "specs/")
function attachMentionAutocomplete(textarea, opts) {
  if (!textarea) return;
  var position = (opts && opts.position) || "below";
  var priorityPrefix = (opts && opts.priorityPrefix) || "";

  let dropdown = null;
  let selectedIndex = -1;
  let currentMatches = [];
  // Tracks the async load so we can cancel stale renders.
  let renderGeneration = 0;

  function closeDropdown() {
    if (dropdown) {
      dropdown.remove();
      dropdown = null;
    }
    selectedIndex = -1;
    currentMatches = [];
  }

  function selectFile(file) {
    const info = _mentionGetQuery(textarea);
    if (!info) {
      closeDropdown();
      return;
    }
    const { atIdx } = info;
    const cursorPos = textarea.selectionStart;
    const before = textarea.value.substring(0, atIdx);
    const after = textarea.value.substring(cursorPos);
    const insert = "@" + file + " ";
    textarea.value = before + insert + after;
    const newPos = before.length + insert.length;
    textarea.setSelectionRange(newPos, newPos);
    // Notify listeners (e.g. auto-save in tasks.js).
    textarea.dispatchEvent(new Event("input", { bubbles: true }));
    closeDropdown();
    textarea.focus();
  }

  function renderItems(matches) {
    if (!dropdown) {
      dropdown = document.createElement("div");
      dropdown.className = "mention-dropdown";
      document.body.appendChild(dropdown);
    }

    const rect = textarea.getBoundingClientRect();
    dropdown.style.left = rect.left + "px";
    dropdown.style.width = Math.max(320, rect.width) + "px";
    if (position === "above") {
      dropdown.style.bottom = window.innerHeight - rect.top + 4 + "px";
      dropdown.style.top = "auto";
    } else {
      dropdown.style.top = rect.bottom + 4 + "px";
      dropdown.style.bottom = "auto";
    }

    dropdown.innerHTML = "";

    if (matches.length === 0) {
      const empty = document.createElement("div");
      empty.className = "mention-item mention-empty";
      empty.textContent = "No matching files";
      dropdown.appendChild(empty);
      currentMatches = [];
      return;
    }

    currentMatches = matches;
    // Auto-select first item when dropdown opens; clamp when result count shrinks.
    if (selectedIndex < 0) selectedIndex = 0;
    selectedIndex = Math.min(selectedIndex, matches.length - 1);
    matches.forEach((file, i) => {
      const item = document.createElement("div");
      item.className =
        "mention-item" + (i === selectedIndex ? " mention-item-selected" : "");
      item.dataset.index = i;

      const parts = file.split("/");
      const basename = parts.pop();
      const dir = parts.join("/");

      const nameEl = document.createElement("span");
      nameEl.className = "mention-filename";
      nameEl.textContent = basename;

      const pathEl = document.createElement("span");
      pathEl.className = "mention-path";
      pathEl.textContent = dir ? dir + "/" : "";

      item.appendChild(pathEl);
      item.appendChild(nameEl);

      item.addEventListener("mousedown", (e) => {
        e.preventDefault(); // Keep textarea focused.
        selectFile(file);
      });
      dropdown.appendChild(item);
    });
  }

  async function update() {
    const info = _mentionGetQuery(textarea);
    if (!info) {
      closeDropdown();
      return;
    }

    const gen = ++renderGeneration;
    const files = await _mentionLoadFiles();
    if (gen !== renderGeneration) return; // Stale — a newer update superseded this one.

    const matches = _mentionFilter(files, info.query, priorityPrefix);
    renderItems(matches);
  }

  textarea.addEventListener("input", update);

  // Also re-evaluate on cursor movement (keyboard nav, click inside textarea).
  textarea.addEventListener("keyup", (e) => {
    if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key)) update();
  });
  textarea.addEventListener("click", update);

  textarea.addEventListener("keydown", (e) => {
    if (!dropdown) return;

    if (e.key === "ArrowDown") {
      e.preventDefault();
      selectedIndex =
        currentMatches.length > 0
          ? (selectedIndex + 1) % currentMatches.length
          : 0;
      renderItems(currentMatches);
      _scrollSelectedIntoView(dropdown, selectedIndex);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      selectedIndex =
        currentMatches.length > 0
          ? (selectedIndex - 1 + currentMatches.length) % currentMatches.length
          : 0;
      renderItems(currentMatches);
      _scrollSelectedIntoView(dropdown, selectedIndex);
    } else if ((e.key === "Enter" || e.key === "Tab") && selectedIndex >= 0) {
      e.preventDefault();
      selectFile(currentMatches[selectedIndex]);
    } else if (e.key === "Escape") {
      e.stopPropagation();
      closeDropdown();
    }
  });

  textarea.addEventListener("blur", () => {
    // Delay slightly so a mousedown on a dropdown item fires first.
    setTimeout(closeDropdown, 150);
  });

  // Reposition or close on window scroll/resize.
  window.addEventListener("scroll", closeDropdown, { passive: true });
  window.addEventListener("resize", closeDropdown, { passive: true });
}

// Attach to all prompt textareas that exist at load time.
attachMentionAutocomplete(document.getElementById("new-prompt"));
attachMentionAutocomplete(document.getElementById("modal-edit-prompt"));
attachMentionAutocomplete(document.getElementById("modal-retry-prompt"));
