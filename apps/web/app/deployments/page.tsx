"use client";

import { Rocket, RefreshCw, CheckCircle2, AlertCircle, Clock, RotateCcw, GitBranch, ArrowUpRight } from "lucide-react";
import { MOCK_DEPLOYMENTS } from "@/lib/api";

export default function DeploymentsPage() {
  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold text-white tracking-tight flex items-center gap-2">
            Deployment Control Engine
          </h1>
          <p className="text-xs text-gray-400 font-mono mt-0.5">
            Deployment orchestration, live step streaming, GitHub correlation, and one-click rollbacks.
          </p>
        </div>
      </div>

      {/* Deployments List */}
      <div className="console-card space-y-4">
        <div className="flex items-center justify-between border-b border-[#1A1D2B] pb-3">
          <div className="flex items-center space-x-2">
            <Rocket className="w-4 h-4 text-brand-blue" />
            <h2 className="text-sm font-bold text-white font-mono uppercase tracking-wide">Deployment History</h2>
          </div>
          <span className="text-xs font-mono text-gray-500">Showing last 3 deployments</span>
        </div>

        <div className="space-y-4">
          {MOCK_DEPLOYMENTS.map((d) => (
            <div key={d.id} className="p-4 bg-[#090A0F] border border-[#1A1D2B] rounded-xl space-y-3 hover:border-[#272B3F] transition-colors">
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
                <div className="flex items-center space-x-3">
                  <div className={`w-8 h-8 rounded-lg flex items-center justify-center font-mono font-bold text-xs ${
                    d.status === 'SUCCEEDED' ? 'bg-emerald-500/20 text-emerald-400 border border-emerald-500/30' : 'bg-rose-500/20 text-rose-400 border border-rose-500/30'
                  }`}>
                    {d.status === 'SUCCEEDED' ? <CheckCircle2 className="w-4 h-4" /> : <AlertCircle className="w-4 h-4" />}
                  </div>
                  <div>
                    <div className="flex items-center space-x-2">
                      <h3 className="font-bold text-white text-sm font-mono">{d.version}</h3>
                      <span className="text-xs font-mono text-gray-500">({d.git_sha})</span>
                    </div>
                    <p className="text-xs text-gray-400 mt-0.5">{d.commit_message}</p>
                  </div>
                </div>

                <div className="flex items-center space-x-3">
                  <span className={`text-xs font-mono font-bold px-2 py-0.5 rounded ${
                    d.status === 'SUCCEEDED' ? 'bg-emerald-500/20 text-emerald-400' : 'bg-rose-500/20 text-rose-400'
                  }`}>
                    {d.status}
                  </span>

                  <button className="flex items-center space-x-1.5 bg-[#1A1D2B] hover:bg-[#272B3F] text-gray-300 hover:text-white px-2.5 py-1 rounded text-xs font-mono border border-[#272B3F] transition-colors">
                    <RotateCcw className="w-3 h-3 text-amber-400" />
                    <span>Rollback</span>
                  </button>
                </div>
              </div>

              {/* Steps Progress Stream */}
              <div className="pt-2 border-t border-[#1A1D2B] flex flex-wrap items-center gap-2 text-xs font-mono text-gray-500">
                <span className="text-emerald-400 font-semibold">Step 1: Pull Image ✓</span>
                <span>→</span>
                <span className="text-emerald-400 font-semibold">Step 2: Pre-check ✓</span>
                <span>→</span>
                <span className="text-emerald-400 font-semibold">Step 3: Task Update ✓</span>
                <span>→</span>
                <span className="text-emerald-400 font-semibold">Step 4: Health Probe ✓</span>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
