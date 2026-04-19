// Flows tab: split-pane layout. Left rail lists built-in + user
// flows with search; right pane renders a flow's chip-chain preview
// (read-only for built-ins) or an editor with drag-and-drop step
// reorder for user-authored flows.

(function () {
  "use strict";

  var flows = [];
  var agentsCache = []; // populated from /api/agents lazily
  var agentsPromise = null;
  var selectedSlug = null;
  var draft = null;
  var searchQuery = "";

  function loadFlows(opts) {
    var listEl = document.getElementById("flows-rail-list");
    if (!listEl) return;
    void opts;

    fetch(Routes.flows.list(), { headers: authHeaders() })
      .then(function (r) {
        if (!r.ok) throw new Error("HTTP " + r.status);
        return r.json();
      })
      .then(function (rows) {
        listEl.removeAttribute("aria-busy");
        flows = Array.isArray(rows) ? rows : [];
        if (
          selectedSlug &&
          !draft &&
          !flows.find(function (f) {
            return f.slug === selectedSlug;
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
          '<p class="flows-mode__empty">Failed to load flows: ' +
          escapeHTML(String(err)) +
          "</p>";
      });
  }

  function ensureAgents() {
    if (agentsCache.length > 0) return Promise.resolve(agentsCache);
    if (agentsPromise) return agentsPromise;
    agentsPromise = fetch(Routes.agents.list(), { headers: authHeaders() })
      .then(function (r) {
        return r.ok ? r.json() : [];
      })
      .then(function (rows) {
        agentsCache = Array.isArray(rows) ? rows : [];
        return agentsCache;
      })
      .catch(function () {
        return [];
      })
      .finally(function () {
        agentsPromise = null;
      });
    return agentsPromise;
  }

  function filteredFlows() {
    if (!searchQuery) return flows.slice();
    var q = searchQuery.toLowerCase();
    return flows.filter(function (f) {
      if ((f.slug || "").toLowerCase().indexOf(q) !== -1) return true;
      if ((f.name || "").toLowerCase().indexOf(q) !== -1) return true;
      if ((f.description || "").toLowerCase().indexOf(q) !== -1) return true;
      return false;
    });
  }

  function renderRail() {
    var listEl = document.getElementById("flows-rail-list");
    if (!listEl) return;
    listEl.innerHTML = "";

    var shown = filteredFlows();
    if (shown.length === 0 && !draft) {
      listEl.innerHTML =
        '<p class="flows-mode__empty">' +
        (searchQuery ? "No matches." : "No flows registered.") +
        "</p>";
      return;
    }

    if (draft) listEl.appendChild(railItem(draft, { isDraft: true }));
    var builtIns = shown.filter(function (f) {
      return f.builtin;
    });
    var user = shown.filter(function (f) {
      return !f.builtin;
    });
    if (builtIns.length) {
      listEl.appendChild(groupHeader("Built-in"));
      builtIns.forEach(function (f) {
        listEl.appendChild(railItem(f));
      });
    }
    if (user.length) {
      listEl.appendChild(groupHeader("User-authored"));
      user.forEach(function (f) {
        listEl.appendChild(railItem(f));
      });
    }
  }

  function groupHeader(label) {
    var h = document.createElement("div");
    h.className = "flows-rail__group";
    h.textContent = label;
    return h;
  }

  function railItem(flow, opts) {
    opts = opts || {};
    var el = document.createElement("button");
    el.type = "button";
    el.className = "flows-rail__item";
    el.setAttribute("data-slug", flow.slug);
    if (opts.isDraft) el.classList.add("flows-rail__item--draft");
    if (!flow.builtin && !opts.isDraft)
      el.classList.add("flows-rail__item--user");
    var isSelected =
      (opts.isDraft && draft) ||
      (!opts.isDraft && !draft && selectedSlug === flow.slug);
    if (isSelected) el.classList.add("flows-rail__item--active");

    var name = document.createElement("span");
    name.className = "flows-rail__name";
    name.textContent = flow.name || flow.slug || "(untitled)";
    el.appendChild(name);

    var meta = document.createElement("span");
    meta.className = "flows-rail__meta";
    if (opts.isDraft) {
      meta.textContent = "draft";
    } else if (flow.steps) {
      meta.textContent = flow.steps.length + " step" +
        (flow.steps.length === 1 ? "" : "s");
    }
    if (meta.textContent) el.appendChild(meta);

    el.addEventListener("click", function () {
      if (opts.isDraft) return;
      draft = null;
      selectedSlug = flow.slug;
      renderRail();
      renderDetail();
    });
    return el;
  }

  function renderDetail() {
    var detail = document.getElementById("flows-detail");
    if (!detail) return;
    detail.innerHTML = "";

    var flow = null;
    var editing = false;
    if (draft) {
      flow = draft;
      editing = true;
    } else if (selectedSlug) {
      flow = flows.find(function (f) {
        return f.slug === selectedSlug;
      });
      editing = flow && !flow.builtin;
    }

    if (!flow) {
      var empty = document.createElement("div");
      empty.className = "flows-mode__empty-detail";
      empty.innerHTML =
        "<p>Pick a flow on the left, or click <strong>+ New Flow</strong> above.</p>";
      detail.appendChild(empty);
      return;
    }

    detail.appendChild(renderHead(flow, editing));
    if (editing || draft) {
      detail.appendChild(renderEditor(flow));
    } else {
      detail.appendChild(renderReadOnlyBody(flow));
    }
  }

  function renderHead(flow, editing) {
    var head = document.createElement("div");
    head.className = "flows-detail__head";
    var titleWrap = document.createElement("div");
    var title = document.createElement("h3");
    title.className = "flows-detail__title";
    title.textContent = flow.name || flow.slug || "(untitled)";
    titleWrap.appendChild(title);
    var subtitle = document.createElement("div");
    subtitle.className = "flows-detail__subtitle";
    var badge = flow.builtin
      ? '<span class="flows-detail__badge">built-in</span>'
      : '<span class="flows-detail__badge flows-detail__badge--user">user</span>';
    subtitle.innerHTML = badge + "<code>" + escapeHTML(flow.slug) + "</code>";
    titleWrap.appendChild(subtitle);
    head.appendChild(titleWrap);

    var actions = document.createElement("div");
    actions.className = "flows-detail__actions";
    if (draft) {
      // Actions live inside the editor.
    } else if (flow.builtin) {
      actions.appendChild(
        btn("Clone", "flows-detail__btn-primary", function () {
          startClone(flow);
        }),
      );
    } else if (editing) {
      actions.appendChild(
        btn("Delete", "flows-detail__btn-danger", function () {
          deleteFlow(flow.slug);
        }),
      );
    }
    head.appendChild(actions);
    return head;
  }

  function renderReadOnlyBody(flow) {
    var body = document.createElement("div");
    body.className = "flows-detail__body";
    if (flow.description) {
      var desc = document.createElement("p");
      desc.className = "flows-detail__desc";
      desc.textContent = flow.description;
      body.appendChild(desc);
    }
    body.appendChild(buildChain(flow.steps || []));
    return body;
  }

  // buildChain: one chip per step, grouped into parallel clusters
  // via the same transitive-closure rule the engine uses.
  function buildChain(steps) {
    var container = document.createElement("div");
    container.className = "flows-detail__chain";
    if (steps.length === 0) return container;
    var groups = groupParallel(steps);
    groups.forEach(function (group, gi) {
      if (gi > 0) container.appendChild(arrow("→"));
      if (group.length === 1) {
        container.appendChild(chip(group[0]));
      } else {
        var box = document.createElement("span");
        box.className = "flows-detail__parallel";
        for (var i = 0; i < group.length; i++) {
          if (i > 0) box.appendChild(arrow("‖"));
          box.appendChild(chip(group[i]));
        }
        container.appendChild(box);
      }
    });
    return container;
  }

  function chip(step) {
    var el = document.createElement("button");
    el.type = "button";
    el.className = "flows-chip";
    var label = step.agent_name || step.agent_slug;
    if (step.optional) label += "?";
    el.textContent = label;
    el.title = step.input_from
      ? 'Prompted by the output of step "' + step.input_from + '"'
      : "Receives the task prompt";
    el.addEventListener("click", function (e) {
      e.stopPropagation();
      if (typeof window.switchMode === "function") {
        window.switchMode("agents", { persist: true });
      }
    });
    return el;
  }

  function arrow(glyph) {
    var el = document.createElement("span");
    el.className = "flows-chain__sep";
    el.textContent = glyph;
    return el;
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

  // --- Editor with drag-and-drop step reorder ---

  function renderEditor(flow) {
    var form = document.createElement("form");
    form.className = "flows-detail__editor";

    form.appendChild(
      inputRow("Slug", "slug", flow.slug || "my-flow", {
        disabled: !draft && !flow.builtin,
        hint: "kebab-case, 2-40 chars",
      }),
    );
    form.appendChild(inputRow("Name", "name", flow.name || ""));
    form.appendChild(
      inputRow("Description", "description", flow.description || ""),
    );

    // Clone-editable copy of the steps array. Drag-and-drop and
    // optional/remove controls mutate this array in place so the
    // save path serialises exactly what the user sees.
    var steps = (flow.steps || []).map(function (s) {
      return {
        agent_slug: s.agent_slug || "",
        optional: !!s.optional,
        input_from: s.input_from || "",
        run_in_parallel_with: (s.run_in_parallel_with || []).slice(),
      };
    });
    if (steps.length === 0) {
      steps.push({
        agent_slug: "",
        optional: false,
        input_from: "",
        run_in_parallel_with: [],
      });
    }

    var stepsWrap = document.createElement("div");
    stepsWrap.className = "flows-detail__field";
    var stepsLabel = document.createElement("span");
    stepsLabel.className = "flows-detail__field-label";
    stepsLabel.textContent = "Steps";
    stepsWrap.appendChild(stepsLabel);
    var stepsList = document.createElement("div");
    stepsList.className = "flows-detail__steps";
    stepsWrap.appendChild(stepsList);

    var stepsHint = document.createElement("span");
    stepsHint.className = "flows-detail__field-hint";
    stepsHint.textContent =
      "Drag the handle to reorder. Tick optional for steps the flow can skip.";
    stepsWrap.appendChild(stepsHint);

    var addBtn = document.createElement("button");
    addBtn.type = "button";
    addBtn.className = "flows-detail__step-add";
    addBtn.textContent = "+ Add step";
    addBtn.addEventListener("click", function () {
      steps.push({
        agent_slug: "",
        optional: false,
        input_from: "",
        run_in_parallel_with: [],
      });
      renderSteps(stepsList, steps, addBtn);
    });
    stepsWrap.appendChild(addBtn);
    form.appendChild(stepsWrap);

    ensureAgents().then(function () {
      renderSteps(stepsList, steps, addBtn);
    });

    var err = document.createElement("p");
    err.className = "flows-detail__editor-err";
    err.hidden = true;
    form.appendChild(err);

    var actions = document.createElement("div");
    actions.className = "flows-detail__editor-actions";
    var cancel = btn(
      "Cancel",
      "flows-detail__btn-ghost",
      function () {
        draft = null;
        if (!flow.builtin && flow.slug) selectedSlug = flow.slug;
        renderRail();
        renderDetail();
      },
      "button",
    );
    var save = btn(
      draft ? "Create" : "Save",
      "flows-detail__btn-primary",
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
      var payload = {
        slug: form.querySelector('[name="slug"]').value.trim(),
        name: form.querySelector('[name="name"]').value.trim(),
        description: form.querySelector('[name="description"]').value.trim(),
        steps: steps.map(function (s) {
          return {
            agent_slug: s.agent_slug,
            optional: s.optional,
            input_from: s.input_from,
            run_in_parallel_with: s.run_in_parallel_with,
          };
        }),
      };
      var isCreate = !!draft;
      var req = isCreate
        ? fetch(Routes.flows.create(), {
            method: "POST",
            headers: jsonHeaders(),
            body: JSON.stringify(payload),
          })
        : fetch(
            Routes.flows.update().replace(
              "{slug}",
              encodeURIComponent(flow.slug),
            ),
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
          loadFlows({ force: true });
        })
        .catch(function (e2) {
          err.textContent = String(e2.message || e2);
          err.hidden = false;
          save.disabled = false;
        });
    });
    return form;
  }

  function renderSteps(container, steps, addBtn) {
    container.innerHTML = "";
    steps.forEach(function (step, i) {
      container.appendChild(renderStepRow(step, i, steps, container, addBtn));
    });
    // Enable DnD reorder if Sortable.js is available. Runs once
    // per list; Sortable ignores re-init safely.
    if (
      typeof window.Sortable === "function" &&
      !container.dataset.sortableBound
    ) {
      container.dataset.sortableBound = "true";
      window.Sortable.create(container, {
        handle: ".flows-detail__step-drag",
        animation: 120,
        ghostClass: "sortable-ghost",
        onEnd: function (evt) {
          if (evt.oldIndex === evt.newIndex) return;
          var item = steps.splice(evt.oldIndex, 1)[0];
          steps.splice(evt.newIndex, 0, item);
          // Re-render so the data-step-idx attributes stay in sync
          // with the visual order.
          renderSteps(container, steps, addBtn);
        },
      });
    }
  }

  function renderStepRow(step, i, all, container, addBtn) {
    var row = document.createElement("div");
    row.className = "flows-detail__step";
    row.setAttribute("data-step-idx", String(i));

    var drag = document.createElement("span");
    drag.className = "flows-detail__step-drag";
    drag.textContent = "⋮⋮";
    drag.title = "Drag to reorder";
    row.appendChild(drag);

    var idx = document.createElement("span");
    idx.className = "flows-detail__step-idx";
    idx.textContent = i + 1 + ".";
    row.appendChild(idx);

    var agentSelect = document.createElement("select");
    agentSelect.className = "flows-detail__step-agent";
    var placeholder = document.createElement("option");
    placeholder.value = "";
    placeholder.textContent = "(pick an agent)";
    agentSelect.appendChild(placeholder);
    agentsCache.forEach(function (a) {
      var o = document.createElement("option");
      o.value = a.slug;
      o.textContent = a.title + " (" + a.slug + ")";
      if (a.slug === step.agent_slug) o.selected = true;
      agentSelect.appendChild(o);
    });
    if (!step.agent_slug) placeholder.selected = true;
    agentSelect.addEventListener("change", function () {
      step.agent_slug = agentSelect.value;
    });
    row.appendChild(agentSelect);

    var optLabel = document.createElement("label");
    optLabel.className = "flows-detail__step-check";
    var optCb = document.createElement("input");
    optCb.type = "checkbox";
    optCb.checked = !!step.optional;
    optCb.addEventListener("change", function () {
      step.optional = optCb.checked;
    });
    optLabel.appendChild(optCb);
    var optSpan = document.createElement("span");
    optSpan.textContent = "optional";
    optLabel.appendChild(optSpan);
    row.appendChild(optLabel);

    var remove = document.createElement("button");
    remove.type = "button";
    remove.className = "flows-detail__step-remove";
    remove.textContent = "✕";
    remove.title = "Remove step";
    remove.addEventListener("click", function () {
      if (all.length <= 1) return;
      all.splice(i, 1);
      renderSteps(container, all, addBtn);
    });
    row.appendChild(remove);

    return row;
  }

  function startClone(flow) {
    draft = {
      slug: suggestCloneSlug(flow.slug),
      name: (flow.name || "") + " (copy)",
      description: flow.description || "",
      steps: (flow.steps || []).map(function (s) {
        return {
          agent_slug: s.agent_slug,
          optional: s.optional,
          input_from: s.input_from || "",
          run_in_parallel_with: (s.run_in_parallel_with || []).slice(),
        };
      }),
    };
    selectedSlug = null;
    renderRail();
    renderDetail();
  }

  function openNewEditor() {
    draft = {
      slug: "my-flow",
      name: "",
      description: "",
      steps: [{ agent_slug: "", optional: false }],
    };
    selectedSlug = null;
    renderRail();
    renderDetail();
  }

  function deleteFlow(slug) {
    var ok = true;
    if (typeof window.confirm === "function") {
      ok = window.confirm("Delete flow " + slug + "?");
    }
    if (!ok) return;
    fetch(Routes.flows.delete().replace("{slug}", encodeURIComponent(slug)), {
      method: "DELETE",
      headers: authHeaders(),
    })
      .then(function (r) {
        if (!r.ok && r.status !== 204) {
          return r.text().then(function (t) {
            throw new Error(t || "HTTP " + r.status);
          });
        }
      })
      .then(function () {
        selectedSlug = null;
        loadFlows({ force: true });
      })
      .catch(function (e) {
        if (typeof window.showAlert === "function") {
          window.showAlert("Delete failed: " + e.message);
        }
      });
  }

  function inputRow(label, name, value, opts) {
    opts = opts || {};
    var wrap = document.createElement("label");
    wrap.className = "flows-detail__field";
    var l = document.createElement("span");
    l.className = "flows-detail__field-label";
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
      h.className = "flows-detail__field-hint";
      h.textContent = opts.hint;
      wrap.appendChild(h);
    }
    return wrap;
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

  function bindSearch() {
    var s = document.getElementById("flows-rail-search");
    if (!s || s.dataset.bound === "true") return;
    s.dataset.bound = "true";
    s.addEventListener("input", function () {
      searchQuery = s.value || "";
      renderRail();
    });
  }

  function loadFlowsPublic(opts) {
    bindSearch();
    loadFlows(opts);
  }

  window.loadFlows = loadFlowsPublic;
  window.__flows_test = {
    renderRail: renderRail,
    renderDetail: renderDetail,
    startClone: startClone,
    openNewEditor: openNewEditor,
    buildChain: buildChain,
    groupParallel: groupParallel,
    suggestCloneSlug: suggestCloneSlug,
    _setState: function (state) {
      if (state.flows) flows = state.flows;
      if (state.agentsCache) agentsCache = state.agentsCache;
      if (state.selectedSlug !== undefined) selectedSlug = state.selectedSlug;
      if (state.draft !== undefined) draft = state.draft;
      if (state.searchQuery !== undefined) searchQuery = state.searchQuery;
    },
  };
})();
