"use client";

import { AlertTriangle, CheckCircle2, Clock, MessageSquare, ShieldAlert, ArrowUpRight } from "lucide-react";
import { MOCK_INCIDENTS } from "@/lib/api";

export default function IncidentsPage() {
  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold text-white tracking-tight flex items-center gap-2">
            Incident Command Center
          </h1>
          <p className="text-xs text-gray-400 font-mono mt-0.5">
            Automated operational timelines correlating deployments, health probes, and OpenTelemetry alerts.
          </p>
        </div>
      </div>

      <div className="console-card space-y-4">
        <div className="flex items-center justify-between border-b border-[#1A1D2B] pb-3">
          <div className="flex items-center space-x-2">
            <AlertTriangle className="w-4 h-4 text-rose-400" />
            <h2 className="text-sm font-bold text-white font-mono uppercase tracking-wide">Active Incidents</h2>
          </div>
        </div>

        <div className="space-y-4">
          {MOCK_INCIDENTS.map((inc) => (
            <div key={inc.id} className="p-4 bg-[#090A0F] border border-rose-500/30 rounded-xl space-y-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center space-x-3">
                  <span className="text-xs font-bold font-mono bg-rose-500/20 text-rose-400 border border-rose-500/30 px-2 py-0.5 rounded">
                    {inc.severity}
                  </span>
                  <h3 className="font-bold text-white text-sm">{inc.title}</h3>
                </div>

                <span className="text-xs font-mono font-bold bg-amber-500/20 text-amber-400 px-2.5 py-0.5 rounded border border-amber-500/30">
                  {inc.status}
                </span>
              </div>

              <p className="text-xs text-gray-400 font-mono">{inc.summary}</p>

              {/* Operational Correlation Timeline */}
              <div className="pt-3 border-t border-[#1A1D2B] space-y-2">
                <span className="text-[11px] font-mono font-bold text-gray-400 uppercase tracking-wider">Automated Event Timeline</span>
                <div className="space-y-1.5 text-xs font-mono text-gray-400 pl-2 border-l-2 border-brand-blue/40">
                  <div className="flex items-center space-x-2">
                    <span className="text-gray-500">14:02 UTC</span>
                    <span className="text-emerald-400">Deployment #103 started (v1.2.1-rc1)</span>
                  </div>
                  <div className="flex items-center space-x-2">
                    <span className="text-gray-500">14:05 UTC</span>
                    <span className="text-amber-400">p95 latency spike detected (&gt; 750ms)</span>
                  </div>
                  <div className="flex items-center space-x-2">
                    <span className="text-gray-500">14:06 UTC</span>
                    <span className="text-rose-400">Alert triggered: Payments service marked DEGRADED</span>
                  </div>
                  <div className="flex items-center space-x-2">
                    <span className="text-gray-500">14:08 UTC</span>
                    <span className="text-brand-cyan">Incident opened automatically by AlertEngine</span>
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
