import type { NextRequest } from "next/server";

// Same-origin proxy to the Go API. The target is read at request time so the
// same image works locally and in Docker Compose.
export const dynamic = "force-dynamic";

const FORWARDED = ["authorization", "content-type", "idempotency-key", "x-correlation-id"];

async function proxy(req: NextRequest): Promise<Response> {
  const base = process.env.CONTROL_PLANE_URL ?? "http://localhost:8080";
  // EventSource cannot set headers, so the SSE endpoint accepts the JWT as a
  // `token` query param; the proxy translates it back into Authorization and
  // never forwards it in the URL.
  const search = new URLSearchParams(req.nextUrl.search);
  const queryToken = search.get("token");
  search.delete("token");
  const qs = search.toString();
  const target = `${base}${req.nextUrl.pathname}${qs ? `?${qs}` : ""}`;
  const headers = new Headers();
  for (const h of FORWARDED) {
    const v = req.headers.get(h);
    if (v) headers.set(h, v);
  }
  if (queryToken && !headers.has("authorization")) {
    headers.set("authorization", `Bearer ${queryToken}`);
  }
  const hasBody = req.method !== "GET" && req.method !== "HEAD";
  try {
    const res = await fetch(target, {
      method: req.method,
      headers,
      body: hasBody ? await req.arrayBuffer() : undefined,
      cache: "no-store",
      // Abort the upstream request when the downstream client disconnects, so
      // SSE streams (and slow responses) do not pin server resources forever.
      signal: req.signal,
    });
    const out = new Headers();
    for (const h of ["content-type", "x-correlation-id"]) {
      const v = res.headers.get(h);
      if (v) out.set(h, v);
    }
    return new Response(res.body, { status: res.status, headers: out });
  } catch {
    return Response.json(
      { error: { code: "api_unreachable", message: "the ShipPulse API is not reachable" } },
      { status: 502 },
    );
  }
}

export { proxy as GET, proxy as POST };
