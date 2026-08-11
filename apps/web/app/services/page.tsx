"use client";

import { Server, ArrowUpRight, GitBranch, Cpu, Network, ShieldCheck } from "lucide-react";
import Link from "next/link";
import { MOCK_SERVICES } from "@/lib/api";

export default function ServiceCatalogPage() {
  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold text-white tracking-tight flex items-center gap-2">
            Service Catalog & Dependencies
          </h1>
          <p className="text-xs text-gray-400 font-mono mt-0.5">
            Registered microservices, environment mappings, and interactive dependency topology graph.
          </p>
        </div>
      </div>

      {/* SVG Dependency Graph Card */}
      <div className="console-card space-y-4">
        <div className="flex items-center justify-between border-b border-[#1A1D2B] pb-3">
          <div className="flex items-center space-x-2">
            <Network className="w-4 h-4 text-brand-cyan" />
            <h2 className="text-sm font-bold text-white font-mono uppercase tracking-wide">Interactive Dependency Graph</h2>
          </div>
          <span className="text-xs font-mono text-gray-500">4 Microservices Registered</span>
        </div>

        <div className="bg-[#090A0F] border border-[#1A1D2B] rounded-xl p-6 relative overflow-hidden flex items-center justify-center min-h-[220px]">
          <svg className="w-full max-w-2xl h-48" viewBox="0 0 600 200">
            <defs>
              <linearGradient id="grad-active" x1="0%" y1="0%" x2="100%" y2="0%">
                <stop offset="0%" stopColor="#3B82F6" stopOpacity="0.8" />
                <stop offset="100%" stopColor="#06B6D4" stopOpacity="0.8" />
              </linearGradient>
            </defs>

            {/* Connecting Lines */}
            <path d="M 120 100 L 280 60" stroke="url(#grad-active)" strokeWidth="2" strokeDasharray="4 4" className="animate-pulse" />
            <path d="M 120 100 L 280 140" stroke="url(#grad-active)" strokeWidth="2" />
            <path d="M 280 140 L 480 100" stroke="#F59E0B" strokeWidth="2" strokeDasharray="2 2" />

            {/* Nodes */}
            {/* 1. Web Dashboard */}
            <g transform="translate(60, 75)">
              <rect x="0" y="0" width="120" height="50" rx="8" fill="#10121B" stroke="#3B82F6" strokeWidth="1.5" />
              <text x="60" y="24" fill="#FFFFFF" fontSize="11" fontWeight="bold" textAnchor="middle" fontFamily="sans-serif">Web Dashboard</text>
              <text x="60" y="38" fill="#10B981" fontSize="9" textAnchor="middle" fontFamily="monospace">HEALTHY</text>
            </g>

            {/* 2. API Gateway */}
            <g transform="translate(220, 35)">
              <rect x="0" y="0" width="120" height="50" rx="8" fill="#10121B" stroke="#06B6D4" strokeWidth="1.5" />
              <text x="60" y="24" fill="#FFFFFF" fontSize="11" fontWeight="bold" textAnchor="middle" fontFamily="sans-serif">API Gateway</text>
              <text x="60" y="38" fill="#10B981" fontSize="9" textAnchor="middle" fontFamily="monospace">HEALTHY</text>
            </g>

            {/* 3. Payments Service */}
            <g transform="translate(220, 115)">
              <rect x="0" y="0" width="120" height="50" rx="8" fill="#10121B" stroke="#F59E0B" strokeWidth="1.5" />
              <text x="60" y="24" fill="#FFFFFF" fontSize="11" fontWeight="bold" textAnchor="middle" fontFamily="sans-serif">Payments Engine</text>
              <text x="60" y="38" fill="#F59E0B" fontSize="9" textAnchor="middle" fontFamily="monospace">DEGRADED</text>
            </g>

            {/* 4. Async Worker */}
            <g transform="translate(420, 75)">
              <rect x="0" y="0" width="120" height="50" rx="8" fill="#10121B" stroke="#8B5CF6" strokeWidth="1.5" />
              <text x="60" y="24" fill="#FFFFFF" fontSize="11" fontWeight="bold" textAnchor="middle" fontFamily="sans-serif">Async Worker</text>
              <text x="60" y="38" fill="#10B981" fontSize="9" textAnchor="middle" fontFamily="monospace">HEALTHY</text>
            </g>
          </svg>
        </div>
      </div>

      {/* Services Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {MOCK_SERVICES.map((s) => (
          <div key={s.id} className="console-card space-y-3">
            <div className="flex items-start justify-between">
              <div className="flex items-center space-x-3">
                <div className="w-10 h-10 rounded-lg bg-brand-blue/10 border border-brand-blue/30 flex items-center justify-center text-brand-blue">
                  <Server className="w-5 h-5" />
                </div>
                <div>
                  <h3 className="font-bold text-white text-sm">{s.name}</h3>
                  <span className="text-xs font-mono text-gray-500">{s.slug}</span>
                </div>
              </div>

              <span className={`text-[10px] font-mono font-bold px-2 py-0.5 rounded ${
                s.slug === 'payments-service' ? 'badge-degraded' : 'badge-healthy'
              }`}>
                {s.slug === 'payments-service' ? 'DEGRADED' : 'HEALTHY'}
              </span>
            </div>

            <p className="text-xs text-gray-400 font-mono leading-relaxed">{s.description}</p>

            <div className="flex items-center justify-between text-xs font-mono pt-3 border-t border-[#1A1D2B]">
              <span className="text-gray-500">Runtime: <strong className="text-gray-300">{s.runtime}</strong></span>
              <span className="text-gray-500">Version: <strong className="text-gray-300">{s.current_version}</strong></span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
