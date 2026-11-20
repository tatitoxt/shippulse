"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense } from "react";
import { ErrorBox, StatusBadge, Time } from "@/components/ui";
import { api } from "@/lib/api";
import type { Deployment } from "@/lib/types";
import { usePoll } from "@/lib/usePoll";

export default function DeploymentsPage() {
  return (
    <Suspense>
      <DeploymentList />
    </Suspense>
  );
}

function DeploymentList() {
  const serviceId = useSearchParams().get("service_id") ?? "";
  const query = serviceId ? `?service_id=${encodeURIComponent(serviceId)}` : "";
  const { data, error } = usePoll(
    () => api<{ deployments: Deployment[] }>(`/deployments${query}`).then((r) => r.deployments),
    2000,
  );
  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-white">Deployments</h1>
        {serviceId && (
          <Link href="/deployments" className="text-xs text-sky-400 hover:underline">
            show all services
          </Link>
        )}
      </div>
      <ErrorBox error={error?.message ?? null} />
      {data && data.length === 0 && (
        <p className="text-sm text-slate-400">
          No deployments yet. Start one from <Link className="text-sky-400" href="/services">Services</Link> or run{" "}
          <code className="font-mono">make demo</code>.
        </p>
      )}
      {data && data.length > 0 && (
        <div className="card overflow-x-auto p-0">
          <table className="w-full text-sm" data-testid="deployments-table">
            <thead>
              <tr>
                <th>Service</th>
                <th>Env</th>
                <th>Version</th>
                <th>Status</th>
                <th>Rollback</th>
                <th>Source</th>
                <th>Requested</th>
              </tr>
            </thead>
            <tbody>
              {data.map((d) => {
                const rb = d.rollbacks?.[d.rollbacks.length - 1];
                return (
                  <tr key={d.id} data-testid={`deployment-row-${d.version}`}>
                    <td>
                      <Link className="text-sky-400 hover:underline" href={`/deployments/${d.id}`}>
                        {d.service_slug}
                      </Link>
                    </td>
                    <td>{d.environment_slug}</td>
                    <td className="font-mono">{d.version}</td>
                    <td><StatusBadge status={d.status} /></td>
                    <td>
                      {rb ? (
                        <span className="text-xs">
                          <StatusBadge status={rb.status} /> → <span className="font-mono">{rb.to_version}</span>
                        </span>
                      ) : (
                        <span className="text-slate-600">—</span>
                      )}
                    </td>
                    <td className="text-xs text-slate-400">{d.source}</td>
                    <td><Time value={d.created_at} /></td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
