export type Envelope<T> = {
  success: boolean;
  code: number;
  message: string;
  data?: T;
  meta?: { requestId?: string; page?: number; pageSize?: number; total?: number };
};

const tokenKey = "xphone.token";

export function getToken(): string {
  return sessionStorage.getItem(tokenKey) ?? "";
}

export function setToken(token: string) {
  if (token) sessionStorage.setItem(tokenKey, token);
  else sessionStorage.removeItem(tokenKey);
}

export function wsURL(path: string): string {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  return `${proto}://${location.host}${path}?token=${encodeURIComponent(getToken())}`;
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (!(init.body instanceof FormData) && !headers.has("Content-Type") && init.body) {
    headers.set("Content-Type", "application/json");
  }
  const token = getToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  let response: Response;
  try {
    response = await fetch(path, { ...init, headers });
  } catch {
    throw new Error("连不上控制面。请确认 API 已在 127.0.0.1:18110 启动。");
  }
  const body = (await response.json()) as Envelope<T>;
  if (!body.success) {
    throw new Error(body.message || "request failed");
  }
  return body.data as T;
}

export type Host = {
  id: string;
  name: string;
  addr: string;
  status: string;
  capacity: number;
  binderfs: string;
  lastHeartbeatAt: number;
  runningCount: number;
};

export type Profile = {
  id: string;
  brand: string;
  model: string;
  width: number;
  height: number;
  dpi: number;
  locale: string;
  timezone: string;
  gpu: string;
  cpuCores: number;
  memoryMb: number;
  touchPoints: number;
};

export type Instance = {
  id: string;
  name: string;
  hostId: string;
  profileId: string;
  status: string;
  httpProxy?: string;
  adbPort: number;
  adbSerial?: string;
  containerId?: string;
  runtimeKind: string;
  lastError?: string;
  profileReport?: string;
};

export type Group = {
  id: string;
  name: string;
  profileId: string;
  instanceIds: string[];
};

export type Task = {
  id: string;
  instanceId: string;
  type: string;
  status: string;
  log: string;
  claimedAt: number;
  finishedAt: number;
};

export type Step = { name: string; ok: boolean; detail: string };
export type ProfileReport = { ok: boolean; steps: Step[]; gaps: string[] };
