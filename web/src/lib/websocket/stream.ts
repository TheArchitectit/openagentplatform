import {
  isDashboardEvent,
  type DashboardEvent,
  type DashboardEventMap,
  type DashboardEventType,
} from './events';

export interface EventSource {
  send(data: string | ArrayBufferLike | Blob | ArrayBufferView): boolean;
}

type EventHandler<T extends DashboardEventType> = (
  event: DashboardEvent<T>,
) => void;

export class EventStream {
  private readonly source: EventSource;
  private readonly handlers = new Map<DashboardEventType, Set<EventHandler<DashboardEventType>>>();

  constructor(source: EventSource) {
    this.source = source;
  }

  subscribe<T extends DashboardEventType>(type: T, handler: EventHandler<T>): () => void {
    let handlers = this.handlers.get(type);
    if (!handlers) {
      handlers = new Set();
      this.handlers.set(type, handlers);
      this.source.send(JSON.stringify({ type: 'subscribe', event: type }));
    }
    handlers.add(handler as EventHandler<DashboardEventType>);

    return () => this.unsubscribe(type, handler);
  }

  unsubscribe<T extends DashboardEventType>(type: T, handler: EventHandler<T>): void {
    const handlers = this.handlers.get(type);
    if (!handlers) return;

    handlers.delete(handler as EventHandler<DashboardEventType>);
    if (handlers.size === 0) {
      this.handlers.delete(type);
      this.source.send(JSON.stringify({ type: 'unsubscribe', event: type }));
    }
  }

  dispatch(raw: string | DashboardEvent): boolean {
    let event: unknown = raw;
    if (typeof raw === 'string') {
      try {
        event = JSON.parse(raw);
      } catch {
        return false;
      }
    }

    if (!isDashboardEvent(event)) return false;
    const handlers = this.handlers.get(event.type);
    if (!handlers) return true;

    for (const handler of handlers) {
      try {
        handler(event);
      } catch {
        // A failing dashboard subscriber must not block other handlers.
      }
    }
    return true;
  }

  listener<T extends DashboardEventType>(type: T): EventHandler<T> {
    return (event) => this.dispatch(event as DashboardEvent<DashboardEventType>);
  }
}

export type DashboardEventHandler<T extends DashboardEventType> = (
  data: DashboardEventMap[T],
  event: DashboardEvent<T>,
) => void;
