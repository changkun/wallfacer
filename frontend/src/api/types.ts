export interface Me {
  sub: string;
  email: string;
  name: string;
  picture?: string;
  auth_url?: string;
}

// Local-mode RFC 8628 device-code sign-in. POST /api/auth/device/start returns
// the code + verification URL; GET /api/auth/device/poll reports progress.
export interface DeviceStartResponse {
  verification_uri: string;
  verification_uri_complete?: string;
  user_code: string;
  expires_in: number;
}

export interface DevicePollResponse {
  status: 'idle' | 'pending' | 'done' | 'denied' | 'expired' | 'failed';
  error?: string;
}

export type TaskStatus = 'backlog' | 'in_progress' | 'waiting' | 'committing' | 'done' | 'failed' | 'cancelled';

// --- Unified spec+task graph (GET /api/graph) ---
// Mirrors internal/graph.Graph. The Map renders and drives this.

export type GraphNodeKind = 'spec' | 'task';
export type GraphEdgeKind = 'containment' | 'dispatch' | 'spec_dep' | 'task_dep';
export type GraphAction =
  | 'dispatch'
  | 'undispatch'
  | 'validate'
  | 'force-complete'
  | 'unstale'
  | 'unarchive'
  | 'start';

export interface GraphNode {
  id: string; // "spec:<path>" or "task:<uuid>"
  kind: GraphNodeKind;
  label: string;
  status: string; // spec lifecycle or task status
  ref: string; // spec path or task id, for deep-jumps + actions
  depth: number;
  available_actions?: GraphAction[];
}

export interface GraphEdge {
  from: string;
  to: string;
  kind: GraphEdgeKind;
}

export interface Graph {
  nodes: GraphNode[];
  edges: GraphEdge[];
  critical_path: string[];
  blocked: string[];
}

export interface TaskUsage {
  input_tokens: number;
  output_tokens: number;
  cache_read_input_tokens: number;
  cache_creation_input_tokens: number;
  cost_usd: number;
}

export interface Task {
  id: string;
  title: string;
  title_generating?: boolean;
  prompt: string;
  criteria?: string;
  status: TaskStatus;
  archived: boolean;
  result: string | null;
  stop_reason: string | null;
  turns: number;
  timeout: number;
  usage: TaskUsage;
  sandbox: string;
  position: number;
  created_at: string;
  updated_at: string;
  branch_name: string;
  commit_message: string;
  model: string;
  kind: string;
  tags: string[];
  impact_score?: number;
  depends_on: string[];
  failure_category: string;
  fresh_start: boolean;
  is_test_run: boolean;
  last_test_result: string;
  session_id: string | null;
  worktree_paths: Record<string, string>;
  usage_breakdown: Record<string, TaskUsage>;
  routine_interval_seconds?: number;
  routine_enabled?: boolean;
  routine_next_run?: string | null;
  routine_last_fired_at?: string | null;
  // Budget guardrails — 0 / missing means unlimited.
  max_cost_usd?: number;
  max_input_tokens?: number;
  test_run_start_turn?: number;
  scheduled_at?: string | null;
  prompt_history?: string[];
  retry_history?: RetryRecord[];
  parent_task_id?: string | null;
  spec_source_path?: string;
  environment?: ExecutionEnvironment | null;
  // Review verdict: open findings when the last review session ended (0 =
  // approved). Absent = no review has finished since the task's last turn.
  review_unresolved?: number;
  review_headline?: string;
  // Present (non-empty string) only for tasks that ran on the in-process
  // topos harness: the opaque JSON of the run's trace. The thin parsed shape
  // is served by GET /api/tasks/{id}/trace (see AgentTrace).
  trace?: string | null;
}

// Trace of an in-process run (GET /api/tasks/{id}/trace). status is the node
// lifecycle; kind is the handoff type between agents. A native run records
// one node and no edge; traces stored by earlier multi-agent runs also carry
// handoff edges, and they still render.
export type TraceNodeStatus = 'running' | 'done' | 'failed' | string;
export type TraceEdgeKind = 'delegate' | 'deliver' | 'next' | string;
export interface TraceNode {
  id: string;
  name: string;
  role: string;
  status: TraceNodeStatus;
  grants?: string[];
  sandbox?: string;
}
export interface TraceEdge {
  from: string;
  to: string;
  kind: TraceEdgeKind;
}
export interface TaskTrace {
  nodes: TraceNode[];
  edges: TraceEdge[];
}

// Review transcript (GET /api/tasks/{id}/review/transcript): the task's newest
// review session, round by round.
export interface ReviewFinding {
  severity: 'high' | 'medium' | 'low' | string;
  claim: string;
  location?: string;
}
export interface ReviewerAnswer {
  verdict: 'approve' | 'changes_requested' | string;
  findings: ReviewFinding[];
  summary?: string;
  model?: string;
  harness?: string;
  tokens?: number;
  ts: string;
}
export interface ReviewFailedAttempt {
  code: string;
  message: string;
  // The reviewer's output when it answered in a form that could not be read.
  raw?: string;
  ts: string;
}
export interface ReviewRound {
  round: number;
  reviewer?: ReviewerAnswer;
  failed_attempts?: ReviewFailedAttempt[];
  // The message the findings were sent to the task in.
  feedback?: string;
  // The task's reply after the turn the feedback started.
  reply?: string;
}
export interface ReviewRunConfig {
  max_rounds: number;
  cost_cap: number;
  // Empty when WALLFACER_REVIEW_MODEL is not set.
  reviewer_model: string;
}
export interface ReviewSkip {
  code: string;
  message: string;
  detail?: string;
}
export interface ReviewOutcome {
  termination: 'approved' | 'max_rounds' | 'cost_cap' | 'skipped' | 'superseded' | 'unreadable' | string;
  rounds: number;
  unresolved: number;
  headline?: string;
  tokens: number;
  usd: number;
  skip?: ReviewSkip;
}
export interface ReviewTranscript {
  session_id: string;
  running: boolean;
  // Recorded by an earlier version of the review; its rounds are not shown.
  legacy?: boolean;
  config?: ReviewRunConfig;
  outcome?: ReviewOutcome;
  rounds: ReviewRound[];
  // Set when the server could not read the transcript to its end; rounds then
  // holds only the rounds before that point.
  truncated?: boolean;
}

// Runtime environment captured at the start of a task run (reproducibility
// provenance). Mirrors store.ExecutionEnvironment.
export interface ExecutionEnvironment {
  container_image?: string;
  container_digest?: string;
  model_name?: string;
  api_base_url?: string;
  sandbox?: string;
  recorded_at?: string;
}

export interface RetryRecord {
  retired_at: string;
  prompt: string;
  status: string;
  result?: string;
  session_id?: string;
  turns: number;
  cost_usd: number;
  failure_category?: string;
}

// --- Workspace registry (GET/POST/PUT/DELETE /api/workspaces) ---
// A workspace is a first-class object with a stable id, owned by a user/org,
// holding a mutable set of folder paths. Identity is decoupled from membership:
// editing folders never loses history. `dormant` marks a workspace recovered
// from history whose folders may need re-pointing; `active` marks the one whose
// board is currently shown.
export interface Workspace {
  id: string;
  name: string;
  folders: string[];
  dormant: boolean;
  active: boolean;
  // Per-workspace parallelism overrides. null/absent means "use the global
  // default". Surfaced as the registry editor's Max parallel / Max test
  // parallel inputs.
  max_parallel?: number | null;
  max_test_parallel?: number | null;
}

export interface WorkspaceGroup {
  // id ties a saved group back to its first-class workspace in the registry, so
  // group actions (switch / rename / delete) route through the id-based
  // endpoints instead of the legacy path-based PUT.
  id?: string;
  name?: string;
  workspaces: string[];
  key: string;
  max_parallel?: number;
  max_test_parallel?: number;
}

export interface ServerConfig {
  workspaces: string[];
  // workspace_id is the stable id of the active workspace (the new workspace
  // model). Absent on older payloads; consumers fall back to folder basenames.
  workspace_id?: string;
  workspace_browser_path?: string;
  workspace_groups?: WorkspaceGroup[];
  prompts_dir?: string;
  autoimplement: boolean;
  autotest: boolean;
  autosubmit: boolean;
  autosync: boolean;
  autopush: boolean;
  max_parallel: number;
  sandboxes: string[];
  // Per-harness usability: id -> installed & activated. Absent = treat as usable.
  sandbox_usable?: Record<string, boolean>;
  // Harnesses the chat runtime can launch: the subprocess harnesses. An
  // in-process harness (topos) can be usable for tasks and still be absent here.
  chat_sandboxes?: string[];
  default_sandbox: string;
  terminal_enabled: boolean;
  auth_enabled: boolean;
  // Whether the browser redirect behind /login can complete on this instance.
  // False when the server is bound to a port its redirect URL does not name.
  auth_redirect_enabled?: boolean;
  ideation_categories?: string[];
  active_groups?: { key: string; in_progress: number; waiting: number }[];
}

export interface EnvConfig {
  secret_store?: 'file' | 'keyring';
  oauth_token: string;
  api_key: string;
  base_url: string;
  openai_api_key: string;
  openai_base_url: string;
  cursor_api_key: string;
  default_model: string;
  title_model: string;
  codex_default_model: string;
  codex_title_model: string;
  default_sandbox: string;
  sandbox_by_activity?: Record<string, string>;
  max_parallel_tasks: number;
  max_test_parallel_tasks: number;
  max_agents: number;
  agent_nice: number;
  review_forks: number;
  review_rounds: number;
  review_cost_cap: number;
  oversight_interval: number;
  archived_tasks_per_page: number;
  auto_push_enabled: boolean;
  auto_push_threshold: number;
}

export interface EnvUpdatePayload {
  secret_store?: 'file' | 'keyring';
  oauth_token?: string;
  api_key?: string;
  base_url?: string;
  openai_api_key?: string;
  openai_base_url?: string;
  cursor_api_key?: string;
  default_model?: string;
  title_model?: string;
  codex_default_model?: string;
  codex_title_model?: string;
  default_sandbox?: string;
  sandbox_by_activity?: Record<string, string>;
  max_parallel_tasks?: number;
  max_test_parallel_tasks?: number;
  max_agents?: number;
  agent_nice?: number;
  review_forks?: number;
  review_rounds?: number;
  review_cost_cap?: number;
  oversight_interval?: number;
  archived_tasks_per_page?: number;
  auto_push_enabled?: boolean;
  auto_push_threshold?: number;
}

export interface SystemPromptTemplate {
  name: string;
  has_override: boolean;
  content: string;
}

export interface SandboxTestResponse {
  task_id: string;
  sandbox: string;
  status: string;
  last_test_result?: string;
  result?: string;
  stop_reason?: string;
  reauth_available?: boolean;
}

export interface TaskCommit {
 repository: string;
 hash: string;
 subject: string;
 author: string;
 authored_at: string;
 attempt: number;
 turn: number;
 patch: string;
 patch_truncated?: boolean;
}
