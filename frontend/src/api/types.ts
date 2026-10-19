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
  failure_category: string;
  fresh_start: boolean;
  session_id: string | null;
  worktree_paths: Record<string, string>;
  usage_breakdown: Record<string, TaskUsage>;
  // Budget guardrails — 0 / missing means unlimited.
  max_cost_usd?: number;
  max_input_tokens?: number;
  prompt_history?: string[];
  retry_history?: RetryRecord[];
  parent_task_id?: string | null;
  spec_source_path?: string;
  environment?: ExecutionEnvironment | null;
}

// Runtime environment captured at the start of a task run (reproducibility
// provenance). Mirrors store.ExecutionEnvironment.
export interface ExecutionEnvironment {
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
// A workspace is a first-class object with a stable id and one folder.
// Identity is decoupled from the folder: re-pointing never loses history.
// `folders` holds that one folder; `dormant` marks a workspace whose folder is
// still to be chosen, holding none (recovered history) or several (saved before
// a workspace became one folder). `active` marks the one whose board is shown.
export interface Workspace {
  id: string;
  name: string;
  folders: string[];
  dormant: boolean;
  active: boolean;
  // Per-workspace parallel task limit. null/absent means "use the global
  // default". Surfaced as the registry editor's Max parallel input.
  max_parallel?: number | null;
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
}

export interface ServerConfig {
  workspaces: string[];
  // workspace_id is the stable id of the active workspace (the new workspace
  // model). Absent on older payloads; consumers fall back to folder basenames.
  workspace_id?: string;
  workspace_browser_path?: string;
  workspace_groups?: WorkspaceGroup[];
  max_parallel: number;
  sandboxes: string[];
  // Per-harness usability: id -> installed & activated. Absent = treat as usable.
  sandbox_usable?: Record<string, boolean>;
  default_sandbox: string;
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
  codex_default_model: string;
  default_sandbox: string;
  sandbox_by_activity?: Record<string, string>;
  max_parallel_tasks: number;
  max_agents: number;
  agent_nice: number;
  oversight_interval: number;
  archived_tasks_per_page: number;
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
  codex_default_model?: string;
  default_sandbox?: string;
  sandbox_by_activity?: Record<string, string>;
  max_parallel_tasks?: number;
  max_agents?: number;
  agent_nice?: number;
  oversight_interval?: number;
  archived_tasks_per_page?: number;
}

export interface SandboxTestResponse {
  task_id: string;
  sandbox: string;
  status: string;
  passed: boolean;
  result?: string;
  stop_reason?: string;
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
