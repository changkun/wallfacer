(function() {
  var AXIS_H = 24;  // px reserved for the time axis
  var LANE_H = 22;  // px height of each span block
  var LANE_GAP = 3; // px gap between lanes

  function labelHue(s) {
    var h = 5381;
    for (var i = 0; i < s.length; i++) {
      h = ((h << 5) + h) ^ s.charCodeAt(i);
    }
    return Math.abs(h) % 360;
  }

  // Greedy lane-packing: assign each span to the lowest lane whose last
  // endpoint is <= the span's startMs. Spans must be pre-sorted by startMs.
  function assignLanes(spans) {
    var laneEnds = []; // laneEnds[i] = endMs of last span assigned to lane i
    return spans.map(function(span) {
      var lane = -1;
      for (var i = 0; i < laneEnds.length; i++) {
        if (laneEnds[i] <= span.startMs) {
          lane = i;
          break;
        }
      }
      if (lane === -1) {
        lane = laneEnds.length;
        laneEnds.push(0);
      }
      laneEnds[lane] = span.endMs;
      return { span: span, lane: lane };
    });
  }

  function formatMs(ms) {
    if (ms < 1000) return ms.toFixed(0) + 'ms';
    return (ms / 1000).toFixed(1) + 's';
  }

  function loadFlamegraph(taskId) {
    var container = document.getElementById('modal-flamegraph-container');
    if (!container) return;

    container.innerHTML = '<span style="color:var(--text-muted,#888);font-size:12px;">Loading spans\u2026</span>';

    fetch('/api/tasks/' + taskId + '/spans')
      .then(function(res) { return res.json(); })
      .catch(function() { return []; })
      .then(function(records) {
        var container = document.getElementById('modal-flamegraph-container');
        if (!container) return;

        if (!records || records.length === 0) {
          container.innerHTML = '<span style="color:var(--text-muted,#888);font-size:12px;">No span data available.</span>';
          return;
        }

        // Compute global time bounds
        var globalStartMs = Infinity;
        var globalEndMs = -Infinity;
        records.forEach(function(r) {
          var s = r.started_at ? new Date(r.started_at).getTime() : 0;
          var e = r.ended_at ? new Date(r.ended_at).getTime() : s + (r.duration_ms || 0);
          if (s < globalStartMs) globalStartMs = s;
          if (e > globalEndMs) globalEndMs = e;
        });
        var total = (globalEndMs - globalStartMs) || 1;

        // Map records to normalized span objects
        var spans = records.map(function(r) {
          var s = r.started_at ? new Date(r.started_at).getTime() : globalStartMs;
          var e = r.ended_at ? new Date(r.ended_at).getTime() : s + (r.duration_ms || 0);
          var label = escapeHtml(r.phase) + (r.label ? ':' + escapeHtml(r.label) : '');
          return {
            label: label,
            rawLabel: r.phase + (r.label ? ':' + r.label : ''),
            startMs: s,
            endMs: e,
            durationMs: e - s,
          };
        });

        // Sort by startMs, then assign lanes
        spans.sort(function(a, b) { return a.startMs - b.startMs; });
        var assigned = assignLanes(spans);

        var numLanes = 0;
        assigned.forEach(function(a) { if (a.lane >= numLanes) numLanes = a.lane + 1; });

        var totalH = AXIS_H + numLanes * (LANE_H + LANE_GAP);

        // Build time axis ticks
        var tickFractions = [0, 0.25, 0.5, 0.75, 1];
        var axisHtml = tickFractions.map(function(f) {
          var pct = (f * 100).toFixed(2);
          var ms = f * total;
          var label = formatMs(ms);
          var align = f === 0 ? 'left' : f === 1 ? 'right' : 'center';
          var transform = f === 0 ? '' : f === 1 ? 'translateX(-100%)' : 'translateX(-50%)';
          return '<span style="position:absolute;left:' + pct + '%;font-size:10px;' +
            'color:var(--text-muted,#888);transform:' + transform + ';' +
            'text-align:' + align + ';white-space:nowrap;">' +
            escapeHtml(label) + '</span>';
        }).join('');

        // Build span blocks
        var blocksHtml = assigned.map(function(a) {
          var span = a.span;
          var left = ((span.startMs - globalStartMs) / total * 100).toFixed(2);
          var width = Math.max(span.durationMs / total * 100, 0.5).toFixed(2);
          var top = AXIS_H + a.lane * (LANE_H + LANE_GAP);
          var hue = labelHue(span.rawLabel);
          var color = 'hsl(' + hue + ',55%,52%)';
          var tooltip = escapeHtml(span.rawLabel) + ' — ' + escapeHtml(formatMs(span.durationMs));
          return '<div title="' + tooltip + '" style="' +
            'position:absolute;' +
            'left:' + left + '%;' +
            'width:' + width + '%;' +
            'top:' + top + 'px;' +
            'height:' + LANE_H + 'px;' +
            'background:' + color + ';' +
            'border-radius:3px;' +
            'box-sizing:border-box;' +
            'overflow:hidden;' +
            'display:flex;align-items:center;padding:0 4px;' +
            '">' +
            '<span style="font-size:10px;color:#fff;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;">' +
            span.label +
            '</span></div>';
        }).join('');

        container.innerHTML =
          '<div style="position:relative;width:100%;height:' + totalH + 'px;' +
          'margin-bottom:8px;">' +
          '<div style="position:relative;height:' + AXIS_H + 'px;' +
          'border-bottom:1px solid var(--border,#333);margin-bottom:2px;">' +
          axisHtml +
          '</div>' +
          blocksHtml +
          '</div>';
      });
  }

  window.loadFlamegraph = loadFlamegraph;
  // Expose internals for testing
  window._flamegraph = { labelHue: labelHue, assignLanes: assignLanes };
})();
