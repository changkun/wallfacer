---
title: Server-side /spec-new directive parser and scaffold interception
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/chat-first-mode/spec-scaffold-library.md
  - specs/local/spec-coordination/spec-planning-ux/chat-first-mode/agent-system-prompts.md
affects:
  - internal/handler/planning.go
  - internal/handler/planning_directive.go
effort: large
created: 2026-04-12
updated: 2026-04-12
author: changkun
dispatched_task_id: null
---

# Server-side /spec-new directive parser and scaffold interception

## Goal

Parse the agent's NDJSON output stream for `/spec-new <path> [title=...] [status=...] [effort=...]` directives. On first match, call `spec.Scaffold` to create the file with valid frontmatter, then capture the agent's subsequent body content and append it to the scaffolded file. Fenced code blocks shield their contents from parsing so the agent can quote the grammar without double-scaffolding.

## What to do

1. Create `internal/handler/planning_directive.go` with a streaming parser:
   ```go
   type DirectiveScanner struct {
       inFence   bool  // true inside ``` ... ``` blocks
       directives []Directive
       // capture body lines belonging to the current directive
   }
   type Directive struct {
       Path   string
       Title  string
       Status spec.Status
       Effort spec.Effort
       Body   string  // lines after this directive, up to the next or EOT
   }
   // ScanLine processes one assistant text-content line at a time.
   func (s *DirectiveScanner) ScanLine(line string)
   func (s *DirectiveScanner) Directives() []Directive
   ```
   The scanner:
   - Tracks fence state: a line whose trimmed content starts with ` ``` ` toggles `inFence`.
   - Recognises `/spec-new` only when the line's first non-whitespace content is `/spec-new` AND `!inFence`.
   - Parses directive args using a simple key=value tokenizer that handles quoted values (`title="Auth Refactor"`).
   - Lines following a recognised directive and preceding either EOT or the next directive (outside fences) are appended to that directive's `Body`. Lines inside fences that appear in the body are preserved verbatim.
2. In `internal/handler/planning.go:SendPlanningMessage`, extend the NDJSON processing that runs after `handle.Wait()`:
   - Iterate the `assistant` type stream-json entries, split each content block's `text` field into lines, feed them to the `DirectiveScanner`.
   - After the stream ends, for each scanner-produced directive:
     a. Call `spec.Scaffold({Path, Title, Status, Effort, Author: resolveAuthor()})`. On error, emit a system-event into the chat (`planner.Message{Role: "system", Content: "Couldn't create <path>: <err>"}`), skip this directive, continue with the next.
     b. On success, append the directive's `Body` to the scaffolded file as the content below the frontmatter — open the file, write after the closing `---` separator.
     c. Record the directive's successful scaffolding on the thread meta (optional: a slice of bootstrapped paths on the thread) for UI auto-focus.
3. Directive scanning must NOT block the stream — it runs alongside the existing tee into the live log. The scaffold call happens post-stream so the agent's live output still flows unchanged into the chat pane. The UI-visible result: user sees the agent typing a directive line + body content in chat, the spec-tree SSE fires just after the turn ends, layout transitions and focused view populates with the captured body content.
4. When the scanner recognises a directive but `spec.Scaffold` later errors (name collision, invalid path), the agent's body content still lands in chat as normal assistant text — the directive was merely a request, not a guarantee.

## Tests

- `internal/handler/planning_directive_test.go` (new):
  - `TestDirectiveScanner_Simple`: `/spec-new specs/local/foo.md title="Foo"` + body lines → one directive with the title and body.
  - `TestDirectiveScanner_NoDirective`: plain chat lines → zero directives.
  - `TestDirectiveScanner_FencedDirectiveIgnored`: ` ```\n/spec-new specs/local/bar.md\n``` ` → zero directives; the fenced line is treated as body-content of any enclosing directive (or discarded if none).
  - `TestDirectiveScanner_MultipleDirectives`: two `/spec-new` lines in one turn → two directives, each with body content up to the next directive.
  - `TestDirectiveScanner_ImbalancedFence`: unclosed fence — remaining lines are treated as inside-fence; no spurious directives recognised.
  - `TestDirectiveScanner_ArgParsing`: `title="quoted with spaces"`, `status=drafted`, `effort=medium` — all parsed correctly; unknown keys silently ignored (forward-compat).
  - `TestDirectiveScanner_BodyIncludesMarkdown`: body content can include code fences, lists, etc.; reproduced byte-for-byte.
- `internal/handler/planning_test.go` (extend):
  - `TestSendPlanningMessage_NoDirective_NoFilesCreated`: simulated stream with plain chat → no `specs/**/*.md` files appear.
  - `TestSendPlanningMessage_WithDirective_ScaffoldsAndAppends`: simulated stream with `/spec-new` + body → scaffolded file exists with valid frontmatter and the body is appended after the frontmatter block.
  - `TestSendPlanningMessage_MultipleDirectives`: two directives, both scaffolded independently.
  - `TestSendPlanningMessage_ScaffoldError_SystemBubble`: `/spec-new` with a path that collides with an existing file → a `system` role message appears in the thread's history explaining the error; agent's body content still appears as an assistant message.
  - `TestSendPlanningMessage_StreamUnchanged`: the live log buffer and streamed output to `/api/planning/messages/stream` contain the raw agent response including the directive line — the directive is not stripped from the user-visible stream.

## Boundaries

- **Do NOT** implement the `/create <title>` slash-command expansion here — that's `create-command-expansion.md`.
- **Do NOT** create `specs/README.md` on first scaffold — that's `readme-autocreate.md`.
- **Do NOT** strip the directive line from the agent's live stream output. The UX is that the user sees the agent's raw response; server-side action (scaffold) happens in parallel.
- **Do NOT** try to parse directives from the user's own messages. Only agent-authored `assistant` blocks are scanned.
- **Do NOT** add a "discard directive" undo command. Archival of an unwanted spec is the recovery path per the parent spec's non-goals.
