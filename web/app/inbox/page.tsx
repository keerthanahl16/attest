"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import {
  API_URL,
  api,
  clearSession,
  getSession,
  type Session,
} from "@/lib/api";

type Workspace = { id: string; name: string; slug: string };
type Approval = {
  id: string;
  tool_name: string;
  idempotency_key: string;
  step_input: unknown;
  requested_at: string;
  run_id: string;
};
type Run = {
  id: string;
  status: string;
  idempotency_key: string;
  created_at: string;
  result?: unknown;
  error_message?: string;
};
type Event = {
  id: string;
  kind: string;
  message: string;
  created_at: string;
  run_id?: string;
};

const DEMO_API_KEY = "atk_demo_local_only_do_not_use_in_prod";

export default function InboxPage() {
  const router = useRouter();
  const [session, setSession] = useState<Session | null>(null);
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [approvals, setApprovals] = useState<Approval[]>([]);
  const [runs, setRuns] = useState<Run[]>([]);
  const [events, setEvents] = useState<Event[]>([]);
  const [selectedRun, setSelectedRun] = useState<string | null>(null);
  const [runEvents, setRunEvents] = useState<Event[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [demoNote, setDemoNote] = useState("");

  const token = session?.access_token;

  const refresh = useCallback(async () => {
    if (!token || !workspace) return;
    const [a, r, e] = await Promise.all([
      api<{ approvals: Approval[] }>(`/v1/workspaces/${workspace.id}/approvals`, { token }),
      api<{ runs: Run[] }>(`/v1/workspaces/${workspace.id}/runs`, { token }),
      api<{ events: Event[] }>(`/v1/workspaces/${workspace.id}/events`, { token }),
    ]);
    setApprovals(a.approvals || []);
    setRuns(r.runs || []);
    setEvents(e.events || []);
  }, [token, workspace]);

  useEffect(() => {
    const s = getSession();
    if (!s) {
      router.replace("/");
      return;
    }
    setSession(s);
    (async () => {
      try {
        const res = await api<{ workspaces: Workspace[] }>("/v1/workspaces", {
          token: s.access_token,
        });
        const ws = res.workspaces?.[0];
        if (!ws) {
          setError("No workspace found. Restart API with SEED_DEMO=true.");
          return;
        }
        setWorkspace(ws);
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to load workspaces");
      }
    })();
  }, [router]);

  useEffect(() => {
    if (!workspace || !token) return;
    refresh().catch((err) => setError(err.message));
    const id = setInterval(() => {
      refresh().catch(() => undefined);
    }, 2000);
    return () => clearInterval(id);
  }, [workspace, token, refresh]);

  useEffect(() => {
    if (!token || !workspace || !selectedRun) return;
    api<{ events: Event[] }>(`/v1/workspaces/${workspace.id}/runs/${selectedRun}`, { token })
      .then((res) => setRunEvents(res.events || []))
      .catch(() => undefined);
  }, [selectedRun, token, workspace]);

  const pendingCount = approvals.length;

  const statusPill = useMemo(
    () => (status: string) => {
      if (status.includes("await") || status === "queued") return "pill warn";
      if (status === "succeeded") return "pill ok";
      if (status === "failed" || status === "rejected") return "pill danger";
      return "pill muted";
    },
    []
  );

  async function decide(id: string, approve: boolean) {
    if (!token || !workspace) return;
    setBusy(true);
    setError("");
    try {
      await api(`/v1/workspaces/${workspace.id}/approvals/${id}/${approve ? "approve" : "reject"}`, {
        method: "POST",
        token,
        body: JSON.stringify({ reason: approve ? "Looks correct" : "Blocked in demo" }),
      });
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Decision failed");
    } finally {
      setBusy(false);
    }
  }

  async function fireDemoRefund() {
    setBusy(true);
    setDemoNote("");
    setError("");
    try {
      const key = `refund-demo-${Date.now()}`;
      const first = await fetch(`${API_URL}/v1/agent/runs`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-API-Key": DEMO_API_KEY,
        },
        body: JSON.stringify({
          idempotency_key: key,
          tool: "issue_refund",
          input: {
            amount: 5000,
            currency: "INR",
            order_id: "ord_demo_42",
            reason: "Customer requested refund via agent",
          },
        }),
      }).then((r) => r.json());

      const second = await fetch(`${API_URL}/v1/agent/runs`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-API-Key": DEMO_API_KEY,
        },
        body: JSON.stringify({
          idempotency_key: key,
          tool: "issue_refund",
          input: {
            amount: 5000,
            currency: "INR",
            order_id: "ord_demo_42",
            reason: "duplicate click",
          },
        }),
      }).then((r) => r.json());

      setDemoNote(
        `Created run ${first.run?.id?.slice(0, 8)}… · duplicate replayed=${String(
          second.replayed
        )} (same run id)`
      );
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Demo run failed");
    } finally {
      setBusy(false);
    }
  }

  async function fireSafeEmail() {
    setBusy(true);
    setError("");
    try {
      await fetch(`${API_URL}/v1/agent/runs`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-API-Key": DEMO_API_KEY,
        },
        body: JSON.stringify({
          idempotency_key: `email-${Date.now()}`,
          tool: "send_email",
          input: {
            to: "customer@example.com",
            subject: "Your transfer update",
            body: "Safe tool — no approval required.",
          },
        }),
      });
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Email demo failed");
    } finally {
      setBusy(false);
    }
  }

  if (!session) {
    return <div className="shell muted">Loading…</div>;
  }

  return (
    <div className="shell">
      <nav className="nav">
        <div>
          <p className="brand">
            Attest<span>.</span>
          </p>
          <p className="muted" style={{ margin: 0 }}>
            {workspace ? `${workspace.name} · ${session.user.name}` : "Loading workspace…"}
          </p>
        </div>
        <div className="row">
          <span className="pill warn">{pendingCount} pending</span>
          <button
            className="btn secondary"
            onClick={() => {
              clearSession();
              router.replace("/");
            }}
          >
            Sign out
          </button>
        </div>
      </nav>

      {error ? <p className="error">{error}</p> : null}

      <div className="grid grid-2">
        <div className="stack">
          <section className="card">
            <div className="row" style={{ justifyContent: "space-between" }}>
              <h2 className="section-title">Approval inbox</h2>
              <div className="row">
                <button className="btn secondary" disabled={busy} onClick={fireSafeEmail}>
                  Fire safe email
                </button>
                <button className="btn" disabled={busy} onClick={fireDemoRefund}>
                  Simulate refund request
                </button>
              </div>
            </div>
            <p className="muted">
              Irreversible tools wait here. Approve once — the worker posts the mock refund and
              duplicates with the same idempotency key return the original run.
            </p>
            {demoNote ? <p className="mono">{demoNote}</p> : null}

            <div className="stack" style={{ marginTop: 16 }}>
              {approvals.length === 0 ? (
                <p className="muted">No pending approvals. Simulate a refund to see one appear.</p>
              ) : (
                approvals.map((a) => (
                  <div className="approval" key={a.id}>
                    <div className="row" style={{ justifyContent: "space-between" }}>
                      <h3>{a.tool_name}</h3>
                      <span className="pill warn">awaiting approval</span>
                    </div>
                    <p className="mono muted">key: {a.idempotency_key}</p>
                    <pre>{JSON.stringify(a.step_input, null, 2)}</pre>
                    <div className="row" style={{ marginTop: 12 }}>
                      <button className="btn" disabled={busy} onClick={() => decide(a.id, true)}>
                        Approve
                      </button>
                      <button
                        className="btn danger"
                        disabled={busy}
                        onClick={() => decide(a.id, false)}
                      >
                        Reject
                      </button>
                      <button
                        className="btn secondary"
                        onClick={() => setSelectedRun(a.run_id)}
                      >
                        Timeline
                      </button>
                    </div>
                  </div>
                ))
              )}
            </div>
          </section>

          <section className="card">
            <h2 className="section-title">Recent runs</h2>
            <div className="stack">
              {runs.length === 0 ? (
                <p className="muted">No runs yet.</p>
              ) : (
                runs.map((run) => (
                  <button
                    key={run.id}
                    className="approval"
                    style={{ textAlign: "left", cursor: "pointer", width: "100%" }}
                    onClick={() => setSelectedRun(run.id)}
                  >
                    <div className="row" style={{ justifyContent: "space-between" }}>
                      <span className="mono">{run.idempotency_key}</span>
                      <span className={statusPill(run.status)}>{run.status}</span>
                    </div>
                    <p className="muted" style={{ margin: "6px 0 0" }}>
                      {new Date(run.created_at).toLocaleString()}
                    </p>
                  </button>
                ))
              )}
            </div>
          </section>
        </div>

        <div className="stack">
          <section className="card">
            <h2 className="section-title">
              {selectedRun ? "Run timeline" : "Workspace activity"}
            </h2>
            <ul className="timeline">
              {(selectedRun ? runEvents : events).map((ev) => (
                <li key={ev.id}>
                  <span className="dot" />
                  <div>
                    <div className="row">
                      <strong>{ev.kind}</strong>
                      <span className="muted mono">
                        {new Date(ev.created_at).toLocaleTimeString()}
                      </span>
                    </div>
                    <p className="muted" style={{ margin: "4px 0 0" }}>
                      {ev.message}
                    </p>
                  </div>
                </li>
              ))}
              {(selectedRun ? runEvents : events).length === 0 ? (
                <li>
                  <span className="dot" />
                  <p className="muted">Quiet for now — fire a demo run.</p>
                </li>
              ) : null}
            </ul>
            {selectedRun ? (
              <button
                className="btn secondary"
                style={{ marginTop: 12 }}
                onClick={() => setSelectedRun(null)}
              >
                Show workspace feed
              </button>
            ) : null}
          </section>

          <section className="card">
            <h2 className="section-title">Agent API key</h2>
            <p className="muted">
              Seeded for local demos. Agents send this as <span className="mono">X-API-Key</span>.
            </p>
            <pre className="mono">{DEMO_API_KEY}</pre>
          </section>
        </div>
      </div>
    </div>
  );
}
