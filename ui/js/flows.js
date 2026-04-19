// Flows tab: renders the read-only catalog of built-in flows.
// Each flow renders as a row with a one-line chip chain showing its
// step order, optional steps (trailing ?), and parallel-sibling
// groups (separated from the preceding step by a ‖ divider).
// Clicking a chip cross-navigates to the Agents tab with the
// corresponding agent expanded. Editable flows ship in a follow-up.

(function () {
  "use strict";

  var loaded = false;

  function loadFlows(opts) {
    if (loaded && !(opts && opts.force)) return;
    var listEl = document.getElementById("flows-list");
    if (!listEl) return;

    var url =
      window.apiRoutes && window.apiRoutes.flows
        ? window.apiRoutes.flows.list()
        : "/api/flows";

    fetch(url, { headers: authHeaders() })
      .then(function (r) {
        if (!r.ok) throw new Error("HTTP " + r.status);
        return r.json();
      })
      .then(function (rows) {
        loaded = true;
        listEl.removeAttribute("aria-busy");
        if (!Array.isArray(rows) || rows.length === 0) {
          listEl.innerHTML =
            '<p class="flows-mode__empty">No flows registered.</p>';
          return;
        }
        listEl.innerHTML = "";
        rows.forEach(function (row) {
          listEl.appendChild(renderFlow(row));
        });
      })
      .catch(function (err) {
        listEl.removeAttribute("aria-busy");
        listEl.innerHTML =
          '<p class="flows-mode__empty">Failed to load flows: ' +
          escapeHTML(String(err)) +
          "</p>";
      });
  }

  // renderFlow produces one collapsed flow card with its chip chain.
  function renderFlow(flow) {
    var card = document.createElement("div");
    card.className = "flows-row";
    card.setAttribute("data-slug", flow.slug);

    var header = document.createElement("div");
    header.className = "flows-row__header";

    var name = document.createElement("div");
    name.className = "flows-row__name";
    name.textContent = flow.name || flow.slug;

    var badge = document.createElement("span");
    badge.className = "flows-row__badge";
    badge.textContent = flow.builtin ? "built-in" : "custom";

    header.appendChild(name);
    header.appendChild(badge);
    card.appendChild(header);

    if (flow.description) {
      var desc = document.createElement("p");
      desc.className = "flows-row__desc";
      desc.textContent = flow.description;
      card.appendChild(desc);
    }

    var chain = buildChain(flow.steps || []);
    card.appendChild(chain);

    return card;
  }

  // buildChain renders the step chips left-to-right, collapsing
  // transitively-mutual RunInParallelWith groups into a parallel
  // cluster separated by the previous step with a ‖ divider.
  function buildChain(steps) {
    var container = document.createElement("div");
    container.className = "flows-row__chain";
    if (steps.length === 0) return container;

    // Group steps using the same transitive-closure rule as the
    // engine: any step listing another as a parallel sibling lands
    // in the same group.
    var groups = groupParallel(steps);

    groups.forEach(function (group, gi) {
      if (gi > 0) {
        container.appendChild(arrow("→"));
      }
      if (group.length === 1) {
        container.appendChild(chip(group[0]));
      } else {
        for (var i = 0; i < group.length; i++) {
          if (i > 0) {
            container.appendChild(arrow("‖"));
          }
          container.appendChild(chip(group[i]));
        }
      }
    });
    return container;
  }

  function groupParallel(steps) {
    var bySlug = {};
    steps.forEach(function (s, i) {
      bySlug[s.agent_slug] = i;
    });
    var adj = steps.map(function (s) {
      var peers = [];
      (s.run_in_parallel_with || []).forEach(function (p) {
        if (typeof bySlug[p] === "number" && bySlug[p] !== bySlug[s.agent_slug])
          peers.push(bySlug[p]);
      });
      return peers;
    });
    var assigned = steps.map(function () {
      return -1;
    });
    var groups = [];
    steps.forEach(function (_, i) {
      if (assigned[i] !== -1) return;
      var gid = groups.length;
      var queue = [i];
      assigned[i] = gid;
      var members = [i];
      while (queue.length) {
        var cur = queue.shift();
        adj[cur].forEach(function (n) {
          if (assigned[n] === -1) {
            assigned[n] = gid;
            members.push(n);
            queue.push(n);
          }
        });
      }
      members.sort(function (a, b) {
        return a - b;
      });
      groups.push(
        members.map(function (idx) {
          return steps[idx];
        }),
      );
    });
    return groups;
  }

  function chip(step) {
    var el = document.createElement("button");
    el.type = "button";
    el.className = "flows-chip";
    el.classList.add("flows-chip");
    var label = step.agent_name || step.agent_slug;
    if (step.optional) label += "?";
    el.textContent = label;
    el.title = step.input_from
      ? "Prompted by the output of step “" + step.input_from + "”"
      : "Receives the task prompt";
    el.addEventListener("click", function (e) {
      e.stopPropagation();
      if (typeof window.switchMode === "function") {
        window.switchMode("agents", { persist: true });
      }
      // Best-effort: scroll the corresponding agent row into view.
      window.setTimeout(function () {
        var target = document.querySelector(
          '.agents-row[data-slug="' + cssEscape(step.agent_slug) + '"]',
        );
        if (target && target.scrollIntoView) {
          target.scrollIntoView({ behavior: "smooth", block: "center" });
        }
      }, 60);
    });
    return el;
  }

  function arrow(glyph) {
    var el = document.createElement("span");
    el.className = "flows-chain__sep";
    el.classList.add("flows-chain__sep");
    el.textContent = glyph;
    return el;
  }

  function cssEscape(s) {
    if (window.CSS && typeof window.CSS.escape === "function") {
      return window.CSS.escape(s);
    }
    return String(s).replace(/"/g, '\\"');
  }

  function authHeaders() {
    if (typeof window.getAuthHeaders === "function") {
      return window.getAuthHeaders();
    }
    return {};
  }

  function escapeHTML(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
  }

  window.loadFlows = loadFlows;
  window.__flows_test = {
    renderFlow: renderFlow,
    buildChain: buildChain,
    groupParallel: groupParallel,
  };
})();
