// --- Usage Statistics Modal ---

(function () {
  var modal, loadingEl, errorEl, emptyEl, contentEl;
  var summaryEl, byStatusTbody, bySubAgentTbody, periodSelect;
  var setState;

  function init() {
    modal = document.getElementById("usage-stats-modal");
    loadingEl = document.getElementById("usage-stats-loading");
    errorEl = document.getElementById("usage-stats-error");
    emptyEl = document.getElementById("usage-stats-empty");
    contentEl = document.getElementById("usage-stats-content");
    summaryEl = document.getElementById("usage-stats-summary");
    byStatusTbody = document.getElementById("usage-stats-by-status-tbody");
    bySubAgentTbody = document.getElementById("usage-stats-by-sub-agent-tbody");
    periodSelect = document.getElementById("usage-stats-period");

    bindModalBackdropClose(modal, closeUsageStats);

    periodSelect.addEventListener("change", fetchStats);
    setState = createModalStateController({
      loadingEl: loadingEl,
      errorEl: errorEl,
      emptyEl: emptyEl,
      contentEl: contentEl,
      contentState: "content",
    });
  }

  function fetchStats() {
    var days = periodSelect ? periodSelect.value : "7";
    loadJsonEndpoint(
      "/api/usage?days=" + encodeURIComponent(days),
      renderStats,
      setState,
    );
  }

  // Status badge colours (mirrors existing badge-* CSS classes via inline style).
  var statusColors = {
    done: { bg: "var(--badge-done-bg)", fg: "var(--badge-done-fg)" },
    failed: { bg: "var(--badge-failed-bg)", fg: "var(--badge-failed-fg)" },
    cancelled: {
      bg: "var(--badge-cancelled-bg)",
      fg: "var(--badge-cancelled-fg)",
    },
    in_progress: {
      bg: "var(--badge-inprogress-bg)",
      fg: "var(--badge-inprogress-fg)",
    },
    waiting: { bg: "var(--badge-waiting-bg)", fg: "var(--badge-waiting-fg)" },
    backlog: { bg: "var(--badge-backlog-bg)", fg: "var(--badge-backlog-fg)" },
    committing: {
      bg: "var(--badge-committing-bg)",
      fg: "var(--badge-committing-fg)",
    },
  };

  function statusBadge(status) {
    var c = statusColors[status] || {
      bg: "var(--bg-raised)",
      fg: "var(--text-muted)",
    };
    return (
      '<span style="display:inline-block;padding:1px 7px;border-radius:999px;font-size:11px;font-weight:600;background:' +
      c.bg +
      ";color:" +
      c.fg +
      ';">' +
      escapeHtml(status.replace("_", "\u00a0")) +
      "</span>"
    );
  }

  var agentLabels = {
    implementation: "Implementation",
    test: "Test",
    refinement: "Refinement",
    title: "Title gen.",
    oversight: "Oversight",
    "oversight-test": "Oversight (test)",
  };

  function agentLabel(key) {
    return agentLabels[key] || escapeHtml(key);
  }

  function fmtTokens(n) {
    if (!n) return "\u2014";
    return n.toLocaleString();
  }

  function fmtCost(usd) {
    if (!usd) return "\u2014";
    return "$" + usd.toFixed(4);
  }

  function usageRow(label, usage, isBold) {
    var totalTokens = (usage.input_tokens || 0) + (usage.output_tokens || 0);
    var labelCell = typeof label === "string" ? { text: label } : label;
    return createHoverRow([
      {
        style: "padding:6px 10px;" + (isBold ? "font-weight:600;" : ""),
        text: labelCell.text,
        html: labelCell.html,
      },
      {
        text: fmtTokens(usage.input_tokens),
        style: "padding:6px 10px;text-align:right;color:var(--text-muted);",
      },
      {
        text: fmtTokens(usage.output_tokens),
        style: "padding:6px 10px;text-align:right;color:var(--text-muted);",
      },
      {
        text: fmtTokens(totalTokens || 0),
        style: "padding:6px 10px;text-align:right;color:var(--text-muted);",
      },
      {
        text: fmtCost(usage.cost_usd),
        style:
          "padding:6px 10px;text-align:right;font-weight:600;color:var(--accent);",
      },
    ]);
  }

  function renderStats(data) {
    var total = data.total || {};
    var byStatus = data.by_status || {};
    var bySubAgent = data.by_sub_agent || {};

    var hasData =
      total.cost_usd > 0 ||
      Object.keys(byStatus).length > 0 ||
      Object.keys(bySubAgent).length > 0;

    if (!hasData && data.task_count === 0) {
      setState("empty");
      return;
    }

    // Summary bar.
    var periodLabel =
      data.period_days === 0
        ? "all time"
        : "last " + data.period_days + " days";
    summaryEl.textContent =
      data.task_count +
      " task" +
      (data.task_count === 1 ? "" : "s") +
      " \u00b7 " +
      periodLabel +
      " \u00b7 total cost: " +
      (total.cost_usd ? "$" + total.cost_usd.toFixed(4) : "$0.0000");

    // By-Status table.
    byStatusTbody.innerHTML = "";
    var statusKeys = Object.keys(byStatus).sort();
    if (statusKeys.length === 0) {
      appendNoDataRow(byStatusTbody, 5, "No data");
    } else {
      statusKeys.forEach(function (status) {
        byStatusTbody.appendChild(
          usageRow({ html: statusBadge(status) }, byStatus[status], false),
        );
      });
      byStatusTbody.appendChild(
        usageRow({ html: "<strong>Total</strong>" }, total, true),
      );
    }

    // By-Sub-Agent table.
    bySubAgentTbody.innerHTML = "";
    var agentKeys = Object.keys(bySubAgent).sort();
    if (agentKeys.length === 0) {
      appendNoDataRow(bySubAgentTbody, 5, "No data");
    } else {
      agentKeys.forEach(function (key) {
        bySubAgentTbody.appendChild(
          usageRow(agentLabel(key), bySubAgent[key], false),
        );
      });
      bySubAgentTbody.appendChild(
        usageRow({ html: "<strong>Total</strong>" }, total, true),
      );
    }

    setState("content");
  }

  window.showUsageStats = function () {
    openModalPanel(modal);
    fetchStats();
  };

  window.closeUsageStats = function () {
    closeModalPanel(modal);
  };

  document.addEventListener("DOMContentLoaded", init);
})();
