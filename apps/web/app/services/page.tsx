"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { usePrincipal } from "@/components/Shell";
import { ErrorBox } from "@/components/ui";
import { api, newIdempotencyKey } from "@/lib/api";
import { FAILURE_PROFILES, type Deployment, type RuntimeStatus, type Service } from "@/lib/types";
import { useLive } from "@/lib/useLive";

type ServiceWithRuntime = Service & { runtimes: RuntimeStatus[] };

async function loadServices(): Promise<ServiceWithRuntime[]> {
  const { services } = await api<{ services: Service[] }>("/services");
  return Promise.all(
    services.map(async (s) => {
      const rt = await api<{ runtimes: RuntimeStatus[] }>(`/services/${s.id}/runtime`);
      return { ...s, runtimes: rt.runtimes };
    }),
  );
}

export default function ServicesPage() {
  const { data, error } = useLive(loadServices);
  if (error) return <ErrorBox error={error.message} />;
  if (!data) return <p className="text-sm text-slate-400">Loading services…</p>;
  return (
    <div className="grid gap-6 lg:grid-cols-[1fr_320px]">
      <section className="space-y-4">
        <h1 className="text-xl font-semibold text-white">Services</h1>
        {data.map((s) => (
          <div key={s.id} className="card" data-testid={`service-${s.slug}`}>
            <div className="mb-3 flex items-baseline justify-between">
              <div>
                <h2 className="font-medium text-white">{s.name}</h2>
                <p className="font-mono text-xs text-slate-500">
                  {s.project_name} · {s.repository ?? "no repository linked"}
                </p>
              </div>
              <Link className="text-xs text-sky-400 hover:underline" href={`/deployments?service_id=${s.id}`}>
                release history →
              </Link>
            </div>
            <table className="w-full text-sm">
              <thead>
                <tr>
                  <th>Environment</th>
                  <th>Stable version</th>
                  <th>Simulated runtime</th>
                </tr>
              </thead>
              <tbody>
                {s.environments.map((e) => {
                  const rt = s.runtimes.find((r) => r.environment === e.environment_slug);
                  return (
                    <tr key={e.environment_id} data-testid={`env-${s.slug}-${e.environment_slug}`}>
                      <td>{e.environment_name}</td>
                      <td className="font-mono" data-testid="stable-version">
                        {e.current_version ?? <span className="text-slate-600">never deployed</span>}
                      </td>
                      <td className="font-mono" data-testid="observed-version">
                        {rt?.observed ? (
                          <>
                            {rt.observed.version} <span className="text-xs text-slate-500">({rt.observed.profile})</span>
                          </>
                        ) : (
                          <span className="text-slate-600">{rt?.provider_error ?? "not running"}</span>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ))}
      </section>
      <DeployForm services={data} />
    </div>
  );
}

function DeployForm({ services }: { services: Service[] }) {
  const me = usePrincipal();
  const router = useRouter();
  const canDeploy = me?.role !== "VIEWER";
  const [serviceId, setServiceId] = useState(services[0]?.id ?? "");
  const service = services.find((s) => s.id === serviceId);
  const [envId, setEnvId] = useState(services[0]?.environments[0]?.environment_id ?? "");
  const [version, setVersion] = useState("");
  const [profile, setProfile] = useState<string>("healthy");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const d = await api<Deployment>("/deployments", {
        method: "POST",
        idempotencyKey: newIdempotencyKey(),
        body: JSON.stringify({ service_id: serviceId, environment_id: envId, version, failure_profile: profile }),
      });
      router.push(`/deployments/${d.id}`);
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  }

  return (
    <aside className="card h-fit space-y-3">
      <h2 className="font-medium text-white">New deployment</h2>
      {!canDeploy && (
        <p data-testid="viewer-notice" className="text-sm text-amber-300">
          Your role is VIEWER: you can inspect releases but not deploy or roll back.
        </p>
      )}
      <ErrorBox error={error} />
      <form onSubmit={submit} className="space-y-3">
        <fieldset disabled={!canDeploy || busy} className="space-y-3">
          <div>
            <label className="label" htmlFor="service">Service</label>
            <select
              id="service"
              className="input"
              value={serviceId}
              onChange={(e) => {
                setServiceId(e.target.value);
                setEnvId(services.find((s) => s.id === e.target.value)?.environments[0]?.environment_id ?? "");
              }}
            >
              {services.map((s) => (
                <option key={s.id} value={s.id}>{s.slug}</option>
              ))}
            </select>
          </div>
          <div>
            <label className="label" htmlFor="environment">Environment</label>
            <select id="environment" className="input" value={envId} onChange={(e) => setEnvId(e.target.value)}>
              {service?.environments.map((e) => (
                <option key={e.environment_id} value={e.environment_id}>{e.environment_slug}</option>
              ))}
            </select>
          </div>
          <div>
            <label className="label" htmlFor="version">Version</label>
            <input id="version" className="input font-mono" placeholder="v1.4.0" value={version} onChange={(e) => setVersion(e.target.value)} required />
          </div>
          <div>
            <label className="label" htmlFor="profile">Simulated outcome</label>
            <select id="profile" className="input" value={profile} onChange={(e) => setProfile(e.target.value)}>
              {FAILURE_PROFILES.map((p) => (
                <option key={p} value={p}>{p}</option>
              ))}
            </select>
            <p className="mt-1 text-xs text-slate-500">
              The simulated runtime behaves according to this profile; nothing is deployed to real infrastructure.
            </p>
          </div>
          <button className="btn w-full" type="submit">Deploy</button>
        </fieldset>
      </form>
    </aside>
  );
}
