"use client";

import { 
  Activity, 
  AlertTriangle, 
  ArrowUpRight, 
  CheckCircle2, 
  Clock, 
  Cpu, 
  Layers, 
  RefreshCw, 
  Rocket, 
  Server, 
  ShieldCheck, 
  TrendingDown, 
  TrendingUp, 
  XCircle 
} from "lucide-react";
import Link from "next/link";
import { MOCK_SERVICES, MOCK_DEPLOYMENTS, MOCK_INCIDENTS, MOCK_SLOS } from "@/lib/api";

export default function DashboardPage() {
  return (
    <div className="space-y-6">
      {/* Page Title & Controls */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold text-white tracking-tight flex items-center gap-2">
            Control Plane Overview
            <span className="text-xs font-mono font-normal bg-brand-blue/10 text-brand-blue px-2 py-0.5 rounded border border-brand-blue/20">LIVE REALTIME</span>
          </h1>
          <p className="text-xs text-gray-400 font-mono mt-0.5">
            Real-time control center for deployments, service health, incidents, and OpenTelemetry signals.
          </p>
        </div>

        <div className="flex items-center space-x-2">
          <Link
            href="/deployments"
            className="flex items-center space-x-2 bg-brand-blue hover:bg-blue-600 text-white px-3.5 py-1.5 rounded-lg text-xs font-semibold shadow-lg shadow-blue-500/20 transition-all cursor-pointer"
          >
            <Rocket className="w-3.5 h-3.5" />
            <span>New Deployment</span>
          </Link>
        </div>
      </div>

      {/* Active Incident Banner if Any */}
      {MOCK_INCIDENTS.length > 0 && (
        <div className="bg-rose-500/10 border border-rose-500/30 rounded-xl p-4 flex flex-col sm:flex-row sm:items-center justify-between gap-3 shadow-[0_0_20px_rgba(244,63,94,0.1)] animate-pulse">
          <div className="flex items-center space-x-3">
            <div className="w-9 h-9 rounded-lg bg-rose-500/20 border border-rose-500/40 flex items-center justify-center text-rose-400">
              <AlertTriangle className="w-5 h-5" />
            </div>
            <div>
              <div className="flex items-center space-x-2">
                <span className="text-xs font-bold font-mono bg-rose-500/30 text-rose-300 px-1.5 py-0.5 rounded">
                  {MOCK_INCIDENTS[0].severity}
                </span>
                <h3 className="text-sm font-semibold text-white">{MOCK_INCIDENTS[0].title}</h3>
              </div>
              <p className="text-xs text-gray-400 mt-0.5 font-mono">{MOCK_INCIDENTS[0].summary}</p>
            </div>
          </div>

          <Link
            href={`/incidents/${MOCK_INCIDENTS[0].id}`}
            className="self-start sm:self-auto bg-rose-500 text-white hover:bg-rose-600 px-3 py-1.5 rounded-lg text-xs font-semibold transition-colors flex items-center gap-1 font-mono"
          >
            <span>Investigate Incident</span>
            <ArrowUpRight className="w-3.5 h-3.5" />
          </Link>
        </div>
      )}

      {/* Top Metrics Cards Grid */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <div className="console-card">
          <div className="flex items-center justify-between text-xs text-gray-400 font-mono mb-2">
            <span>SERVICE HEALTH</span>
            <Server className="w-4 h-4 text-emerald-400" />
          </div>
          <div className="flex items-baseline justify-between">
            <span className="text-2xl font-extrabold text-white font-mono">3 / 4</span>
            <span className="text-xs font-mono text-emerald-400 bg-emerald-500/10 px-2 py-0.5 rounded border border-emerald-500/20">
              75% Healthy
            </span>
          </div>
          <p className="text-[11px] text-gray-500 font-mono mt-1">1 Degraded (Payments Engine)</p>
        </div>

        <div className="console-card">
          <div className="flex items-center justify-between text-xs text-gray-400 font-mono mb-2">
            <span>DEPLOYMENTS (24H)</span>
            <Rocket className="w-4 h-4 text-brand-blue" />
          </div>
          <div className="flex items-baseline justify-between">
            <span className="text-2xl font-extrabold text-white font-mono">14</span>
            <span className="text-xs font-mono text-emerald-400 flex items-center gap-1">
              <TrendingUp className="w-3 h-3" /> 100% Rate
            </span>
          </div>
          <p className="text-[11px] text-gray-500 font-mono mt-1">Last deploy 15 mins ago (v1.9.3)</p>
        </div>

        <div className="console-card">
          <div className="flex items-center justify-between text-xs text-gray-400 font-mono mb-2">
            <span>P95 LATENCY</span>
            <Activity className="w-4 h-4 text-brand-cyan" />
          </div>
          <div className="flex items-baseline justify-between">
            <span className="text-2xl font-extrabold text-white font-mono">42ms</span>
            <span className="text-xs font-mono text-emerald-400 flex items-center gap-1">
              <TrendingDown className="w-3 h-3" /> -8ms vs avg
            </span>
          </div>
          <p className="text-[11px] text-gray-500 font-mono mt-1">Target &lt; 250ms</p>
        </div>

        <div className="console-card">
          <div className="flex items-center justify-between text-xs text-gray-400 font-mono mb-2">
            <span>ERROR BUDGET</span>
            <ShieldCheck className="w-4 h-4 text-purple-400" />
          </div>
          <div className="flex items-baseline justify-between">
            <span className="text-2xl font-extrabold text-white font-mono">88.5%</span>
            <span className="text-xs font-mono text-purple-400 bg-purple-500/10 px-2 py-0.5 rounded border border-purple-500/20">
              30-Day Window
            </span>
          </div>
          <p className="text-[11px] text-gray-500 font-mono mt-1">SLO Target 99.9%</p>
        </div>
      </div>

      {/* Two Column Section: Services Status & Recent Deployments */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Left Column: Service Matrix */}
        <div className="lg:col-span-2 console-card space-y-4">
          <div className="flex items-center justify-between border-b border-[#1A1D2B] pb-3">
            <div className="flex items-center space-x-2">
              <Server className="w-4 h-4 text-brand-blue" />
              <h2 className="text-sm font-bold text-white font-mono uppercase tracking-wide">Production Service Catalog</h2>
            </div>
            <Link href="/services" className="text-xs font-mono text-brand-blue hover:underline flex items-center gap-1">
              <span>View Graph</span>
              <ArrowUpRight className="w-3 h-3" />
            </Link>
          </div>

          <div className="divide-y divide-[#1A1D2B]">
            {MOCK_SERVICES.map((service) => (
              <div key={service.id} className="py-3 flex items-center justify-between hover:bg-[#1A1D2B]/50 px-2 rounded-lg transition-colors">
                <div className="flex items-center space-x-3">
                  <div className={`w-2.5 h-2.5 rounded-full ${service.slug === 'payments-service' ? 'bg-amber-400 animate-pulse' : 'bg-emerald-400'}`} />
                  <div>
                    <Link href={`/services/${service.id}`} className="text-sm font-semibold text-white hover:text-brand-blue transition-colors">
                      {service.name}
                    </Link>
                    <div className="flex items-center space-x-2 text-[11px] font-mono text-gray-500 mt-0.5">
                      <span>{service.runtime}</span>
                      <span>•</span>
                      <span>{service.current_version}</span>
                      <span>•</span>
                      <span>{service.owner_team}</span>
                    </div>
                  </div>
                </div>

                <div className="flex items-center space-x-3">
                  <span className={`text-[10px] font-mono font-bold px-2 py-0.5 rounded ${
                    service.slug === 'payments-service' ? 'badge-degraded' : 'badge-healthy'
                  }`}>
                    {service.slug === 'payments-service' ? 'DEGRADED' : 'HEALTHY'}
                  </span>
                </div>
              </div>
            ))}
          </div>
        </div>

        {/* Right Column: Recent Deployments Feed */}
        <div className="console-card space-y-4">
          <div className="flex items-center justify-between border-b border-[#1A1D2B] pb-3">
            <div className="flex items-center space-x-2">
              <Rocket className="w-4 h-4 text-brand-cyan" />
              <h2 className="text-sm font-bold text-white font-mono uppercase tracking-wide">Recent Deployments</h2>
            </div>
            <Link href="/deployments" className="text-xs font-mono text-brand-blue hover:underline">
              All
            </Link>
          </div>

          <div className="space-y-3">
            {MOCK_DEPLOYMENTS.map((deploy) => (
              <div key={deploy.id} className="p-3 bg-[#090A0F] border border-[#1A1D2B] rounded-lg space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-bold text-white font-mono">{deploy.version}</span>
                  <span className={`text-[10px] font-mono font-bold px-1.5 py-0.5 rounded ${
                    deploy.status === 'SUCCEEDED' ? 'bg-emerald-500/20 text-emerald-400' : 'bg-rose-500/20 text-rose-400'
                  }`}>
                    {deploy.status}
                  </span>
                </div>
                <p className="text-xs text-gray-400 line-clamp-1">{deploy.commit_message}</p>
                <div className="flex items-center justify-between text-[10px] font-mono text-gray-500 pt-1 border-t border-[#1A1D2B]/50">
                  <span>{deploy.author}</span>
                  <span>{deploy.git_sha}</span>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
