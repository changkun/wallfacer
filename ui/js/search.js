// --- Search / filter ---

/**
 * Returns true when task t matches the current filterQuery.
 * Matching is case-insensitive and checks both title and prompt fields.
 */
function matchesFilter(t) {
  if (!filterQuery) return true;
  const q = filterQuery.toLowerCase();
  return (t.title || '').toLowerCase().includes(q) ||
    (t.prompt || '').toLowerCase().includes(q);
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
    '</mark>' +
    escapeHtml(text.slice(idx + query.length))
  );
}

// Wire up the search input and clear button once the DOM is ready.
(function initSearch() {
  function setup() {
    const input = document.getElementById('task-search');
    const clearBtn = document.getElementById('task-search-clear');
    if (!input) return;

    input.addEventListener('input', function() {
      filterQuery = this.value;
      if (clearBtn) clearBtn.style.display = filterQuery ? '' : 'none';
      render();
    });

    if (clearBtn) {
      clearBtn.addEventListener('click', function() {
        input.value = '';
        filterQuery = '';
        this.style.display = 'none';
        render();
        input.focus();
      });
    }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', setup);
  } else {
    setup();
  }
})();
