// Mirrors packages/openapi/openapi.yaml. Keep in sync.

export type CaptureSource = 'web' | 'obsidian' | 'telegram' | 'api' | 'feishu' | 'mcp';

export interface CaptureReq {
  content: string;
  source?: CaptureSource;
  ts?: string;
}

export interface CaptureRes {
  path: string;
}

export interface Note {
  path: string;
  title: string;
  category?: string;
  tags?: string[];
  frontmatter?: Record<string, unknown>;
  body?: string;
}

export interface SearchHit {
  path: string;
  title: string;
  snippet: string;
  score: number;
}

export interface TidyReq {
  paths?: string[];
  all?: boolean;
}

export interface TidyResultItem {
  source_path: string;
  target_path: string;
  status: 'ok' | 'partial' | 'failed';
  reason?: string;
}

export interface TidyRes {
  job_id?: string;
  items?: TidyResultItem[];
}

export type TidyMode = 'manual' | 'scheduled' | 'threshold';

export interface Settings {
  llm?: {
    base_url?: string;
    model?: string;
    api_key_set?: boolean;
  };
  embed?: {
    base_url?: string;
    model?: string;
  };
  tidy?: {
    top_k?: number;
    max_tokens?: number;
    mode?: TidyMode;
    cron?: string;
    prompt?: string;
  };
  auto_tidy?: {
    enabled?: boolean;
    threshold?: number;
    cron_spec?: string;
  };
}

export interface SettingsUpdate {
  llm?: {
    base_url?: string;
    model?: string;
    api_key?: string;
  };
  embed?: {
    base_url?: string;
    model?: string;
  };
  tidy?: {
    top_k?: number;
    max_tokens?: number;
    mode?: TidyMode;
    cron?: string;
    prompt?: string;
  };
  auto_tidy?: {
    enabled?: boolean;
    threshold?: number;
    cron_spec?: string;
  };
}

export interface ApiToken {
  id: string;
  name: string;
  created_at: string;
  last_used_at?: string;
}
