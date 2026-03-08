(function() {
  var AXIS_H = 24;   // px reserved for the time axis
  var LANE_H = 22;   // px height of each span block
  var LANE_GAP = 3;  // px gap between lanes
  var PHASE_H = 28;  // px reserved for the oversight phase band row

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

  // Convert OversightPhase[] into rendering-ready region objects.
  // Each region covers [phase[i].timestamp, phase[i+1].timestamp), with
  // the last region extending to globalEndMs. Regions are clamped to
  // [globalStartMs, globalEndMs] and zero-width regions are skipped.
  //
  // When phase timestamps are missing or invalid (Go zero-value time.Time
  // serialises to "0001-01-01T00:00:00Z" which gives a large negative number
  // in JS — not NaN — so the old isNaN guard missed it), the function falls
  // back to distributing phases evenly across the timeline so that all phases
  // remain visible rather than collapsing into a single full-width last block.
  function computePhaseRegions(phases, globalStartMs, globalEndMs) {
    if (!phases || phases.length === 0) return [];

    // Parse timestamps: null = invalid.
    // A Go zero-value time.Time ("0001-01-01T00:00:00Z") produces a large
    // negative getTime() value, so we treat any ms < 0 (or NaN) as invalid.
    var rawStarts = phases.map(function(p) {
      var ms = p.timestamp ? new Date(p.timestamp).getTime() : NaN;
      return (!isNaN(ms) && ms >= 0) ? ms : null;
    });

    var validCount = rawStarts.filter(function(ms) { return ms !== null; }).length;

    if (validCount === 0) {
      // No valid timestamps at all (e.g. Claude returned empty strings and the
      // backend stored zero-value times): distribute phases evenly so every
      // phase is visible with a proportional width.
      var regions = [];
      var total = globalEndMs - globalStartMs;
      for (var i = 0; i < phases.length; i++) {
        var sMs = Math.round(globalStartMs + (i / phases.length) * total);
        var eMs = i + 1 < phases.length
          ? Math.round(globalStartMs + ((i + 1) / phases.length) * total)
          : globalEndMs;
        if (sMs >= eMs) continue;
        regions.push({
          startMs: sMs,
          endMs: eMs,
          title: phases[i].title || '',
          summary: phases[i].summary || '',
          hue: labelHue(phases[i].title || ''),
        });
      }
      return regions;
    }

    // At least some valid timestamps: render phases that have valid timestamps,
    // using the next valid timestamp (or globalEndMs) as the region end.
    var regions = [];
    for (var i = 0; i < phases.length; i++) {
      if (rawStarts[i] === null) continue; // skip phases without valid timestamp
      var startMs = rawStarts[i];
      var endMs = globalEndMs;
      for (var j = i + 1; j < phases.length; j++) {
        if (rawStarts[j] !== null) { endMs = rawStarts[j]; break; }
      }
      if (isNaN(endMs)) endMs = globalEndMs;
      // clamp to global bounds
      if (startMs < globalStartMs) startMs = globalStartMs;
      if (endMs > globalEndMs) endMs = globalEndMs;
      if (startMs >= endMs) continue; // skip zero-width regions
      regions.push({
        startMs: startMs,
        endMs: endMs,
        title: phases[i].title || '',
        summary: phases[i].summary || '',
        hue: labelHue(phases[i].title || ''),
      });
    }
    return regions;
  }

  // Find the oversight phase region that contains span.startMs.
  // Returns the region object or null if none matches.
  function findPhaseForSpan(span, phaseRegions) {
    for (var i = 0; i < phaseRegions.length; i++) {
      var r = phaseRegions[i];
      if (span.startMs >= r.startMs && span.startMs < r.endMs) {
        return r;
      }
    }
    return null;
  }

  // Build a cumulative cost SVG polyline from turn-usage records, aligned to agent_turn_N spans.
  // Returns an HTML string (SVG element) or empty string if there is no data.
  function buildCostChart(turnUsages, spans, globalStartMs, total) {
    // Only include implementation/test turns (not sub-agent overhead).
    var implTurns = turnUsages.filter(function(u) {
      return u.sub_agent === 'implementation' || u.sub_agent === 'test';
    });
    if (implTurns.length === 0) return '';

    // Build turn number → span end X-position mapping from agent_turn_N spans.
    var turnXPct = {};
    spans.forEach(function(span) {
      var m = span.rawLabel.match(/^agent_turn:agent_turn_(\d+)$/);
      if (m) {
        var turnNum = parseInt(m[1], 10);
        var xPct = total > 0 ? ((span.endMs - globalStartMs) / total * 100) : 0;
        turnXPct[turnNum] = xPct;
      }
    });

    // Compute cumulative cost points (start at 0).
    var cumCost = 0;
    var points = [{ xPct: 0, cost: 0 }];
    implTurns.forEach(function(u) {
      cumCost += (u.cost_usd || 0);
      var xPct = turnXPct[u.turn];
      if (xPct !== undefined) {
        points.push({ xPct: xPct, cost: cumCost });
      }
    });
    if (points.length < 2) return '';

    var maxCost = points[points.length - 1].cost;
    if (maxCost <= 0) return '';

    var chartH = 48;
    var padding = 4;
    var innerH = chartH - padding * 2;

    var polyPoints = points.map(function(p) {
      var x = p.xPct.toFixed(3) + '%';
      var y = (padding + innerH * (1 - p.cost / maxCost)).toFixed(1);
      return x + ',' + y;
    }).join(' ');

    var totalLabel = '$' + maxCost.toFixed(4);
    var lastPt = points[points.length - 1];
    var lastX = lastPt.xPct.toFixed(3) + '%';
    var lastY = padding.toFixed(1);

    return '<div style="position:relative;width:100%;height:' + chartH + 'px;margin-top:4px;" ' +
      'title="Cumulative cost across turns (impl/test only). Total: ' + escapeHtml(totalLabel) + '">' +
      '<svg width="100%" height="' + chartH + '" style="display:block;overflow:visible;">' +
      '<polyline points="' + escapeHtml(polyPoints) + '" ' +
      'fill="none" stroke="hsl(200,60%,55%)" stroke-width="1.5" stroke-linejoin="round"/>' +
      '<text x="' + escapeHtml(lastX) + '" y="' + lastY + '" ' +
      'font-size="9" fill="hsl(200,60%,65%)" text-anchor="end" dy="-2">' +
      escapeHtml(totalLabel) + '</text>' +
      '</svg>' +
      '<span style="position:absolute;left:0;top:' + padding + 'px;font-size:9px;' +
      'color:var(--text-muted,#888);">cost</span>' +
      '</div>';
  }

  function loadFlamegraph(taskId) {
    var container = document.getElementById('modal-flamegraph-container');
    if (!container) return;

    container.innerHTML = '<span style="color:var(--text-muted,#888);font-size:12px;">Loading\u2026</span>';

    var spansUrl = '/api/tasks/' + taskId + '/spans';
    var oversightUrl = '/api/tasks/' + taskId + '/oversight';
    var turnUsageUrl = '/api/tasks/' + taskId + '/turn-usage';

    Promise.all([
      fetch(spansUrl).then(function(res) { return res.json(); }).catch(function() { return []; }),
      fetch(oversightUrl).then(function(res) { return res.json(); }).catch(function() { return null; }),
      fetch(turnUsageUrl).then(function(res) { return res.json(); }).catch(function() { return []; }),
    ]).then(function(results) {
      var records = results[0];
      var oversightData = results[1];
      var turnUsages = results[2] || [];

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

      // Compute oversight phase regions (if oversight data is ready)
      var phaseRegions = [];
      if (oversightData && oversightData.status === 'ready' &&
          oversightData.phases && oversightData.phases.length > 0) {
        phaseRegions = computePhaseRegions(oversightData.phases, globalStartMs, globalEndMs);
      }
      var phaseOffset = phaseRegions.length > 0 ? PHASE_H : 0;

      var totalH = AXIS_H + phaseOffset + numLanes * (LANE_H + LANE_GAP);

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

      // Build phase band HTML (empty string when no phases)
      var phaseBandHtml = '';
      if (phaseRegions.length > 0) {
        phaseBandHtml = phaseRegions.map(function(r) {
          var left = ((r.startMs - globalStartMs) / total * 100).toFixed(2);
          var width = Math.max((r.endMs - r.startMs) / total * 100, 0.5).toFixed(2);
          var top = AXIS_H + 2;
          var height = PHASE_H - 4;
          var bg = 'hsl(' + r.hue + ',30%,30%)';
          var displayTitle = r.title.length > 30 ? r.title.slice(0, 30) + '\u2026' : r.title;
          var tooltipText = 'Phase: ' + escapeHtml(r.title) + '\n' + escapeHtml(r.summary);
          return '<div title="' + tooltipText + '" style="' +
            'position:absolute;' +
            'left:' + left + '%;' +
            'width:' + width + '%;' +
            'top:' + top + 'px;' +
            'height:' + height + 'px;' +
            'background:' + bg + ';' +
            'border-radius:3px;' +
            'box-sizing:border-box;' +
            'overflow:hidden;' +
            'display:flex;align-items:center;padding:0 4px;' +
            '">' +
            '<span style="font-size:9px;color:#fff;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;">' +
            escapeHtml(displayTitle) +
            '</span></div>';
        }).join('');
      }

      // Build span blocks
      var blocksHtml = assigned.map(function(a) {
        var span = a.span;
        var left = ((span.startMs - globalStartMs) / total * 100).toFixed(2);
        var width = Math.max(span.durationMs / total * 100, 0.5).toFixed(2);
        var top = AXIS_H + phaseOffset + a.lane * (LANE_H + LANE_GAP);
        var hue = labelHue(span.rawLabel);
        var color = 'hsl(' + hue + ',55%,52%)';
        var startOffset = formatMs(span.startMs - globalStartMs);
        var tooltip = escapeHtml(span.rawLabel) + ' | start: ' + escapeHtml(startOffset) + ' | dur: ' + escapeHtml(formatMs(span.durationMs));
        var phaseMatch = findPhaseForSpan(span, phaseRegions);
        if (phaseMatch) {
          tooltip += ' | oversight: ' + escapeHtml(phaseMatch.title);
        }
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
        var rowPhaseMatch = findPhaseForSpan(span, phaseRegions);
        var oversightCell = rowPhaseMatch
          ? '<td style="padding:3px 6px;white-space:nowrap;max-width:120px;overflow:hidden;text-overflow:ellipsis;" title="' + escapeHtml(rowPhaseMatch.title) + '">' + escapeHtml(rowPhaseMatch.title) + '</td>'
          : '<td style="padding:3px 6px;color:var(--text-muted,#888);">&mdash;</td>';
        return '<tr style="border-bottom:1px solid var(--border,#333);">' +
          '<td style="padding:3px 6px;white-space:nowrap;">' + swatch + phase + '</td>' +
          '<td style="padding:3px 6px;color:var(--text-muted,#888);white-space:nowrap;">' + label + '</td>' +
          oversightCell +
          '<td style="padding:3px 6px;text-align:right;white-space:nowrap;">' + startOffset + '</td>' +
          '<td style="padding:3px 6px;text-align:right;white-space:nowrap;">' + escapeHtml(formatMs(span.durationMs)) + '</td>' +
          '<td style="padding:3px 6px;text-align:right;white-space:nowrap;color:var(--text-muted,#888);">' + pct + '%</td>' +
          '</tr>';
      }).join('');

      var tableHtml = '<table style="width:100%;border-collapse:collapse;font-size:11px;margin-top:12px;">' +
        '<thead><tr style="border-bottom:1px solid var(--border,#333);color:var(--text-muted,#888);">' +
        '<th style="padding:3px 6px;text-align:left;font-weight:500;">Phase</th>' +
        '<th style="padding:3px 6px;text-align:left;font-weight:500;">Label</th>' +
        '<th style="padding:3px 6px;text-align:left;font-weight:500;">Oversight Phase</th>' +
        '<th style="padding:3px 6px;text-align:right;font-weight:500;">Start</th>' +
        '<th style="padding:3px 6px;text-align:right;font-weight:500;">Duration</th>' +
        '<th style="padding:3px 6px;text-align:right;font-weight:500;">%</th>' +
        '</tr></thead>' +
        '<tbody>' + rowsHtml + '</tbody>' +
        '</table>';

      var costChartHtml = buildCostChart(turnUsages, spans, globalStartMs, total);

      container.innerHTML =
        '<div style="position:relative;width:100%;height:' + totalH + 'px;' +
        'margin-bottom:8px;">' +
        '<div style="position:relative;height:' + AXIS_H + 'px;' +
        'border-bottom:1px solid var(--border,#333);margin-bottom:2px;">' +
        axisHtml +
        '</div>' +
        phaseBandHtml +
        blocksHtml +
        '</div>' +
        costChartHtml +
        tableHtml;
    });
  }

  window.loadFlamegraph = loadFlamegraph;
  // Expose internals for testing
  window._flamegraph = { labelHue: labelHue, assignLanes: assignLanes, computePhaseRegions: computePhaseRegions, findPhaseForSpan: findPhaseForSpan, buildCostChart: buildCostChart };
})();
