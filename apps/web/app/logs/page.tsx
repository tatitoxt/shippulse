"use client";

import { useState } from "react";
import { FileText, Search, Filter, RefreshCw, Terminal, ExternalLink } from "lucide-react";

export default function LogsPage() {
  const [filter, setQuery] = useState("");

  const logs = [
    { time: "14:15:02.102", service: "api-gateway", severity: "INFO", trace_id: "7f90ab12", message: "HTTP GET /api/v1/services - 200 OK (12ms)" },
    { time: "14:15:01.884", service: "payments-service", severity: "WARN", trace_id: "3e110c4d", message: "Stripe API rate limit approaching threshold (85% capacity)" },
    { time: "14:14:58.410", service: "payments-service", severity: "ERROR", trace_id: "3e110c4d", message: "Gateway Timeout calling upstream payment gateway: Connection reset by peer" },
    { time: "14:14:55.012", service: "async-worker", severity: "INFO", trace_id: "88a10f92", message: "NATS Event processed: shippulse.events.deployment.completed" },
  ];

  return (
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold text-white tracking-tight flex items-center gap-2">
            ClickHouse Log Explorer
          </h1>
          <p className="text-xs text-gray-400 font-mono mt-0.5">
            High-throughput log stream with OpenTelemetry trace correlation links.
          </p>
        </div>
      </div>

      {/* Filter Bar */}
      <div className="console-card flex items-center space-x-3 py-3">
        <Search className="w-4 h-4 text-gray-500" />
        <input
          type="text"
          placeholder="Filter logs by service, error, trace ID..."
          value={filter}
          onChange={(e) => setQuery(e.target.value)}
          className="bg-transparent flex-1 text-xs font-mono text-white placeholder-gray-500 focus:outline-none"
        />
      </div>

      {/* Log Console Output */}
      <div className="bg-[#090A0F] border border-[#1A1D2B] rounded-xl p-4 font-mono text-xs space-y-2 max-h-[500px] overflow-y-auto">
        {logs.map((log, i) => (
          <div key={i} className="flex items-start space-x-3 hover:bg-[#10121B] p-2 rounded transition-colors font-mono">
            <span className="text-gray-500 select-none">{log.time}</span>
            <span className="text-brand-blue font-semibold">{log.service}</span>
            <span className={`px-1.5 py-0.5 rounded text-[10px] font-bold ${
              log.severity === 'ERROR' ? 'bg-rose-500/20 text-rose-400' :
              log.severity === 'WARN' ? 'bg-amber-500/20 text-amber-400' : 'bg-blue-500/20 text-blue-400'
            }`}>
              {log.severity}
            </span>
            <span className="text-gray-300 flex-1">{log.message}</span>
            <a href={`/traces`} className="text-brand-cyan hover:underline flex items-center gap-1 text-[11px]">
              <span>trace:{log.trace_id}</span>
              <ExternalLink className="w-3 h-3" />
            </a>
          </div>
        ))}
      </div>
    </div>
  );
}
