# Changelog

Every tag has a section here, and the section is the body of the GitHub
release. A tag without one is refused at the pre-push and fails the release
workflow. Write under `Unreleased` as work lands; `lateregate release vX.Y.Z`
turns that into the tag's section, commits, tags and pushes.

A section says what changed for whoever uses the release, not what was
committed: the commit log already holds that.

## Unreleased

- Signing in requests no audience any more: the session token belongs to
  the identity provider, and the coordination connector presents a
  five-minute actor token minted for wallfacer instead of the login token.
  `AUTH_AUDIENCE` now names only what the API verifies. Anyone signed in
  before this release signs in once more.
