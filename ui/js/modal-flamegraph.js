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

    container.innerHTML = '<span style="color:var(--text-muted,#888);font-size:12px;">Loading\u2026</span>';

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
          var startOffset = formatMs(span.startMs - globalStartMs);
          var tooltip = escapeHtml(span.rawLabel) + ' | start: ' + escapeHtml(startOffset) + ' | dur: ' + escapeHtml(formatMs(span.durationMs));
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

        // Build detail table sorted by duration descending
        var sortedByDuration = spans.slice().sort(function(a, b) { return b.durationMs - a.durationMs; });
        var rowsHtml = sortedByDuration.map(function(span) {
          var startOffset = formatMs(span.startMs - globalStartMs);
          var pct = total > 0 ? ((span.durationMs / total) * 100).toFixed(1) : '0.0';
          var hue = labelHue(span.rawLabel);
          var swatch = '<span style="display:inline-block;width:8px;height:8px;border-radius:2px;' +
            'background:hsl(' + hue + ',55%,52%);margin-right:4px;flex-shrink:0;"></span>';
          var parts = span.rawLabel.split(':');
          var phase = escapeHtml(parts[0]);
          var label = escapeHtml(parts.slice(1).join(':'));
          return '<tr style="border-bottom:1px solid var(--border,#333);">' +
            '<td style="padding:3px 6px;white-space:nowrap;">' + swatch + phase + '</td>' +
            '<td style="padding:3px 6px;color:var(--text-muted,#888);white-space:nowrap;">' + label + '</td>' +
            '<td style="padding:3px 6px;text-align:right;white-space:nowrap;">' + startOffset + '</td>' +
            '<td style="padding:3px 6px;text-align:right;white-space:nowrap;">' + escapeHtml(formatMs(span.durationMs)) + '</td>' +
            '<td style="padding:3px 6px;text-align:right;white-space:nowrap;color:var(--text-muted,#888);">' + pct + '%</td>' +
            '</tr>';
        }).join('');

        var tableHtml = '<table style="width:100%;border-collapse:collapse;font-size:11px;margin-top:12px;">' +
          '<thead><tr style="border-bottom:1px solid var(--border,#333);color:var(--text-muted,#888);">' +
          '<th style="padding:3px 6px;text-align:left;font-weight:500;">Phase</th>' +
          '<th style="padding:3px 6px;text-align:left;font-weight:500;">Label</th>' +
          '<th style="padding:3px 6px;text-align:right;font-weight:500;">Start</th>' +
          '<th style="padding:3px 6px;text-align:right;font-weight:500;">Duration</th>' +
          '<th style="padding:3px 6px;text-align:right;font-weight:500;">%</th>' +
          '</tr></thead>' +
          '<tbody>' + rowsHtml + '</tbody>' +
          '</table>';

        container.innerHTML =
          '<div style="position:relative;width:100%;height:' + totalH + 'px;' +
          'margin-bottom:8px;">' +
          '<div style="position:relative;height:' + AXIS_H + 'px;' +
          'border-bottom:1px solid var(--border,#333);margin-bottom:2px;">' +
          axisHtml +
          '</div>' +
          blocksHtml +
          '</div>' +
          tableHtml;
      });
  }

  window.loadFlamegraph = loadFlamegraph;
  // Expose internals for testing
  window._flamegraph = { labelHue: labelHue, assignLanes: assignLanes };
})();
