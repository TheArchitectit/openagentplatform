// useChannel — thin React hook over the existing multiplexed WsClient.
//
// Subscribes to a named channel on the singleton WebSocket and returns
// the latest envelope received on that channel. Unsubscribes on unmount.
//
// This is the live-update hook called out in the dashboard-foundation
// task. We intentionally do NOT add an SSE client — the existing
// lib/websocket.ts WsClient is already multiplexed with reconnect,
// heartbeat, and channel support, which is superior to SSE.
//
// Usage:
//   const lastEnvelope = useChannel('alerts');
//   if (lastEnvelope) { ... render based on event type ... }

import { useEffect, useRef, useState } from "react";
import {
	getWsClient,
	type Channel,
	type WsEnvelope,
	type Status,
} from "@/lib/websocket";

export interface UseChannelResult {
	/** Most recent envelope received on this channel (undefined until first event). */
	last: WsEnvelope | null;
	/** Current WebSocket connection status. */
	status: Status;
}

export function useChannel(channel: Channel): UseChannelResult {
	const [last, setLast] = useState<WsEnvelope | null>(null);
	const [status, setStatus] = useState<Status>("closed");
	const mountedRef = useRef(true);

	useEffect(() => {
		mountedRef.current = true;
		const ws = getWsClient();
		setStatus(ws.getStatus());

		const statusInterval = setInterval(() => {
			if (mountedRef.current) setStatus(ws.getStatus());
		}, 1000);

		const handler = (env: WsEnvelope) => {
			if (mountedRef.current) setLast(env);
		};

		const unsub = ws.subscribe(channel, handler);

		return () => {
			mountedRef.current = false;
			clearInterval(statusInterval);
			unsub();
		};
	}, [channel]);

	return { last, status };
}

export default useChannel;
