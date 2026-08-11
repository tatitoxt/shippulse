"use client";

import { useEffect, useState } from "react";
import { Search, Server, Rocket, AlertTriangle, FileText, Activity, Terminal, X } from "lucide-react";
import Link from "next/navigation";

export default function CommandPalette({ isOpen, onClose }: { isOpen: boolean; onClose: () => void }) {
  const [query, setQuery] = useState("");

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        if (isOpen) onClose();
        else {
          // Open
          const evt = new CustomEvent("open-command-palette");
          window.dispatchEvent(evt);
        }
      }
      if (e.key === "Escape" && isOpen) {
        onClose();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  const commands = [
    { name: "Service Catalog", href: "/services", icon: Server, group: "Navigation" },
    { name: "Deployments Engine", href: "/deployments", icon: Rocket, group: "Navigation" },
    { name: "Incident Command Center", href: "/incidents", icon: AlertTriangle, group: "Navigation" },
    { name: "Log Explorer & Tail", href: "/logs", icon: FileText, group: "Observability" },
    { name: "Distributed Tracing", href: "/traces", icon: Activity, group: "Observability" },
    { name: "Environment Overview", href: "/environments", icon: Terminal, group: "Operations" },
  ];

  const filtered = commands.filter(c => c.name.toLowerCase().includes(query.toLowerCase()));

  return (
    <div className="fixed inset-0 z-50 bg-black/70 backdrop-blur-sm flex items-start justify-center pt-24 px-4">
      <div className="bg-[#10121B] border border-[#272B3F] w-full max-w-xl rounded-xl shadow-2xl overflow-hidden animate-in fade-in zoom-in-95 duration-150">
        <div className="flex items-center px-4 py-3 border-b border-[#1A1D2B]">
          <Search className="w-5 h-5 text-gray-400 mr-3" />
          <input
            type="text"
            placeholder="Type a command or search services (⌘K)..."
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            autoFocus
            className="bg-transparent w-full text-white placeholder-gray-500 focus:outline-none text-sm font-mono"
          />
          <button onClick={onClose} className="text-gray-500 hover:text-gray-300">
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="max-h-80 overflow-y-auto p-2">
          {filtered.length === 0 ? (
            <div className="p-4 text-center text-xs text-gray-500 font-mono">No matching commands found</div>
          ) : (
            filtered.map((item, i) => (
              <a
                key={i}
                href={item.href}
                onClick={onClose}
                className="flex items-center px-3 py-2.5 rounded-lg hover:bg-[#1A1D2B] text-sm text-gray-300 hover:text-white transition-colors cursor-pointer group"
              >
                <item.icon className="w-4 h-4 text-brand-blue mr-3 group-hover:scale-110 transition-transform" />
                <span className="font-medium">{item.name}</span>
                <span className="ml-auto text-xs font-mono text-gray-500">{item.group}</span>
              </a>
            ))
          )}
        </div>

        <div className="bg-[#090A0F] px-4 py-2 border-t border-[#1A1D2B] flex items-center justify-between text-xs text-gray-500 font-mono">
          <span>Use ↑↓ to navigate</span>
          <span>ESC to close</span>
        </div>
      </div>
    </div>
  );
}
