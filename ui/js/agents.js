// Agents tab: renders the merged catalog of built-in + user-
// authored sub-agent roles. Clicking a row toggles an inline panel
// with the full prompt template body fetched lazily from
// /api/agents/{slug}. Built-in rows can be cloned into a new
// user-authored agent; user-authored rows can be edited inline or
// deleted.

(function () {
  "use strict";

  var loaded = false;

  // loadAgents fetches the list and renders the rows. Idempotent —
  // a successful load is cached so repeated tab switches don't
  // re-fetch. Pass {force: true} after a write to refresh.
  function loadAgents(opts) {
    if (loaded && !(opts && opts.force)) return;
    var listEl = document.getElementById("agents-list");
    if (!listEl) return;

    // Surface the workspace-default harness in the tab header so
    // "use workspace default" on individual agents has a concrete
    // resolution the user can see and jump to change.
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

  // renderRow produces one collapsed agent card. Built-in rows get
  // a Clone button; user-authored rows get Edit + Delete buttons.
  function renderRow(agent) {
    var card = document.createElement("div");
    card.className = "agents-row";
    card.setAttribute("data-slug", agent.slug);
    card.setAttribute("tabindex", "0");
    if (!agent.builtin) card.classList.add("agents-row--user");

    var header = document.createElement("div");
    header.className = "agents-row__header";

    var name = document.createElement("div");
    name.className = "agents-row__name";
    name.textContent = agent.title || agent.slug;

    var meta = document.createElement("div");
    meta.className = "agents-row__meta";
    var capLabel = capabilitiesLabel(agent.capabilities);
    var turnLabel = agent.multiturn ? "multi-turn" : "single-turn";
    var harness = agent.harness
      ? "harness: " + agent.harness
      : "harness: inherit";
    meta.textContent =
      (capLabel ? capLabel + " · " : "") + turnLabel + " · " + harness;

    var actions = document.createElement("div");
    actions.className = "agents-row__actions";
    if (agent.builtin) {
      var clone = buttonEl("Clone", "agents-row__clone", function (e) {
        e.stopPropagation();
        openEditor(card, agent, { mode: "clone" });
      });
      clone.title = "Clone this agent into a user-authored copy you can edit.";
      actions.appendChild(clone);
    } else {
      var edit = buttonEl("Edit", "agents-row__clone", function (e) {
        e.stopPropagation();
        openEditor(card, agent, { mode: "edit" });
      });
      var del = buttonEl("Delete", "agents-row__delete", function (e) {
        e.stopPropagation();
        deleteAgent(agent.slug);
      });
      actions.appendChild(edit);
      actions.appendChild(del);
    }

    header.appendChild(name);
    header.appendChild(meta);
    header.appendChild(actions);
    card.appendChild(header);
    if (agent.description) {
      var desc = document.createElement("p");
      desc.className = "agents-row__desc";
      desc.textContent = agent.description;
      card.appendChild(desc);
    }

    var body = document.createElement("div");
    body.className = "agents-row__body";
    body.hidden = true;
    card.appendChild(body);

    var onToggle = function () {
      if (card.classList.contains("agents-row--editing")) return;
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
      var url = Routes.agents.get().replace("{slug}", encodeURIComponent(slug));
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

  // openEditor renders an inline form under the row so the user
  // can fill in a new slug (clone mode) or tweak fields (edit
  // mode) without leaving the tab. Submit posts to /api/agents;
  // cancel restores the row to its summary state.
  function openEditor(card, agent, opts) {
    // Collapse the expanded panel so the editor isn't fighting it
    // for space.
    var body = card.querySelector(".agents-row__body");
    if (body) body.hidden = true;
    card.classList.remove("agents-row--open");
    card.classList.add("agents-row--editing");

    var existing = card.querySelector(".agents-row__editor");
    if (existing) existing.remove();

    var editor = document.createElement("form");
    editor.className = "agents-row__editor";
    editor.addEventListener("click", function (e) {
      e.stopPropagation();
    });

    var isClone = opts.mode === "clone";
    var slugValue = isClone ? suggestCloneSlug(agent.slug) : agent.slug;

    editor.appendChild(
      labeledInput("Slug", "slug", slugValue, {
        disabled: !isClone,
        hint: "kebab-case, 2-40 chars",
      }),
    );
    editor.appendChild(labeledInput("Title", "title", agent.title || ""));
    editor.appendChild(
      labeledInput("Description", "description", agent.description || ""),
    );
    editor.appendChild(
      labeledSelect(
        "Harness",
        "harness",
        agent.harness || "",
        [
          { value: "", label: "— use workspace default —" },
          { value: "claude", label: "Claude" },
          { value: "codex", label: "Codex" },
        ],
        "Pin this agent to a specific coding harness. Empty inherits from the task-level sandbox or WALLFACER_DEFAULT_SANDBOX.",
      ),
    );
    editor.appendChild(
      labeledCheckbox("Multi-turn", "multiturn", !!agent.multiturn),
    );
    editor.appendChild(
      labeledTextarea(
        "Capabilities (one per line)",
        "capabilities",
        (agent.capabilities || []).join("\n"),
      ),
    );
    // Inline system prompt body. Editing this writes a prompt_tmpl
    // field to the agent's YAML. For user-authored agents it
    // overrides any prompt_template_name lookup. Fetch the full
    // agent on edit so the current body is prefilled.
    editor.appendChild(
      labeledTextarea(
        "System prompt — optional",
        "prompt_tmpl",
        agent.prompt_tmpl || "",
        {
          rows: 8,
          hint:
            "Leave empty to inherit from the built-in prompt template. " +
            "Runtime use of custom prompt bodies ships in a follow-up; " +
            "today the field is persisted and displayed but the runner " +
            "still loads the named template for built-in agent slots.",
        },
      ),
    );

    var err = document.createElement("p");
    err.className = "agents-row__editor-err";
    err.hidden = true;
    editor.appendChild(err);

    var actions = document.createElement("div");
    actions.className = "agents-row__editor-actions";
    var submit = buttonEl(isClone ? "Save clone" : "Save", "agents-row__save");
    submit.type = "submit";
    var cancel = buttonEl("Cancel", "agents-row__cancel", function () {
      closeEditor(card);
    });
    cancel.type = "button";
    actions.appendChild(cancel);
    actions.appendChild(submit);
    editor.appendChild(actions);

    editor.addEventListener("submit", function (e) {
      e.preventDefault();
      err.hidden = true;
      var payload = readEditorPayload(editor);
      submit.disabled = true;
      var req = isClone
        ? fetch(Routes.agents.create(), {
            method: "POST",
            headers: jsonHeaders(),
            body: JSON.stringify(payload),
          })
        : fetch(
            Routes.agents
              .update()
              .replace("{slug}", encodeURIComponent(agent.slug)),
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
          loadAgents({ force: true });
        })
        .catch(function (e2) {
          err.textContent = String(e2.message || e2);
          err.hidden = false;
          submit.disabled = false;
        });
    });

    card.appendChild(editor);
    var first = editor.querySelector("input, textarea, select");
    if (first && typeof first.focus === "function") first.focus();
  }

  function closeEditor(card) {
    var editor = card.querySelector(".agents-row__editor");
    if (editor) editor.remove();
    card.classList.remove("agents-row--editing");
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
        loadAgents({ force: true });
      })
      .catch(function (err) {
        if (typeof window.showAlert === "function") {
          window.showAlert("Delete failed: " + err.message);
        }
      });
  }

  function readEditorPayload(editor) {
    var slug = editor.querySelector('[name="slug"]').value.trim();
    var title = editor.querySelector('[name="title"]').value.trim();
    var description = editor.querySelector('[name="description"]').value.trim();
    var harness = editor.querySelector('[name="harness"]').value;
    var multiturn = editor.querySelector('[name="multiturn"]').checked;
    var caps = editor
      .querySelector('[name="capabilities"]')
      .value.split("\n")
      .map(function (s) {
        return s.trim();
      })
      .filter(Boolean);
    var promptTmplEl = editor.querySelector('[name="prompt_tmpl"]');
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

  function suggestCloneSlug(base) {
    var suggestion = base + "-copy";
    // Cap at 40 chars; "-copy" is 5, so slice base to 35 for the
    // long-name fallback.
    return suggestion.length <= 40 ? suggestion : base.slice(0, 35) + "-copy";
  }

  // --- DOM helpers ---

  function buttonEl(label, cls, onClick) {
    var b = document.createElement("button");
    b.type = "button";
    b.className = cls;
    b.textContent = label;
    if (onClick) b.addEventListener("click", onClick);
    return b;
  }

  function labeledInput(label, name, value, opts) {
    opts = opts || {};
    var wrap = document.createElement("label");
    wrap.className = "agents-row__field";
    var l = document.createElement("span");
    l.className = "agents-row__field-label";
    l.textContent = label;
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

  function labeledTextarea(label, name, value, opts) {
    opts = opts || {};
    var wrap = document.createElement("label");
    wrap.className = "agents-row__field";
    var l = document.createElement("span");
    l.className = "agents-row__field-label";
    l.textContent = label;
    var t = document.createElement("textarea");
    t.name = name;
    t.rows = opts.rows || 3;
    t.value = value || "";
    if (opts.hint) t.title = opts.hint;
    wrap.appendChild(l);
    wrap.appendChild(t);
    if (opts.hint) {
      var h = document.createElement("span");
      h.className = "agents-row__field-hint";
      h.textContent = opts.hint;
      wrap.appendChild(h);
    }
    return wrap;
  }

  function labeledSelect(label, name, value, opts, hint) {
    var wrap = document.createElement("label");
    wrap.className = "agents-row__field";
    var l = document.createElement("span");
    l.className = "agents-row__field-label";
    l.textContent = label;
    var s = document.createElement("select");
    s.name = name;
    if (hint) s.title = hint;
    opts.forEach(function (opt) {
      var o = document.createElement("option");
      o.value = opt.value;
      o.textContent = opt.label;
      if (opt.value === value) o.selected = true;
      s.appendChild(o);
    });
    wrap.appendChild(l);
    wrap.appendChild(s);
    if (hint) {
      var h = document.createElement("span");
      h.className = "agents-row__field-hint";
      h.textContent = hint;
      wrap.appendChild(h);
    }
    return wrap;
  }

  function labeledCheckbox(label, name, checked) {
    var wrap = document.createElement("label");
    wrap.className = "agents-row__field agents-row__field--check";
    var i = document.createElement("input");
    i.type = "checkbox";
    i.name = name;
    i.checked = !!checked;
    var l = document.createElement("span");
    l.textContent = label;
    wrap.appendChild(i);
    wrap.appendChild(l);
    return wrap;
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

  // openNewEditor inserts a standalone editor card at the top of
  // the list so the user can author an agent from scratch. Submit
  // reuses the same POST path Clone uses; Cancel removes the card.
  function openNewEditor() {
    var listEl = document.getElementById("agents-list");
    if (!listEl) return;
    // Collapse any existing draft so there's only one open at a time.
    var existing = listEl.querySelector(".agents-row--draft");
    if (existing) existing.remove();

    var card = document.createElement("div");
    card.className = "agents-row agents-row--user agents-row--draft";
    card.setAttribute("data-slug", "(new)");
    var header = document.createElement("div");
    header.className = "agents-row__header";
    var name = document.createElement("div");
    name.className = "agents-row__name";
    name.textContent = "New agent";
    var meta = document.createElement("div");
    meta.className = "agents-row__meta";
    meta.textContent = "draft";
    header.appendChild(name);
    header.appendChild(meta);
    card.appendChild(header);
    listEl.insertBefore(card, listEl.firstChild);

    // Reuse the Clone editor with a blank role so the user fills in
    // every field. Mode "clone" keeps the slug input enabled.
    openEditor(
      card,
      {
        slug: "my-agent",
        title: "",
        description: "",
        harness: "",
        multiturn: false,
        capabilities: [],
      },
      { mode: "clone" },
    );
    // Scroll the draft into view so the user doesn't have to hunt
    // for the form when the list is long.
    if (typeof card.scrollIntoView === "function") {
      card.scrollIntoView({ behavior: "smooth", block: "start" });
    }
  }

  window.loadAgents = loadAgents;
  window.__agents_test = {
    renderRow: renderRow,
    expandAgent: expandAgent,
    openEditor: openEditor,
    closeEditor: closeEditor,
    readEditorPayload: readEditorPayload,
    suggestCloneSlug: suggestCloneSlug,
    openNewEditor: openNewEditor,
  };
})();
