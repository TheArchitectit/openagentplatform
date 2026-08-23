export interface DashboardEventMap {
  'kpi.updated': KpiUpdatedEvent;
  'agent.status.changed': AgentStatusChangedEvent;
  'alert.created': AlertCreatedEvent;
  'task.updated': TaskUpdatedEvent;
}

export type DashboardEventType = keyof DashboardEventMap;

export interface DashboardEvent<T extends DashboardEventType = DashboardEventType> {
  type: T;
  data: DashboardEventMap[T];
  timestamp: string;
}

export interface KpiUpdatedEvent {
  key: string;
  value: number;
  previousValue?: number;
}

export interface AgentStatusChangedEvent {
  agentId: string;
  status: 'online' | 'offline' | 'busy' | 'error';
  previousStatus?: 'online' | 'offline' | 'busy' | 'error';
}

export interface AlertCreatedEvent {
  id: string;
  severity: 'info' | 'warning' | 'error' | 'critical';
  message: string;
}

export interface TaskUpdatedEvent {
  taskId: string;
  status: 'pending' | 'working' | 'completed' | 'failed' | 'cancelled';
  progress?: number;
}

export function isDashboardEvent(value: unknown): value is DashboardEvent {
  if (!value || typeof value !== 'object') return false;
  const candidate = value as Record<string, unknown>;
  return (
    typeof candidate.type === 'string' &&
    isDashboardEventType(candidate.type) &&
    typeof candidate.data === 'object' &&
    candidate.data !== null &&
    typeof candidate.timestamp === 'string'
  );
}

export function isDashboardEventType(value: string): value is DashboardEventType {
  return value === 'kpi.updated' ||
    value === 'agent.status.changed' ||
    value === 'alert.created' ||
    value === 'task.updated';
}
