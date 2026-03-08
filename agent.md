## Commit + push strategy

- Keep commits small and scoped to one change.
- Avoid mixing unrelated file edits (especially generated artifacts like coverage files).
- Use the project’s commit style: `<scope>: <description>` (lowercase, concise).
- Stage only intended files and commit once per logical change.
- Push once after the commit is created (no intermediate pushes).
- If additional follow-up changes are needed, make a new small commit after the first push.

## Quick sequence (this change)

1. Stage changes:
   - `git add internal/runner/oversight.go internal/runner/oversight_test.go`
2. Commit:
   - `git commit -m "internal/runner: broaden codex oversight tool parsing"`
3. Push once:
   - `git push origin main`

## Reference for this request

- Commit created: `cfedb3f`
- Branch: `main`
- Remote pushed: `origin/main`
