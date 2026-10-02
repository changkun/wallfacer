// Package agents is the catalog of the built-in sub-agent roles: impl, test,
// title, oversight and commit-msg. A Role names what an agent is, what prompt
// template it renders, and what capabilities it needs, but not how the runner
// dispatches it. Runner-side plumbing (mount profile, parse function,
// sandbox-routing activity bucket) lives behind a slug-keyed binding table in
// internal/runner, so the descriptors stay free of container orchestration
// knowledge.
package agents
