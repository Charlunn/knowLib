// Tiny typed fetch wrapper. Goes through our /api/* proxy route, which attaches
// the JWT cookie and forwards to API_INTERNAL_URL.
//
// All paths are relative to the Next.js origin so cookies are sent by default.

import type {
  ApiToken,
  CaptureReq,
  CaptureRes,
  Note,
  SearchHit,
  Settings,
  SettingsUpdate,
  TidyReq,
  TidyRes,
  AIOpsResponse,
} from './types';

export class ApiError extends Error {
  status: number;
  body: unknown;
  constructor(status: number, message: string, body: unknown) {
    super(message);
    this.status = status;
    this.body = body;
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !headers.has('content-type')) {
    headers.set('content-type', 'application/json');
  }
  const res = await fetch(path, {
    ...init,
    headers,
    credentials: 'include',
    cache: 'no-store',
  });
  const ct = res.headers.get('content-type') ?? '';
  const isJson = ct.includes('application/json');
  // 204 / empty body shortcut
  if (res.status === 204) {
    return undefined as T;
  }
  // Non-JSON path (e.g. binary downloads handled outside this helper)
  if (!isJson) {
    if (!res.ok) {
      throw new ApiError(res.status, res.statusText, await res.text());
    }
    return undefined as T;
  }
  const data: unknown = await res.json();
  if (!res.ok) {
    const msg =
      data && typeof data === 'object' && 'error' in data && typeof (data as { error: unknown }).error === 'string'
        ? (data as { error: string }).error
        : res.statusText;
    throw new ApiError(res.status, msg, data);
  }
  return data as T;
}

export const api = {
  async login(totp: string): Promise<{ ok: true }> {
    return request('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ totp }),
    });
  },

  async logout(): Promise<void> {
    await request('/api/auth/logout', { method: 'POST' });
  },

  async capture(req: CaptureReq): Promise<CaptureRes> {
    return request<CaptureRes>('/api/capture', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  },

  async inbox(): Promise<Note[]> {
    return request<Note[]>('/api/inbox');
  },

  async tidy(req: TidyReq): Promise<TidyRes> {
    return request<TidyRes>('/api/tidy', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  },

  async search(q: string, k = 10): Promise<SearchHit[]> {
    const params = new URLSearchParams({ q, k: String(k) });
    return request<SearchHit[]>(`/api/search?${params}`);
  },

  async note(path: string): Promise<Note> {
    const params = new URLSearchParams({ path });
    return request<Note>(`/api/note?${params}`);
  },

  async list(category?: string): Promise<Note[]> {
    const params = new URLSearchParams();
    if (category) params.set('category', category);
    const qs = params.toString();
    return request<Note[]>(`/api/list${qs ? `?${qs}` : ''}`);
  },

  async getSettings(): Promise<Settings> {
    return request<Settings>('/api/settings');
  },

  async updateSettings(s: SettingsUpdate): Promise<Settings> {
    return request<Settings>('/api/settings', {
      method: 'PUT',
      body: JSON.stringify(s),
    });
  },

  async testLlm(prompt: string): Promise<{ output: string }> {
    return request<{ output: string }>('/api/settings/test-llm', {
      method: 'POST',
      body: JSON.stringify({ prompt }),
    });
  },

  async listTokens(): Promise<ApiToken[]> {
    return request<ApiToken[]>('/api/tokens');
  },

  async createToken(name: string): Promise<{ token: string; meta: ApiToken }> {
    return request<{ token: string; meta: ApiToken }>('/api/tokens', {
      method: 'POST',
      body: JSON.stringify({ name }),
    });
  },

  async revokeToken(id: string): Promise<void> {
    await request<void>(`/api/tokens/${encodeURIComponent(id)}`, { method: 'DELETE' });
  },

  async aiAction(req: {
    action: 'deep_tidy' | 'polish_logic' | 'rewrite' | 'knowledge_check';
    paths?: string[];
    folder?: string;
    preview_only?: boolean;
  }): Promise<AIOpsResponse> {
    return request<AIOpsResponse>('/api/ai-action', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  },

  async applyAiAction(resp: AIOpsResponse): Promise<{ applied: true }> {
    return request<{ applied: true }>('/api/ai-action/apply', {
      method: 'POST',
      body: JSON.stringify(resp),
    });
  },
};
