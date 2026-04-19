// Flows tab: renders the merged catalog of built-in and user-
// authored flows. Each flow renders as a row with a one-line chip
// chain showing its step order, optional steps (trailing ?), and
// parallel-sibling groups (separated from the preceding step by a
// ‖ divider). Clicking a chip cross-navigates to the Agents tab
// with the corresponding agent expanded. Built-in flows expose a
// Clone action; user-authored flows expose Edit + Delete.

(function () {
  "use strict";

  var loaded = false;

  function loadFlows(opts) {
    if (loaded && !(opts && opts.force)) return;
    var listEl = document.getElementById("flows-list");
    if (!listEl) return;

    fetch(Routes.flows.list(), { headers: authHeaders() })
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

  // renderFlow produces one flow card with its chip chain plus
  // Clone / Edit / Delete actions depending on whether the flow
  // is built-in.
  function renderFlow(flow) {
    var card = document.createElement("div");
    card.className = "flows-row";
    card.setAttribute("data-slug", flow.slug);
    if (!flow.builtin) card.classList.add("flows-row--user");

    var header = document.createElement("div");
    header.className = "flows-row__header";

    var name = document.createElement("div");
    name.className = "flows-row__name";
    name.textContent = flow.name || flow.slug;

    var badge = document.createElement("span");
    badge.className = "flows-row__badge";
    badge.textContent = flow.builtin ? "built-in" : "custom";

    var actions = document.createElement("div");
    actions.className = "flows-row__actions";
    if (flow.builtin) {
      var clone = btn("Clone", "flows-row__clone", function (e) {
        e.stopPropagation();
        openEditor(card, flow, { mode: "clone" });
      });
      clone.title = "Clone this flow into a user-authored copy you can edit.";
      actions.appendChild(clone);
    } else {
      actions.appendChild(
        btn("Edit", "flows-row__clone", function (e) {
          e.stopPropagation();
          openEditor(card, flow, { mode: "edit" });
        }),
      );
      actions.appendChild(
        btn("Delete", "flows-row__delete", function (e) {
          e.stopPropagation();
          deleteFlow(flow.slug);
        }),
      );
    }

    header.appendChild(name);
    header.appendChild(badge);
    header.appendChild(actions);
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

  // openEditor renders an inline editor under the flow card.
  // Clone mode pre-fills from an existing flow with a new slug;
  // edit mode tweaks a user-authored flow in place. The step
  // editor is intentionally flat: a list of rows with per-step
  // agent dropdown, optional flag, and up/down/remove buttons.
  // The more elaborate DAG editor is future work.
  function openEditor(card, flow, opts) {
    var existing = card.querySelector(".flows-row__editor");
    if (existing) existing.remove();
    card.classList.add("flows-row--editing");

    var isClone = opts.mode === "clone";
    var form = document.createElement("form");
    form.className = "flows-row__editor";
    form.addEventListener("click", function (e) {
      e.stopPropagation();
    });

    var slugValue = isClone ? suggestCloneSlug(flow.slug) : flow.slug;

    form.appendChild(
      labeledInput("Slug", "slug", slugValue, {
        disabled: !isClone,
        hint: "kebab-case, 2-40 chars",
      }),
    );
    form.appendChild(labeledInput("Name", "name", flow.name || ""));
    form.appendChild(
      labeledInput("Description", "description", flow.description || ""),
    );

    // Load the current agents catalog so each step's agent-slug
    // dropdown has options. We stuff the promise on the form so the
    // submit handler can wait on it (tests mock fetch synchronously).
    var agentsPromise = fetch(Routes.agents.list(), { headers: authHeaders() })
      .then(function (r) {
        return r.ok ? r.json() : [];
      })
      .catch(function () {
        return [];
      });

    var stepsContainer = document.createElement("div");
    stepsContainer.className = "flows-row__steps";
    var initialSteps = (flow.steps || []).map(function (s) {
      return {
        agent_slug: s.agent_slug,
        optional: !!s.optional,
        input_from: s.input_from || "",
        run_in_parallel_with: (s.run_in_parallel_with || []).slice(),
      };
    });
    if (initialSteps.length === 0) {
      initialSteps.push({
        agent_slug: "",
        optional: false,
        input_from: "",
        run_in_parallel_with: [],
      });
    }
    renderSteps(stepsContainer, initialSteps, agentsPromise);
    form.appendChild(label("Steps", stepsContainer));

    var addBtn = btn("+ Add step", "flows-row__step-add", function () {
      initialSteps.push({
        agent_slug: "",
        optional: false,
        input_from: "",
        run_in_parallel_with: [],
      });
      renderSteps(stepsContainer, initialSteps, agentsPromise);
    });
    addBtn.type = "button";
    form.appendChild(addBtn);

    var err = document.createElement("p");
    err.className = "flows-row__editor-err";
    err.hidden = true;
    form.appendChild(err);

    var actions = document.createElement("div");
    actions.className = "flows-row__editor-actions";
    var save = btn(isClone ? "Save clone" : "Save", "flows-row__save");
    save.type = "submit";
    var cancel = btn("Cancel", "flows-row__cancel", function () {
      closeEditor(card);
    });
    cancel.type = "button";
    actions.appendChild(cancel);
    actions.appendChild(save);
    form.appendChild(actions);

    form.addEventListener("submit", function (e) {
      e.preventDefault();
      err.hidden = true;
      save.disabled = true;
      var payload = readFlowPayload(form, initialSteps);
      var req = isClone
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
        .then(function () {
          closeEditor(card);
          loadFlows({ force: true });
        })
        .catch(function (e2) {
          err.textContent = String(e2.message || e2);
          err.hidden = false;
          save.disabled = false;
        });
    });

    card.appendChild(form);
    var first = form.querySelector("input, select, textarea");
    if (first && typeof first.focus === "function") first.focus();
  }

  function closeEditor(card) {
    var e = card.querySelector(".flows-row__editor");
    if (e) e.remove();
    card.classList.remove("flows-row--editing");
  }

  function renderSteps(container, steps, agentsPromise) {
    container.innerHTML = "";
    steps.forEach(function (step, i) {
      container.appendChild(renderStep(step, i, steps, container, agentsPromise));
    });
  }

  function renderStep(step, i, all, container, agentsPromise) {
    var row = document.createElement("div");
    row.className = "flows-row__step";
    row.setAttribute("data-step-idx", String(i));

    var agentSelect = document.createElement("select");
    agentSelect.className = "flows-row__step-agent";
    // One placeholder option while the agents catalog loads.
    var placeholder = document.createElement("option");
    placeholder.value = step.agent_slug || "";
    placeholder.textContent = step.agent_slug || "(pick an agent)";
    agentSelect.appendChild(placeholder);
    agentsPromise.then(function (list) {
      agentSelect.innerHTML = "";
      (list || []).forEach(function (a) {
        var o = document.createElement("option");
        o.value = a.slug;
        o.textContent = a.title + " (" + a.slug + ")";
        if (a.slug === step.agent_slug) o.selected = true;
        agentSelect.appendChild(o);
      });
    });
    agentSelect.addEventListener("change", function () {
      step.agent_slug = agentSelect.value;
    });
    row.appendChild(agentSelect);

    var optionalWrap = document.createElement("label");
    optionalWrap.className = "flows-row__step-check";
    var optionalBox = document.createElement("input");
    optionalBox.type = "checkbox";
    optionalBox.checked = !!step.optional;
    optionalBox.addEventListener("change", function () {
      step.optional = optionalBox.checked;
    });
    optionalWrap.appendChild(optionalBox);
    var optLabel = document.createElement("span");
    optLabel.textContent = "optional";
    optionalWrap.appendChild(optLabel);
    row.appendChild(optionalWrap);

    var up = btn("↑", "flows-row__step-nav", function () {
      if (i === 0) return;
      var tmp = all[i - 1];
      all[i - 1] = all[i];
      all[i] = tmp;
      renderSteps(container, all, agentsPromise);
    });
    up.type = "button";
    up.disabled = i === 0;
    row.appendChild(up);

    var down = btn("↓", "flows-row__step-nav", function () {
      if (i >= all.length - 1) return;
      var tmp = all[i + 1];
      all[i + 1] = all[i];
      all[i] = tmp;
      renderSteps(container, all, agentsPromise);
    });
    down.type = "button";
    down.disabled = i >= all.length - 1;
    row.appendChild(down);

    var remove = btn("✕", "flows-row__step-remove", function () {
      if (all.length <= 1) return;
      all.splice(i, 1);
      renderSteps(container, all, agentsPromise);
    });
    remove.type = "button";
    row.appendChild(remove);

    return row;
  }

  function readFlowPayload(form, steps) {
    return {
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
        loadFlows({ force: true });
      })
      .catch(function (e) {
        if (typeof window.showAlert === "function") {
          window.showAlert("Delete failed: " + e.message);
        }
      });
  }

  function suggestCloneSlug(base) {
    var suggestion = base + "-copy";
    return suggestion.length <= 40 ? suggestion : base.slice(0, 35) + "-copy";
  }

  // --- DOM helpers ---

  function btn(text, cls, onClick) {
    var b = document.createElement("button");
    b.type = "button";
    b.className = cls;
    b.textContent = text;
    if (onClick) b.addEventListener("click", onClick);
    return b;
  }

  function labeledInput(lbl, name, value, opts) {
    opts = opts || {};
    var wrap = document.createElement("label");
    wrap.className = "flows-row__field";
    var l = document.createElement("span");
    l.className = "flows-row__field-label";
    l.textContent = lbl;
    var i = document.createElement("input");
    i.type = "text";
    i.name = name;
    i.value = value || "";
    if (opts.disabled) i.disabled = true;
    if (opts.hint) i.title = opts.hint;
    wrap.appendChild(l);
    wrap.appendChild(i);
    return wrap;
  }

  function label(text, child) {
    var wrap = document.createElement("label");
    wrap.className = "flows-row__field";
    var l = document.createElement("span");
    l.className = "flows-row__field-label";
    l.textContent = text;
    wrap.appendChild(l);
    wrap.appendChild(child);
    return wrap;
  }

  function jsonHeaders() {
    var h = authHeaders();
    h["Content-Type"] = "application/json";
    return h;
  }

  window.loadFlows = loadFlows;
  window.__flows_test = {
    renderFlow: renderFlow,
    buildChain: buildChain,
    groupParallel: groupParallel,
    openEditor: openEditor,
    closeEditor: closeEditor,
    readFlowPayload: readFlowPayload,
    suggestCloneSlug: suggestCloneSlug,
  };
})();
