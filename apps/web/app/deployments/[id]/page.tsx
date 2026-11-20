"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";
import { usePrincipal } from "@/components/Shell";
import { ErrorBox, StatusBadge, Time } from "@/components/ui";
import { api, ApiError, newIdempotencyKey } from "@/lib/api";
import { TERMINAL, type AuditEvent, type Deployment, type RuntimeStatus } from "@/lib/types";
import { usePoll } from "@/lib/usePoll";

interface Evidence {
  deployment: Deployment;
  audit: AuditEvent[];
  runtime: RuntimeStatus | undefined;
}

async function loadEvidence(id: string): Promise<Evidence> {
  const deployment = await api<Deployment>(`/deployments/${id}`);
  const [audit, rt] = await Promise.all([
    api<{ audit_events: AuditEvent[] }>(`/audit-events?subject_id=${id}`),
    api<{ runtimes: RuntimeStatus[] }>(`/services/${deployment.service_id}/runtime`),
  ]);
  return {
    deployment,
    audit: audit.audit_events,
    runtime: rt.runtimes.find((r) => r.environment === deployment.environment_slug),
  };
}

const settled = (e: Evidence | null) =>
  !!e && TERMINAL.has(e.deployment.status) && (e.deployment.rollbacks ?? []).every((r) => TERMINAL.has(r.status));

export default function DeploymentDetail() {
  const { id } = useParams<{ id: string }>();
  const { data, error, refresh } = usePoll(() => loadEvidence(id), 1000, (d) => !settled(d));

  if (error instanceof ApiError && error.status === 404) {
    return (
      <div data-testid="not-found" className="card text-sm text-slate-300">
        Deployment not found in your organization. <Link className="text-sky-400" href="/deployments">Back to deployments</Link>
      </div>
    );
  }
  if (error && !data) return <ErrorBox error={error.message} />;
  if (!data) return <p className="text-sm text-slate-400">Loading deployment…</p>;
  const d = data.deployment;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <Link href="/deployments" className="text-xs text-sky-400 hover:underline">← deployments</Link>
          <h1 className="mt-1 text-xl font-semibold text-white">
            {d.service_slug} <span className="text-slate-500">/</span> {d.environment_slug}{" "}
            <span className="font-mono text-sky-300">{d.version}</span>
          </h1>
          <p className="mt-1 text-xs text-slate-500">
            {d.source === "github" ? "GitHub deployment webhook" : "manual"} · by {d.actor} · profile{" "}
            <span className="font-mono">{d.failure_profile}</span> · previous stable{" "}
            <span className="font-mono">{d.previous_version ?? "none"}</span> · correlation{" "}
            <span className="font-mono">{d.correlation_id}</span>
          </p>
        </div>
        <div className="flex items-center gap-3">
          <StatusBadge status={d.status} testId="deployment-status" />
          <Actions deployment={d} onDone={refresh} />
        </div>
      </div>

      {d.error_code && (
        <div className="rounded border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-sm text-rose-200" data-testid="failure">
          <span className="font-mono">{d.error_code}</span>: {d.error_message}
        </div>
      )}

      <div className="grid gap-6 lg:grid-cols-2">
        <section className="card">
          <h2 className="mb-3 font-medium text-white">Lifecycle steps</h2>
          <ol className="space-y-2">
            {(d.steps ?? []).map((s) => (
              <li key={s.name} data-testid={`step-${s.name}`} className="flex items-start gap-3 text-sm">
                <span className="w-5 font-mono text-xs text-slate-500">{s.ordinal}</span>
                <div className="flex-1">
                  <div className="flex items-center gap-2">
                    <span className="font-mono">{s.name}</span>
                    <StatusBadge status={s.status} testId={`step-${s.name}-status`} />
                    {s.attempts > 1 && <span className="text-xs text-slate-500">{s.attempts} attempts</span>}
                  </div>
                  {s.safe_output && <p className="text-xs text-slate-400">{s.safe_output}</p>}
                </div>
                <Time value={s.finished_at ?? s.started_at} />
              </li>
            ))}
          </ol>
        </section>

        <section className="card space-y-3" data-testid="runtime-evidence">
          <h2 className="font-medium text-white">Runtime evidence</h2>
          <dl className="grid grid-cols-2 gap-2 text-sm">
            <dt className="text-slate-400">Promoted stable version</dt>
            <dd className="font-mono" data-testid="stable-version">{data.runtime?.stable_version ?? "none"}</dd>
            <dt className="text-slate-400">Simulated runtime is running</dt>
            <dd className="font-mono" data-testid="observed-version">
              {data.runtime?.observed?.version ?? data.runtime?.provider_error ?? "nothing"}
            </dd>
          </dl>
          <h3 className="pt-2 text-sm font-medium text-white">Rollbacks</h3>
          {(d.rollbacks ?? []).length === 0 && <p className="text-sm text-slate-500">No rollback for this deployment.</p>}
          {(d.rollbacks ?? []).map((r) => (
            <div key={r.id} data-testid={`rollback-${r.trigger}`} className="rounded border border-slate-800 p-2 text-sm">
              <div className="flex items-center gap-2">
                <span className="text-xs uppercase text-slate-400">{r.trigger}</span>
                <span className="font-mono">
                  {r.from_version} → {r.to_version}
                </span>
                <StatusBadge status={r.status} testId={`rollback-${r.trigger}-status`} />
              </div>
              <p className="text-xs text-slate-400">{r.safe_output || r.reason}</p>
            </div>
          ))}
        </section>
      </div>

      <section className="card overflow-x-auto p-0">
        <h2 className="px-4 pt-4 font-medium text-white">Audit trail</h2>
        <table className="w-full text-sm" data-testid="audit-trail">
          <thead>
            <tr>
              <th>When</th>
              <th>Action</th>
              <th>Actor</th>
              <th>Details</th>
            </tr>
          </thead>
          <tbody>
            {data.audit.map((a) => (
              <tr key={a.id}>
                <td><Time value={a.occurred_at} /></td>
                <td className="font-mono text-xs">{a.action}</td>
                <td className="text-xs">{a.actor}</td>
                <td className="font-mono text-xs text-slate-400">{JSON.stringify(a.metadata)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </div>
  );
}

function Actions({ deployment: d, onDone }: { deployment: Deployment; onDone: () => void }) {
  const me = usePrincipal();
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  if (me?.role === "VIEWER") return null;

  const run = async (path: string, body?: object) => {
    setBusy(true);
    setError(null);
    try {
      await api(path, { method: "POST", idempotencyKey: newIdempotencyKey(), body: body ? JSON.stringify(body) : undefined });
      onDone();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex items-center gap-2">
      {d.status === "queued" && (
        <button className="btn-secondary" disabled={busy} onClick={() => run(`/deployments/${d.id}/cancel`)}>
          Cancel
        </button>
      )}
      {(d.status === "succeeded" || d.status === "failed") && d.previous_version && (
        <button
          className="btn-secondary"
          disabled={busy}
          onClick={() => run(`/deployments/${d.id}/rollback`, { reason: "manual rollback from console" })}
        >
          Roll back to {d.previous_version}
        </button>
      )}
      {error && <span className="text-xs text-rose-300">{error}</span>}
    </div>
  );
}
