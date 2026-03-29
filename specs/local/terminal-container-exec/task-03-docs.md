# Task 3: Documentation

**Status:** Done
**Depends on:** Task 2
**Phase:** Documentation
**Effort:** Small

## Goal

Update user-facing and internal documentation to cover container exec terminal sessions.

## What to do

1. Update `docs/internals/api-and-transport.md` — add the `container` field to the `create_session` message in the WebSocket protocol table. Add a note about container session behavior.

2. Update `CLAUDE.md` terminal section to mention container exec support.

3. Update `docs/guide/configuration.md` if any new env vars are added (unlikely).

4. Add a brief section to the appropriate user guide explaining:
   - How to open a container shell (click container button in terminal tab bar).
   - What containers are shown (running wallfacer task containers).
   - How container sessions differ from host shell sessions.

## Tests

- Run `make test` to verify nothing is broken.

## Boundaries

- Do NOT change code — documentation only.
- Do NOT document cloud deployment exec (K8s/remote Docker) — that's future work.
