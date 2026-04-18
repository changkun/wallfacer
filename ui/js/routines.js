// Routine card UI — interval selector, enabled toggle, countdown, and
// "run now" button rendered inside the task card for Kind=routine tasks.
// All functions are globals because the task-board bundle does not use
// ES modules; render.js and inline onclick handlers call them directly.

// ROUTINE_INTERVAL_OPTIONS is the set of interval choices shown in the
// selector. Users with unusual cadences can still PATCH the endpoint
// directly; keeping the picker short avoids a tangled multi-step UX.
const ROUTINE_INTERVAL_OPTIONS = [1, 5, 15, 30, 60, 180, 360, 720, 1440];

// formatRoutineCountdown turns a next-run ISO timestamp into a
// "in 3m 12s" / "fired just now" / "paused" label.
// Exposed so ui/js/tests can exercise the formatter without DOM state.
function formatRoutineCountdown(nextRunISO, enabled) {
  if (!enabled) return "paused";
  if (!nextRunISO) return "re-arming\u2026";
  const next = new Date(nextRunISO);
  if (Number.isNaN(next.getTime())) return "\u2014";
  const diffMs = next - Date.now();
  if (diffMs <= 0) return "fired just now";
  const totalSec = Math.floor(diffMs / 1000);
  const h = Math.floor(totalSec / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  const s = totalSec % 60;
  if (h > 0) return `in ${h}h ${m}m`;
  if (m > 0) return `in ${m}m ${s}s`;
  return `in ${s}s`;
}

// formatRoutineLastFired returns a short "fired 3m ago" label for the
// routine's last-fired timestamp. Empty when no fire has happened yet.
function formatRoutineLastFired(lastFiredISO) {
  if (!lastFiredISO) return "";
  const fired = new Date(lastFiredISO);
  if (Number.isNaN(fired.getTime())) return "";
  const diffMs = Date.now() - fired;
  if (diffMs < 0) return "";
  const sec = Math.floor(diffMs / 1000);
  if (sec < 60) return `fired ${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `fired ${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `fired ${hr}h ago`;
  return `fired ${Math.floor(hr / 24)}d ago`;
}

// currentIntervalOptions returns the selector options including the
// task's current interval so an unusual value (set via API) stays
// representable in the UI.
function currentIntervalOptions(currentMinutes) {
  const out = new Set(ROUTINE_INTERVAL_OPTIONS);
  if (currentMinutes && currentMinutes > 0) out.add(currentMinutes);
  return Array.from(out).sort((a, b) => a - b);
}

// renderRoutineFooter returns the HTML fragment that render.js appends
// inside a routine card below the usual prompt/tags block.
function renderRoutineFooter(t) {
  const minutes = Math.max(
    1,
    Math.round((t.routine_interval_seconds || 0) / 60),
  );
  const enabled = !!t.routine_enabled;
  const nextRun = t.routine_next_run || null;
  const lastFired = t.routine_last_fired_at || null;
  const spawnKind = t.routine_spawn_kind || "";

  const options = currentIntervalOptions(minutes)
    .map(
      (m) =>
        `<option value="${m}"${m === minutes ? " selected" : ""}>${m} min</option>`,
    )
    .join("");

  const spawnBadge = spawnKind
    ? `<span class="badge badge-routine-spawn" title="Spawns ${escapeHtml(spawnKind)} tasks">${escapeHtml(spawnKind)}</span>`
    : `<span class="badge badge-routine-spawn" title="Spawns regular tasks">task</span>`;

  return `
    <div class="routine-footer" onclick="event.stopPropagation()">
      <div class="routine-footer-row">
        <span class="badge badge-routine" title="Routine schedule">routine</span>
        ${spawnBadge}
        <span class="routine-next-run" data-routine-id="${t.id}" data-routine-next="${escapeHtml(nextRun || "")}" data-routine-enabled="${enabled ? "1" : "0"}" title="Next scheduled fire">${escapeHtml(formatRoutineCountdown(nextRun, enabled))}</span>
      </div>
      <div class="routine-footer-row">
        <label class="routine-interval-label">
          Every
          <select class="routine-interval-select" onchange="onRoutineIntervalChange('${t.id}', this.value)" aria-label="Routine interval">
            ${options}
          </select>
        </label>
        <label class="routine-enabled-label">
          <input type="checkbox" class="routine-enabled-toggle" ${enabled ? "checked" : ""} onchange="onRoutineEnabledChange('${t.id}', this.checked)" aria-label="Routine enabled">
          <span>Enabled</span>
        </label>
        <button type="button" class="routine-trigger-btn" onclick="onRoutineTrigger('${t.id}')" title="Spawn an instance task now">Run now</button>
      </div>
      ${lastFired ? `<div class="routine-footer-row routine-last-fired">${escapeHtml(formatRoutineLastFired(lastFired))}</div>` : ""}
    </div>
  `;
}

// routineUrl substitutes {id} in a Routes.routines.* template. Matches
// the pattern used elsewhere in the bundle (see planning-chat.js).
function routineUrl(routeFn, id) {
  return routeFn().replace("{id}", encodeURIComponent(id));
}

// onRoutineIntervalChange PATCHes the routine's schedule when the user
// picks a new interval. On failure the UI falls back to fetchTasks so
// the select returns to the server's authoritative value.
async function onRoutineIntervalChange(id, minutesStr) {
  const minutes = parseInt(minutesStr, 10);
  if (!Number.isFinite(minutes) || minutes < 1) return;
  try {
    await api(routineUrl(Routes.routines.updateSchedule, id), {
      method: "PATCH",
      body: JSON.stringify({ interval_minutes: minutes }),
    });
    // Board refresh happens via the SSE stream; fallback to a refetch
    // when stream plumbing is unavailable (e.g. in unit tests).
    if (typeof fetchTasks === "function") fetchTasks();
  } catch (e) {
    if (typeof showAlert === "function") {
      showAlert("Error updating routine interval: " + e.message);
    }
    if (typeof fetchTasks === "function") fetchTasks();
  }
}

// onRoutineEnabledChange flips the enabled flag via PATCH. Disabling
// clears RoutineNextRun on the server so the countdown immediately
// switches to "paused".
async function onRoutineEnabledChange(id, enabled) {
  try {
    await api(routineUrl(Routes.routines.updateSchedule, id), {
      method: "PATCH",
      body: JSON.stringify({ enabled }),
    });
    if (typeof fetchTasks === "function") fetchTasks();
  } catch (e) {
    if (typeof showAlert === "function") {
      showAlert("Error toggling routine: " + e.message);
    }
    if (typeof fetchTasks === "function") fetchTasks();
  }
}

// onRoutineTrigger hits POST /trigger and flashes a toast. The server
// fires asynchronously; the SSE stream will surface the new instance
// task card when it appears.
async function onRoutineTrigger(id) {
  try {
    await api(routineUrl(Routes.routines.trigger, id), { method: "POST" });
    if (typeof showToast === "function") {
      showToast("Routine fired", { variant: "success" });
    }
  } catch (e) {
    if (typeof showAlert === "function") {
      showAlert("Error triggering routine: " + e.message);
    }
  }
}

// createRoutineFromPrompt posts a new routine card. Exported for the
// command-palette / composer integrations that ship in follow-ups.
async function createRoutineFromPrompt(
  prompt,
  {
    intervalMinutes,
    spawnKind = "",
    enabled = true,
    tags = [],
    goal = "",
  } = {},
) {
  return api(Routes.routines.create(), {
    method: "POST",
    body: JSON.stringify({
      prompt,
      goal,
      interval_minutes: intervalMinutes,
      spawn_kind: spawnKind,
      enabled,
      tags,
    }),
  });
}

// tickRoutineCountdowns rewrites every visible countdown span so it
// stays accurate without a re-render. Called from a page-level
// setInterval so one timer drives every card.
function tickRoutineCountdowns() {
  const spans = document.querySelectorAll(".routine-next-run");
  for (const el of spans) {
    const enabled = el.getAttribute("data-routine-enabled") === "1";
    const nextRun = el.getAttribute("data-routine-next") || null;
    el.textContent = formatRoutineCountdown(nextRun, enabled);
  }
}

// Start the countdown ticker once. Guarded with a sentinel so hot
// reloads in dev mode don't stack intervals.
if (typeof window !== "undefined" && !window.__routineCountdownTicker) {
  window.__routineCountdownTicker = setInterval(tickRoutineCountdowns, 1000);
}

// Export for tests (vitest runs in Node; window is undefined there).
if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    formatRoutineCountdown,
    formatRoutineLastFired,
    currentIntervalOptions,
    renderRoutineFooter,
    createRoutineFromPrompt,
    tickRoutineCountdowns,
  };
}
