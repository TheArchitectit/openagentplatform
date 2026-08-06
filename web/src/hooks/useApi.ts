// useApi — typed fetch hook layered on the existing apiFetch (which
// handles auth / 401 redirects). Adds exponential-backoff retry and
// optional polling so components don't need to hand-roll fetch logic.
//
// This does NOT duplicate auth — apiFetch in lib/api.ts handles credentials,
// 401 bounce, and JSON parsing. useApi only adds retry + polling on top.
//
// Usage:
//   const { data, isLoading, error, refetch } = useApi<MyType>({
//     path: '/agents',
//     pollIntervalMs: 10_000,
//   });

import { useCallback, useEffect, useRef, useState } from "react";
import { apiFetch, ApiError } from "@/lib/api";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface UseApiOptions {
	/** API path or full URL forwarded to apiFetch. */
	path: string;
	/** Polling interval in ms; pass 0 or leave undefined to disable. */
	pollIntervalMs?: number;
	/** Retry on failure with exponential backoff (default: true). */
	retry?: boolean;
	/** Maximum retry attempts (default: 3). */
	maxRetries?: number;
	/** Base delay in ms before the first retry (doubles on each attempt, default: 1_000). */
	retryBaseMs?: number;
	/** Do not fetch until this is true (default: true). */
	enabled?: boolean;
}

export interface UseApiResult<T> {
	data: T | undefined;
	isLoading: boolean;
	error: Error | null;
	/** Manually re-fetch. Useful for "refresh" buttons. */
	refetch: () => Promise<void>;
}

// ---------------------------------------------------------------------------
// Retry helper
// ---------------------------------------------------------------------------

function sleep(ms: number) {
	return new Promise<void>((resolve) => setTimeout(resolve, ms));
}

async function fetchWithRetry<T>(
	path: string,
	opts: { maxRetries: number; baseMs: number; signal?: AbortSignal },
): Promise<T> {
	let lastError: Error | null = null;
	for (let attempt = 1; attempt <= opts.maxRetries; attempt++) {
		try {
			return await apiFetch<T>(path, { signal: opts.signal });
		} catch (err) {
			lastError = err instanceof Error ? err : new Error(String(err));
			// Do not retry 4xx client errors (except 429).
			if (err instanceof ApiError) {
				const status = err.status;
				if (status >= 400 && status < 500 && status !== 429) throw err;
			}
			if (attempt < opts.maxRetries) {
				const delay = opts.baseMs * 2 ** (attempt - 1);
				await sleep(delay);
			}
		}
	}
	// eslint-disable-next-line @typescript-eslint/no-throw-literal
	throw lastError ?? new Error("fetchWithRetry: unexpected null error");
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export function useApi<T = unknown>(options: UseApiOptions): UseApiResult<T> {
	const {
		path,
		pollIntervalMs,
		retry = false,
		maxRetries: maxRetriesRaw,
		retryBaseMs = 1_000,
		enabled = true,
	} = options;

	const maxRetries = maxRetriesRaw ?? (retry ? 3 : 1);

	const [data, setData] = useState<T | undefined>(undefined);
	const [isLoading, setIsLoading] = useState(enabled);
	const [error, setError] = useState<Error | null>(null);

	const mountedRef = useRef(true);
	const pollTimerRef = useRef<ReturnType<typeof setInterval>>();
	const abortRef = useRef<AbortController>();

	const fetchFn = useCallback(async () => {
		// Cancel any in-flight request.
		abortRef.current?.abort();
		const controller = new AbortController();
		abortRef.current = controller;

		setIsLoading(true);
		setError(null);
		try {
			const result = await fetchWithRetry<T>(path, {
				maxRetries,
				baseMs: retryBaseMs,
				signal: controller.signal,
			});
			if (mountedRef.current && !controller.signal.aborted) {
				setData(result);
				setError(null);
			}
		} catch (err) {
			if (mountedRef.current && !controller.signal.aborted) {
				setError(err instanceof Error ? err : new Error(String(err)));
			}
		} finally {
			if (mountedRef.current) {
				setIsLoading(false);
			}
		}
	}, [path, maxRetries, retryBaseMs]);

	useEffect(() => {
		mountedRef.current = true;
		if (enabled) {
			void fetchFn();
		} else {
			setIsLoading(false);
		}
		return () => {
			mountedRef.current = false;
		};
	}, [fetchFn, enabled]);

	// Polling
	useEffect(() => {
		if (!pollIntervalMs || pollIntervalMs <= 0 || !enabled) return;
		pollTimerRef.current = setInterval(() => {
			void fetchFn();
		}, pollIntervalMs);
		return () => {
			if (pollTimerRef.current) clearInterval(pollTimerRef.current);
		};
	}, [pollIntervalMs, fetchFn, enabled]);

	// Cleanup in-flight requests on unmount.
	useEffect(() => {
		return () => {
			abortRef.current?.abort();
		};
	}, []);

	return {
		data,
		isLoading,
		error,
		refetch: fetchFn,
	};
}
