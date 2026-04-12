// Shared floating table of contents with scroll spy and pretext-powered
// text exclusion zone.  Used by both the docs and spec views.
//
// Usage:
//   buildFloatingToc(bodyEl, scrollEl, anchorEl, { headingSelector, idPrefix })
//   teardownFloatingToc()

(function () {
  "use strict";

  var _tocId = "floating-toc";

  // --- State (survives scroll, cleared on teardown) ---
  var _items = null;
  var _layout = null;
  var _scrollHandler = null;
  var _scrollRaf = null;
  var _resizeHandler = null;
  var _resizeTimer = null;
  var _spyHandler = null;
  var _spyScrollEl = null;

  // -------------------------------------------------------
  // Public API
  // -------------------------------------------------------

  /**
   * Build and append a floating TOC.
   *
   * @param {Element} bodyEl   - The inner element containing rendered markdown.
   * @param {Element} scrollEl - The scrollable parent of bodyEl.
   * @param {Element} anchorEl - A position:relative container the TOC is
   *                             appended to (stays fixed while bodyEl scrolls).
   * @param {Object}  opts
   * @param {string}  opts.headingSelector  - CSS selector for headings
   *                                          (default "h1, h2, h3, h4").
   * @param {string}  opts.idPrefix         - Prefix for auto-generated heading
   *                                          IDs (default "toc-heading").
   */
  function buildFloatingToc(bodyEl, scrollEl, anchorEl, opts) {
    teardownFloatingToc();

    opts = opts || {};
    var selector = opts.headingSelector || "h1, h2, h3, h4";
    var prefix = opts.idPrefix || "toc-heading";

    var headings = bodyEl.querySelectorAll(selector);
    if (!headings || headings.length < 2) return;

    var toc = document.createElement("div");
    toc.id = _tocId;
    toc.className = "spec-toc";

    var title = document.createElement("div");
    title.className = "spec-toc__title";
    title.textContent = "Contents";
    toc.appendChild(title);

    var links = [];
    for (var i = 0; i < headings.length; i++) {
      var h = headings[i];
      if (!h.id) {
        h.id =
          prefix +
          "-" +
          h.textContent
            .toLowerCase()
            .replace(/[^a-z0-9]+/g, "-")
            .replace(/^-|-$/g, "");
      }
      var level = parseInt(h.tagName.substring(1), 10);
      var link = document.createElement("a");
      link.className = "spec-toc__link spec-toc__link--h" + level;
      link.href = "#" + h.id;
      link.textContent = h.textContent;
      link.setAttribute("data-toc-target", h.id);
      link.addEventListener(
        "click",
        (function (targetId, allLinks) {
          return function (e) {
            e.preventDefault();
            var el = document.getElementById(targetId);
            if (el) el.scrollIntoView({ behavior: "smooth", block: "start" });
            // Immediately highlight — scrollIntoView may not fire scroll event
            // if the target is already visible.
            for (var m = 0; m < allLinks.length; m++) {
              allLinks[m].classList.toggle(
                "spec-toc__link--active",
                allLinks[m].getAttribute("data-toc-target") === targetId,
              );
            }
          };
        })(h.id, links),
      );
      toc.appendChild(link);
      links.push(link);
    }

    anchorEl.appendChild(toc);

    // Scroll spy — highlight active heading.
    _setupScrollSpy(scrollEl, headings, links);

    // Exclusion zone — reflow text around the TOC.
    _setupExclusion(scrollEl, bodyEl, toc);
  }

  function teardownFloatingToc() {
    // Remove DOM.
    var existing = document.getElementById(_tocId);
    if (existing) existing.remove();

    // Teardown exclusion zone.
    _teardownExclusion();

    // Teardown scroll spy.
    if (_spyScrollEl && _spyHandler) {
      _spyScrollEl.removeEventListener("scroll", _spyHandler);
    }
    _spyHandler = null;
    _spyScrollEl = null;
  }

  // Expose globally.
  window.buildFloatingToc = buildFloatingToc;
  window.teardownFloatingToc = teardownFloatingToc;

  // -------------------------------------------------------
  // Scroll spy
  // -------------------------------------------------------

  function _setupScrollSpy(scrollEl, headings, links) {
    if (_spyScrollEl && _spyHandler) {
      _spyScrollEl.removeEventListener("scroll", _spyHandler);
    }
    _spyHandler = function () {
      var activeId = "";
      var containerRect = scrollEl.getBoundingClientRect();
      for (var i = 0; i < headings.length; i++) {
        var rect = headings[i].getBoundingClientRect();
        if (rect.top - containerRect.top <= 40) {
          activeId = headings[i].id;
        }
      }
      for (var j = 0; j < links.length; j++) {
        links[j].classList.toggle(
          "spec-toc__link--active",
          links[j].getAttribute("data-toc-target") === activeId,
        );
      }
    };
    _spyScrollEl = scrollEl;
    scrollEl.addEventListener("scroll", _spyHandler, { passive: true });
    _spyHandler(); // initial highlight
  }

  // -------------------------------------------------------
  // Exclusion zone (pretext-powered)
  // -------------------------------------------------------

  function _setupExclusion(scrollEl, innerEl, toc) {
    _prepare(scrollEl, innerEl);
    _relayout(scrollEl, innerEl, toc);
    _apply();

    _scrollHandler = function () {
      if (_scrollRaf) return;
      _scrollRaf = requestAnimationFrame(function () {
        _scrollRaf = null;
        _apply();
      });
    };
    scrollEl.addEventListener("scroll", _scrollHandler);

    _resizeHandler = function () {
      clearTimeout(_resizeTimer);
      _resizeTimer = setTimeout(function () {
        var t = document.getElementById(_tocId);
        if (!scrollEl || !innerEl || !t) return;
        _relayout(scrollEl, innerEl, t);
        _apply();
      }, 100);
    };
    window.addEventListener("resize", _resizeHandler);
  }

  function _prepare(scrollEl, innerEl) {
    var pt = window.pretext || null;
    var blocks = innerEl.querySelectorAll(":scope > *");
    if (blocks.length === 0) return;

    var innerCS = getComputedStyle(innerEl);
    var padT = parseFloat(innerCS.paddingTop) || 0;
    var scrollRect = scrollEl.getBoundingClientRect();

    var items = [];
    for (var i = 0; i < blocks.length; i++) {
      var block = blocks[i];
      var rect = block.getBoundingClientRect();
      var contentY = rect.top - scrollRect.top + scrollEl.scrollTop - padT;
      var isText = block.tagName === "P" || /^H[1-6]$/.test(block.tagName);
      var item = { el: block, contentY: contentY };

      if (isText && pt) {
        var cs = getComputedStyle(block);
        var font = cs.fontWeight + " " + cs.fontSize + " " + cs.fontFamily;
        var lh = parseFloat(cs.lineHeight);
        if (Number.isNaN(lh)) lh = parseFloat(cs.fontSize) * 1.7;
        try {
          item.prepared = pt.prepare(block.textContent || "", font);
          item.lineHeight = lh;
          item.overhead = Math.max(
            0,
            rect.height - pt.layout(item.prepared, rect.width, lh).height,
          );
        } catch (_e) {
          item.prepared = null;
        }
      }
      items.push(item);
    }

    for (var k = 0; k < items.length; k++) {
      items[k].heightAtSetup = items[k].el.getBoundingClientRect().height;
    }
    for (var j = 0; j < items.length; j++) {
      if (j === 0) {
        items[j].gap = items[j].contentY;
      } else {
        var pe = items[j - 1].contentY + items[j - 1].heightAtSetup;
        items[j].gap = Math.max(0, items[j].contentY - pe);
      }
    }

    _items = items;
  }

  function _relayout(scrollEl, innerEl, toc) {
    if (!_items) return;

    for (var c = 0; c < _items.length; c++) {
      _items[c].el.style.maxWidth = "";
    }

    var pt = window.pretext || null;
    var tocW = toc.offsetWidth + 24;
    var innerCS = getComputedStyle(innerEl);
    var padL = parseFloat(innerCS.paddingLeft) || 0;
    var padR = parseFloat(innerCS.paddingRight) || 0;
    var padT = parseFloat(innerCS.paddingTop) || 0;
    var fullWidth = innerEl.clientWidth - padL - padR;
    var narrowWidth = Math.max(fullWidth - tocW, 80);

    var tocRect = toc.getBoundingClientRect();
    var innerRect = innerEl.getBoundingClientRect();

    if (tocRect.left >= innerRect.right) {
      _layout = null;
      return;
    }

    var scrollRect = scrollEl.getBoundingClientRect();
    var tocScrollTop = tocRect.top - scrollRect.top;
    var tocScrollBottom = tocRect.bottom - scrollRect.top;

    for (var i = 0; i < _items.length; i++) {
      var item = _items[i];

      if (item.prepared && pt) {
        item.heightFull =
          pt.layout(item.prepared, fullWidth, item.lineHeight).height +
          item.overhead;
        item.heightNarrow =
          pt.layout(item.prepared, narrowWidth, item.lineHeight).height +
          item.overhead;
      } else {
        item.heightFull = item.el.getBoundingClientRect().height;
        var origMW = item.el.style.maxWidth;
        item.el.style.maxWidth = narrowWidth + "px";
        item.heightNarrow = item.el.getBoundingClientRect().height;
        item.el.style.maxWidth = origMW;
      }
    }

    _layout = {
      narrowWidth: narrowWidth,
      tocScrollTop: tocScrollTop,
      tocScrollBottom: tocScrollBottom,
      innerPadTop: padT,
      scrollEl: scrollEl,
    };
  }

  function _apply() {
    if (!_layout || !_items) return;
    var d = _layout;
    var scrollTop = d.scrollEl.scrollTop;
    var tocTop = scrollTop + d.tocScrollTop - d.innerPadTop;
    var tocBottom = scrollTop + d.tocScrollBottom - d.innerPadTop;

    var y = 0;
    for (var i = 0; i < _items.length; i++) {
      var item = _items[i];
      y += item.gap;
      var overlap = y + item.heightFull > tocTop && y < tocBottom;
      if (overlap) {
        item.el.style.maxWidth = d.narrowWidth + "px";
        y += item.heightNarrow;
      } else {
        item.el.style.maxWidth = "";
        y += item.heightFull;
      }
    }
  }

  function _teardownExclusion() {
    if (_layout) {
      var scrollEl = _layout.scrollEl;
      if (scrollEl && _scrollHandler) {
        scrollEl.removeEventListener("scroll", _scrollHandler);
      }
    }
    if (_items) {
      for (var i = 0; i < _items.length; i++) {
        _items[i].el.style.maxWidth = "";
      }
    }
    _scrollHandler = null;
    _items = null;
    _layout = null;
    if (_scrollRaf) {
      cancelAnimationFrame(_scrollRaf);
      _scrollRaf = null;
    }
    if (_resizeHandler) {
      window.removeEventListener("resize", _resizeHandler);
      _resizeHandler = null;
    }
    clearTimeout(_resizeTimer);
    _resizeTimer = null;
  }
})();
