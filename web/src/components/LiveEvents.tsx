// LiveEvents — real-time event feed panel fed by the multiplexed WebSocket.
//
// Shows a rolling log of events from the `alerts` channel (highest signal)
// plus a status indicator for the WebSocket connection (auto-reconnect).
//
// Usage:
//   import { LiveEvents } from "@/components/LiveEvents";
//   // Can be placed in a drawer, sidebar, or as a standalone panel.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Bell, BellOff, RefreshCw, Wifi, WifiOff } from "lucide-react";
import { getWsClient, type WsEnvelope, type Status } from "@/lib/websocket";
import { cn } from "@/lib/cn";

export interface LiveEvent {
	id: string;
	channel: string;
	event: string;
	timestamp: Date;
	data?: unknown;
}

interface LiveEventsProps {
	/** Maximum number of events to keep in the feed. Default 50. */
	maxEvents?: number;
	/** Channels to subscribe to. Default ['alerts']. */
	channels?: string[];
	/** Called when the panel is unmounted. */
	onUnmount?: () => void;
}

const DEFAULT_CHANNELS = ["alerts"];
const DEFAULT_MAX_EVENTS = 50;

function formatEventTime(ts: Date): string {
	const now = new Date();
	const diffMs = now.getTime() - ts.getTime();
	const sec = Math.floor(diffMs / 1000);
	if (sec < 60) return `${sec}s ago`;
	const min = Math.floor(sec / 60);
	if (min < 60) return `${min}m ago`;
	const hr = Math.floor(min / 60);
	if (hr < 24) return `${hr}h ago`;
	const days = Math.floor(hr / 24);
	return `${days}d ago`;
}

function EventBadge({ event }: { event: string }) {
	// Parse event name for badge styling
	const parts = event.split(".");
	const last = parts[parts.length - 1] ?? event;
	const isCritical = ["critical", "error", "failed"].some((s) =>
		last.toLowerCase().includes(s),
	);
	const isWarning = ["warning", "snoozed"].some((s) =>
		last.toLowerCase().includes(s),
	);

	return (
		<span
			className={cn(
				"inline-flex items-center rounded px-1.5 py-0.5 text-xs font-mono",
				isCritical && "bg-red-500/20 text-red-400",
				isWarning && "bg-yellow-500/20 text-yellow-400",
				!isCritical && !isWarning && "bg-blue-500/20 text-blue-400",
			)}
		>
			{last}
		</span>
	);
}

export function LiveEvents({
	maxEvents = DEFAULT_MAX_EVENTS,
	channels = DEFAULT_CHANNELS,
	onUnmount,
}: LiveEventsProps) {
	const [events, setEvents] = useState<LiveEvent[]>([]);
	const [status, setStatus] = useState<Status>(() => getWsClient().getStatus());
	const clientRef = useRef(getWsClient());
	const unsubscribeRef = useRef<(() => void) | null>(null);

	const handleStatusChange = useCallback((s: Status) => {
		setStatus(s);
	}, []);

	useEffect(() => {
		const client = clientRef.current;
		// @ts-expect-error — onStatusChange is private but is the only subscribe mechanism
		client.onStatusChange = handleStatusChange;
		setStatus(client.getStatus() as string);

		// Subscribe to requested channels
		const handler = (env: WsEnvelope) => {
			if (env.type === "event" && env.channel && env.event) {
				setEvents((prev) => {
					const newEvent: LiveEvent = {
						id: `${env.channel}:${env.event}:${Date.now()}`,
						channel: env.channel,
						event: env.event,
						timestamp: new Date(),
						data: env.data,
					};
					const updated = [newEvent, ...prev];
					return updated.slice(0, maxEvents);
				});
			}
		};

		for (const ch of channels) {
			client.subscribe(ch as any, handler);
		}

		unsubscribeRef.current = () => {
			for (const ch of channels) {
				client.unsubscribe(ch as any, handler);
			}
		};

		return () => {
			unsubscribeRef.current?.();
			onUnmount?.();
		};
	}, [channels, handleStatusChange, maxEvents, onUnmount]);

	const connectionStatus = useMemo(() => {
		switch (status) {
			case "open":
				return { label: "Connected", icon: Wifi, color: "text-emerald-400" };
			case "connecting":
				return {
					label: "Connecting...",
					icon: RefreshCw,
					color: "text-yellow-400",
				};
			case "closing":
				return { label: "Closing", icon: WifiOff, color: "text-gray-400" };
			default:
				return { label: "Disconnected", icon: WifiOff, color: "text-red-400" };
		}
	}, [status]);

	const ConnectionIcon = connectionStatus.icon;

	return (
		<div className="flex h-full flex-col">
			{/* Header */}
			<div className="flex items-center justify-between border-b border-slate-700 p-3">
				<div className="flex items-center gap-2">
					<Bell className="h-4 w-4 text-blue-400" />
					<span className="font-semibold text-slate-100">Live Events</span>
					<span className="ml-2 rounded-full bg-slate-700 px-2 py-0.5 text-xs text-slate-400">
						{events.length}
					</span>
				</div>
				<div
					className={cn(
						"flex items-center gap-1.5 text-xs",
						connectionStatus.color,
					)}
				>
					<ConnectionIcon className="h-3.5 w-3.5" />
					<span>{connectionStatus.label}</span>
				</div>
			</div>

			{/* Event Feed */}
			<div className="flex-1 overflow-y-auto p-3">
				{events.length === 0 ? (
					<div className="flex h-full items-center justify-center">
						<div className="text-center text-sm text-slate-500">
							{status === "open" ? (
								<>
									<WifiOff className="mx-auto mb-2 h-8 w-8 text-slate-600" />
									<p>No events yet</p>
									<p className="text-xs">Waiting for alerts...</p>
								</>
							) : (
								<>
									<RefreshCw
										className={cn(
											"mx-auto mb-2 h-8 w-8 animate-spin text-slate-600",
											status === "connecting" && "animate-pulse",
										)}
									/>
									<p>Connecting...</p>
								</>
							)}
						</div>
					</div>
				) : (
					<div className="space-y-2">
						{events.map((e) => (
							<div
								key={e.id}
								className="rounded border border-slate-700 bg-slate-800/50 p-2"
							>
								<div className="flex items-center justify-between gap-2">
									<EventBadge event={e.event} />
									<span className="text-xs text-slate-500">
										{formatEventTime(e.timestamp)}
									</span>
								</div>
								<div className="mt-1 text-xs text-slate-400">
									Channel:{" "}
									<span className="font-mono text-slate-300">{e.channel}</span>
								</div>
								{e.data && typeof e.data === "object" && (
									<div className="mt-1 font-mono text-xs text-slate-500">
										{JSON.stringify(e.data).slice(0, 100)}
										{JSON.stringify(e.data).length > 100 && "..."}
									</div>
								)}
							</div>
						))}
					</div>
				)}
			</div>

			{/* Footer */}
			<div className="border-t border-slate-700 p-2 text-center text-xs text-slate-500">
				Auto-scrolling • {channels.join(", ")} channel(s)
			</div>
		</div>
	);
}

export default LiveEvents;
