"use client";

import Link from "next/link";
import { ErrorBox, Time } from "@/components/ui";
import { api } from "@/lib/api";
import type { AuditEvent } from "@/lib/types";
import { useLive } from "@/lib/useLive";

export default function AuditPage() {
  const { data, error } = useLive(() =>
    api<{ audit_events: AuditEvent[] }>("/audit-events?limit=200").then((r) => r.audit_events),
  );
  return (
    <section className="space-y-4">
      <h1 className="text-xl font-semibold text-white">Audit log</h1>
      <p className="text-sm text-slate-400">Append-only record of every request, step and rollback in your organization.</p>
      <ErrorBox error={error?.message ?? null} />
      <div className="card overflow-x-auto p-0">
        <table className="w-full text-sm" data-testid="audit-log">
          <thead>
            <tr>
              <th>When</th>
              <th>Action</th>
              <th>Actor</th>
              <th>Subject</th>
              <th>Details</th>
            </tr>
          </thead>
          <tbody>
            {(data ?? []).map((a) => (
              <tr key={a.id}>
                <td><Time value={a.occurred_at} /></td>
                <td className="font-mono text-xs">{a.action}</td>
                <td className="text-xs">{a.actor}</td>
                <td className="text-xs">
                  {a.subject_type === "deployment" ? (
                    <Link className="text-sky-400 hover:underline" href={`/deployments/${a.subject_id}`}>
                      {a.subject_type} {a.subject_id.slice(0, 8)}
                    </Link>
                  ) : (
                    <span>
                      {a.subject_type} {a.subject_id.slice(0, 8)}
                    </span>
                  )}
                </td>
                <td className="font-mono text-xs text-slate-400">{JSON.stringify(a.metadata)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
