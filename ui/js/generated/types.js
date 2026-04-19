// GENERATED — DO NOT EDIT MANUALLY.
// Regenerate with: make api-contract
// Source: internal/apicontract/routes.go, internal/store/models.go

/**
 * Identifies the kind of event stored in a task's audit trail.
 *
 * @typedef {'state_change'|'output'|'feedback'|'error'|'system'|'span_start'|'span_end'} EventType
 */

/**
 * Captures the runtime environment used for a task execution.
 *
 * @typedef {Object} ExecutionEnvironment
 * @property {string} container_image
 * @property {string} container_digest
 * @property {string} model_name
 * @property {string} api_base_url
 * @property {string} instructions_hash
 * @property {string} sandbox
 * @property {string} recorded_at - ISO 8601 timestamp
 */

/**
 * Core domain model: a unit of work executed by an agent.
 *
 * @typedef {Object} Task
 * @property {number} schema_version
 * @property {string} id
 * @property {string} title
 * @property {string} prompt
 * @property {Array.<string>} prompt_history
 * @property {Array.<Object>} retry_history
 * @property {TaskStatus} status
 * @property {boolean} archived
 * @property {string|null} session_id
 * @property {boolean} fresh_start
 * @property {string|null} result
 * @property {string|null} stop_reason
 * @property {number} turns
 * @property {number} timeout
 * @property {number} max_cost_usd
 * @property {number} max_input_tokens
 * @property {TaskUsage} usage
 * @property {string} sandbox
 * @property {Object.<string, string>} sandbox_by_activity
 * @property {Object.<string, TaskUsage>} usage_breakdown
 * @property {ExecutionEnvironment|null} environment
 * @property {number} position
 * @property {string} created_at - ISO 8601 timestamp
 * @property {string} updated_at - ISO 8601 timestamp
 * @property {Object.<string, string>} worktree_paths
 * @property {string} branch_name
 * @property {Object.<string, string>} commit_hashes
 * @property {Object.<string, string>} base_commit_hashes
 * @property {string} commit_message
 * @property {boolean} mount_worktrees
 * @property {string} model
 * @property {string|null} model_override
 * @property {boolean} is_test_run
 * @property {string} last_test_result
 * @property {number} test_run_start_turn
 * @property {TaskKind} kind
 * @property {Array.<string>} tags
 * @property {string} execution_prompt
 * @property {Array.<string>} depends_on
 * @property {string|null} scheduled_at - ISO 8601 timestamp
 * @property {string} failure_category
 * @property {Array.<number>} truncated_turns
 */

/**
 * A single event in a task's audit trail (event sourcing).
 *
 * @typedef {Object} TaskEvent
 * @property {number} id
 * @property {string} task_id
 * @property {EventType} event_type
 * @property {any} data
 * @property {string} created_at - ISO 8601 timestamp
 */

/**
 * Identifies the execution mode for a task.
 *
 * @typedef {''|'idea-agent'} TaskKind
 */

/**
 * Lifecycle state of a task.
 *
 * @typedef {'backlog'|'in_progress'|'waiting'|'committing'|'done'|'failed'|'cancelled'|'archived'} TaskStatus
 */

/**
 * Token consumption and cost for a task across all turns.
 *
 * @typedef {Object} TaskUsage
 * @property {number} input_tokens
 * @property {number} output_tokens
 * @property {number} cache_read_input_tokens
 * @property {number} cache_creation_input_tokens
 * @property {number} cost_usd
 */
