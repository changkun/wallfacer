// --- Markdown helpers ---

// Configure marked to use highlight.js for syntax highlighting in code blocks.
// Mermaid blocks are rendered as placeholder divs and processed after insertion.
(function () {
  if (typeof marked === "undefined") return;

  function _highlightCode(code, lang) {
    if (typeof hljs === "undefined") return escapeHtml(code);
    if (lang && hljs.getLanguage(lang)) {
      try {
        return hljs.highlight(code, { language: lang }).value;
      } catch (_) {}
    }
    try {
      return hljs.highlightAuto(code).value;
    } catch (_) {}
    return escapeHtml(code);
  }

  // Custom renderer: apply highlight.js to code blocks (marked v9+ removed
  // the highlight option from setOptions) and render mermaid as placeholders.
  var renderer = new marked.Renderer();
  renderer.code = function (code, lang) {
    // marked v14+ passes {text, lang} object; v9 passes (code, lang).
    var codeText = typeof code === "object" ? code.text : code;
    var codeLang = typeof code === "object" ? code.lang : lang;
    if (codeLang === "mermaid") {
      return (
        '<div class="mermaid-block" data-mermaid="' +
        escapeHtml(codeText) +
        '">' +
        '<pre class="mermaid-src"><code>' +
        escapeHtml(codeText) +
        "</code></pre></div>"
      );
    }
    var langClass = codeLang ? ' class="language-' + escapeHtml(codeLang) + '"' : "";
    return "<pre><code" + langClass + ">" + _highlightCode(codeText, codeLang) + "</code></pre>\n";
  };
  marked.setOptions({ renderer: renderer });
})();

function renderMarkdown(text) {
  if (!text) return "";
  if (typeof marked === "undefined") return escapeHtml(text);
  return marked.parse(text);
}

function renderMarkdownInline(text) {
  if (!text) return "";
  if (typeof marked === "undefined") return escapeHtml(text);
  return marked.parseInline(text);
}

function toggleModalSection(section) {
  var renderedEl = document.getElementById("modal-" + section + "-rendered");
  var rawEl = document.getElementById("modal-" + section);
  var btn = document.getElementById("toggle-" + section + "-btn");
  toggleRenderedRaw(renderedEl, rawEl, btn);
}

function copyModalText(section) {
  var rawEl = document.getElementById("modal-" + section);
  var btn = document.getElementById("copy-" + section + "-btn");
  copyWithFeedback(rawEl.textContent, btn);
}

function toggleCardMarkdown(event, btn) {
  event.stopPropagation();
  var card = btn.closest(".card");
  var renderedEls = card.querySelectorAll(".card-md-rendered");
  var rawEls = card.querySelectorAll(".card-md-raw");
  var nowShowingRaw = card.dataset.rawView === "true";
  card.dataset.rawView = nowShowingRaw ? "false" : "true";
  renderedEls.forEach(function (el) {
    el.classList.toggle("hidden", !nowShowingRaw);
  });
  rawEls.forEach(function (el) {
    el.classList.toggle("hidden", nowShowingRaw);
  });
  btn.textContent = nowShowingRaw ? "Raw" : "Preview";
}

function copyCardText(event, taskId) {
  event.stopPropagation();
  var task = tasks.find(function (t) {
    return t.id === taskId;
  });
  if (!task) return;
  var text = task.prompt + (task.result ? "\n\n" + task.result : "");
  copyWithFeedback(text, event.currentTarget, "\u2713");
}
