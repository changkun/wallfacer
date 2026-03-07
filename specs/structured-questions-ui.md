# Plan: Structured Question UI for Waiting Tasks

**Status:** Draft
**Date:** 2026-02-22

---

## Problem

When Claude Code calls `AskUserQuestion` inside a container, wallfacer correctly moves the task
to `waiting` — but the structured question data (options, headers, multi-select flags) is
invisible. The modal shows a plain textarea and the user must type a free-form response, even
though Claude provided specific options to choose from.

---

## How Claude Code Communicates Questions

Claude Code with `--output-format stream-json` emits NDJSON. When it calls `AskUserQuestion`
the stream contains an `assistant` event carrying a `tool_use` content block:

```json
{
  "type": "assistant",
  "message": {
    "content": [
      {
        "type": "tool_use",
        "id": "toolu_abc",
        "name": "AskUserQuestion",
        "input": {
          "questions": [
            {
              "question": "Which approach should we use?",
              "header": "Approach",
              "options": [
                { "label": "Option A", "description": "Uses X strategy" },
                { "label": "Option B", "description": "Uses Y strategy" }
              ],
              "multiSelect": false
            }
          ]
        }
      }
    ]
  }
}
```

The session then ends with `stop_reason: ""`. Wallfacer already handles this via the
`default` branch in `runner/execute.go:177`.

The full NDJSON is available as `rawStdout []byte` at `runner/execute.go:93` and is saved
to disk at `runner/execute.go:94` via `store.SaveTurnOutput`. It is already parsed for the
last `claudeOutput` by `parseOutput` but the intermediate `AskUserQuestion` events are
currently discarded.

When the user submits feedback, `handler/execute.go:53` calls `runner.Run(id, message,
sessionID, true)`. Claude receives the message as a plain user turn and interprets it as
the answer, since the conversation context already contains the question.

---

## Approach

Parse `rawStdout` for `AskUserQuestion` tool calls before transitioning to `waiting`.
Store the extracted questions on the `Task` struct. The UI reads `pending_questions` from
the task and renders radio/checkbox groups instead of the plain textarea. The formatted
answer is submitted through the existing `/api/tasks/{id}/feedback` endpoint unchanged.

No new API endpoints. No new dependencies. Feedback API contract unchanged.

---

## Changes

### 1. `internal/store/models.go` — Add types and Task field

```go
// AskUserQuestionOption is a single selectable option in a structured question.
type AskUserQuestionOption struct {
    Label       string `json:"label"`
    Description string `json:"description,omitempty"`
}

// AskUserQuestionItem is one question block from the AskUserQuestion tool call.
type AskUserQuestionItem struct {
    Question    string                  `json:"question"`
    Header      string                  `json:"header,omitempty"`
    Options     []AskUserQuestionOption `json:"options"`
    MultiSelect bool                    `json:"multi_select"`
}
```

Add to `Task` struct after `UpdatedAt`:

```go
PendingQuestions []AskUserQuestionItem `json:"pending_questions,omitempty"`
```

The `omitempty` tag means existing task JSON files without this field deserialise cleanly,
and the field is omitted from the API response when nil.

---

### 2. `internal/store/tasks.go` — Add `SetPendingQuestions`

Follow the existing method pattern (lock → mutate → `saveTask` → `notify`):

```go
// SetPendingQuestions stores (or clears, when nil) structured questions on a task.
func (s *Store) SetPendingQuestions(_ context.Context, id uuid.UUID, questions []AskUserQuestionItem) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    t, ok := s.tasks[id]
    if !ok {
        return fmt.Errorf("task not found: %s", id)
    }
    t.PendingQuestions = questions
    t.UpdatedAt = time.Now()
    if err := s.saveTask(id, t); err != nil {
        return err
    }
    s.notify()
    return nil
}
```

Pass `nil` to clear. No separate `ClearPendingQuestions` method is needed.

---

### 3. `internal/runner/container.go` — Add `parseAskUserQuestions`

Add a pure helper function that scans NDJSON bytes for `AskUserQuestion` tool inputs.
Placed in `container.go` alongside `parseOutput`.

```go
// parseAskUserQuestions scans raw NDJSON stdout and returns any AskUserQuestion
// tool inputs found in assistant message content blocks.
func parseAskUserQuestions(ndjson []byte) []store.AskUserQuestionItem {
    var result []store.AskUserQuestionItem
    for _, line := range bytes.Split(ndjson, []byte("\n")) {
        line = bytes.TrimSpace(line)
        if len(line) == 0 || line[0] != '{' {
            continue
        }
        var event struct {
            Type    string `json:"type"`
            Message struct {
                Content []struct {
                    Type  string          `json:"type"`
                    Name  string          `json:"name"`
                    Input json.RawMessage `json:"input"`
                } `json:"content"`
            } `json:"message"`
        }
        if err := json.Unmarshal(line, &event); err != nil || event.Type != "assistant" {
            continue
        }
        for _, block := range event.Message.Content {
            if block.Type != "tool_use" || block.Name != "AskUserQuestion" {
                continue
            }
            var input struct {
                Questions []store.AskUserQuestionItem `json:"questions"`
            }
            if err := json.Unmarshal(block.Input, &input); err == nil {
                result = append(result, input.Questions...)
            }
        }
    }
    return result
}
```

Note: the `MultiSelect` JSON key from Claude Code is `"multiSelect"` (camelCase). The struct
tag on `AskUserQuestionItem.MultiSelect` must be `json:"multiSelect"` when deserialising from
the NDJSON, but `json:"multi_select"` when serialising to the API/task JSON. Handle this by
using two structs or a custom `UnmarshalJSON`, or by accepting both tags with `json:"multiSelect"`
and using a separate field alias during store serialisation. **Simplest fix:** use
`json:"multiSelect"` on the struct field and rename to `multi_select` only in the JS layer
(`task.pending_questions[i].multiSelect`).

---

### 4. `internal/runner/execute.go` — Call parser before `waiting` transition

In the `default` case (line 177), before `UpdateTaskStatus`:

```go
default:
    // Empty or unknown stop_reason — waiting for user feedback.
    if cur, _ := r.store.GetTask(bgCtx, taskID); cur != nil && cur.Status == "cancelled" {
        statusSet = true
        return
    }
    // Extract structured questions from the NDJSON stream, if any.
    if questions := parseAskUserQuestions(rawStdout); len(questions) > 0 {
        if err := r.store.SetPendingQuestions(bgCtx, taskID, questions); err != nil {
            logger.Runner.Warn("set pending questions", "task", taskID, "error", err)
        }
    }
    statusSet = true
    r.store.UpdateTaskStatus(bgCtx, taskID, "waiting")
    r.store.InsertEvent(bgCtx, taskID, "state_change", map[string]string{
        "from": "in_progress",
        "to":   "waiting",
    })
    return
```

`rawStdout` is already in scope from line 93.

---

### 5. `internal/handler/execute.go` — Clear questions on feedback

In `SubmitFeedback`, after `UpdateTaskStatus` (line 36), clear the questions so stale
options don't show up after the answer is submitted:

```go
if err := h.store.UpdateTaskStatus(r.Context(), id, "in_progress"); err != nil {
    http.Error(w, err.Error(), http.StatusInternalServerError)
    return
}
// Clear structured questions now that user has answered.
h.store.SetPendingQuestions(r.Context(), id, nil)
```

---

### 6. `ui/index.html` — Add questions section inside feedback section

Replace the current `modal-feedback-section` block (lines 347–354) with a version that
includes both a structured questions panel and the existing plain textarea, toggled by JS:

```html
<!-- Feedback form for waiting tasks -->
<div id="modal-feedback-section" class="hidden mb-4">
  <h3 class="section-title">Provide Feedback</h3>

  <!-- Structured question UI (shown when task.pending_questions is set) -->
  <div id="modal-questions" class="hidden space-y-4 mb-3"></div>

  <!-- Plain textarea fallback (shown when no pending_questions) -->
  <textarea id="modal-feedback" rows="3" placeholder="Type your response..." class="field"></textarea>

  <div class="flex items-center gap-2 mt-2">
    <button id="modal-submit-answers" class="btn btn-yellow hidden"
            onclick="submitStructuredAnswers()">Confirm Answers</button>
    <button id="modal-submit-feedback" class="btn btn-yellow"
            onclick="submitFeedback()">Submit Feedback</button>
    <button onclick="completeTask()" class="btn btn-green">Mark as Done</button>
  </div>
</div>
```

---

### 7. `ui/js/modal.js` — Render questions and toggle visibility

**In `openModal`**, replace line 227:

```js
// Before (line 227):
feedbackSection.classList.toggle('hidden', task.status !== 'waiting');

// After:
const isWaiting = task.status === 'waiting';
feedbackSection.classList.toggle('hidden', !isWaiting);
if (isWaiting) {
  const questions = task.pending_questions;
  const hasQ = questions && questions.length > 0;
  document.getElementById('modal-questions').classList.toggle('hidden', !hasQ);
  document.getElementById('modal-feedback').classList.toggle('hidden', hasQ);
  document.getElementById('modal-submit-answers').classList.toggle('hidden', !hasQ);
  document.getElementById('modal-submit-feedback').classList.toggle('hidden', hasQ);
  if (hasQ) renderPendingQuestions(questions);
}
```

**New function `renderPendingQuestions`:**

```js
function renderPendingQuestions(questions) {
  const container = document.getElementById('modal-questions');
  container.innerHTML = '';
  questions.forEach(function(q, qi) {
    const section = document.createElement('div');
    section.className = 'mb-3';

    const labelEl = document.createElement('p');
    labelEl.className = 'text-sm font-medium mb-1';
    if (q.header) {
      const chip = document.createElement('span');
      chip.className = 'text-xs rounded px-1 mr-1.5';
      chip.style.cssText = 'background:var(--surface-2);color:var(--text-muted)';
      chip.textContent = q.header;
      labelEl.appendChild(chip);
    }
    labelEl.appendChild(document.createTextNode(q.question));
    section.appendChild(labelEl);

    (q.options || []).forEach(function(opt) {
      const row = document.createElement('label');
      row.className = 'flex items-start gap-2 mb-1 cursor-pointer text-sm';

      const input = document.createElement('input');
      // Note: Claude Code uses camelCase "multiSelect" in its tool input
      input.type = (q.multiSelect || q.multi_select) ? 'checkbox' : 'radio';
      input.name = 'q-' + qi;
      input.value = opt.label;
      input.dataset.qi = qi;
      input.className = 'mt-0.5 flex-shrink-0';

      const text = document.createElement('span');
      text.innerHTML = '<strong>' + escapeHtml(opt.label) + '</strong>';
      if (opt.description) {
        text.innerHTML += ' <span style="color:var(--text-muted)">\u2014 ' + escapeHtml(opt.description) + '</span>';
      }
      row.appendChild(input);
      row.appendChild(text);
      section.appendChild(row);
    });

    container.appendChild(section);
  });
}
```

**Also update `extractToolInput` in `modal.js`** to handle `AskUserQuestion` in the pretty
log renderer (line 477, the `default` branch):

```js
case 'AskUserQuestion': {
  const qs = inputObj.questions;
  return qs && qs.length ? qs.map(q => q.question).join(' / ').slice(0, 120) : '';
}
```

---

### 8. `ui/js/tasks.js` — Add `submitStructuredAnswers`

Add alongside `submitFeedback` (after line 96):

```js
async function submitStructuredAnswers() {
  const task = tasks.find(t => t.id === currentTaskId);
  const questions = (task && task.pending_questions) || [];
  const lines = [];

  questions.forEach(function(q, qi) {
    lines.push('Answer to "' + q.question + '":');
    const inputs = document.querySelectorAll('input[name="q-' + qi + '"]');
    inputs.forEach(function(inp) {
      const marker = inp.checked ? '\u2713' : '\u2013';
      const opt = (q.options || []).find(function(o) { return o.label === inp.value; });
      const desc = opt && opt.description ? ' \u2014 ' + opt.description : '';
      lines.push('  ' + marker + ' ' + inp.value + desc);
    });
  });

  const anyChecked = !!document.querySelector('[data-qi]:checked');
  if (!anyChecked) {
    showAlert('Please select at least one option.');
    return;
  }

  try {
    await api('/api/tasks/' + currentTaskId + '/feedback', {
      method: 'POST',
      body: JSON.stringify({ message: lines.join('\n') }),
    });
    closeModal();
    fetchTasks();
  } catch (e) {
    showAlert('Error submitting answers: ' + e.message);
  }
}
```

---

### 9. `ui/js/render.js` — Show question badge on waiting cards

In `updateCard` (line 185), inside the top badge row in the template, add a question
indicator when `pending_questions` is set. In `buildCardActions` (line 152), the existing
"Mark done" button stays unchanged.

Specifically, in the `updateCard` template's header `<div>`, after the spinner:

```js
${(t.status === 'waiting' && t.pending_questions && t.pending_questions.length)
  ? '<span class="badge" style="background:var(--warning-bg,#fef3c7);color:var(--warning-text,#92400e);font-size:9px;">? ' + t.pending_questions.length + (t.pending_questions.length === 1 ? ' question' : ' questions') + '</span>'
  : ''}
```

---

## Data Flow Summary

```
runContainer() returns rawStdout (full NDJSON)
          │
          ▼  parseAskUserQuestions(rawStdout)    [runner/container.go]
    AskUserQuestion found?
          ├── YES → store.SetPendingQuestions(taskID, questions)
          │         task.pending_questions = [...]
          │
          └── NO  → (omitted from task JSON)
          │
          ▼  store.UpdateTaskStatus("waiting")   [runner/execute.go:184]

GET /api/tasks  →  task JSON includes pending_questions if set

openModal(id)                                   [modal.js]
    task.pending_questions?
          ├── YES → renderPendingQuestions()
          │         show radio/checkbox groups
          │         "Confirm Answers" button visible
          │         plain textarea + "Submit Feedback" hidden
          │
          └── NO  → plain textarea + "Submit Feedback" visible

submitStructuredAnswers()                       [tasks.js]
    → formats selected options as plain text
    → POST /api/tasks/{id}/feedback { message }

SubmitFeedback handler                          [handler/execute.go]
    → store.UpdateTaskStatus("in_progress")
    → store.SetPendingQuestions(id, nil)        ← clears questions
    → go runner.Run(id, message, sessionID, true)
```

---

## JSON field name note

Claude Code emits `"multiSelect"` (camelCase). Go's `json` package and the store use
`"multi_select"` (snake_case) for consistency with the rest of the API. The JS layer must
handle both when reading from task JSON (`multi_select`) vs. reading from raw NDJSON in logs
(`multiSelect`). The `renderPendingQuestions` function already guards both with
`q.multiSelect || q.multi_select`.

**Decision:** Store `AskUserQuestionItem` in Go with `json:"multiSelect"` so that
`parseAskUserQuestions` can unmarshal Claude Code's output directly into the same struct,
and the field serialises to the task JSON and API response as `"multiSelect"`. The JS
accesses it as `q.multiSelect` consistently. Update the `renderPendingQuestions` example
above to use only `q.multiSelect`.

---

## Edge Cases

| Scenario | Handling |
|---|---|
| Multiple questions in one `AskUserQuestion` call | Each rendered as its own block |
| `multiSelect: true` | `<input type="checkbox">` instead of radio |
| No option selected | Validate before submit; `showAlert` |
| Task resumed without questions (plain `waiting`) | `pending_questions` null/absent → textarea shown |
| Task crashes during waiting → retry | `ResetTaskForRetry` resets `PendingQuestions` to nil automatically (field not set) |
| Server restart | Questions persist in `task.json`; loaded on startup via existing store load |
| Multiple AskUserQuestion turns | Each turn overwrites questions via `SetPendingQuestions`; cleared on feedback |

---

## Files Changed

| File | Change |
|---|---|
| `internal/store/models.go` | Add `AskUserQuestionOption`, `AskUserQuestionItem` types; add `PendingQuestions` field to `Task` |
| `internal/store/tasks.go` | Add `SetPendingQuestions` method |
| `internal/runner/container.go` | Add `parseAskUserQuestions` helper |
| `internal/runner/execute.go` | Call `parseAskUserQuestions` + `SetPendingQuestions` in `default` case before `waiting` transition |
| `internal/handler/execute.go` | Call `SetPendingQuestions(nil)` in `SubmitFeedback` after status update |
| `ui/index.html` | Add `#modal-questions`, `#modal-submit-answers` inside feedback section; give plain buttons IDs |
| `ui/js/modal.js` | Update `openModal` visibility logic; add `renderPendingQuestions`; add `AskUserQuestion` case in `extractToolInput` |
| `ui/js/tasks.js` | Add `submitStructuredAnswers` |
| `ui/js/render.js` | Add question badge in `updateCard` template |

---

## Testing

1. **Unit test `parseAskUserQuestions`** in `runner/container_test.go` (or a new
   `runner/questions_test.go`): fixture NDJSON with single question, multi-question,
   multi-select, and no `AskUserQuestion` present.
2. **`store.SetPendingQuestions` round-trip test**: set → read back → assert equal; set nil
   → read back → assert nil.
3. **Manual UI test**: run a task with a prompt that causes Claude to call `AskUserQuestion`;
   verify the waiting card shows the badge and the modal renders radio/checkbox options;
   select an answer; verify task moves to `in_progress` with the formatted message logged in
   the event trace.
