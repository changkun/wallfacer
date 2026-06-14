export interface Me {
  sub: string;
  email: string;
  name: string;
  picture?: string;
  auth_url?: string;
}

export type TaskStatus = 'backlog' | 'in_progress' | 'waiting' | 'committing' | 'done' | 'failed' | 'cancelled';

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

export interface WorkspaceGroup {
  name?: string;
  workspaces: string[];
  key: string;
  max_parallel?: number;
  max_test_parallel?: number;
}

export interface ServerConfig {
  workspaces: string[];
  workspace_browser_path?: string;
  workspace_groups?: WorkspaceGroup[];
  prompts_dir?: string;
  autopilot: boolean;
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
