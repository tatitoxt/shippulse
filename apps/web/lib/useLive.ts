"use client";

import { useCallback, useEffect, useRef } from "react";
import { getToken } from "./api";
import { usePoll } from "./usePoll";

// useLive replaces fast polling: the initial load and a slow 30 s backstop poll
// come from usePoll, while GET /api/v1/events (SSE) triggers an instant refresh
// whenever the org's deployments, steps or promoted versions change.
export function useLive<T>(load: () => Promise<T>, active: (d: T | null) => boolean = () => true) {
  const { data, error, refresh } = usePoll(load, 30_000, active);
  const activeRef = useRef(active);
  activeRef.current = active;
  const dataRef = useRef(data);
  dataRef.current = data;

  const onEvent = useCallback(() => {
    if (activeRef.current(dataRef.current)) void refresh();
  }, [refresh]);

  useEffect(() => {
    const token = getToken();
    if (!token) return;
    const es = new EventSource(`/api/v1/events?token=${encodeURIComponent(token)}`);
    es.onmessage = onEvent;
    return () => es.close();
  }, [onEvent]);

  return { data, error, refresh };
}
