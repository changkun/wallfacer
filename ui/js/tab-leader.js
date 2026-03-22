// --- Cross-tab SSE connection sharing ---
//
// Browsers limit HTTP/1.1 connections to ~6 per origin. Each wallfacer tab
// opens 2 persistent SSE connections (tasks stream + git status stream).
// Opening 3+ tabs exhausts the limit, blocking all API calls and freezing
// the UI.
//
// This module elects a single "leader" tab that holds the real SSE
// connections and relays events to "follower" tabs via BroadcastChannel.
// When the leader tab closes, a follower takes over automatically.

(function () {
  "use strict";

  // Fallback: if BroadcastChannel is unsupported, every tab is its own leader.
  if (typeof BroadcastChannel === "undefined") {
    window._sseIsLeader = function () {
      return true;
    };
    window._sseRelay = function () {};
    window._sseOnFollowerEvent = function () {};
    return;
  }

  var ELECTION_MS = 250;
  var channel = new BroadcastChannel("wallfacer-sse-relay");
  var isLeader = false;
  var electionDone = false;
  var electionTimer = null;

  // Follower event handlers, keyed by event name.
  var followerHandlers = {};

  // --- Election ---

  function runElection() {
    electionDone = false;
    channel.postMessage({ type: "who-is-leader" });
    electionTimer = setTimeout(function () {
      if (!electionDone) {
        isLeader = true;
        electionDone = true;
        // If streams were already started as follower, restart as leader.
        if (typeof restartActiveStreams === "function") {
          restartActiveStreams();
        }
      }
    }, ELECTION_MS);
  }

  channel.onmessage = function (e) {
    var msg = e.data;
    if (!msg || !msg.type) return;

    switch (msg.type) {
      case "who-is-leader":
        if (isLeader) {
          channel.postMessage({ type: "i-am-leader" });
        }
        break;

      case "i-am-leader":
        if (!electionDone) {
          // A leader exists; become follower.
          isLeader = false;
          electionDone = true;
          if (electionTimer) {
            clearTimeout(electionTimer);
            electionTimer = null;
          }
        }
        break;

      case "leader-leaving":
        if (!isLeader) {
          // Re-elect after a short random delay to avoid simultaneous claims.
          setTimeout(runElection, Math.floor(Math.random() * 150));
        }
        break;

      // Relayed SSE events from the leader tab.
      case "sse":
        if (!isLeader) {
          var handler = followerHandlers[msg.event];
          if (handler) handler(msg.data, msg.lastEventId);
        }
        break;
    }
  };

  window.addEventListener("beforeunload", function () {
    if (isLeader) {
      channel.postMessage({ type: "leader-leaving" });
    }
    channel.close();
  });

  // --- Public API ---

  /** Returns true if this tab should open real SSE connections. */
  window._sseIsLeader = function () {
    return isLeader;
  };

  /**
   * Relay an SSE event to follower tabs. Called by the leader after processing
   * an event locally.
   */
  window._sseRelay = function (eventName, data, lastEventId) {
    if (!isLeader) return;
    channel.postMessage({
      type: "sse",
      event: eventName,
      data: data,
      lastEventId: lastEventId || null,
    });
  };

  /**
   * Register a handler for a relayed SSE event type on follower tabs.
   */
  window._sseOnFollowerEvent = function (eventName, handler) {
    followerHandlers[eventName] = handler;
  };

  // Start election immediately.
  runElection();
})();
