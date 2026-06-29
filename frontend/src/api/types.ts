// --- Agent registry (GET /api/agents) ---
// The merged built-in + user-authored agent catalog. Mirrors the handler's
// agent response shape; the agent-graph palette renders these as nodes.
export interface Agent {
  slug: string;
  title: string;
  description?: string;
  capabilities?: string[];
  multiturn?: boolean;
  harness?: string;
  builtin: boolean;
}

// --- Flow registry (GET /api/flows) ---
// A flow is an ordered composition of agent steps. FlowStep mirrors the
// handler's StepResponse; agent_name is resolved server-side so the UI needs
// no second round-trip per step.
export interface FlowStep {
  agent_slug: string;
  agent_name?: string;
  optional?: boolean;
  input_from?: string;
  run_in_parallel_with?: string[];
}

// FlowTopology mirrors flow.Topology: whom an agent in a dynamic agentic flow
// may delegate to. orchestrator-worker pins delegation to the entry agent;
// mesh lets any agent delegate recursively.
export type FlowTopology = 'orchestrator-worker' | 'mesh';

export interface Flow {
  slug: string;
  name: string;
  description?: string;
  builtin: boolean;
  steps?: FlowStep[];
  // M3 agentic fields, serialized by GET /api/flows (see
  // internal/handler/flows.go FlowResponse / describeFlow) and accepted on
  // POST/PUT. They are omitted for ordinary flows, so they arrive undefined for
  // a non-agentic flow; the agent-graph topology indicator reads them
  // defensively.
  agentic?: boolean;
  dynamic?: boolean;
  topology?: FlowTopology;
  max_handoff_depth?: number;
}

export interface Me {
  sub: string;
  email: string;
  name: string;
  picture?: string;
  auth_url?: string;
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
  routine_spawn_kind?: string;
  routine_spawn_flow?: string;
  // flow_id is the fleet (flow slug) this task ran against; used by the
  // agent-graph run overlay to find a fleet's runs.
  flow_id?: string;
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
  // Agon adversarial-verification results. Absent = not yet run.
  agon_unresolved?: number;
  agon_headline?: string;
  // Present (non-empty string) only for tasks run via the agentic flow kind;
  // the opaque JSON of the run's agent-graph lineage. The thin parsed shape is
  // served by GET /api/tasks/{id}/lineage (see AgentLineage).
  lineage?: string | null;
}

// Agent-graph lineage of an agentic-flow run (GET /api/tasks/{id}/lineage).
// status is the node lifecycle; kind is the handoff type between agents.
export type LineageNodeStatus = 'running' | 'done' | 'failed' | string;
export type LineageEdgeKind = 'delegate' | 'deliver' | 'next' | string;
export interface LineageNode {
  id: string;
  name: string;
  role: string;
  status: LineageNodeStatus;
  grants?: string[];
  sandbox?: string;
}
export interface LineageEdge {
  from: string;
  to: string;
  kind: LineageEdgeKind;
}
export interface TaskLineage {
  nodes: LineageNode[];
  edges: LineageEdge[];
}

// Agon verification trajectory (GET /api/tasks/{id}/agon/transcript).
export interface AgonRound {
  round: number;
  role: 'critic' | 'proposer' | string;
  body: string;
  ts: string;
}
export interface AgonFork {
  index: number;
  rounds: AgonRound[];
}
export interface AgonRunConfig {
  forks: number;
  max_rounds: number;
  cost_cap: number;
  proposer_model: string;
  critic_models: string[];
}
export interface AgonOutcome {
  termination: string;
  total_attacks: number;
  by_status: Record<string, number>;
  wall_seconds: number;
  tokens: number;
}
export interface AgonTranscript {
  session_id: string;
  running: boolean;
  config?: AgonRunConfig;
  outcome?: AgonOutcome;
  forks: AgonFork[];
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
}

export interface WorkspaceGroup {
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
  default_sandbox: string;
  terminal_enabled: boolean;
  auth_enabled: boolean;
  ideation_categories?: string[];
  active_groups?: { key: string; in_progress: number; waiting: number }[];
}

export interface EnvConfig {
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
  agon_forks: number;
  agon_rounds: number;
  agon_cost_cap: number;
  oversight_interval: number;
  archived_tasks_per_page: number;
  auto_push_enabled: boolean;
  auto_push_threshold: number;
}

export interface EnvUpdatePayload {
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
  agon_forks?: number;
  agon_rounds?: number;
  agon_cost_cap?: number;
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

export interface PromptTemplate {
  id: string;
  name: string;
  body: string;
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
