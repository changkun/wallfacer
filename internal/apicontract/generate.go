package apicontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// jsTypeDef describes a single JSDoc @typedef to emit.
// When EnumValues is non-nil the type is emitted as a string-union typedef
// (e.g. 'backlog'|'in_progress'|…); otherwise it is emitted as an Object
// typedef with @property lines for each field.
type jsTypeDef struct {
	Name       string
	Fields     []jsField
	EnumValues []string // non-nil → emit as string-union, not Object
	Comment    string
}

// jsField describes one @property line inside a JSDoc {Object} typedef.
type jsField struct {
	JSONName string
	JSType   string
	Nullable bool
	Comment  string
}

// jsTypeRegistry is the hand-maintained ordered registry of domain types.
// Entries are ordered alphabetically by Name for deterministic output.
var jsTypeRegistry = []jsTypeDef{
	{
		Name:       "EventType",
		Comment:    "Identifies the kind of event stored in a task's audit trail.",
		EnumValues: []string{"state_change", "output", "feedback", "error", "system", "span_start", "span_end"},
	},
	{
		Name:    "ExecutionEnvironment",
		Comment: "Captures the runtime environment used for a task execution.",
		Fields: []jsField{
			{JSONName: "container_image", JSType: "string"},
			{JSONName: "container_digest", JSType: "string"},
			{JSONName: "model_name", JSType: "string"},
			{JSONName: "api_base_url", JSType: "string"},
			{JSONName: "instructions_hash", JSType: "string"},
			{JSONName: "sandbox", JSType: "string"},
			{JSONName: "recorded_at", JSType: "string", Comment: "ISO 8601 timestamp"},
		},
	},
	{
		Name:    "RefinementSession",
		Comment: "Records a single sandbox-based refinement run.",
		Fields: []jsField{
			{JSONName: "id", JSType: "string"},
			{JSONName: "created_at", JSType: "string", Comment: "ISO 8601 timestamp"},
			{JSONName: "start_prompt", JSType: "string"},
			{JSONName: "messages", JSType: "Array.<Object>"},
			{JSONName: "result", JSType: "string"},
			{JSONName: "result_prompt", JSType: "string"},
		},
	},
	{
		Name:    "Task",
		Comment: "Core domain model: a unit of work executed by an agent.",
		Fields: []jsField{
			{JSONName: "schema_version", JSType: "number"},
			{JSONName: "id", JSType: "string"},
			{JSONName: "title", JSType: "string"},
			{JSONName: "prompt", JSType: "string"},
			{JSONName: "prompt_history", JSType: "Array.<string>"},
			{JSONName: "retry_history", JSType: "Array.<Object>"},
			{JSONName: "refine_sessions", JSType: "Array.<RefinementSession>"},
			{JSONName: "current_refinement", JSType: "Object", Nullable: true},
			{JSONName: "status", JSType: "TaskStatus"},
			{JSONName: "archived", JSType: "boolean"},
			{JSONName: "session_id", JSType: "string", Nullable: true},
			{JSONName: "fresh_start", JSType: "boolean"},
			{JSONName: "result", JSType: "string", Nullable: true},
			{JSONName: "stop_reason", JSType: "string", Nullable: true},
			{JSONName: "turns", JSType: "number"},
			{JSONName: "timeout", JSType: "number"},
			{JSONName: "max_cost_usd", JSType: "number"},
			{JSONName: "max_input_tokens", JSType: "number"},
			{JSONName: "usage", JSType: "TaskUsage"},
			{JSONName: "sandbox", JSType: "string"},
			{JSONName: "sandbox_by_activity", JSType: "Object.<string, string>"},
			{JSONName: "usage_breakdown", JSType: "Object.<string, TaskUsage>"},
			{JSONName: "environment", JSType: "ExecutionEnvironment", Nullable: true},
			{JSONName: "position", JSType: "number"},
			{JSONName: "created_at", JSType: "string", Comment: "ISO 8601 timestamp"},
			{JSONName: "updated_at", JSType: "string", Comment: "ISO 8601 timestamp"},
			{JSONName: "worktree_paths", JSType: "Object.<string, string>"},
			{JSONName: "branch_name", JSType: "string"},
			{JSONName: "commit_hashes", JSType: "Object.<string, string>"},
			{JSONName: "base_commit_hashes", JSType: "Object.<string, string>"},
			{JSONName: "mount_worktrees", JSType: "boolean"},
			{JSONName: "model", JSType: "string"},
			{JSONName: "model_override", JSType: "string", Nullable: true},
			{JSONName: "is_test_run", JSType: "boolean"},
			{JSONName: "last_test_result", JSType: "string"},
			{JSONName: "test_run_start_turn", JSType: "number"},
			{JSONName: "kind", JSType: "TaskKind"},
			{JSONName: "tags", JSType: "Array.<string>"},
			{JSONName: "execution_prompt", JSType: "string"},
			{JSONName: "depends_on", JSType: "Array.<string>"},
			{JSONName: "scheduled_at", JSType: "string", Nullable: true, Comment: "ISO 8601 timestamp"},
			{JSONName: "forked_from", JSType: "string", Nullable: true},
			{JSONName: "failure_category", JSType: "string"},
			{JSONName: "truncated_turns", JSType: "Array.<number>"},
		},
	},
	{
		Name:    "TaskEvent",
		Comment: "A single event in a task's audit trail (event sourcing).",
		Fields: []jsField{
			{JSONName: "id", JSType: "number"},
			{JSONName: "task_id", JSType: "string"},
			{JSONName: "event_type", JSType: "EventType"},
			{JSONName: "data", JSType: "any"},
			{JSONName: "created_at", JSType: "string", Comment: "ISO 8601 timestamp"},
		},
	},
	{
		Name:       "TaskKind",
		Comment:    "Identifies the execution mode for a task.",
		EnumValues: []string{"", "idea-agent"},
	},
	{
		Name:       "TaskStatus",
		Comment:    "Lifecycle state of a task.",
		EnumValues: []string{"backlog", "in_progress", "waiting", "committing", "done", "failed", "cancelled", "archived"},
	},
	{
		Name:    "TaskUsage",
		Comment: "Token consumption and cost for a task across all turns.",
		Fields: []jsField{
			{JSONName: "input_tokens", JSType: "number"},
			{JSONName: "output_tokens", JSType: "number"},
			{JSONName: "cache_read_input_tokens", JSType: "number"},
			{JSONName: "cache_creation_input_tokens", JSType: "number"},
			{JSONName: "cost_usd", JSType: "number"},
		},
	},
}

// GenerateJSTypes returns the content for ui/js/generated/types.js.
//
// The emitted file contains only JSDoc @typedef declarations for the core Go
// domain types defined in internal/store/models.go. It contains no executable
// code — it is a type-only module for IDE tooling and documentation purposes.
//
// This function is deterministic: calling it twice produces identical output,
// which enables the staleness test.
func GenerateJSTypes() string {
	var b bytes.Buffer

	fmt.Fprint(&b, "// GENERATED — DO NOT EDIT MANUALLY.\n")
	fmt.Fprint(&b, "// Regenerate with: make api-contract\n")
	fmt.Fprint(&b, "// Source: internal/apicontract/routes.go, internal/store/models.go\n")

	for _, td := range jsTypeRegistry {
		fmt.Fprint(&b, "\n")
		if len(td.EnumValues) > 0 {
			// String-union typedef.
			fmt.Fprint(&b, "/**\n")
			if td.Comment != "" {
				fmt.Fprintf(&b, " * %s\n", td.Comment)
				fmt.Fprint(&b, " *\n")
			}
			parts := make([]string, len(td.EnumValues))
			for i, v := range td.EnumValues {
				parts[i] = "'" + v + "'"
			}
			fmt.Fprintf(&b, " * @typedef {%s} %s\n", strings.Join(parts, "|"), td.Name)
			fmt.Fprint(&b, " */\n")
		} else {
			// Object typedef with @property lines.
			fmt.Fprint(&b, "/**\n")
			if td.Comment != "" {
				fmt.Fprintf(&b, " * %s\n", td.Comment)
				fmt.Fprint(&b, " *\n")
			}
			fmt.Fprintf(&b, " * @typedef {Object} %s\n", td.Name)
			for _, f := range td.Fields {
				jsType := f.JSType
				if f.Nullable {
					jsType = jsType + "|null"
				}
				if f.Comment != "" {
					fmt.Fprintf(&b, " * @property {%s} %s - %s\n", jsType, f.JSONName, f.Comment)
				} else {
					fmt.Fprintf(&b, " * @property {%s} %s\n", jsType, f.JSONName)
				}
			}
			fmt.Fprint(&b, " */\n")
		}
	}

	return b.String()
}

// routeJSON is the JSON representation emitted to docs/internals/api-contract.json.
type routeJSON struct {
	Method      string   `json:"method"`
	Pattern     string   `json:"pattern"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

// GenerateContractJSON returns the pretty-printed JSON content for
// docs/internals/api-contract.json. It is deterministic and reflects Routes
// exactly so the staleness test can diff it against the committed file.
func GenerateContractJSON() ([]byte, error) {
	rs := make([]routeJSON, len(Routes))
	for i, r := range Routes {
		rs[i] = routeJSON{
			Method:      r.Method,
			Pattern:     r.Pattern,
			Name:        r.Name,
			Description: r.Description,
			Tags:        r.Tags,
		}
	}
	type contract struct {
		GeneratedFrom string      `json:"generated_from"`
		RouteCount    int         `json:"route_count"`
		Routes        []routeJSON `json:"routes"`
	}
	c := contract{
		GeneratedFrom: "internal/apicontract/routes.go",
		RouteCount:    len(Routes),
		Routes:        rs,
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	// Append a trailing newline for POSIX compliance.
	return append(b, '\n'), nil
}

// GenerateRoutesJS returns the content for ui/js/generated/routes.js.
//
// The emitted file defines a global Routes object with typed path-builder
// functions organised by API namespace, plus a top-level task(id) shorthand
// covering all task-specific endpoints (e.g. task(id).diff()).
//
// This function is deterministic: calling it twice with the same Routes
// produces identical output, which enables the staleness test.
func GenerateRoutesJS() string {
	// Partition routes into task-specific (pattern starts with /api/tasks/{id})
	// and collection-level (everything else).
	var taskRoutes, collectionRoutes []Route
	for _, r := range Routes {
		if strings.HasPrefix(r.Pattern, "/api/tasks/{id}") {
			taskRoutes = append(taskRoutes, r)
		} else {
			collectionRoutes = append(collectionRoutes, r)
		}
	}

	// Build namespace order and map from collection routes.
	// Preserve insertion order so the output is stable.
	nsOrder := []string{}
	nsMap := map[string][]Route{}
	for _, r := range collectionRoutes {
		ns := namespaceOf(r.Pattern)
		if _, seen := nsMap[ns]; !seen {
			nsOrder = append(nsOrder, ns)
		}
		nsMap[ns] = append(nsMap[ns], r)
	}

	var b bytes.Buffer

	fmt.Fprint(&b, "// GENERATED — DO NOT EDIT MANUALLY.\n")
	fmt.Fprint(&b, "// Regenerate with: make api-contract\n")
	fmt.Fprint(&b, "// Source: internal/apicontract/routes.go\n")
	fmt.Fprint(&b, "//\n")
	fmt.Fprint(&b, "// Usage:\n")
	fmt.Fprint(&b, "//   fetch(Routes.env.get())                 // GET /api/env\n")
	fmt.Fprint(&b, "//   fetch(task(id).diff())                  // GET /api/tasks/<id>/diff\n")
	fmt.Fprint(&b, "//   new EventSource(Routes.tasks.stream())  // GET /api/tasks/stream\n")
	fmt.Fprint(&b, "\n")
	fmt.Fprint(&b, "/* global Routes, task */\n")
	fmt.Fprint(&b, "\n")
	fmt.Fprint(&b, "var Routes = {\n")

	// Emit non-tasks namespaces first (stable order from Routes).
	for _, ns := range nsOrder {
		if ns == "tasks" {
			continue // emitted last so we can append the task(id) sub-builder
		}
		emitNamespace(&b, ns, nsMap[ns])
	}

	// Emit tasks namespace: collection routes + task(id) sub-builder.
	fmt.Fprint(&b, "\n  tasks: {\n")

	emitted := map[string]bool{}
	for _, r := range nsMap["tasks"] {
		jsName := jsMethodName(r, "tasks")
		if jsName == "" || emitted[jsName] {
			continue
		}
		emitted[jsName] = true
		fmt.Fprintf(&b, "    // %s %s\n", r.Method, r.Pattern)
		fmt.Fprintf(&b, "    %s: function() { return %q; },\n", jsName, r.Pattern)
	}

	// task(id) sub-builder.
	fmt.Fprint(&b, "\n    // task(id) returns an object with path-builder methods for\n")
	fmt.Fprint(&b, "    // all task-instance endpoints. Use the top-level task() alias.\n")
	fmt.Fprint(&b, "    task: function(id) {\n")
	fmt.Fprint(&b, "      return {\n")

	taskEmitted := map[string]bool{}
	for _, r := range taskRoutes {
		jsName := jsTaskMethodName(r)
		if jsName == "" || taskEmitted[jsName] {
			continue
		}
		taskEmitted[jsName] = true
		extra := extraParams(r.Pattern)
		body := buildTaskPathExpr(r.Pattern)
		fmt.Fprintf(&b, "        // %s %s\n", r.Method, r.Pattern)
		fmt.Fprintf(&b, "        %s: function(%s) { return %s; },\n", jsName, extra, body)
	}

	fmt.Fprint(&b, "      };\n")
	fmt.Fprint(&b, "    },\n")
	fmt.Fprint(&b, "  },\n")
	fmt.Fprint(&b, "\n};\n")
	fmt.Fprint(&b, "\n")
	fmt.Fprint(&b, "// Convenience alias: task(id).diff(), task(id).logs(), etc.\n")
	fmt.Fprint(&b, "var task = Routes.tasks.task;\n")

	return b.String()
}

// emitNamespace writes one namespace block into b.
func emitNamespace(b *bytes.Buffer, ns string, routes []Route) {
	fmt.Fprintf(b, "\n  %s: {\n", ns)
	emitted := map[string]bool{}
	for _, r := range routes {
		jsName := jsMethodName(r, ns)
		if jsName == "" || emitted[jsName] {
			continue
		}
		emitted[jsName] = true
		fmt.Fprintf(b, "    // %s %s\n", r.Method, r.Pattern)
		fmt.Fprintf(b, "    %s: function() { return %q; },\n", jsName, r.Pattern)
	}
	fmt.Fprint(b, "  },\n")
}

// namespaceOf returns the second URL path segment (after /api/) for a pattern.
// "/api/tasks/{id}/events" → "tasks".
func namespaceOf(pattern string) string {
	s := strings.TrimPrefix(pattern, "/api/")
	if idx := strings.Index(s, "/"); idx >= 0 {
		return s[:idx]
	}
	return s
}

// jsMethodName returns the JS method name for a collection-level route (no {id}).
// It uses Route.JSName when set; otherwise derives the name from the path suffix
// after /api/<namespace>/ by converting kebab-case and slashes to camelCase.
func jsMethodName(r Route, ns string) string {
	if r.JSName != "" {
		return r.JSName
	}
	prefix := "/api/" + ns
	suffix := strings.TrimPrefix(r.Pattern, prefix)
	suffix = strings.TrimPrefix(suffix, "/")
	if suffix == "" {
		// Route is the namespace root itself; JSName must be set explicitly.
		return ""
	}
	// Remove path-parameter placeholders.
	parts := strings.Split(suffix, "/")
	var clean []string
	for _, p := range parts {
		if !strings.HasPrefix(p, "{") {
			clean = append(clean, p)
		}
	}
	return kebabSlashToCamel(strings.Join(clean, "/"))
}

// jsTaskMethodName returns the JS method name for a task-specific route
// (pattern starts with /api/tasks/{id}).
// It uses Route.JSName when set; otherwise derives from the suffix after {id}.
func jsTaskMethodName(r Route) string {
	if r.JSName != "" {
		return r.JSName
	}
	suffix := strings.TrimPrefix(r.Pattern, "/api/tasks/{id}")
	suffix = strings.TrimPrefix(suffix, "/")
	if suffix == "" {
		// The route is exactly /api/tasks/{id}; JSName must be set explicitly.
		return ""
	}
	parts := strings.Split(suffix, "/")
	var clean []string
	for _, p := range parts {
		if !strings.HasPrefix(p, "{") {
			clean = append(clean, p)
		}
	}
	return kebabSlashToCamel(strings.Join(clean, "/"))
}

// kebabSlashToCamel converts a kebab-case or slash-separated path to camelCase.
// "rebase-on-main" → "rebaseOnMain", "refine/logs" → "refineLogs".
func kebabSlashToCamel(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '/' })
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// buildTaskPathExpr builds the JS expression that constructs the path for a
// task-specific route. {id} is replaced by the `id` closure variable; any
// additional path parameters (e.g. {filename}) become function arguments.
func buildTaskPathExpr(pattern string) string {
	s := pattern
	s = strings.ReplaceAll(s, "{id}", `" + id + "`)
	s = strings.ReplaceAll(s, "{filename}", `" + filename + "`)
	s = `"` + s + `"`
	// Remove empty-string fragments that arise when a placeholder is at the end.
	s = strings.ReplaceAll(s, `""`, "")
	// Remove any trailing "+ " left after the last variable.
	s = strings.TrimSuffix(s, ` + `)
	return s
}

// extraParams returns the extra JS function parameter list (beyond the `id`
// closure variable) needed for routes with additional path placeholders.
func extraParams(pattern string) string {
	if strings.Contains(pattern, "{filename}") {
		return "filename"
	}
	return ""
}
