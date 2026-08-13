// Check type definitions and defaults for the Checks module.
//
// Extracted from check_components.tsx to keep that file focused on the
// CreateCheckModal UI. Re-exported from check_components.tsx so callers
// (e.g. checks/index.tsx) can keep importing from the original path.

import {
  Globe,
  Network,
  Radio,
  HardDrive,
  MemoryStick,
  Cpu,
  ServerCog,
  ShieldCheck,
  FileCode2,
  ScrollText,
} from 'lucide-react';
import type { CheckType } from '@/lib/useChecks';

export interface CheckTypeDef {
  label: string;
  icon: typeof Globe;
  defaults: Record<string, unknown>;
  fields: ConfigField[];
}

export interface ConfigField {
  key: string;
  label: string;
  type: 'text' | 'number' | 'select';
  placeholder?: string;
  options?: { value: string; label: string }[];
  required?: boolean;
}

export const checkTypeDefs: Record<CheckType, CheckTypeDef> = {
  http: {
    label: 'HTTP',
    icon: Globe,
    defaults: { url: '', method: 'GET', expected_status: 200, timeout_secs: 10 },
    fields: [
      { key: 'url', label: 'URL', type: 'text', placeholder: 'https://example.com/health', required: true },
      { key: 'method', label: 'Method', type: 'select', options: [
        { value: 'GET', label: 'GET' },
        { value: 'POST', label: 'POST' },
        { value: 'HEAD', label: 'HEAD' },
      ]},
      { key: 'expected_status', label: 'Expected status code', type: 'number' },
      { key: 'timeout_secs', label: 'Timeout (s)', type: 'number' },
    ],
  },
  tcp: {
    label: 'TCP',
    icon: Network,
    defaults: { host: '', port: 443, timeout_secs: 5 },
    fields: [
      { key: 'host', label: 'Host', type: 'text', placeholder: 'example.com', required: true },
      { key: 'port', label: 'Port', type: 'number', required: true },
      { key: 'timeout_secs', label: 'Timeout (s)', type: 'number' },
    ],
  },
  ping: {
    label: 'Ping',
    icon: Radio,
    defaults: { host: '', count: 3, timeout_secs: 5 },
    fields: [
      { key: 'host', label: 'Host', type: 'text', placeholder: 'example.com', required: true },
      { key: 'count', label: 'Packet count', type: 'number' },
      { key: 'timeout_secs', label: 'Timeout (s)', type: 'number' },
    ],
  },
  disk_usage: {
    label: 'Disk Usage',
    icon: HardDrive,
    defaults: { path: '/', warn_pct: 80, crit_pct: 90 },
    fields: [
      { key: 'path', label: 'Path', type: 'text', placeholder: '/', required: true },
      { key: 'warn_pct', label: 'Warn threshold (%)', type: 'number' },
      { key: 'crit_pct', label: 'Critical threshold (%)', type: 'number' },
    ],
  },
  memory_usage: {
    label: 'Memory Usage',
    icon: MemoryStick,
    defaults: { warn_pct: 80, crit_pct: 90 },
    fields: [
      { key: 'warn_pct', label: 'Warn threshold (%)', type: 'number' },
      { key: 'crit_pct', label: 'Critical threshold (%)', type: 'number' },
    ],
  },
  cpu_usage: {
    label: 'CPU Usage',
    icon: Cpu,
    defaults: { warn_pct: 80, crit_pct: 95, window_secs: 30 },
    fields: [
      { key: 'warn_pct', label: 'Warn threshold (%)', type: 'number' },
      { key: 'crit_pct', label: 'Critical threshold (%)', type: 'number' },
      { key: 'window_secs', label: 'Sample window (s)', type: 'number' },
    ],
  },
  process: {
    label: 'Process',
    icon: ServerCog,
    defaults: { name: '', expected: 'running' },
    fields: [
      { key: 'name', label: 'Process name', type: 'text', placeholder: 'nginx', required: true },
      { key: 'expected', label: 'Expected state', type: 'select', options: [
        { value: 'running', label: 'Running' },
        { value: 'stopped', label: 'Stopped' },
      ]},
    ],
  },
  service: {
    label: 'Service',
    icon: ServerCog,
    defaults: { name: '' },
    fields: [
      { key: 'name', label: 'Service name', type: 'text', placeholder: 'nginx.service', required: true },
    ],
  },
  tls_cert: {
    label: 'TLS Certificate',
    icon: ShieldCheck,
    defaults: { host: '', port: 443, warn_days: 30, crit_days: 7 },
    fields: [
      { key: 'host', label: 'Host', type: 'text', placeholder: 'example.com', required: true },
      { key: 'port', label: 'Port', type: 'number' },
      { key: 'warn_days', label: 'Warn (days remaining)', type: 'number' },
      { key: 'crit_days', label: 'Critical (days remaining)', type: 'number' },
    ],
  },
  script: {
    label: 'Script',
    icon: FileCode2,
    defaults: { script_id: '', timeout_secs: 30 },
    fields: [
      { key: 'script_id', label: 'Script ID', type: 'text', placeholder: 'script-uuid', required: true },
      { key: 'timeout_secs', label: 'Timeout (s)', type: 'number' },
    ],
  },
  log_watch: {
    label: 'Log Watch',
    icon: ScrollText,
    defaults: { path: '', pattern: '' },
    fields: [
      { key: 'path', label: 'Log path', type: 'text', placeholder: '/var/log/syslog', required: true },
      { key: 'pattern', label: 'Regex pattern', type: 'text', required: true },
    ],
  },
};

export const allCheckTypes: CheckType[] = [
  'http', 'tcp', 'ping', 'disk_usage', 'memory_usage', 'cpu_usage',
  'process', 'service', 'tls_cert', 'script', 'log_watch',
];
