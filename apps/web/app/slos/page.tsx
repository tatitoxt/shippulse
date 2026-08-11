"use client";

import { ShieldCheck, TrendingUp, AlertTriangle } from "lucide-react";
import { MOCK_SLOS } from "@/lib/api";

export default function SLOsPage() {
  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold text-white tracking-tight flex items-center gap-2">
            SLOs & Error Budget Burn Rate
          </h1>
          <p className="text-xs text-gray-400 font-mono mt-0.5">
            Real-time Service Level Objectives calculated from OpenTelemetry telemetry.
          </p>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {MOCK_SLOS.map((slo) => (
          <div key={slo.id} className="console-card space-y-4">
            <div className="flex items-center justify-between border-b border-[#1A1D2B] pb-3">
              <div className="flex items-center space-x-2">
                <ShieldCheck className="w-5 h-5 text-purple-400" />
                <h2 className="font-bold text-white text-sm">{slo.name}</h2>
              </div>
              <span className="text-xs font-mono text-gray-400">Target: {slo.target_percentage}%</span>
            </div>

            <div className="grid grid-cols-2 gap-4 text-center font-mono">
              <div className="bg-[#090A0F] p-3 rounded-lg border border-[#1A1D2B]">
                <span className="text-[10px] text-gray-500 uppercase">Current SLI</span>
                <p className="text-xl font-extrabold text-emerald-400 mt-1">{slo.current_sli}%</p>
              </div>

              <div className="bg-[#090A0F] p-3 rounded-lg border border-[#1A1D2B]">
                <span className="text-[10px] text-gray-500 uppercase">Remaining Budget</span>
                <p className="text-xl font-extrabold text-purple-400 mt-1">{slo.error_budget_remaining}%</p>
              </div>
            </div>

            {/* Error Budget Consumption Bar */}
            <div className="space-y-1.5 font-mono text-xs">
              <div className="flex justify-between text-gray-400 text-[11px]">
                <span>30-Day Rolling Error Budget</span>
                <span>{slo.error_budget_remaining}% Available</span>
              </div>
              <div className="h-2.5 bg-[#1A1D2B] rounded-full overflow-hidden">
                <div
                  className="h-full bg-gradient-to-r from-purple-500 to-indigo-500 rounded-full"
                  style={{ width: `${slo.error_budget_remaining}%` }}
                />
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
