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
}

export interface ServerConfig {
  workspaces: string[];
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
