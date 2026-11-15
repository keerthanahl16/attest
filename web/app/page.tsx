"use client";

import { FormEvent, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getSession, saveSession } from "@/lib/api";

export default function LoginPage() {
  const router = useRouter();
  const [email, setEmail] = useState("demo@attest.dev");
  const [password, setPassword] = useState("AttestDemo!123");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (getSession()) router.replace("/inbox");
  }, [router]);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError("");
    try {
      const session = await api<{
        access_token: string;
        refresh_token: string;
        user: { id: string; email: string; name: string };
      }>("/v1/auth/login", {
        method: "POST",
        body: JSON.stringify({ email, password }),
      });
      saveSession(session);
      router.push("/inbox");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Login failed");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="login-wrap">
      <div className="grid" style={{ width: "min(920px, 100%)", gap: 24 }}>
        <div className="hero-copy">
          <p className="brand">
            Attest<span>.</span>
          </p>
          <h1>Approve irreversible AI tool calls — once.</h1>
          <p className="muted">
            Postgres is the source of truth. Agents request work with an idempotency key.
            Humans approve refunds and other side effects. A worker executes exactly once.
          </p>
        </div>

        <div className="card login-card">
          <h2 className="section-title">Sign in</h2>
          <p className="muted" style={{ marginTop: 0 }}>
            Demo: <span className="mono">demo@attest.dev</span> /{" "}
            <span className="mono">AttestDemo!123</span>
          </p>
          <form onSubmit={onSubmit}>
            <div className="field">
              <label htmlFor="email">Email</label>
              <input
                id="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                autoComplete="username"
              />
            </div>
            <div className="field">
              <label htmlFor="password">Password</label>
              <input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
              />
            </div>
            {error ? <p className="error">{error}</p> : null}
            <button className="btn" disabled={loading} type="submit">
              {loading ? "Signing in…" : "Open approval inbox"}
            </button>
          </form>
        </div>
      </div>
    </div>
  );
}
