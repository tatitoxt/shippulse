const STATUS_STYLES: Record<string, string> = {
  queued: "bg-slate-700/40 text-slate-300 border-slate-600",
  awaiting_approval: "bg-amber-500/10 text-amber-300 border-amber-500/40",
  running: "bg-sky-500/10 text-sky-300 border-sky-500/40",
  succeeded: "bg-emerald-500/10 text-emerald-300 border-emerald-500/40",
  failed: "bg-rose-500/10 text-rose-300 border-rose-500/40",
  cancelled: "bg-amber-500/10 text-amber-300 border-amber-500/40",
  skipped: "bg-slate-800 text-slate-500 border-slate-700",
  pending: "bg-slate-800 text-slate-400 border-slate-700",
};

export function StatusBadge({ status, testId }: { status: string; testId?: string }) {
  return (
    <span
      data-testid={testId}
      className={`inline-block rounded border px-1.5 py-0.5 font-mono text-xs ${STATUS_STYLES[status] ?? STATUS_STYLES.pending}`}
    >
      {status}
    </span>
  );
}

export function Time({ value }: { value: string | null | undefined }) {
  if (!value) return <span className="text-slate-600">—</span>;
  const d = new Date(value);
  return (
    <time dateTime={value} title={d.toISOString()} className="font-mono text-xs text-slate-400">
      {d.toISOString().replace("T", " ").slice(0, 19)}Z
    </time>
  );
}

export function ErrorBox({ error }: { error: string | null }) {
  if (!error) return null;
  return (
    <div role="alert" className="rounded border border-rose-500/40 bg-rose-500/10 px-3 py-2 text-sm text-rose-200">
      {error}
    </div>
  );
}
