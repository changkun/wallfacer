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
  instructions_path?: string;
  prompts_dir?: string;
  autopilot: boolean;
  autotest: boolean;
  autosubmit: boolean;
  autosync: boolean;
  autopush: boolean;
  max_parallel: number;
  sandboxes: string[];
  default_sandbox: string;
  host_mode: boolean;
  terminal_enabled: boolean;
  cloud_mode: boolean;
}

export interface EnvConfig {
  oauth_token: string;
  api_key: string;
  base_url: string;
  openai_api_key: string;
  openai_base_url: string;
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
  sandbox_fast: boolean;
  container_network: string;
  container_cpus: string;
  container_memory: string;
}

export interface EnvUpdatePayload {
  oauth_token?: string;
  api_key?: string;
  base_url?: string;
  openai_api_key?: string;
  openai_base_url?: string;
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
  sandbox_fast?: boolean;
  container_cpus?: string;
  container_memory?: string;
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

export interface SandboxImageInfo {
  sandbox: string;
  image: string;
  cached: boolean;
  size?: string;
}

export interface SandboxImagesResponse {
  images: SandboxImageInfo[];
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
