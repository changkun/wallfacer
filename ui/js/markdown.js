// --- Markdown helpers ---

// Configure marked to use highlight.js for syntax highlighting in code blocks.
// Mermaid blocks are rendered as placeholder divs and processed after insertion.
(function () {
  if (typeof marked === "undefined") return;
  marked.setOptions({
    highlight: function (code, lang) {
      if (lang === "mermaid") return code; // handled in post-processing
      if (typeof hljs !== "undefined" && lang && hljs.getLanguage(lang)) {
        try {
          return hljs.highlight(code, { language: lang }).value;
        } catch (_) {}
      }
      if (typeof hljs !== "undefined") {
        try {
          return hljs.highlightAuto(code).value;
        } catch (_) {}
      }
      return code;
    },
  });

  // Custom renderer: mermaid code blocks become divs for post-processing.
  var renderer = new marked.Renderer();
  var origCode = renderer.code.bind(renderer);
  renderer.code = function (code, lang) {
    // marked v14+ passes {text, lang} object; v12 passes (code, lang).
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
    return origCode(code, lang);
  };
  marked.setOptions({ renderer: renderer });
})();

function renderMarkdown(text) {
  if (!text) return "";
  if (typeof marked === "undefined") return escapeHtml(text);
  return marked.parse(text);
}

// renderMermaidBlocks finds .mermaid-block elements inside a container and
// renders them as SVG diagrams using the Mermaid library.
function renderMermaidBlocks(container) {
  if (typeof mermaid === "undefined" || !container) return;
  var blocks = container.querySelectorAll(".mermaid-block");
  if (!blocks || blocks.length === 0) return;

  for (var i = 0; i < blocks.length; i++) {
    (function (block, idx) {
      var code = block.getAttribute("data-mermaid");
      if (!code) return;
      var id = "mermaid-diagram-" + Date.now() + "-" + idx;
      mermaid
        .render(id, code)
        .then(function (result) {
          block.innerHTML = result.svg;
          block.classList.add("mermaid-rendered");
        })
        .catch(function () {
          // Keep the source code block visible on render failure.
        });
    })(blocks[i], i);
  }
}

function renderMarkdownInline(text) {
  if (!text) return "";
  if (typeof marked === "undefined") return escapeHtml(text);
  return marked.parseInline(text);
}

function toggleModalSection(section) {
  const renderedEl = document.getElementById("modal-" + section + "-rendered");
  const rawEl = document.getElementById("modal-" + section);
  const btn = document.getElementById("toggle-" + section + "-btn");
  const showingRaw = !rawEl.classList.contains("hidden");
  if (showingRaw) {
    renderedEl.classList.remove("hidden");
    rawEl.classList.add("hidden");
    btn.textContent = "Raw";
  } else {
    renderedEl.classList.add("hidden");
    rawEl.classList.remove("hidden");
    btn.textContent = "Preview";
  }
}

function copyModalText(section) {
  const rawEl = document.getElementById("modal-" + section);
  const text = rawEl.textContent;
  const btn = document.getElementById("copy-" + section + "-btn");
  navigator.clipboard
    .writeText(text)
    .then(function () {
      const origHTML = btn.innerHTML;
      btn.textContent = "Copied!";
      setTimeout(function () {
        btn.innerHTML = origHTML;
      }, 1500);
    })
    .catch(function () {});
}

function toggleCardMarkdown(event, btn) {
  event.stopPropagation();
  const card = btn.closest(".card");
  const renderedEls = card.querySelectorAll(".card-md-rendered");
  const rawEls = card.querySelectorAll(".card-md-raw");
  const nowShowingRaw = card.dataset.rawView === "true";
  if (nowShowingRaw) {
    card.dataset.rawView = "false";
    renderedEls.forEach(function (el) {
      el.classList.remove("hidden");
    });
    rawEls.forEach(function (el) {
      el.classList.add("hidden");
    });
    btn.textContent = "Raw";
  } else {
    card.dataset.rawView = "true";
    renderedEls.forEach(function (el) {
      el.classList.add("hidden");
    });
    rawEls.forEach(function (el) {
      el.classList.remove("hidden");
    });
    btn.textContent = "Preview";
  }
}

function copyCardText(event, taskId) {
  event.stopPropagation();
  const task = tasks.find(function (t) {
    return t.id === taskId;
  });
  if (!task) return;
  const text = task.prompt + (task.result ? "\n\n" + task.result : "");
  const btn = event.currentTarget;
  navigator.clipboard
    .writeText(text)
    .then(function () {
      const origHTML = btn.innerHTML;
      btn.textContent = "\u2713";
      setTimeout(function () {
        btn.innerHTML = origHTML;
      }, 1500);
    })
    .catch(function () {});
}
