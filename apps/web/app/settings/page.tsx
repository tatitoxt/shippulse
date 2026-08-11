"use client";

import { Settings, Key, Terminal, Users, Webhook, Shield, Copy, Plus } from "lucide-react";

export default function SettingsPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-bold text-white tracking-tight flex items-center gap-2">
          Organization Settings & Security
        </h1>
        <p className="text-xs text-gray-400 font-mono mt-0.5">
          Manage API keys, agent registration tokens, team RBAC roles, and GitHub webhooks.
        </p>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Scoped API Keys */}
        <div className="console-card space-y-4">
          <div className="flex items-center justify-between border-b border-[#1A1D2B] pb-3">
            <div className="flex items-center space-x-2">
              <Key className="w-4 h-4 text-brand-blue" />
              <h2 className="text-sm font-bold text-white font-mono uppercase tracking-wide">Scoped API Tokens</h2>
            </div>
            <button className="flex items-center space-x-1 bg-brand-blue hover:bg-blue-600 text-white px-2.5 py-1 rounded text-xs font-mono transition-colors">
              <Plus className="w-3 h-3" />
              <span>Create Key</span>
            </button>
          </div>

          <div className="space-y-3 font-mono text-xs">
            <div className="p-3 bg-[#090A0F] border border-[#1A1D2B] rounded-lg space-y-1">
              <div className="flex justify-between font-bold text-white">
                <span>CI/CD GitHub Actions Key</span>
                <span className="text-emerald-400 text-[10px]">ACTIVE</span>
              </div>
              <p className="text-gray-500 text-[11px]">sp_live_a7f92bc... • Scopes: deployments:write</p>
            </div>
          </div>
        </div>

        {/* Agent Registration Tokens */}
        <div className="console-card space-y-4">
          <div className="flex items-center justify-between border-b border-[#1A1D2B] pb-3">
            <div className="flex items-center space-x-2">
              <Terminal className="w-4 h-4 text-brand-cyan" />
              <h2 className="text-sm font-bold text-white font-mono uppercase tracking-wide">Agent One-Time Registration Tokens</h2>
            </div>
            <button className="flex items-center space-x-1 bg-[#1A1D2B] hover:bg-[#272B3F] text-gray-300 hover:text-white px-2.5 py-1 rounded text-xs font-mono border border-[#272B3F]">
              <Plus className="w-3 h-3" />
              <span>Generate Token</span>
            </button>
          </div>

          <p className="text-xs text-gray-400 font-mono">
            Generate one-time registration tokens to connect new ShipPulse agents running on Docker or Kubernetes clusters.
          </p>
        </div>
      </div>
    </div>
  );
}
