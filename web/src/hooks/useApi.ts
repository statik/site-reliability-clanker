import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, apiGet, errorMessage } from "@/api/client";
import type { AuditEvent } from "@/api/types";
import { useLive } from "@/state/live";
import { useSession } from "@/state/session";

const DEBOUNCE_MS = 500;

export interface ApiState<T> {
  data: T | null;
  error: string | null;
  /** The HTTP status of the error, or null when there is no error or no response. */
  status: number | null;
  loading: boolean;
  reload: () => void;
}

interface Options {
  /** Loads again when a live event passes this test. A reconnect always loads again. */
  live?: (event: AuditEvent) => boolean;
  /** Loads again on this interval, for data that changes without audit events. */
  pollMs?: number;
  /** Loads the live state even while the dashboard shows a past time. */
  ignoreAsOf?: boolean;
}

interface Settled<T> {
  /** The request that this result answers: path, user and as-of time. */
  key: string;
  version: number;
  data: T | null;
  error: string | null;
  status: number | null;
}

export const anyEvent = () => true;

/**
 * Loads an API path and keeps it current. It sends the as-of time to the endpoints that honour
 * it, keeps the last good data when a reload fails, and reports the server's error text.
 */
export function useApi<T>(path: string | null, options: Options = {}): ApiState<T> {
  const session = useSession();
  const userName = session.userName;
  const asOf = options.ignoreAsOf === true ? null : session.asOf;
  const { subscribe } = useLive();
  const key = `${path ?? ""}|${userName}|${asOf?.getTime() ?? "live"}`;
  const [settled, setSettled] = useState<Settled<T>>({ key: "", version: -1, data: null,
    error: null, status: null });
  const [version, setVersion] = useState(0);
  const reload = useCallback(() => setVersion((current) => current + 1), []);
  const liveTest = useRef(options.live);
  useEffect(() => {
    liveTest.current = options.live;
  });

  useEffect(() => {
    if (path === null) {
      return;
    }
    let cancelled = false;
    apiGet<T>(path, asOf).then(
      (data) => {
        if (!cancelled) {
          setSettled({ key, version, data, error: null, status: null });
        }
      },
      (failure: unknown) => {
        if (!cancelled) {
          setSettled((last) => ({ key, version, data: last.key === key ? last.data : null,
            error: errorMessage(failure),
            status: failure instanceof ApiError ? failure.status : null }));
        }
      },
    );
    return () => {
      cancelled = true;
    };
  }, [path, asOf, key, version]);

  useEffect(() => {
    let timer: number | undefined;
    const unsubscribe = subscribe((notice) => {
      const test = liveTest.current;
      if (test === undefined || (notice !== null && !test(notice))) {
        return;
      }
      // A burst of events gives one reload. The timer is not restarted, so that a steady
      // stream of events cannot postpone the reload for ever.
      timer ??= window.setTimeout(() => {
        timer = undefined;
        setVersion((current) => current + 1);
      }, DEBOUNCE_MS);
    });
    return () => {
      window.clearTimeout(timer);
      unsubscribe();
    };
  }, [subscribe]);

  const pollMs = options.pollMs;
  useEffect(() => {
    if (pollMs === undefined) {
      return;
    }
    const timer = window.setInterval(() => setVersion((current) => current + 1), pollMs);
    return () => window.clearInterval(timer);
  }, [pollMs]);

  const current = settled.key === key && path !== null;
  return {
    data: current ? settled.data : null,
    error: current ? settled.error : null,
    status: current ? settled.status : null,
    loading: path !== null && (settled.key !== key || settled.version !== version),
    reload,
  };
}
