// --- Shared oversight phase rendering ---
// buildPhaseListHTML renders an HTML string for an array of OversightPhase
// objects.  It is used by both the full oversight modal (modal-oversight.js)
// and the collapsible card accordion (render.js) so that the rendering logic
// lives in exactly one place.
function buildPhaseListHTML(phases) {
  if (!phases || phases.length === 0) {
    return '<div class="oversight-empty">No phases recorded.</div>';
  }
  return phases
    .map(function (phase, i) {
      var ts = phase.timestamp
        ? new Date(phase.timestamp).toLocaleTimeString([], {
            hour: "2-digit",
            minute: "2-digit",
          })
        : "";
      var tools = (phase.tools_used || [])
        .map(function (t) {
          return '<span class="oversight-tool">' + escapeHtml(t) + "</span>";
        })
        .join("");
      var commands = (phase.commands || [])
        .map(function (c) {
          return '<li class="oversight-command">' + escapeHtml(c) + "</li>";
        })
        .join("");
      var actions = (phase.actions || [])
        .map(function (a) {
          return '<li class="oversight-action">' + escapeHtml(a) + "</li>";
        })
        .join("");
      return (
        '<div class="oversight-phase">' +
        '<div class="oversight-phase-header">' +
        '<span class="oversight-phase-num">Phase ' +
        (i + 1) +
        "</span>" +
        '<span class="oversight-phase-title">' +
        escapeHtml(phase.title || "") +
        "</span>" +
        (ts ? '<span class="oversight-phase-time">' + ts + "</span>" : "") +
        "</div>" +
        (phase.summary
          ? '<div class="oversight-summary">' +
            escapeHtml(phase.summary) +
            "</div>"
          : "") +
        (tools ? '<div class="oversight-tools">' + tools + "</div>" : "") +
        (commands
          ? '<ul class="oversight-commands">' + commands + "</ul>"
          : "") +
        (actions ? '<ul class="oversight-actions">' + actions + "</ul>" : "") +
        "</div>"
      );
    })
    .join("");
}
