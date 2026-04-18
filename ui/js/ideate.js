// --- Ideation / Brainstorm agent (legacy shim) ---
//
// The dedicated Ideation toggle in Automation/Settings has been
// retired: users now create ideation tasks from the standard composer
// (picking the "Ideation" task type and, optionally, Repeat on a
// schedule). The functions below remain as stubs so any inline event
// handlers that still reference them continue to no-op cleanly until
// the templates and partials are fully regenerated.

function setIdeationRunning() {}
function updateIdeationFromTasks() {}
function toggleIdeation() {}
function toggleIdeationHeader() {}
function updateIdeationInterval() {}
function updateNextRunDisplay() {}
function updateExploitRatioLabel() {}
function updateIdeationExploitRatio() {}
function updateIdeationConfig() {}

// triggerIdeation is kept as a convenience for any surface that still
// binds to a "Brainstorm" button: it hits the /api/ideate shim, which
// in turn creates a one-shot Kind=idea-agent task on the board.
async function triggerIdeation() {
  try {
    const res = await api("/api/ideate", { method: "POST" });
    if (res && res.task_id) {
      waitForTaskDelta(res.task_id);
    } else {
      fetchTasks();
    }
  } catch (e) {
    if (typeof showAlert === "function") {
      showAlert("Error triggering brainstorm: " + e.message);
    }
  }
}
