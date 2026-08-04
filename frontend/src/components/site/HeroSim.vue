<script setup lang="ts">
// Self-playing product simulation for the marketing hero: a stylized board
// where task cards travel Backlog -> In Progress -> Done while the agent
// pipeline on the right pulses through implement -> test -> fan-out.
//
// All motion is CSS keyframes on a shared 20s cycle (two cards run the same
// journey with a -10s phase offset so something is always moving). No JS
// animation loop: the markup is static, so vite-ssg prerenders a complete
// scene and prefers-reduced-motion simply freezes it.
</script>

<template>
  <div class="hero-sim" role="img" aria-label="Animated preview of the Wallfacer task board driving an agent pipeline">
    <div class="hs-chrome">
      <span class="hs-dot" /><span class="hs-dot" /><span class="hs-dot" />
      <span class="hs-title">wallfacer — autonomous board</span>
      <span class="hs-live"><span class="hs-live-dot" />live</span>
    </div>
    <div class="hs-body">
      <!-- Mini board -->
      <div class="hs-board">
        <div class="hs-col">
          <div class="hs-col-head"><span class="hs-chip hs-chip--backlog" />BACKLOG</div>
        </div>
        <div class="hs-col">
          <div class="hs-col-head"><span class="hs-chip hs-chip--progress" />IN PROGRESS</div>
        </div>
        <div class="hs-col">
          <div class="hs-col-head"><span class="hs-chip hs-chip--done" />DONE</div>
        </div>

        <!-- Traveling cards (positioned on the board plane) -->
        <div class="hs-card hs-card--a">
          <span class="hs-card-line hs-card-line--title" />
          <span class="hs-card-line" style="width: 82%" />
          <span class="hs-card-tags"><i class="hs-tag hs-tag--indigo" /><i class="hs-tag" /></span>
          <span class="hs-card-badge">✓</span>
        </div>
        <div class="hs-card hs-card--b">
          <span class="hs-card-line hs-card-line--title" style="width: 58%" />
          <span class="hs-card-line" style="width: 74%" />
          <span class="hs-card-tags"><i class="hs-tag hs-tag--green" /><i class="hs-tag" /></span>
          <span class="hs-card-badge">✓</span>
        </div>
        <div class="hs-card hs-card--static hs-card--s1">
          <span class="hs-card-line hs-card-line--title" style="width: 64%" />
          <span class="hs-card-line" style="width: 78%" />
          <span class="hs-card-tags"><i class="hs-tag hs-tag--amber" /><i class="hs-tag" /></span>
        </div>
      </div>

      <!-- Agent pipeline -->
      <div class="hs-graph">
        <!-- Edges live in the same coordinate space as the nodes: a 0-100 grid
             stretched to fill the panel (preserveAspectRatio="none"), so an
             endpoint at x/y matches a node positioned at left/top x%/y%.
             vector-effect keeps stroke + dash constant in screen px regardless
             of the non-uniform scale, so the connectors stay hairline. -->
        <svg class="hs-edges" viewBox="0 0 100 100" preserveAspectRatio="none" fill="none" aria-hidden="true">
          <path class="hs-edge hs-edge-1" d="M15 44 H35" />
          <path class="hs-edge hs-edge-2" d="M54 44 H70" />
          <path class="hs-edge hs-edge-3" d="M82 44 C 82 30, 83 20, 83 12" />
          <path class="hs-edge hs-edge-3" d="M82 44 H84" />
          <path class="hs-edge hs-edge-3" d="M82 44 C 82 58, 83 68, 83 76" />
        </svg>
        <div class="hs-node hs-node--task" style="left: 4%; top: 44%">task</div>
        <div class="hs-node hs-node--impl" style="left: 36%; top: 44%">implement</div>
        <div class="hs-node hs-node--test" style="left: 72%; top: 44%">test</div>
        <div class="hs-node hs-node--fan hs-node--fan1" style="right: 3%; top: 12%">commit</div>
        <div class="hs-node hs-node--fan hs-node--fan2" style="right: 3%; top: 44%">title</div>
        <div class="hs-node hs-node--fan hs-node--fan3" style="right: 3%; top: 76%">oversight</div>

        <!-- Cost ticker: a CSS steps() tape, no JS -->
        <div class="hs-ticker" aria-hidden="true">
          <span class="hs-ticker-label">2 turns ·</span>
          <span class="hs-ticker-window"><span class="hs-ticker-tape"><span>$0.18</span><span>$0.34</span><span>$0.57</span><span>$0.72</span><span>$0.91</span><span>$0.18</span></span></span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.hero-sim {
  border: 1px solid var(--rule);
  border-radius: 14px;
  background: var(--bg-card);
  box-shadow:
    0 40px 90px -28px var(--accent-glow-strong),
    0 10px 28px rgba(9, 9, 18, 0.10);
  overflow: hidden;
  text-align: left;
  font-family: var(--font-mono);
}

/* Window chrome */
.hs-chrome {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--rule);
  background: var(--bg-sidebar);
}
.hs-dot { width: 9px; height: 9px; border-radius: 50%; background: var(--rule-2); }
.hs-title { margin-left: 8px; font-size: 11px; color: var(--ink-3); letter-spacing: 0.02em; }
.hs-live {
  margin-left: auto;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 10px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--ok);
}
.hs-live-dot {
  width: 6px; height: 6px; border-radius: 50%;
  background: var(--ok);
  animation: hs-blink 2.4s ease-in-out infinite;
}

.hs-body {
  display: grid;
  grid-template-columns: 1.25fr 1fr;
  gap: 0;
}

/* --- Board --- */
.hs-board {
  /* Single source of truth for the board geometry. The columns are a grid, but
     the cards are absolutely positioned on the board plane, so both have to be
     derived from the same numbers or the cards drift out of their column.
     Percentages inside these calcs resolve against the board's padding box —
     the same box the absolute cards are positioned in and the same width the
     grid tracks are cut from — so `left` and `width` stay in one coordinate
     space. */
  --hs-pad: 14px;
  --hs-gap: 10px;
  --hs-col-w: calc((100% - 2 * var(--hs-pad) - 2 * var(--hs-gap)) / 3);
  --hs-head-h: 30px;
  --hs-inset: 8px; /* card gutter inside its column */
  --hs-card-h: 51px; /* border + padding + 2 lines + tag row */

  --hs-x1: calc(var(--hs-pad) + var(--hs-inset));
  --hs-x2: calc(var(--hs-x1) + var(--hs-col-w) + var(--hs-gap));
  --hs-x3: calc(var(--hs-x2) + var(--hs-col-w) + var(--hs-gap));
  --hs-y1: calc(var(--hs-pad) + 1px + var(--hs-head-h) + var(--hs-inset));
  --hs-y2: calc(var(--hs-y1) + var(--hs-card-h) + var(--hs-gap));

  position: relative;
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: var(--hs-gap);
  padding: var(--hs-pad);
  min-height: 250px;
  border-right: 1px solid var(--rule);
  background:
    radial-gradient(circle at 20% 0%, var(--accent-glow) 0%, transparent 55%),
    var(--bg);
}
.hs-col {
  border: 1px solid var(--rule);
  border-radius: 10px;
  background: var(--bg-sunk);
  min-height: 220px;
}
.hs-col-head {
  display: flex;
  align-items: center;
  gap: 6px;
  height: var(--hs-head-h);
  padding: 0 10px;
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 0.1em;
  color: var(--ink-3);
}
.hs-chip { width: 7px; height: 7px; border-radius: 2px; }
.hs-chip--backlog { background: var(--col-backlog); }
.hs-chip--progress { background: var(--col-progress); }
.hs-chip--done { background: var(--col-done); }

/* Cards travel across the three column slots, inset by --hs-inset inside the
   column they sit in. */
.hs-card {
  position: absolute;
  width: calc(var(--hs-col-w) - 2 * var(--hs-inset));
  padding: 9px 10px 8px;
  border: 1px solid var(--rule);
  border-radius: 8px;
  background: var(--bg-card);
  box-shadow: var(--sh-2);
}
.hs-card-line {
  display: block;
  height: 6px;
  border-radius: 3px;
  background: var(--rule);
  margin-bottom: 6px;
}
.hs-card-line--title { width: 70%; background: var(--ink-4); }
.hs-card-tags { display: flex; gap: 4px; }
.hs-tag { width: 26px; height: 8px; border-radius: 4px; background: var(--tint-neutral); }
.hs-tag--indigo { background: var(--tint-plum); }
.hs-tag--green { background: var(--tint-green); }
.hs-tag--amber { background: var(--tint-amber); }
.hs-card-badge {
  position: absolute;
  top: 7px;
  right: 8px;
  font-size: 9px;
  color: var(--ok);
  opacity: 0;
}

.hs-card--a { animation: hs-journey-a 20s cubic-bezier(0.65, 0, 0.35, 1) infinite; }
.hs-card--a .hs-card-badge { animation: hs-badge-a 20s linear infinite; }
.hs-card--b { animation: hs-journey-a 20s cubic-bezier(0.65, 0, 0.35, 1) -10s infinite; }
.hs-card--b .hs-card-badge { animation: hs-badge-a 20s linear -10s infinite; }
.hs-card--static { left: var(--hs-x1); top: var(--hs-y2); }

/* Column slots on the board plane, resolved from the shared geometry vars. */
@keyframes hs-journey-a {
  0%   { left: var(--hs-x1); top: var(--hs-y1); }
  12%  { left: var(--hs-x1); top: var(--hs-y1); }
  20%  { left: var(--hs-x2); top: var(--hs-y1); }
  58%  { left: var(--hs-x2); top: var(--hs-y1); }
  66%  { left: var(--hs-x3); top: var(--hs-y1); }
  96%  { left: var(--hs-x3); top: var(--hs-y1); }
  98%  { left: var(--hs-x3); top: var(--hs-y1); opacity: 1; }
  99%  { opacity: 0; }
  99.5% { left: var(--hs-x1); top: var(--hs-y1); opacity: 0; }
  100% { left: var(--hs-x1); top: var(--hs-y1); opacity: 1; }
}
@keyframes hs-badge-a {
  0%, 60% { opacity: 0; }
  66%, 96% { opacity: 1; }
  100% { opacity: 0; }
}

/* --- Agent graph --- */
.hs-graph {
  position: relative;
  min-height: 250px;
  background:
    radial-gradient(circle at 80% 100%, var(--accent-glow) 0%, transparent 55%),
    var(--bg-elevated);
}
.hs-edges { position: absolute; inset: 0; width: 100%; height: 100%; }
.hs-edge {
  stroke: var(--rule-2);
  stroke-width: 1.5;
  stroke-dasharray: 4 6;
  vector-effect: non-scaling-stroke;
  animation: hs-flow 1.6s linear infinite;
}
.hs-edge-2 { animation-delay: 0.4s; }
.hs-edge-3 { animation-delay: 0.8s; }
@keyframes hs-flow {
  to { stroke-dashoffset: -20; }
}

.hs-node {
  position: absolute;
  transform: translate(-4%, -50%);
  padding: 6px 10px;
  border: 1px solid var(--rule-2);
  border-radius: 7px;
  background: var(--bg-card);
  font-size: 10px;
  color: var(--ink-2);
  box-shadow: var(--sh-1);
  white-space: nowrap;
}
.hs-node--task { border-style: dashed; color: var(--ink-3); }
.hs-node--impl { animation: hs-active 10s ease-in-out infinite; }
.hs-node--test { animation: hs-active 10s ease-in-out -7s infinite; }
.hs-node--fan { font-size: 9px; padding: 5px 8px; transform: translateY(-50%); }
.hs-node--fan1 { animation: hs-active 10s ease-in-out -8.4s infinite; }
.hs-node--fan2 { animation: hs-active 10s ease-in-out -8.2s infinite; }
.hs-node--fan3 { animation: hs-active 10s ease-in-out -8.0s infinite; }

/* A node "runs": indigo ring + lift, then settles. */
@keyframes hs-active {
  0%, 18% { border-color: var(--rule-2); box-shadow: var(--sh-1); color: var(--ink-2); }
  26%, 48% {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-tint), var(--sh-2);
    color: var(--accent);
  }
  60%, 100% { border-color: var(--rule-2); box-shadow: var(--sh-1); color: var(--ink-2); }
}

@keyframes hs-blink {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.35; }
}

/* Cost ticker: vertical tape stepping through values. */
.hs-ticker {
  position: absolute;
  right: 12px;
  bottom: 10px;
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 10px;
  color: var(--ink-3);
}
.hs-ticker-window {
  display: inline-block;
  height: 14px;
  overflow: hidden;
}
.hs-ticker-tape {
  display: flex;
  flex-direction: column;
  line-height: 14px;
  color: var(--ink-2);
  animation: hs-tape 20s steps(5) infinite;
}
@keyframes hs-tape {
  to { transform: translateY(-70px); }
}

/* Small screens: stack board over graph. */
@media (max-width: 720px) {
  .hs-body { grid-template-columns: 1fr; }
  .hs-board { border-right: none; border-bottom: 1px solid var(--rule); }
}

/* Reduced motion: freeze everything on the complete static scene. */
@media (prefers-reduced-motion: reduce) {
  .hs-live-dot,
  .hs-card--a, .hs-card--b,
  .hs-card--a .hs-card-badge, .hs-card--b .hs-card-badge,
  .hs-edge,
  .hs-node--impl, .hs-node--test, .hs-node--fan1, .hs-node--fan2, .hs-node--fan3,
  .hs-ticker-tape {
    animation: none;
  }
  .hs-card--a { left: var(--hs-x2); top: var(--hs-y1); }
  .hs-card--b { left: var(--hs-x3); top: var(--hs-y1); }
  .hs-card--b .hs-card-badge { opacity: 1; }
}
</style>
