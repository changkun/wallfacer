// Package agents is the authoritative catalog of sub-agent role
// descriptors. A Role names what an agent is, what prompt template
// it renders, and what capabilities it needs — but not how the
// runner dispatches it. Runner-side plumbing (mount profile, parse
// function, sandbox-routing activity bucket) lives behind a
// slug-keyed binding table in internal/runner so the descriptors
// stay neutral enough for the Agents tab and the Flow composer to
// consume without pulling in container orchestration knowledge.
package agents
