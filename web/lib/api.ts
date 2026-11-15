const API_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

export type Session = {
  access_token: string;
  refresh_token: string;
  user: { id: string; email: string; name: string };
};

export function getSession(): Session | null {
  if (typeof window === "undefined") return null;
  const raw = localStorage.getItem("attest_session");
  if (!raw) return null;
  try {
    return JSON.parse(raw) as Session;
  } catch {
    return null;
  }
}

export function saveSession(session: Session) {
  localStorage.setItem("attest_session", JSON.stringify(session));
}

export function clearSession() {
  localStorage.removeItem("attest_session");
}

export async function api<T>(
  path: string,
  opts: RequestInit & { token?: string } = {}
): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(opts.headers as Record<string, string> | undefined),
  };
  if (opts.token) headers.Authorization = `Bearer ${opts.token}`;

  const res = await fetch(`${API_URL}${path}`, {
    ...opts,
    headers,
  });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    const msg = body?.error?.message || `Request failed (${res.status})`;
    throw new Error(msg);
  }
  return body as T;
}

export { API_URL };
