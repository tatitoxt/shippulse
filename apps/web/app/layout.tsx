"use client";

import { useState, useEffect } from "react";
import "./globals.css";
import { 
  Activity, 
  AlertTriangle, 
  Box, 
  CheckCircle2, 
  Clock, 
  FileText, 
  Layers, 
  LayoutDashboard, 
  Rocket, 
  Search, 
  Server, 
  Settings, 
  ShieldCheck, 
  Terminal,
  ChevronRight
} from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import CommandPalette from "@/components/CommandPalette";

export default function RootLayout({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const [isCommandPaletteOpen, setIsCommandPaletteOpen] = useState(false);

  useEffect(() => {
    const handleOpen = () => setIsCommandPaletteOpen(true);
    window.addEventListener("open-command-palette", handleOpen);
    return () => window.removeEventListener("open-command-palette", handleOpen);
  }, []);

  const navItems = [
    { name: "Dashboard", href: "/", icon: LayoutDashboard },
    { name: "Service Catalog", href: "/services", icon: Server },
    { name: "Deployments", href: "/deployments", icon: Rocket },
    { name: "Incidents", href: "/incidents", icon: AlertTriangle, badge: "1 Active" },
    { name: "Logs Explorer", href: "/logs", icon: FileText },
    { name: "Traces", href: "/traces", icon: Activity },
    { name: "Environments", href: "/environments", icon: Layers },
    { name: "SLOs & Budgets", href: "/slos", icon: ShieldCheck },
    { name: "Settings", href: "/settings", icon: Settings },
  ];

  return (
    <html lang="en">
      <body className="bg-[#090A0F] text-slate-200 antialiased font-sans flex flex-col min-h-screen">
        {/* Top Header Console Bar */}
        <header className="h-14 bg-[#10121B] border-b border-[#1A1D2B] px-6 flex items-center justify-between sticky top-0 z-40">
          <div className="flex items-center space-x-4">
            <Link href="/" className="flex items-center space-x-2.5 group">
              <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-brand-blue to-brand-cyan flex items-center justify-center font-bold text-white shadow-lg shadow-blue-500/20 group-hover:scale-105 transition-transform">
                SP
              </div>
              <div className="flex flex-col">
                <span className="font-bold text-sm text-white tracking-tight flex items-center gap-1.5">
                  ShipPulse <span className="text-[10px] bg-brand-blue/20 text-brand-blue border border-brand-blue/30 px-1.5 py-0.5 rounded font-mono font-medium">CONTROL PLANE</span>
                </span>
                <span className="text-[11px] text-gray-500 font-mono">v1.0.0-production</span>
              </div>
            </Link>

            <div className="h-4 w-[1px] bg-[#1A1D2B] hidden md:block" />

            <div className="hidden md:flex items-center space-x-2 text-xs font-mono text-gray-400">
              <span className="flex items-center gap-1 text-emerald-400 bg-emerald-500/10 px-2 py-0.5 rounded border border-emerald-500/20">
                <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse" />
                SYSTEM HEALTHY
              </span>
              <span className="text-gray-600">|</span>
              <span>ORG: <strong className="text-gray-300">ShipPulse Global</strong></span>
            </div>
          </div>

          <div className="flex items-center space-x-3">
            <button
              onClick={() => setIsCommandPaletteOpen(true)}
              className="flex items-center space-x-2 bg-[#1A1D2B] hover:bg-[#272B3F] text-gray-400 hover:text-white px-3 py-1.5 rounded-lg border border-[#272B3F] text-xs font-mono transition-colors"
            >
              <Search className="w-3.5 h-3.5" />
              <span>Search / Commands</span>
              <kbd className="bg-[#090A0F] text-gray-400 px-1.5 py-0.5 rounded text-[10px] border border-gray-800">⌘K</kbd>
            </button>

            <div className="w-8 h-8 rounded-full bg-gradient-to-tr from-purple-500 to-indigo-500 flex items-center justify-center text-xs font-bold text-white border border-purple-400/30">
              FP
            </div>
          </div>
        </header>

        {/* Main Content Layout */}
        <div className="flex flex-1">
          {/* Navigation Sidebar */}
          <aside className="w-64 bg-[#090A0F] border-r border-[#1A1D2B] p-4 hidden md:flex flex-col justify-between">
            <div className="space-y-1">
              <div className="px-3 py-2 text-[10px] font-mono text-gray-500 uppercase tracking-wider">
                Control Center
              </div>
              {navItems.map((item) => {
                const isActive = pathname === item.href;
                return (
                  <Link
                    key={item.href}
                    href={item.href}
                    className={`flex items-center justify-between px-3 py-2.5 rounded-lg text-xs font-medium transition-all ${
                      isActive
                        ? "bg-brand-blue/10 text-brand-blue border border-brand-blue/30 shadow-[0_0_12px_rgba(59,130,246,0.1)]"
                        : "text-gray-400 hover:text-white hover:bg-[#10121B]"
                    }`}
                  >
                    <div className="flex items-center space-x-3">
                      <item.icon className={`w-4 h-4 ${isActive ? "text-brand-blue" : "text-gray-500"}`} />
                      <span>{item.name}</span>
                    </div>
                    {item.badge && (
                      <span className="text-[10px] font-mono bg-rose-500/20 text-rose-400 border border-rose-500/30 px-1.5 py-0.5 rounded-full font-bold">
                        {item.badge}
                      </span>
                    )}
                  </Link>
                );
              })}
            </div>

            {/* Agent Connection Indicator Box */}
            <div className="bg-[#10121B] border border-[#1A1D2B] p-3 rounded-xl space-y-2">
              <div className="flex items-center justify-between text-xs font-mono">
                <span className="text-gray-400 flex items-center gap-1.5">
                  <Terminal className="w-3.5 h-3.5 text-brand-cyan" />
                  Agent Daemon
                </span>
                <span className="text-emerald-400 text-[10px] font-bold">ONLINE</span>
              </div>
              <p className="text-[11px] text-gray-500 leading-snug font-mono">
                local-docker-agent connected via outbound TLS.
              </p>
            </div>
          </aside>

          {/* Page Main Viewport */}
          <main className="flex-1 bg-[#090A0F] p-6 overflow-y-auto">
            {children}
          </main>
        </div>

        {/* Command Palette Modal */}
        <CommandPalette isOpen={isCommandPaletteOpen} onClose={() => setIsCommandPaletteOpen(false)} />
      </body>
    </html>
  );
}
