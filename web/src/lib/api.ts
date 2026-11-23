export interface User {
  id: string;
  email: string;
  role: "admin" | "user";
  token_budget: number;
}

export interface MonthUsage {
  month: string;
  tokens: number;
  requests: number;
  budget: number;
}

export interface Session {
  token: string;
  expires_at: string;
  user: User;
}

export interface Doc {
  id: string;
  name: string;
  format: string;
  size: number;
  status: "processing" | "ready" | "failed";
  error?: string;
  chunks: number;
  created_at: string;
}

export interface Source {
  id: string;
  rank: number;
  search_score: number;
  rerank_score?: number;
  payload: {
    doc_name: string;
    title: string;
    section: string[];
    chunk_index: number;
    text: string;
  };
}

export interface Citation {
  index: number;
  doc_name: string;
  section: string;
  cited_text: string;
  verified: boolean;
}

export interface Segment {
  text: string;
  citations: Citation[] | null;
}

export interface Check {
  grounded: number;
  unsupported_claims: string[] | null;
  unverified_quotes: number;
  unsourced_numbers: string[] | null;
  retrieval_confidence: number;
  abstained: boolean;
  hallucination_risk: "low" | "medium" | "high";
  warnings: string[] | null;
}

export interface AskResponse {
  query_id: string;
  text: string;
  segments: Segment[];
  sources: Source[];
  check: Check;
  usage: { model: string; input_tokens: number; output_tokens: number };
  latency_ms: number;
  month_usage: MonthUsage;
}

export interface QueryLog {
  id: string;
  question: string;
  answer: string;
  risk: string;
  grounded: number;
  confidence: number;
  top_sources: string;
  tokens: number;
  latency_ms: number;
  feedback: number;
  created_at: string;
}

const KEY = "rag.session";

export function loadSession(): Session | null {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return null;
    const s = JSON.parse(raw) as Session;
    return new Date(s.expires_at) > new Date() ? s : null;
  } catch {
    return null;
  }
}

export function saveSession(s: Session | null) {
  try {
    if (s) localStorage.setItem(KEY, JSON.stringify(s));
    else localStorage.removeItem(KEY);
  } catch {
    /* private mode: the session lasts until the tab closes */
  }
  window.dispatchEvent(new Event("rag-session"));
}

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {};
  const s = loadSession();
  if (s) headers.Authorization = `Bearer ${s.token}`;
  let payload: BodyInit | undefined;
  if (body instanceof FormData) payload = body;
  else if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }
  const res = await fetch(path, { method, headers, body: payload });
  if (res.status === 401 && s) saveSession(null);
  if (res.status === 204) return undefined as T;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, (data as { error?: string }).error ?? `Request failed (${res.status})`);
  return data as T;
}

export const api = {
  register: (email: string, password: string) => call<Session>("POST", "/api/auth/register", { email, password }),
  login: (email: string, password: string) => call<Session>("POST", "/api/auth/login", { email, password }),
  me: () => call<{ user: User; usage: MonthUsage }>("GET", "/api/me"),
  documents: () => call<Doc[]>("GET", "/api/documents"),
  upload: (files: File[]) => {
    const fd = new FormData();
    files.forEach((f) => fd.append("file", f));
    return call<Doc[]>("POST", "/api/documents", fd);
  },
  deleteDocument: (id: string) => call<void>("DELETE", `/api/documents/${id}`),
  ask: (question: string) => call<AskResponse>("POST", "/api/ask", { question }),
  queries: (flagged: boolean) => call<QueryLog[]>("GET", `/api/queries${flagged ? "?flagged=1" : ""}`),
  feedback: (id: string, helpful: boolean) => call<void>("POST", `/api/queries/${id}/feedback`, { helpful }),
};

export const fmtBytes = (n: number) =>
  n < 1024 ? `${n} B` : n < 1 << 20 ? `${(n / 1024).toFixed(0)} KB` : `${(n / (1 << 20)).toFixed(1)} MB`;
export const fmtNum = (n: number) => n.toLocaleString("en-US");
