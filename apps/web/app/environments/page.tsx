"use client";

import { Layers, Server, Rocket, AlertTriangle, ShieldCheck } from "lucide-react";
import { MOCK_SERVICES } from "@/lib/api";

export default function EnvironmentsPage() {
  const environments = [
    { name: "Production", slug: "production", color: "#EF4444", status: "1 Degraded", activeServices: 4, lastDeploy: "15 mins ago" },
    { name: "Staging", slug: "staging", color: "#F59E0B", status: "All Healthy", activeServices: 4, lastDeploy: "2 hours ago" },
    { name: "Development", slug: "development", color: "#10B981", status: "All Healthy", activeServices: 4, lastDeploy: "4 hours ago" },
  ];

  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold text-white tracking-tight flex items-center gap-2">
            Environment Overview
          </h1>
          <p className="text-xs text-gray-400 font-mono mt-0.5">
            Active microservice health and deployment status partitioned across environments.
          </p>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {environments.map((e) => (
          <div key={e.slug} className="console-card space-y-4">
            <div className="flex items-center justify-between border-b border-[#1A1D2B] pb-3">
              <div className="flex items-center space-x-2">
                <div className="w-3 h-3 rounded-full" style={{ backgroundColor: e.color }} />
                <h2 className="font-bold text-white text-base">{e.name}</h2>
              </div>
              <span className={`text-[10px] font-mono font-bold px-2 py-0.5 rounded ${
                e.status.includes('Degraded') ? 'badge-degraded' : 'badge-healthy'
              }`}>
                {e.status}
              </span>
            </div>

            <div className="space-y-2 font-mono text-xs text-gray-400">
              <div className="flex justify-between">
                <span>Active Services:</span>
                <strong className="text-white">{e.activeServices}</strong>
              </div>
              <div className="flex justify-between">
                <span>Last Deployment:</span>
                <strong className="text-white">{e.lastDeploy}</strong>
              </div>
            </div>

            <div className="pt-2 border-t border-[#1A1D2B] space-y-2">
              <span className="text-[11px] font-mono text-gray-500 uppercase font-bold">Services</span>
              <div className="space-y-1.5">
                {MOCK_SERVICES.map((s) => (
                  <div key={s.id} className="flex items-center justify-between text-xs font-mono bg-[#090A0F] p-2 rounded border border-[#1A1D2B]">
                    <span className="text-gray-300">{s.name}</span>
                    <span className="text-gray-500">{s.current_version}</span>
                  </div>
                ))}
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
