// Agents tab: renders the read-only catalog of built-in sub-agent
// roles. Clicking a row toggles an inline panel with the full prompt
// template body fetched lazily from /api/agents/{slug}. Editable
// agents ship in a follow-up task; the Clone button is a stub.

(function () {
  "use strict";

  var loaded = false;

  // loadAgents fetches the list and renders the rows. Idempotent — a
  // successful load is cached so repeated tab switches don't re-fetch.
  function loadAgents(opts) {
    if (loaded && !(opts && opts.force)) return;
    var listEl = document.getElementById("agents-list");
    if (!listEl) return;

    var url =
      window.apiRoutes && window.apiRoutes.agents
        ? window.apiRoutes.agents.list()
        : "/api/agents";

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
            '<p class="agents-mode__empty">No agents registered.</p>';
          return;
        }
        listEl.innerHTML = "";
        rows.forEach(function (row) {
          listEl.appendChild(renderRow(row));
        });
      })
      .catch(function (err) {
        listEl.removeAttribute("aria-busy");
        listEl.innerHTML =
          '<p class="agents-mode__empty">Failed to load agents: ' +
          escapeHTML(String(err)) +
          "</p>";
      });
  }

  // renderRow produces one collapsed agent card.
  function renderRow(agent) {
    var card = document.createElement("div");
    card.className = "agents-row";
    card.setAttribute("data-slug", agent.slug);
    card.setAttribute("role", "button");
    card.setAttribute("tabindex", "0");

    var header = document.createElement("div");
    header.className = "agents-row__header";

    var name = document.createElement("div");
    name.className = "agents-row__name";
    name.textContent = agent.title || agent.slug;

    var meta = document.createElement("div");
    meta.className = "agents-row__meta";
    var capLabel = capabilitiesLabel(agent.capabilities);
    var turnLabel = agent.multiturn ? "multi-turn" : "single-turn";
    meta.textContent = capLabel ? capLabel + " · " + turnLabel : turnLabel;

    var desc = document.createElement("p");
    desc.className = "agents-row__desc";
    desc.textContent = agent.description || "";

    var clone = document.createElement("button");
    clone.type = "button";
    clone.className = "agents-row__clone";
    clone.textContent = "Clone";
    clone.disabled = true;
    clone.title = "Editable agents ship next";
    clone.addEventListener("click", function (e) {
      e.stopPropagation();
    });

    header.appendChild(name);
    header.appendChild(meta);
    header.appendChild(clone);
    card.appendChild(header);
    if (desc.textContent) card.appendChild(desc);

    var body = document.createElement("div");
    body.className = "agents-row__body";
    body.hidden = true;
    card.appendChild(body);

    var onToggle = function () {
      expandAgent(agent.slug, card, body);
    };
    card.addEventListener("click", onToggle);
    card.addEventListener("keydown", function (e) {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        onToggle();
      }
    });

    return card;
  }

  function expandAgent(slug, card, body) {
    if (!card.classList.contains("agents-row--open")) {
      var url =
        window.apiRoutes && window.apiRoutes.agents
          ? window.apiRoutes.agents
              .get()
              .replace("{slug}", encodeURIComponent(slug))
          : "/api/agents/" + encodeURIComponent(slug);
      body.innerHTML = '<p class="agents-row__loading">Loading…</p>';
      body.hidden = false;
      card.classList.add("agents-row--open");
      fetch(url, { headers: authHeaders() })
        .then(function (r) {
          return r.ok ? r.json() : Promise.reject(r.status);
        })
        .then(function (full) {
          body.innerHTML = "";
          var tmplLabel = document.createElement("div");
          tmplLabel.className = "agents-row__tmpl-label";
          tmplLabel.textContent = full.prompt_tmpl
            ? "Prompt template"
            : "Prompt: consumed from the task prompt directly.";
          body.appendChild(tmplLabel);
          if (full.prompt_tmpl) {
            var pre = document.createElement("pre");
            pre.className = "agents-row__tmpl";
            pre.textContent = full.prompt_tmpl;
            body.appendChild(pre);
          }
          if (full.capabilities && full.capabilities.length) {
            var foot = document.createElement("div");
            foot.className = "agents-row__foot";
            foot.textContent = "Capabilities: " + full.capabilities.join(", ");
            body.appendChild(foot);
          }
        })
        .catch(function (err) {
          body.innerHTML =
            '<p class="agents-row__loading">Failed: ' +
            escapeHTML(String(err)) +
            "</p>";
        });
    } else {
      card.classList.remove("agents-row--open");
      body.hidden = true;
    }
  }

  function capabilitiesLabel(caps) {
    if (!caps || caps.length === 0) return "no workspace access";
    return caps
      .map(function (c) {
        switch (c) {
          case "workspace.read":
            return "workspace read";
          case "workspace.write":
            return "workspace write";
          case "board.context":
            return "board context";
          default:
            return c;
        }
      })
      .join(" · ");
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

  window.loadAgents = loadAgents;
  window.__agents_test = { renderRow: renderRow, expandAgent: expandAgent };
})();
