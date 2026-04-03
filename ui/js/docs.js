// --- Documentation viewer ---

var _docsEntries = [];
var _docsCurrentSlug = "";
var _docsLoaded = false;

// Pre-load the docs index in the background so cmd+k search can find docs
// from any view, even before the user navigates to the Docs page.
document.addEventListener("DOMContentLoaded", function () {
  if (!_docsEntries.length) {
    api("/api/docs")
      .then(function (entries) {
        _docsEntries = entries || [];
      })
      .catch(function () {});
  }
});

// openDocs switches to docs mode and optionally loads a specific doc.
function openDocs(slug) {
  switchMode("docs");
  if (slug) loadDoc(slug);
}

// _ensureDocsLoaded is called by _applyMode when entering docs mode.
// It fetches the docs index on first visit and renders the default page.
async function _ensureDocsLoaded() {
  if (_docsLoaded) return;
  _docsLoaded = true;

  if (!_docsEntries.length) {
    try {
      _docsEntries = await api("/api/docs");
    } catch (e) {
      _docsEntries = [];
    }
  }
  renderDocsNav();
  if (!_docsCurrentSlug) loadDoc("guide/usage");
}

function renderDocsNav() {
  var nav = document.getElementById("docs-nav");
  if (!nav) return;
  var categories = {};
  _docsEntries.forEach(function (entry) {
    if (!categories[entry.category]) categories[entry.category] = [];
    categories[entry.category].push(entry);
  });
  var html = "";
  var catLabels = { guide: "User Guide", internals: "Technical Reference" };
  // Render categories in a fixed order: guide first, then internals.
  var catOrder = ["guide", "internals"];
  catOrder.forEach(function (cat) {
    if (!categories[cat]) return;
    html += '<div style="margin-bottom:12px;">';
    html +=
      '<div style="font-size:10px;font-weight:700;text-transform:uppercase;letter-spacing:0.05em;color:var(--text-muted);margin-bottom:6px;">' +
      escapeHtml(catLabels[cat] || cat) +
      "</div>";
    categories[cat].forEach(function (entry) {
      var active = entry.slug === _docsCurrentSlug;
      // Show step number for ordered guide docs (skip the index page which is usage.md, order=9).
      var prefix = "";
      var isIndex =
        entry.slug === "guide/usage" || entry.slug === "internals/internals";
      if (entry.order && !isIndex) {
        prefix =
          '<span style="display:inline-block;width:16px;height:16px;line-height:16px;text-align:center;border-radius:50%;background:var(--bg-raised);color:var(--text-muted);font-size:9px;font-weight:700;margin-right:4px;flex-shrink:0;">' +
          entry.order +
          "</span>";
      }
      // Mark index pages distinctly.
      if (isIndex) {
        prefix =
          '<span style="font-size:10px;margin-right:4px;">&#9776;</span>';
      }
      html +=
        '<button type="button" onclick="loadDoc(\'' +
        escapeHtml(entry.slug) +
        '\')" style="display:flex;align-items:center;width:100%;text-align:left;padding:4px 8px;margin-bottom:2px;border:none;border-radius:4px;background:' +
        (active ? "var(--bg-input)" : "transparent") +
        ";color:" +
        (active ? "var(--text-primary)" : "inherit") +
        ";font-size:12px;cursor:pointer;font-weight:" +
        (active ? "600" : "400") +
        ';" onmouseover="this.style.background=\'var(--bg-input)\'" onmouseout="this.style.background=\'' +
        (active ? "var(--bg-input)" : "transparent") +
        "'\">" +
        prefix +
        '<span style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap;">' +
        escapeHtml(entry.title) +
        "</span></button>";
    });
    html += "</div>";
  });
  nav.innerHTML = html;
}

async function loadDoc(slug) {
  _docsCurrentSlug = slug;
  renderDocsNav();
  teardownFloatingToc();
  var content = document.getElementById("docs-content");
  if (!content) return;
  content.innerHTML =
    '<div style="color:var(--text-muted);font-size:12px;">Loading...</div>';
  try {
    var res = await fetch(withAuthToken("/api/docs/" + slug));
    if (!res.ok) throw new Error("Not found");
    var md = await res.text();
    content.innerHTML = renderMarkdown(md);
    // Append prev/next navigation for ordered guide docs.
    _appendDocNav(content, slug);
    content.scrollTop = 0;
    await _mdRender.enhanceMarkdown(content, {
      links: true,
      linkHandler: "docs",
      basePath: slug,
    });
    var wrapper = document.getElementById("docs-content-wrapper");
    if (wrapper) {
      buildFloatingToc(content, content, wrapper, {
        headingSelector: "h2, h3",
        idPrefix: "doc-heading",
      });
    }
  } catch (e) {
    content.innerHTML =
      '<div style="color:var(--text-muted);">Failed to load document.</div>';
  }
}


// Append previous/next navigation bar for ordered docs (guide or internals).
function _appendDocNav(container, currentSlug) {
  // Determine the category of the current slug.
  var cat = currentSlug.startsWith("internals/") ? "internals" : "guide";
  var indexSlug = cat === "guide" ? "guide/usage" : "internals/internals";
  // Build ordered list of entries in this category (exclude the index page).
  var ordered = _docsEntries
    .filter(function (e) {
      return e.category === cat && e.order && e.slug !== indexSlug;
    })
    .sort(function (a, b) {
      return a.order - b.order;
    });
  var idx = -1;
  for (var i = 0; i < ordered.length; i++) {
    if (ordered[i].slug === currentSlug) {
      idx = i;
      break;
    }
  }
  if (idx === -1) return; // Not an ordered doc.

  var prev = idx > 0 ? ordered[idx - 1] : null;
  var next = idx < ordered.length - 1 ? ordered[idx + 1] : null;
  if (!prev && !next) return;

  var nav = document.createElement("div");
  nav.style.cssText =
    "display:flex;justify-content:space-between;align-items:center;margin-top:32px;padding-top:16px;border-top:1px solid var(--border);font-size:13px;";

  var linkStyle = "color:var(--accent);cursor:pointer;text-decoration:none;";
  var leftHtml = "";
  var rightHtml = "";
  if (prev) {
    leftHtml =
      '<a href="#" style="' +
      linkStyle +
      '" data-doc-slug="' +
      escapeHtml(prev.slug) +
      '">&larr; ' +
      prev.order +
      ". " +
      escapeHtml(prev.title) +
      "</a>";
  }
  if (next) {
    rightHtml =
      '<a href="#" style="' +
      linkStyle +
      '" data-doc-slug="' +
      escapeHtml(next.slug) +
      '">' +
      next.order +
      ". " +
      escapeHtml(next.title) +
      " &rarr;</a>";
  }
  nav.innerHTML = "<div>" + leftHtml + "</div><div>" + rightHtml + "</div>";

  // Wire click handlers.
  var links = nav.querySelectorAll("a[data-doc-slug]");
  for (var j = 0; j < links.length; j++) {
    links[j].onclick = (function (slug) {
      return function (e) {
        e.preventDefault();
        loadDoc(slug);
      };
    })(links[j].getAttribute("data-doc-slug"));
  }
  container.appendChild(nav);
}
