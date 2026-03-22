// --- Search / filter ---

/**
 * Returns true when task t matches the current filterQuery.
 * Matching is case-insensitive and checks both title and prompt fields.
 */
function matchesFilter(t) {
  if (!filterQuery) return true;
  const q = filterQuery.toLowerCase();
  const tokens = q.split(/\s+/).filter(Boolean);
  const tagTokens = tokens
    .filter((tok) => tok.startsWith("#"))
    .map((tok) => tok.slice(1));
  const textTokens = tokens.filter((tok) => !tok.startsWith("#"));

  if (tagTokens.length > 0) {
    const taskTags = (t.tags || []).map((tag) => String(tag).toLowerCase());
    if (!tagTokens.every((tagToken) => taskTags.includes(tagToken)))
      return false;
    if (textTokens.length === 0) return true;
  }

  const title = (t.title || "").toLowerCase();
  const prompt = (t.prompt || "").toLowerCase();
  const tagText = (t.tags || []).join(" ").toLowerCase();
  return textTokens.every(
    (tok) =>
      title.includes(tok) || prompt.includes(tok) || tagText.includes(tok),
  );
}

/**
 * Escapes text for safe HTML embedding and wraps the first occurrence of
 * query with a <mark> element for visual highlighting.
 * Falls back to plain escapeHtml when there is no query or no match found.
 */
function highlightMatch(text, query) {
  if (!query || !text) return escapeHtml(text);
  const idx = text.toLowerCase().indexOf(query.toLowerCase());
  if (idx === -1) return escapeHtml(text);
  return (
    escapeHtml(text.slice(0, idx)) +
    '<mark class="search-highlight">' +
    escapeHtml(text.slice(idx, idx + query.length)) +
    "</mark>" +
    escapeHtml(text.slice(idx + query.length))
  );
}

// ─── Server-side search (query starts with @) ─────────────────────────────

let _searchTimer = null;

function triggerServerSearch(rawQuery) {
  const q = rawQuery.slice(1).trim(); // strip leading @
  clearTimeout(_searchTimer);
  if (Array.from(q).length < 2) {
    hideSearchPanel();
    return;
  }
  _searchTimer = setTimeout(() => {
    apiGet("/api/tasks/search?q=" + encodeURIComponent(q))
      .then((results) => renderSearchPanel(results, q))
      .catch(() => hideSearchPanel());
  }, 250);
}

function renderSearchPanel(results, q) {
  const panel = document.getElementById("search-results-panel");
  if (!panel) return;
  if (!results || results.length === 0) {
    panel.innerHTML =
      '<div class="search-no-results">No results for <em>' +
      escapeHtml(q) +
      "</em></div>";
  } else {
    panel.innerHTML = results
      .map((r) => {
        const badge =
          '<span class="search-field-badge search-field-badge--' +
          escapeHtml(r.matched_field) +
          '">' +
          escapeHtml(r.matched_field) +
          "</span>";
        const label = escapeHtml(r.title || r.id);
        // r.snippet is already HTML-escaped by the server — embed as innerHTML directly.
        return (
          '<div class="search-result-item" data-id="' +
          escapeHtml(r.id) +
          '">' +
          badge +
          " <strong>" +
          label +
          "</strong>" +
          '<div class="search-result-snippet">' +
          r.snippet +
          "</div>" +
          "</div>"
        );
      })
      .join("");
    panel.querySelectorAll(".search-result-item").forEach((el) => {
      el.addEventListener("click", () => {
        hideSearchPanel();
        openModal(el.dataset.id);
      });
    });
  }
  panel.style.display = "block";
}

function hideSearchPanel() {
  const panel = document.getElementById("search-results-panel");
  if (panel) panel.style.display = "none";
}

// Wire up the search input and clear button once the DOM is ready.
(function initSearch() {
  function setup() {
    const input = document.getElementById("task-search");
    const clearBtn = document.getElementById("task-search-clear");
    if (!input) return;

    // Create the server-search results panel once.
    const panel = document.createElement("div");
    panel.id = "search-results-panel";
    panel.className = "search-results-panel";
    panel.style.display = "none";
    input.parentElement.appendChild(panel);

    input.addEventListener("input", function () {
      filterQuery = this.value;
      if (clearBtn) clearBtn.style.display = filterQuery ? "block" : "none";
      if (filterQuery.startsWith("@")) {
        triggerServerSearch(filterQuery);
      } else {
        hideSearchPanel();
        render();
      }
    });

    input.addEventListener("keydown", (e) => {
      if (e.key === "Escape") {
        hideSearchPanel();
        input.blur();
      }
    });

    document.addEventListener("click", (e) => {
      const wrapper = document.querySelector(".task-search-wrapper");
      if (wrapper && !wrapper.contains(e.target)) hideSearchPanel();
    });

    if (clearBtn) {
      clearBtn.addEventListener("click", function () {
        input.value = "";
        filterQuery = "";
        this.style.display = "none";
        hideSearchPanel();
        render();
        input.focus();
      });
    }
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", setup);
  } else {
    setup();
  }
})();

// Press '/' to focus the search bar when no text input is active.
document.addEventListener("keydown", (e) => {
  const tag = document.activeElement.tagName;
  if (e.key === "/" && tag !== "INPUT" && tag !== "TEXTAREA") {
    e.preventDefault();
    const input = document.getElementById("task-search");
    if (input) input.focus();
  }
});
