"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { api, setToken } from "@/lib/api";
import { ErrorBox } from "@/components/ui";

export default function LoginPage() {
  const router = useRouter();
  const [email, setEmail] = useState("operator@acme.example");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const res = await api<{ token: string }>("/auth/login", {
        method: "POST",
        body: JSON.stringify({ email, password }),
      });
      setToken(res.token);
      router.replace("/deployments");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center p-6">
      <form onSubmit={submit} className="card w-full max-w-sm space-y-4">
        <div>
          <h1 className="text-lg font-semibold text-white">ShipPulse</h1>
          <p className="text-sm text-slate-400">Sign in with a seeded demo account.</p>
        </div>
        <ErrorBox error={error} />
        <div>
          <label className="label" htmlFor="email">Email</label>
          <input id="email" className="input" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="username" />
        </div>
        <div>
          <label className="label" htmlFor="password">Password</label>
          <input
            id="password"
            type="password"
            className="input"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
          />
        </div>
        <button className="btn w-full" disabled={busy} type="submit">
          {busy ? "Signing in…" : "Sign in"}
        </button>
        <p className="text-xs text-slate-500">
          Demo users: operator@acme.example (engineer), viewer@acme.example (viewer), operator@globex.example
          (second tenant). The password is the DEMO_PASSWORD you started the stack with.
        </p>
      </form>
    </main>
  );
}
