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
    name.textContent = agent.name;

    var meta = document.createElement("div");
    meta.className = "agents-row__meta";
    meta.textContent =
      agent.activity +
      " · " +
      mountLabel(agent.mount_mode) +
      (agent.single_turn ? " · single-turn" : " · multi-turn");

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
          var timeout = full.timeout_sec
            ? full.timeout_sec + "s timeout"
            : "caller-owned timeout";
          var foot = document.createElement("div");
          foot.className = "agents-row__foot";
          foot.textContent = timeout;
          body.appendChild(foot);
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

  function mountLabel(mode) {
    switch (mode) {
      case "none":
        return "no mounts";
      case "read-only":
        return "workspace read-only";
      case "read-write":
        return "worktree read-write";
      default:
        return mode || "unknown mount";
    }
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
