"use client";

import { Activity, Clock, Layers, AlertCircle, CheckCircle2 } from "lucide-react";

export default function TracesPage() {
  const spans = [
    { name: "HTTP POST /api/v1/checkout", service: "api-gateway", duration: "128ms", status: "200 OK", depth: 0, width: "100%", color: "#3B82F6" },
    { name: "Auth Token Verification", service: "api-gateway", duration: "12ms", status: "OK", depth: 1, width: "15%", color: "#06B6D4" },
    { name: "gRPC ChargeCustomer", service: "payments-service", duration: "98ms", status: "ERROR", depth: 1, width: "75%", color: "#F43F5E" },
    { name: "Stripe API Request", service: "payments-service", duration: "84ms", status: "TIMEOUT", depth: 2, width: "65%", color: "#F59E0B" },
  ];

  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold text-white tracking-tight flex items-center gap-2">
            Distributed Tracing Waterfall
          </h1>
          <p className="text-xs text-gray-400 font-mono mt-0.5">
            OpenTelemetry span timeline breakdown across microservice call chains.
          </p>
        </div>
      </div>

      <div className="console-card space-y-4">
        <div className="flex items-center justify-between border-b border-[#1A1D2B] pb-3 font-mono text-xs">
          <span className="text-white font-bold">Trace ID: <strong className="text-brand-cyan">3e110c4d-9a80</strong></span>
          <span className="text-gray-400">Total Duration: <strong className="text-white">128ms</strong> (4 Spans)</span>
        </div>

        {/* Span Waterfall Timeline */}
        <div className="space-y-3 pt-2">
          {spans.map((span, i) => (
            <div key={i} className="space-y-1.5" style={{ paddingLeft: `${span.depth * 24}px` }}>
              <div className="flex items-center justify-between text-xs font-mono">
                <div className="flex items-center space-x-2">
                  <span className="font-bold text-white">{span.name}</span>
                  <span className="text-[10px] bg-[#1A1D2B] text-gray-400 px-1.5 py-0.5 rounded">{span.service}</span>
                </div>
                <div className="flex items-center space-x-3">
                  <span className="text-gray-400">{span.duration}</span>
                  <span className={`text-[10px] font-bold px-1.5 py-0.5 rounded ${
                    span.status === 'ERROR' || span.status === 'TIMEOUT' ? 'bg-rose-500/20 text-rose-400' : 'bg-emerald-500/20 text-emerald-400'
                  }`}>
                    {span.status}
                  </span>
                </div>
              </div>

              {/* Span Timeline Bar */}
              <div className="h-2 bg-[#1A1D2B] rounded-full overflow-hidden w-full">
                <div
                  className="h-full rounded-full transition-all duration-300"
                  style={{ width: span.width, backgroundColor: span.color }}
                />
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
