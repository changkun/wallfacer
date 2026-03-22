// --- Span Statistics Modal ---

(function () {
  var modal, loadingEl, errorEl, emptyEl, contentEl, summaryEl, tbody;
  var setState;

  // Human-readable metadata for known execution phases.
  var PHASE_INFO = {
    worktree_setup: {
      label: "Worktree Setup",
      desc: "Creates an isolated git worktree for the task",
    },
    agent_turn: {
      label: "Agent Turn",
      desc: "One execution turn of the Claude Code agent (start → stop_reason)",
    },
    container_run: {
      label: "Container Run",
      desc: "Full sandbox container lifecycle from start to exit",
    },
    commit: {
      label: "Commit Pipeline",
      desc: "Commits and pushes task changes to the git repository",
    },
  };

  function phaseLabel(phase) {
    return (PHASE_INFO[phase] && PHASE_INFO[phase].label) || phase;
  }

  function phaseDesc(phase) {
    return (
      (PHASE_INFO[phase] && PHASE_INFO[phase].desc) || "Custom execution phase"
    );
  }

  // Returns an inline CSS color string based on duration thresholds.
  // Green < 5 s, amber 5–30 s, red ≥ 30 s.
  function colorStyleForMs(ms) {
    if (ms == null) return "";
    if (ms < 5000) return "color:#22863a;";
    if (ms < 30000) return "color:#d97706;";
    return "color:#dc2626;";
  }

  // Returns HTML for a compact bar showing p50 relative to the global max.
  function barHtml(p50Ms, globalMaxMs) {
    if (!globalMaxMs || p50Ms == null) return "";
    var pct = Math.min(100, Math.round((p50Ms / globalMaxMs) * 100));
    return (
      '<div style="background:var(--border);border-radius:2px;height:4px;' +
      'width:72px;margin-top:4px;overflow:hidden;">' +
      '<div style="background:var(--accent);height:100%;width:' +
      pct +
      "%;" +
      'border-radius:2px;"></div>' +
      "</div>"
    );
  }

  var throughputEl;

  function init() {
    modal = document.getElementById("span-stats-modal");
    loadingEl = document.getElementById("span-stats-loading");
    errorEl = document.getElementById("span-stats-error");
    emptyEl = document.getElementById("span-stats-empty");
    contentEl = document.getElementById("span-stats-content");
    summaryEl = document.getElementById("span-stats-summary");
    tbody = document.getElementById("span-stats-tbody");
    throughputEl = document.getElementById("span-stats-throughput");

    bindModalBackdropClose(modal, closeSpanStats);
    setState = createModalStateController({
      loadingEl: loadingEl,
      errorEl: errorEl,
      emptyEl: emptyEl,
      contentEl: contentEl,
      contentState: "table",
    });
  }

  function fetchStats() {
    loadJsonEndpoint("/api/debug/spans", renderStats, setState);
  }

  function fmtSeconds(s) {
    if (s === undefined || s === null || s === 0) return "\u2014";
    if (s < 60) return s.toFixed(1) + "s";
    return (s / 60).toFixed(1) + "m";
  }

  function renderThroughput(tp) {
    if (!throughputEl) return;
    if (!tp) {
      throughputEl.innerHTML = "";
      return;
    }

    var hasData = tp.total_completed > 0 || tp.total_failed > 0;

    // Stat tiles.
    var tiles = [
      {
        label: "Completed",
        value: hasData ? String(tp.total_completed) : "\u2014",
      },
      { label: "Failed", value: hasData ? String(tp.total_failed) : "\u2014" },
      {
        label: "Success",
        value: hasData ? tp.success_rate_pct.toFixed(1) + "%" : "\u2014",
      },
      { label: "Median", value: fmtSeconds(tp.median_execution_s) },
      { label: "P95", value: fmtSeconds(tp.p95_execution_s) },
    ];

    var tilesHtml = tiles
      .map(function (tile) {
        return (
          '<div style="display:flex;flex-direction:column;gap:2px;min-width:72px;">' +
          '<span style="font-size:11px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.4px;">' +
          tile.label +
          "</span>" +
          '<span style="font-size:20px;font-weight:700;line-height:1.2;">' +
          tile.value +
          "</span>" +
          "</div>"
        );
      })
      .join("");

    // Mini bar chart for daily completions (last 30 days).
    var chartHtml = "";
    var daily = tp.daily_completions || [];
    if (daily.length > 0) {
      var maxCount = 0;
      daily.forEach(function (d) {
        if (d.count > maxCount) maxCount = d.count;
      });
      var bars = daily
        .map(function (d) {
          var heightPct =
            maxCount > 0
              ? Math.max(4, Math.round((d.count / maxCount) * 100))
              : 4;
          var filled = d.count > 0;
          return (
            '<div title="' +
            escapeHtml(d.date) +
            ": " +
            d.count +
            '" style="' +
            'flex:1;height:32px;display:flex;align-items:flex-end;">' +
            '<div style="width:100%;height:' +
            heightPct +
            "%;" +
            "background:" +
            (filled ? "var(--accent)" : "var(--border)") +
            ";" +
            'border-radius:2px 2px 0 0;"></div>' +
            "</div>"
          );
        })
        .join("");
      chartHtml =
        '<div style="display:flex;flex-direction:column;gap:4px;flex:1;min-width:160px;">' +
        '<span style="font-size:11px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.4px;">Daily completions (30d)</span>' +
        '<div style="display:flex;gap:2px;align-items:flex-end;height:36px;">' +
        bars +
        "</div>" +
        "</div>";
    }

    throughputEl.innerHTML = tilesHtml + chartHtml;
  }

  function renderStats(data) {
    var phases = data.phases || {};
    var keys = Object.keys(phases).sort();

    renderThroughput(data.throughput);

    var tp = data.throughput || {};
    var hasThroughput = tp.total_completed > 0 || tp.total_failed > 0;
    if (keys.length === 0 && !hasThroughput) {
      setState("empty");
      return;
    }

    // Compute the global max across all phases for proportional bar scaling.
    var globalMaxMs = 0;
    keys.forEach(function (k) {
      if (phases[k].max_ms > globalMaxMs) globalMaxMs = phases[k].max_ms;
    });

    summaryEl.innerHTML =
      "<strong>" +
      data.tasks_scanned +
      "</strong> tasks scanned &middot; " +
      "<strong>" +
      data.spans_total +
      "</strong> spans across " +
      "<strong>" +
      keys.length +
      "</strong> phase" +
      (keys.length === 1 ? "" : "s");

    tbody.innerHTML = "";
    keys.forEach(function (phase) {
      var s = phases[phase];
      var tr = createHoverRow([
        {
          html:
            '<div style="font-weight:500;font-size:12px;">' +
            escapeHtml(phaseLabel(phase)) +
            "</div>" +
            '<div style="font-size:11px;color:var(--text-muted);margin-top:2px;">' +
            escapeHtml(phaseDesc(phase)) +
            "</div>",
        },
        {
          text: s.count,
          style:
            "padding:8px 10px;text-align:right;color:var(--text-muted);font-size:12px;",
        },
        {
          text: fmtMs(s.min_ms),
          style:
            "padding:8px 10px;text-align:right;color:var(--text-muted);font-size:12px;",
        },
        {
          html:
            '<div style="font-weight:600;">' +
            fmtMs(s.p50_ms) +
            "</div>" +
            barHtml(s.p50_ms, globalMaxMs),
          style: "padding:8px 10px;text-align:right;font-size:12px;",
        },
        {
          text:
            s.count > 0 ? (s.sum_ms / s.count).toFixed(0) + " ms" : "\u2014",
          style:
            "padding:8px 10px;text-align:right;font-size:12px;color:var(--text-muted);",
        },
        {
          text: fmtMs(s.p95_ms),
          style:
            "padding:8px 10px;text-align:right;font-size:12px;font-weight:500;" +
            colorStyleForMs(s.p95_ms),
        },
        {
          text: fmtMs(s.p99_ms),
          style:
            "padding:8px 10px;text-align:right;font-size:12px;" +
            colorStyleForMs(s.p99_ms),
        },
        {
          text: fmtMs(s.max_ms),
          style:
            "padding:8px 10px;text-align:right;color:var(--text-muted);font-size:12px;",
        },
      ]);

      tbody.appendChild(tr);
    });
    setState("table");
  }

  function fmtMs(ms) {
    if (ms === undefined || ms === null) return "\u2014";
    if (ms < 1000) return ms + "ms";
    return (ms / 1000).toFixed(1) + "s";
  }

  window.showSpanStats = function () {
    openModalPanel(modal);
    fetchStats();
  };

  window.closeSpanStats = function () {
    closeModalPanel(modal);
  };

  document.addEventListener("DOMContentLoaded", init);
})();
