// Agents tab: split-pane layout. Left rail lists the merged
// built-in + user-authored catalog with search and grouping; right
// pane renders a selected agent's detail and editor. Built-in rows
// expose a Clone action; user-authored rows edit in place.

(function () {
  "use strict";

  // Module state. `agents` is the full catalog from /api/agents;
  // `selectedSlug` drives detail-pane rendering; `draft` is non-null
  // while a New Agent / Clone is being composed.
  var agents = [];
  var selectedSlug = null;
  var draft = null;
  var searchQuery = "";

  function loadAgents(opts) {
    var listEl = document.getElementById("agents-rail-list");
    if (!listEl) return;
    if (opts && opts.force) {
      // Explicit refresh after a write. Keep selection if it still
      // exists post-reload; otherwise clear.
    }

    // Surface the workspace-default harness in the tab header so
    // "(use workspace default)" on individual agents has a
    // concrete resolution the user can see and jump to change.
    var defaultEl = document.getElementById("agents-mode-default-harness");
    if (defaultEl) {
      var sb =
        (typeof defaultSandbox === "string" && defaultSandbox) || "claude";
      defaultEl.textContent = sb;
    }

    fetch(Routes.agents.list(), { headers: authHeaders() })
      .then(function (r) {
        if (!r.ok) throw new Error("HTTP " + r.status);
        return r.json();
      })
      .then(function (rows) {
        listEl.removeAttribute("aria-busy");
        agents = Array.isArray(rows) ? rows : [];
        // If the previously-selected agent vanished, clear the
        // selection so the detail pane shows the empty state.
        if (
          selectedSlug &&
          !draft &&
          !agents.find(function (a) {
            return a.slug === selectedSlug;
          })
        ) {
          selectedSlug = null;
        }
        renderRail();
        renderDetail();
      })
      .catch(function (err) {
        listEl.removeAttribute("aria-busy");
        listEl.innerHTML =
          '<p class="agents-mode__empty">Failed to load agents: ' +
          escapeHTML(String(err)) +
          "</p>";
      });
  }

  function filteredAgents() {
    if (!searchQuery) return agents.slice();
    var q = searchQuery.toLowerCase();
    return agents.filter(function (a) {
      if ((a.slug || "").toLowerCase().indexOf(q) !== -1) return true;
      if ((a.title || "").toLowerCase().indexOf(q) !== -1) return true;
      if ((a.description || "").toLowerCase().indexOf(q) !== -1) return true;
      return false;
    });
  }

  function renderRail() {
    var listEl = document.getElementById("agents-rail-list");
    if (!listEl) return;
    listEl.innerHTML = "";

    var shown = filteredAgents();
    if (shown.length === 0 && !draft) {
      listEl.innerHTML =
        '<p class="agents-mode__empty">' +
        (searchQuery ? "No matches." : "No agents registered.") +
        "</p>";
      return;
    }

    if (draft) {
      listEl.appendChild(railItem(draft, { isDraft: true }));
    }
    var builtIns = shown.filter(function (a) {
      return a.builtin;
    });
    var user = shown.filter(function (a) {
      return !a.builtin;
    });

    if (builtIns.length) {
      listEl.appendChild(groupHeader("Built-in"));
      builtIns.forEach(function (a) {
        listEl.appendChild(railItem(a));
      });
    }
    if (user.length) {
      listEl.appendChild(groupHeader("User-authored"));
      user.forEach(function (a) {
        listEl.appendChild(railItem(a));
      });
    }
  }

  function groupHeader(label) {
    var h = document.createElement("div");
    h.className = "agents-rail__group";
    h.textContent = label;
    return h;
  }

  function railItem(agent, opts) {
    opts = opts || {};
    var el = document.createElement("button");
    el.type = "button";
    el.className = "agents-rail__item";
    el.setAttribute("data-slug", agent.slug);
    if (opts.isDraft) el.classList.add("agents-rail__item--draft");
    if (!agent.builtin && !opts.isDraft)
      el.classList.add("agents-rail__item--user");
    var isSelected =
      (opts.isDraft && draft) ||
      (!opts.isDraft && !draft && selectedSlug === agent.slug);
    if (isSelected) el.classList.add("agents-rail__item--active");

    var name = document.createElement("span");
    name.className = "agents-rail__name";
    name.textContent = agent.title || agent.slug || "(untitled)";
    el.appendChild(name);

    var meta = document.createElement("span");
    meta.className = "agents-rail__meta";
    if (opts.isDraft) {
      meta.textContent = "draft";
    } else if (agent.harness) {
      meta.textContent = agent.harness;
    }
    if (meta.textContent) el.appendChild(meta);

    el.addEventListener("click", function () {
      if (opts.isDraft) {
        // Clicking the draft entry is a no-op; the detail pane
        // already shows the editor.
        return;
      }
      // Abandon any in-flight draft when the user picks an
      // existing agent; keep the workflow single-focus.
      draft = null;
      selectedSlug = agent.slug;
      renderRail();
      renderDetail();
    });
    return el;
  }

  // renderDetail paints the right pane for the current selection or
  // for the draft if one is in flight. Empty state shows when
  // nothing is selected.
  function renderDetail() {
    var detail = document.getElementById("agents-detail");
    if (!detail) return;
    detail.innerHTML = "";

    var role = null;
    var editing = false;
    if (draft) {
      role = draft;
      editing = true;
    } else if (selectedSlug) {
      role = agents.find(function (a) {
        return a.slug === selectedSlug;
      });
      editing = role && !role.builtin;
    }

    if (!role) {
      var empty = document.createElement("div");
      empty.className = "agents-mode__empty-detail";
      empty.innerHTML =
        "<p>Pick an agent on the left, or click <strong>+ New Agent</strong> above.</p>";
      detail.appendChild(empty);
      return;
    }

    detail.appendChild(renderReadOnlyHeader(role, editing));
    if (editing || draft) {
      detail.appendChild(renderEditor(role));
    } else {
      detail.appendChild(renderReadOnlyBody(role));
    }
  }

  function renderReadOnlyHeader(role, editing) {
    var head = document.createElement("div");
    head.className = "agents-detail__head";

    var titleWrap = document.createElement("div");
    var title = document.createElement("h3");
    title.className = "agents-detail__title";
    title.textContent = role.title || role.slug || "(untitled)";
    titleWrap.appendChild(title);
    var subtitle = document.createElement("div");
    subtitle.className = "agents-detail__subtitle";
    var badge = role.builtin
      ? '<span class="agents-detail__badge">built-in</span>'
      : '<span class="agents-detail__badge agents-detail__badge--user">user</span>';
    subtitle.innerHTML = badge + "<code>" + escapeHTML(role.slug) + "</code>";
    titleWrap.appendChild(subtitle);
    head.appendChild(titleWrap);

    var actions = document.createElement("div");
    actions.className = "agents-detail__actions";
    if (draft) {
      // New Agent / Clone is in flight; actions live inside the
      // editor so no extra buttons up here.
    } else if (role.builtin) {
      actions.appendChild(
        btn("Clone", "agents-detail__btn-primary", function () {
          startClone(role);
        }),
      );
    } else if (editing) {
      actions.appendChild(
        btn("Delete", "agents-detail__btn-danger", function () {
          deleteAgent(role.slug);
        }),
      );
    }
    head.appendChild(actions);
    return head;
  }

  function renderReadOnlyBody(role) {
    var body = document.createElement("div");
    body.className = "agents-detail__body";

    body.appendChild(kv("Description", role.description || ""));
    body.appendChild(kv("Harness", role.harness || "(use workspace default)"));
    body.appendChild(
      kv("Capabilities", (role.capabilities || []).join(", ") || "(none)"),
    );
    body.appendChild(
      kv("Turn model", role.multiturn ? "multi-turn" : "single-turn"),
    );

    // Prompt template body is loaded lazily via /api/agents/{slug}
    // for built-in rows to keep the list payload small.
    var tmplSection = document.createElement("div");
    tmplSection.className = "agents-detail__section";
    var tmplLabel = document.createElement("div");
    tmplLabel.className = "agents-detail__section-label";
    tmplLabel.textContent = "System prompt";
    tmplSection.appendChild(tmplLabel);

    var pre = document.createElement("pre");
    pre.className = "agents-detail__tmpl";
    pre.textContent = "Loading...";
    tmplSection.appendChild(pre);
    body.appendChild(tmplSection);

    var url = Routes.agents
      .get()
      .replace("{slug}", encodeURIComponent(role.slug));
    fetch(url, { headers: authHeaders() })
      .then(function (r) {
        return r.ok ? r.json() : null;
      })
      .then(function (full) {
        if (full && full.prompt_tmpl) {
          pre.textContent = full.prompt_tmpl;
        } else {
          pre.textContent =
            "(no system prompt; the agent consumes the task prompt directly)";
          pre.classList.add("agents-detail__tmpl--empty");
        }
      })
      .catch(function () {
        pre.textContent = "(failed to load template)";
      });

    return body;
  }

  function renderEditor(role) {
    var form = document.createElement("form");
    form.className = "agents-detail__editor";

    var slugValue = role.slug || "my-agent";
    form.appendChild(
      inputRow("Slug", "slug", slugValue, {
        disabled: !draft && !role.builtin,
        hint: "kebab-case, 2-40 chars",
      }),
    );
    form.appendChild(inputRow("Title", "title", role.title || ""));
    form.appendChild(
      inputRow("Description", "description", role.description || ""),
    );

    form.appendChild(harnessRow(role.harness || ""));
    form.appendChild(capabilitiesRow(role.capabilities || []));
    form.appendChild(
      checkboxRow(
        "Multi-turn",
        "multiturn",
        !!role.multiturn,
        "Advisory only: the runner's binding table is the source of truth for dispatch.",
      ),
    );

    form.appendChild(promptRow(role.prompt_tmpl || ""));

    var err = document.createElement("p");
    err.className = "agents-detail__editor-err";
    err.hidden = true;
    form.appendChild(err);

    var actions = document.createElement("div");
    actions.className = "agents-detail__editor-actions";
    var cancel = btn(
      "Cancel",
      "agents-detail__btn-ghost",
      function () {
        draft = null;
        if (!role.builtin && role.slug) selectedSlug = role.slug;
        renderRail();
        renderDetail();
      },
      "button",
    );
    var save = btn(
      draft ? "Create" : "Save",
      "agents-detail__btn-primary",
      null,
      "submit",
    );
    actions.appendChild(cancel);
    actions.appendChild(save);
    form.appendChild(actions);

    form.addEventListener("submit", function (e) {
      e.preventDefault();
      err.hidden = true;
      save.disabled = true;
      var payload = readEditorPayload(form);
      var isCreate = !!draft;
      var req = isCreate
        ? fetch(Routes.agents.create(), {
            method: "POST",
            headers: jsonHeaders(),
            body: JSON.stringify(payload),
          })
        : fetch(
            Routes.agents
              .update()
              .replace("{slug}", encodeURIComponent(role.slug)),
            {
              method: "PUT",
              headers: jsonHeaders(),
              body: JSON.stringify(payload),
            },
          );
      req
        .then(function (r) {
          if (r.ok) return r.json();
          return r.text().then(function (text) {
            throw new Error(text || "HTTP " + r.status);
          });
        })
        .then(function (saved) {
          draft = null;
          selectedSlug = saved.slug || payload.slug;
          loadAgents({ force: true });
        })
        .catch(function (e2) {
          err.textContent = String(e2.message || e2);
          err.hidden = false;
          save.disabled = false;
        });
    });
    return form;
  }

  function readEditorPayload(form) {
    var slug = form.querySelector('[name="slug"]').value.trim();
    var title = form.querySelector('[name="title"]').value.trim();
    var description = form.querySelector('[name="description"]').value.trim();
    var harness = form.querySelector('[name="harness"]').value || "";
    var multiturn = form.querySelector('[name="multiturn"]').checked;
    var caps = Array.from(form.querySelectorAll('[name="capability"]:checked'))
      .map(function (el) {
        return el.value;
      })
      .filter(Boolean);
    var promptTmplEl = form.querySelector('[name="prompt_tmpl"]');
    var promptTmpl = promptTmplEl ? promptTmplEl.value : "";
    return {
      slug: slug,
      title: title,
      description: description,
      harness: harness,
      multiturn: multiturn,
      capabilities: caps,
      prompt_tmpl: promptTmpl,
    };
  }

  function startClone(role) {
    draft = {
      slug: suggestCloneSlug(role.slug),
      title: role.title || "",
      description: role.description || "",
      harness: role.harness || "",
      multiturn: !!role.multiturn,
      capabilities: (role.capabilities || []).slice(),
      prompt_tmpl: role.prompt_tmpl || "",
    };
    // If prompt_tmpl wasn't in the list row, fetch the full detail
    // so the clone starts with a populated body.
    if (!role.prompt_tmpl && role.slug) {
      var url = Routes.agents
        .get()
        .replace("{slug}", encodeURIComponent(role.slug));
      fetch(url, { headers: authHeaders() })
        .then(function (r) {
          return r.ok ? r.json() : null;
        })
        .then(function (full) {
          if (full && full.prompt_tmpl && draft) {
            draft.prompt_tmpl = full.prompt_tmpl;
            renderDetail();
          }
        })
        .catch(function () {});
    }
    selectedSlug = null;
    renderRail();
    renderDetail();
  }

  function openNewEditor() {
    draft = {
      slug: "my-agent",
      title: "",
      description: "",
      harness: "",
      multiturn: false,
      capabilities: [],
      prompt_tmpl: "",
    };
    selectedSlug = null;
    renderRail();
    renderDetail();
  }

  function deleteAgent(slug) {
    var ok = true;
    if (typeof window.confirm === "function") {
      ok = window.confirm("Delete agent " + slug + "?");
    }
    if (!ok) return;
    var url = Routes.agents
      .delete()
      .replace("{slug}", encodeURIComponent(slug));
    fetch(url, { method: "DELETE", headers: authHeaders() })
      .then(function (r) {
        if (!r.ok && r.status !== 204) {
          return r.text().then(function (t) {
            throw new Error(t || "HTTP " + r.status);
          });
        }
      })
      .then(function () {
        selectedSlug = null;
        loadAgents({ force: true });
      })
      .catch(function (err) {
        if (typeof window.showAlert === "function") {
          window.showAlert("Delete failed: " + err.message);
        }
      });
  }

  // --- Editor field builders ---

  function inputRow(label, name, value, opts) {
    opts = opts || {};
    var wrap = document.createElement("label");
    wrap.className = "agents-detail__field";
    var l = document.createElement("span");
    l.className = "agents-detail__field-label";
    l.textContent = label;
    var input = document.createElement("input");
    input.type = "text";
    input.name = name;
    input.value = value || "";
    if (opts.disabled) input.disabled = true;
    if (opts.hint) input.title = opts.hint;
    wrap.appendChild(l);
    wrap.appendChild(input);
    if (opts.hint) {
      var h = document.createElement("span");
      h.className = "agents-detail__field-hint";
      h.textContent = opts.hint;
      wrap.appendChild(h);
    }
    return wrap;
  }

  // harnessRow renders the harness pin as a three-way segmented
  // button group so the choice reads like a toggle, not a dropdown.
  function harnessRow(current) {
    var wrap = document.createElement("div");
    wrap.className = "agents-detail__field";
    var l = document.createElement("span");
    l.className = "agents-detail__field-label";
    l.textContent = "Harness";
    wrap.appendChild(l);

    var seg = document.createElement("div");
    seg.className = "agents-detail__segment";
    var hidden = document.createElement("input");
    hidden.type = "hidden";
    hidden.name = "harness";
    hidden.value = current || "";
    wrap.appendChild(hidden);

    [
      { value: "", label: "Default" },
      { value: "claude", label: "Claude" },
      { value: "codex", label: "Codex" },
    ].forEach(function (opt) {
      var b = document.createElement("button");
      b.type = "button";
      b.className = "agents-detail__segment-btn";
      if ((current || "") === opt.value) {
        b.classList.add("agents-detail__segment-btn--active");
      }
      b.textContent = opt.label;
      b.addEventListener("click", function () {
        hidden.value = opt.value;
        Array.from(seg.children).forEach(function (c) {
          c.classList.remove("agents-detail__segment-btn--active");
        });
        b.classList.add("agents-detail__segment-btn--active");
      });
      seg.appendChild(b);
    });
    wrap.appendChild(seg);

    var h = document.createElement("span");
    h.className = "agents-detail__field-hint";
    h.textContent =
      "Default inherits from the workspace setting. " +
      "Claude and Codex pin this agent to a specific harness regardless of task or env config.";
    wrap.appendChild(h);
    return wrap;
  }

  function capabilitiesRow(current) {
    var wrap = document.createElement("div");
    wrap.className = "agents-detail__field";
    var l = document.createElement("span");
    l.className = "agents-detail__field-label";
    l.textContent = "Capabilities";
    wrap.appendChild(l);

    var group = document.createElement("div");
    group.className = "agents-detail__checks";
    [
      {
        value: "workspace.read",
        label: "workspace.read",
        hint: "read workspace files",
      },
      {
        value: "workspace.write",
        label: "workspace.write",
        hint: "write + commit changes",
      },
      {
        value: "board.context",
        label: "board.context",
        hint: "see sibling tasks",
      },
    ].forEach(function (cap) {
      var lab = document.createElement("label");
      lab.className = "agents-detail__check";
      var cb = document.createElement("input");
      cb.type = "checkbox";
      cb.name = "capability";
      cb.value = cap.value;
      cb.checked = current.indexOf(cap.value) !== -1;
      var span = document.createElement("span");
      span.textContent = cap.label;
      span.title = cap.hint;
      lab.appendChild(cb);
      lab.appendChild(span);
      group.appendChild(lab);
    });
    wrap.appendChild(group);
    return wrap;
  }

  function checkboxRow(label, name, checked, hint) {
    var wrap = document.createElement("label");
    wrap.className = "agents-detail__field agents-detail__field--check";
    var cb = document.createElement("input");
    cb.type = "checkbox";
    cb.name = name;
    cb.checked = !!checked;
    var span = document.createElement("span");
    span.textContent = label;
    wrap.appendChild(cb);
    wrap.appendChild(span);
    if (hint) {
      var h = document.createElement("span");
      h.className = "agents-detail__field-hint";
      h.textContent = hint;
      wrap.appendChild(h);
    }
    return wrap;
  }

  function promptRow(value) {
    var wrap = document.createElement("div");
    wrap.className = "agents-detail__field agents-detail__field--prompt";
    var l = document.createElement("span");
    l.className = "agents-detail__field-label";
    l.textContent = "System Prompt";
    wrap.appendChild(l);
    var ta = document.createElement("textarea");
    ta.name = "prompt_tmpl";
    ta.rows = 14;
    ta.value = value || "";
    wrap.appendChild(ta);
    var h = document.createElement("span");
    h.className = "agents-detail__field-hint";
    h.textContent =
      "Optional preamble prepended to every invocation of this agent " +
      "through the flow engine. The agent sees this text first, then " +
      "a blank line, then the caller's prompt. Leave empty to use the " +
      "agent's default behaviour. Note: built-in sub-agents invoked by " +
      "the implement turn loop (title, oversight, commit-msg) use " +
      "their embedded templates regardless; put custom prompts on a " +
      "clone referenced from a custom flow.";
    wrap.appendChild(h);
    return wrap;
  }

  function kv(label, value) {
    var row = document.createElement("div");
    row.className = "agents-detail__kv";
    var k = document.createElement("span");
    k.className = "agents-detail__kv-key";
    k.textContent = label;
    var v = document.createElement("span");
    v.className = "agents-detail__kv-value";
    v.textContent = value;
    row.appendChild(k);
    row.appendChild(v);
    return row;
  }

  function btn(text, cls, onClick, type) {
    var b = document.createElement("button");
    b.type = type || "button";
    b.className = cls;
    b.textContent = text;
    if (onClick) b.addEventListener("click", onClick);
    return b;
  }

  function suggestCloneSlug(base) {
    var suggestion = base + "-copy";
    return suggestion.length <= 40 ? suggestion : base.slice(0, 35) + "-copy";
  }

  function authHeaders() {
    if (typeof window.getAuthHeaders === "function") {
      return window.getAuthHeaders();
    }
    return {};
  }

  function jsonHeaders() {
    var h = authHeaders();
    h["Content-Type"] = "application/json";
    return h;
  }

  function escapeHTML(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
  }

  // Bind the search input once per tab load. The input is inside the
  // static partial so it's safe to bind on first loadAgents() too.
  function bindSearch() {
    var s = document.getElementById("agents-rail-search");
    if (!s || s.dataset.bound === "true") return;
    s.dataset.bound = "true";
    s.addEventListener("input", function () {
      searchQuery = s.value || "";
      renderRail();
    });
  }

  // loadAgentsPublic is the window-facing entry point. It wraps
  // loadAgents with the one-time search binding; kept as a named
  // wrapper so biome's no-function-reassign rule stays happy.
  function loadAgentsPublic(opts) {
    bindSearch();
    loadAgents(opts);
  }

  window.loadAgents = loadAgentsPublic;
  window.__agents_test = {
    renderRail: renderRail,
    renderDetail: renderDetail,
    startClone: startClone,
    openNewEditor: openNewEditor,
    readEditorPayload: readEditorPayload,
    suggestCloneSlug: suggestCloneSlug,
    _setState: function (state) {
      if (state.agents) agents = state.agents;
      if (state.selectedSlug !== undefined) selectedSlug = state.selectedSlug;
      if (state.draft !== undefined) draft = state.draft;
      if (state.searchQuery !== undefined) searchQuery = state.searchQuery;
    },
  };
})();
