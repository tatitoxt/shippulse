"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError } from "./api";

// usePoll loads data immediately and then every intervalMs while active() is true.
export function usePoll<T>(load: () => Promise<T>, intervalMs: number, active: (d: T | null) => boolean = () => true) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<ApiError | Error | null>(null);
  const loadRef = useRef(load);
  loadRef.current = load;
  const activeRef = useRef(active);
  activeRef.current = active;

  const refresh = useCallback(async () => {
    try {
      const d = await loadRef.current();
      setData(d);
      setError(null);
      return d;
    } catch (e) {
      setError(e as Error);
      return null;
    }
  }, []);

  useEffect(() => {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const tick = async () => {
      const d = await refresh();
      if (!stopped && activeRef.current(d)) timer = setTimeout(tick, intervalMs);
    };
    tick();
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [refresh, intervalMs]);

  return { data, error, refresh };
}
