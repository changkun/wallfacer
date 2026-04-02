// --- Unified markdown post-processor ---
//
// After inserting HTML from renderMarkdown() into a container, call
// enhanceMarkdown(container, options) to apply mermaid diagrams, rewrite
// code-file links, and optionally build a table of contents.
//
// This module consolidates the mermaid rendering previously split between
// docs.js (_renderMermaidBlocks + _ensureMermaid + _expandDiagram) and
// markdown.js (renderMermaidBlocks), and the link-rewriting logic from
// docs.js (_rewriteDocLinks) and spec-mode.js (_onSpecBodyLinkClick).

var _mdRender = (function () {
  var _mermaidLoaded = false;
  var _mermaidRenderSeq = 0;

  // --- Mermaid lazy-loading and initialization ---

  function _ensureMermaid() {
    if (_mermaidLoaded) return Promise.resolve();
    return new Promise(function (resolve) {
      var script = document.createElement("script");
      script.src = "https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js";
      script.onload = function () {
        if (typeof mermaid !== "undefined") {
          _initMermaidTheme();
        }
        _mermaidLoaded = true;
        resolve();
      };
      script.onerror = function () {
        resolve();
      };
      document.head.appendChild(script);
    });
  }

  function _initMermaidTheme() {
    var cs = getComputedStyle(document.documentElement);
    try {
      mermaid.initialize({
        startOnLoad: false,
        theme: "base",
        themeVariables: {
          primaryColor: cs.getPropertyValue("--bg-input").trim(),
          primaryTextColor: cs.getPropertyValue("--text").trim(),
          primaryBorderColor: cs.getPropertyValue("--border").trim(),
          lineColor: cs.getPropertyValue("--text-muted").trim(),
          secondaryColor: cs.getPropertyValue("--bg-card").trim(),
          tertiaryColor: cs.getPropertyValue("--bg-raised").trim(),
          background: cs.getPropertyValue("--bg-card").trim(),
          mainBkg: cs.getPropertyValue("--bg-input").trim(),
          nodeBorder: cs.getPropertyValue("--border").trim(),
          clusterBkg: cs.getPropertyValue("--bg-card").trim(),
          clusterBorder: cs.getPropertyValue("--border").trim(),
          titleColor: cs.getPropertyValue("--text").trim(),
          edgeLabelBackground: cs.getPropertyValue("--bg-card").trim(),
          nodeTextColor: cs.getPropertyValue("--text").trim(),
          actorTextColor: cs.getPropertyValue("--text").trim(),
          actorBkg: cs.getPropertyValue("--bg-input").trim(),
          actorBorder: cs.getPropertyValue("--border").trim(),
          signalColor: cs.getPropertyValue("--text").trim(),
          signalTextColor: cs.getPropertyValue("--text").trim(),
          labelBoxBkgColor: cs.getPropertyValue("--bg-input").trim(),
          labelBoxBorderColor: cs.getPropertyValue("--border").trim(),
          labelTextColor: cs.getPropertyValue("--text").trim(),
          loopTextColor: cs.getPropertyValue("--text").trim(),
          noteBkgColor: cs.getPropertyValue("--bg-input").trim(),
          noteTextColor: cs.getPropertyValue("--text").trim(),
          noteBorderColor: cs.getPropertyValue("--border").trim(),
          activationBkgColor: cs.getPropertyValue("--bg-input").trim(),
          activationBorderColor: cs.getPropertyValue("--border").trim(),
          sequenceNumberColor: cs.getPropertyValue("--text").trim(),
          fontFamily: "inherit",
          fontSize: "13px",
        },
      });
    } catch (initErr) {
      console.error("mermaid init:", initErr);
    }
  }

  // --- Mermaid block rendering ---

  // Render mermaid blocks in a container. Handles both the custom renderer
  // output (.mermaid-block with data-mermaid attr) and raw code blocks that
  // marked may produce in different versions.
  async function _renderMermaidBlocks(container) {
    if (typeof mermaid === "undefined") return;

    // Primary: blocks produced by the custom marked renderer in markdown.js.
    var blocks = container.querySelectorAll(".mermaid-block");

    // Fallback: scan for code blocks with mermaid language class or content heuristic.
    if (!blocks || blocks.length === 0) {
      var codeCandidates = container.querySelectorAll(
        "pre code.language-mermaid, pre code.mermaid",
      );
      var extra = [];
      if (codeCandidates.length > 0) {
        for (var c = 0; c < codeCandidates.length; c++) {
          extra.push(codeCandidates[c]);
        }
      } else {
        // Heuristic: detect mermaid content in untagged code blocks.
        var allCodes = container.querySelectorAll("pre code");
        for (var j = 0; j < allCodes.length; j++) {
          var text = allCodes[j].textContent.trim();
          if (
            /^(graph|flowchart|sequenceDiagram|stateDiagram|classDiagram|gantt|pie|erDiagram|journey)\b/.test(
              text,
            )
          ) {
            extra.push(allCodes[j]);
          }
        }
      }
      if (extra.length > 0) {
        _renderCodeElements(extra);
        return;
      }
      return;
    }

    // Render .mermaid-block elements (from custom marked renderer).
    for (var i = 0; i < blocks.length; i++) {
      var block = blocks[i];
      var code = block.getAttribute("data-mermaid");
      if (!code) continue;
      var id = "mermaid-diagram-" + Date.now() + "-" + ++_mermaidRenderSeq;
      try {
        var result = await mermaid.render(id, code);
        var div = document.createElement("div");
        div.className = "mermaid-diagram";
        div.innerHTML = result.svg;
        div.title = "Click to expand";
        div.addEventListener(
          "click",
          (function (d) {
            return function () {
              _expandDiagram(d);
            };
          })(div),
        );
        block.innerHTML = "";
        block.appendChild(div);
        block.classList.add("mermaid-rendered");
      } catch (_) {
        // Keep the source code block visible on render failure.
      }
    }
  }

  // Render raw code elements (fallback path for code blocks not wrapped by
  // the custom marked renderer).
  async function _renderCodeElements(elements) {
    for (var i = 0; i < elements.length; i++) {
      var el = elements[i];
      var pre = el.tagName === "PRE" ? el : el.parentElement;
      var source = el.textContent;
      var id = "mermaid-" + ++_mermaidRenderSeq;
      try {
        var result = await mermaid.render(id, source);
        var div = document.createElement("div");
        div.className = "mermaid-diagram";
        div.innerHTML = result.svg;
        div.title = "Click to expand";
        div.addEventListener(
          "click",
          (function (d) {
            return function () {
              _expandDiagram(d);
            };
          })(div),
        );
        pre.replaceWith(div);
      } catch (_) {
        // Leave the code block as-is on render failure.
      }
    }
  }

  // --- Click-to-expand mermaid diagram overlay ---

  function _expandDiagram(sourceDiv) {
    var svg = sourceDiv.querySelector("svg");
    if (!svg) return;

    var overlay = document.createElement("div");
    overlay.className = "diagram-overlay";

    var viewport = document.createElement("div");
    viewport.className = "diagram-overlay__viewport";

    var surface = document.createElement("div");
    surface.className = "diagram-overlay__surface";
    var clone = svg.cloneNode(true);
    var vb = clone.getAttribute("viewBox");
    if (vb) {
      var parts = vb.split(/[\s,]+/);
      var vbW = parseFloat(parts[2]) || 800;
      var vbH = parseFloat(parts[3]) || 600;
      clone.setAttribute("width", vbW);
      clone.setAttribute("height", vbH);
    }
    clone.removeAttribute("style");
    surface.appendChild(clone);
    viewport.appendChild(surface);

    var toolbar = document.createElement("div");
    toolbar.className = "diagram-overlay__toolbar";
    toolbar.innerHTML =
      '<button type="button" title="Zoom in">+</button>' +
      '<button type="button" title="Zoom out">&minus;</button>' +
      '<button type="button" title="Reset view">Fit</button>' +
      '<span class="diagram-overlay__hint">Scroll to zoom &middot; drag to pan</span>' +
      '<button type="button" title="Close">&times;</button>';

    overlay.appendChild(viewport);
    overlay.appendChild(toolbar);

    var scale = 1,
      tx = 0,
      ty = 0;
    var dragging = false,
      dragStartX = 0,
      dragStartY = 0,
      txStart = 0,
      tyStart = 0;

    function applyTransform() {
      surface.style.transform =
        "translate(" + tx + "px," + ty + "px) scale(" + scale + ")";
    }

    function zoomTo(newScale, cx, cy) {
      var ratio = newScale / scale;
      tx = cx - ratio * (cx - tx);
      ty = cy - ratio * (cy - ty);
      scale = newScale;
      applyTransform();
    }

    function resetView() {
      scale = 1;
      tx = 0;
      ty = 0;
      applyTransform();
    }

    var btns = toolbar.querySelectorAll("button");
    btns[0].onclick = function () {
      zoomTo(scale * 1.3, viewport.clientWidth / 2, viewport.clientHeight / 2);
    };
    btns[1].onclick = function () {
      zoomTo(scale / 1.3, viewport.clientWidth / 2, viewport.clientHeight / 2);
    };
    btns[2].onclick = resetView;
    btns[3].onclick = removeOverlay;

    viewport.addEventListener(
      "wheel",
      function (e) {
        e.preventDefault();
        var rect = viewport.getBoundingClientRect();
        var cx = e.clientX - rect.left;
        var cy = e.clientY - rect.top;
        var factor = e.deltaY < 0 ? 1.15 : 1 / 1.15;
        zoomTo(Math.max(0.1, Math.min(10, scale * factor)), cx, cy);
      },
      { passive: false },
    );

    viewport.addEventListener("mousedown", function (e) {
      if (e.button !== 0) return;
      dragging = true;
      dragStartX = e.clientX;
      dragStartY = e.clientY;
      txStart = tx;
      tyStart = ty;
      viewport.style.cursor = "grabbing";
      e.preventDefault();
    });
    window.addEventListener("mousemove", onMouseMove);
    window.addEventListener("mouseup", onMouseUp);
    function onMouseMove(e) {
      if (!dragging) return;
      tx = txStart + (e.clientX - dragStartX);
      ty = tyStart + (e.clientY - dragStartY);
      applyTransform();
    }
    function onMouseUp() {
      if (!dragging) return;
      dragging = false;
      viewport.style.cursor = "";
    }

    function onKey(e) {
      if (e.key === "Escape") {
        e.stopImmediatePropagation();
        removeOverlay();
        return;
      }
      var cx = viewport.clientWidth / 2,
        cy = viewport.clientHeight / 2;
      if (e.key === "=" || e.key === "+") {
        zoomTo(scale * 1.3, cx, cy);
        e.preventDefault();
      } else if (e.key === "-") {
        zoomTo(scale / 1.3, cx, cy);
        e.preventDefault();
      } else if (e.key === "0") {
        resetView();
        e.preventDefault();
      } else if (e.key === "ArrowLeft") {
        tx += 50;
        applyTransform();
        e.preventDefault();
      } else if (e.key === "ArrowRight") {
        tx -= 50;
        applyTransform();
        e.preventDefault();
      } else if (e.key === "ArrowUp") {
        ty += 50;
        applyTransform();
        e.preventDefault();
      } else if (e.key === "ArrowDown") {
        ty -= 50;
        applyTransform();
        e.preventDefault();
      }
    }

    function removeOverlay() {
      overlay.remove();
      document.removeEventListener("keydown", onKey, true);
      window.removeEventListener("mousemove", onMouseMove);
      window.removeEventListener("mouseup", onMouseUp);
    }

    document.addEventListener("keydown", onKey, true);
    document.body.appendChild(overlay);
    requestAnimationFrame(function () {
      var svgRect = clone.getBoundingClientRect();
      var vpRect = viewport.getBoundingClientRect();
      if (svgRect.width > 0 && svgRect.height > 0) {
        var fitScale =
          Math.min(
            vpRect.width / svgRect.width,
            vpRect.height / svgRect.height,
            2,
          ) * 0.9;
        scale = fitScale;
        tx = (vpRect.width - svgRect.width * fitScale) / 2;
        ty = (vpRect.height - svgRect.height * fitScale) / 2;
        applyTransform();
      }
    });
  }

  // --- Link rewriting ---

  // Rewrite links that point to code files (not http URLs, not anchors) so they
  // open in the file explorer preview panel instead of navigating away.
  // Options:
  //   linkHandler: "explorer" (default) — open in file explorer
  //   linkHandler: "docs" — navigate within docs viewer (calls loadDoc)
  //   linkHandler: "spec" — navigate within spec mode (calls focusSpec)
  //   linkHandler: function(resolvedPath, e) — custom handler
  //   basePath: base directory for resolving relative paths
  //   workspace: workspace key (for spec mode navigation)
  function _rewriteLinks(container, opts) {
    var links = container.querySelectorAll("a[href]");
    var basePath = opts.basePath || "";
    var baseDir = basePath.substring(0, basePath.lastIndexOf("/") + 1);

    for (var i = 0; i < links.length; i++) {
      var a = links[i];
      var href = a.getAttribute("href");
      if (!href || href.startsWith("http") || href.startsWith("#")) continue;

      var handler = opts.linkHandler || "explorer";

      if (handler === "docs") {
        // Docs mode: only rewrite .md links to navigate within the docs viewer.
        if (!href.endsWith(".md") && !href.includes(".md#")) continue;
        var resolved = _resolvePath(baseDir, href);
        var anchor = "";
        var hashIdx = resolved.indexOf("#");
        if (hashIdx !== -1) {
          anchor = resolved.substring(hashIdx);
          resolved = resolved.substring(0, hashIdx);
        }
        resolved = resolved.replace(/\.md$/, "");
        resolved = resolved.replace(/\/$/, "");
        a.setAttribute("href", "#");
        a.setAttribute("data-doc-slug", resolved);
        a.onclick = (function (slug) {
          return function (e) {
            e.preventDefault();
            if (typeof loadDoc === "function") loadDoc(slug);
          };
        })(resolved);
      } else if (handler === "spec") {
        // Spec mode: rewrite .md links to navigate within spec mode.
        if (!href.endsWith(".md")) continue;
        var specResolved = _resolvePath(baseDir, href);
        a.setAttribute("href", "#");
        a.onclick = (function (path, ws) {
          return function (e) {
            e.preventDefault();
            if (typeof focusSpec === "function") focusSpec(path, ws);
          };
        })(specResolved, opts.workspace);
      } else {
        // Explorer or custom handler: rewrite file links to open in explorer.
        // Skip .md links in docs/spec contexts — those are handled above.
        // For general prose content, treat relative links as file paths.
        var filePath = _resolvePath(baseDir, href);
        a.setAttribute("href", "#");
        if (typeof handler === "function") {
          a.onclick = (function (path, fn) {
            return function (e) {
              e.preventDefault();
              fn(path, e);
            };
          })(filePath, handler);
        } else {
          // Default: open in explorer preview.
          a.onclick = (function (path) {
            return function (e) {
              e.preventDefault();
              if (typeof openExplorerFile === "function") {
                openExplorerFile(path);
              }
            };
          })(filePath);
        }
      }
    }
  }

  function _resolvePath(base, rel) {
    var parts = (base + rel).split("/");
    var resolved = [];
    for (var i = 0; i < parts.length; i++) {
      if (parts[i] === "..") {
        resolved.pop();
      } else if (parts[i] !== "." && parts[i] !== "") {
        resolved.push(parts[i]);
      }
    }
    return resolved.join("/");
  }

  // --- Public API ---

  // enhanceMarkdown(container, options) applies post-processing to a container
  // that has already had its innerHTML set via renderMarkdown().
  //
  // Options:
  //   mermaid: true (default) — render mermaid diagrams
  //   links: true | false (default) — rewrite file links
  //   linkHandler: "explorer" | "docs" | "spec" | function — how to handle links
  //   basePath: string — base path for resolving relative links
  //   workspace: string — workspace key (for spec link handler)
  //
  // Returns a Promise that resolves when all async rendering is complete.
  async function enhanceMarkdown(container, options) {
    if (!container) return;
    var opts = options || {};

    // Mermaid rendering (default: on).
    if (opts.mermaid !== false) {
      await _ensureMermaid();
      await _renderMermaidBlocks(container);
    }

    // Link rewriting (default: off — opt-in to avoid breaking contexts
    // where links should remain as-is, like card previews).
    if (opts.links) {
      _rewriteLinks(container, opts);
    }
  }

  return {
    enhanceMarkdown: enhanceMarkdown,
    // Expose for testing.
    _resolvePath: _resolvePath,
    _ensureMermaid: _ensureMermaid,
    _expandDiagram: _expandDiagram,
  };
})();
