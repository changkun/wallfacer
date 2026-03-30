---
title: Dependency minimap renderer
status: validated
depends_on:
  - specs/local/spec-coordination/spec-planning-ux/spec-explorer/spec-tree-renderer.md
affects:
  - ui/js/
  - ui/index.html
effort: medium
created: 2026-03-30
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Dependency minimap renderer

## Goal

Render a small SVG dependency graph below the spec explorer showing the focused spec's 1-hop upstream (depends_on) and downstream (depended-on-by) neighborhood. Nodes are status-colored rectangles, edges are lines, clicking a node navigates to that spec.

## What to do

1. In `ui/index.html`, add a minimap container below the explorer tree:
   ```html
   <div id="spec-minimap" class="spec-minimap hidden">
     <div class="spec-minimap__header">Dependencies</div>
     <svg id="spec-minimap-svg" class="spec-minimap__svg"></svg>
   </div>
   ```

2. Create `ui/js/spec-minimap.js` with the minimap renderer:
   ```javascript
   function renderMinimap(specPath, treeData) {
     const svg = document.getElementById("spec-minimap-svg");
     if (!svg || !treeData) return;

     // Build reverse dependency index from treeData.nodes
     const reverseIndex = buildReverseDeps(treeData.nodes);

     // Collect 1-hop neighborhood
     const focused = treeData.nodes.find(n => n.path === specPath);
     if (!focused) { svg.innerHTML = ""; return; }

     const upstream = (focused.spec.depends_on || [])
       .map(dep => treeData.nodes.find(n => n.path === dep))
       .filter(Boolean);

     const downstream = (reverseIndex[specPath] || [])
       .map(dep => treeData.nodes.find(n => n.path === dep))
       .filter(Boolean);

     // Layout: upstream on the left, focused in center, downstream on the right
     // Compute positions based on node count
     // Render SVG: colored rectangles for nodes, lines for edges
   }
   ```

3. Implement the SVG rendering:
   - **Node colors**: `complete` → green (`#d4edda`), `validated` → blue (`#cce5ff`), `drafted` → yellow (`#fff3cd`), `vague` → gray (`#e2e3e5`), `stale` → red (`#f8d7da`)
   - **Node dimensions**: 120px wide, 28px tall, rounded corners
   - **Node label**: spec title (truncated to fit), centered text
   - **Focused node**: highlighted border (2px accent color)
   - **Edges**: straight lines connecting right edge of upstream to left edge of focused, and right edge of focused to left edge of downstream
   - **Click handler**: clicking a node calls `focusSpec(node.path, workspace)`

4. Implement `buildReverseDeps(nodes)`:
   ```javascript
   function buildReverseDeps(nodes) {
     const reverse = {};
     for (const node of nodes) {
       for (const dep of (node.spec.depends_on || [])) {
         if (!reverse[dep]) reverse[dep] = [];
         reverse[dep].push(node.path);
       }
     }
     return reverse;
   }
   ```

5. Wire to `focusSpec()`: when a spec is focused, call `renderMinimap(specPath, _specTreeData)`. Show the minimap container, or hide it if the spec has no dependencies and no dependents.

6. Include `spec-minimap.js` in `ui/index.html`.

## Tests

- `TestMinimapUpstream`: Spec with 2 `depends_on` entries shows 2 upstream nodes.
- `TestMinimapDownstream`: Spec depended on by 3 others shows 3 downstream nodes.
- `TestMinimapNodeColors`: Each status maps to the correct fill color.
- `TestMinimapClickNavigates`: Clicking an upstream/downstream node calls `focusSpec()` with the correct path.
- `TestMinimapHiddenWhenNoDeps`: Spec with no upstream and no downstream hides the minimap container.
- `TestMinimapFocusedNodeHighlighted`: The focused spec node has a distinct border style.
- `TestBuildReverseDeps`: Given 3 nodes where A depends on B, and C depends on B, `buildReverseDeps` returns `{B: [A, C]}`.

## Boundaries

- Do NOT implement 2-hop or full transitive closure — 1-hop only. An "expand" button can be added later.
- Do NOT add a layout library (dagre, ELK.js) — use simple column-based positioning.
- Do NOT render the minimap when in board mode.
