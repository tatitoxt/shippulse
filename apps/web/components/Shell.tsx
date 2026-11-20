"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useContext, useEffect, useState } from "react";
import { api, clearToken, getToken } from "@/lib/api";
import type { Principal } from "@/lib/types";

const PrincipalContext = createContext<Principal | null>(null);
export const usePrincipal = () => useContext(PrincipalContext);

const NAV = [
  { href: "/services", label: "Services" },
  { href: "/deployments", label: "Deployments" },
  { href: "/audit", label: "Audit log" },
];

export function Shell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const [me, setMe] = useState<Principal | null>(null);
  const isLogin = pathname === "/login";

  useEffect(() => {
    if (isLogin) return;
    if (!getToken()) {
      router.replace("/login");
      return;
    }
    api<Principal>("/me").then(setMe).catch(() => undefined);
  }, [isLogin, router]);

  if (isLogin) return <>{children}</>;
  if (!me) return <div className="p-8 text-sm text-slate-400">Loading…</div>;

  return (
    <PrincipalContext.Provider value={me}>
      <header className="border-b border-slate-800 bg-slate-900/80">
        <div className="mx-auto flex max-w-6xl items-center justify-between px-6 py-3">
          <div className="flex items-center gap-6">
            <Link href="/deployments" className="font-semibold text-white">
              ShipPulse <span className="text-xs font-normal text-slate-400">Release Safety Console</span>
            </Link>
            <nav className="flex gap-1">
              {NAV.map((n) => (
                <Link
                  key={n.href}
                  href={n.href}
                  className={`rounded px-2 py-1 text-sm ${
                    pathname.startsWith(n.href) ? "bg-slate-800 text-white" : "text-slate-400 hover:text-white"
                  }`}
                >
                  {n.label}
                </Link>
              ))}
            </nav>
          </div>
          <div className="flex items-center gap-3 text-sm">
            <span data-testid="whoami" className="text-slate-400">
              {me.email} · <span className="text-slate-200">{me.organization}</span> ·{" "}
              <span className="font-mono text-xs">{me.role}</span>
            </span>
            <button
              className="btn-secondary"
              onClick={() => {
                clearToken();
                router.replace("/login");
              }}
            >
              Sign out
            </button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-6 py-6">{children}</main>
    </PrincipalContext.Provider>
  );
}
