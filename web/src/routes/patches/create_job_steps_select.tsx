import { Search, Loader2 } from 'lucide-react';
import { type PatchCatalogItem } from '@/lib/usePatches';
import { type Agent } from '@/lib/useAgents';
import { SeverityBadge } from '@/components/severity-badge';

// ---------------------------------------------------------------------------
// PatchesStep — select patches from the catalog
// ---------------------------------------------------------------------------

export function PatchesStep({
  catalog,
  isLoading,
  search,
  onSearchChange,
  catalogFilter,
  onCatalogFilterChange,
  selected,
  onToggle,
}: {
  catalog: PatchCatalogItem[];
  isLoading: boolean;
  search: string;
  onSearchChange: (v: string) => void;
  catalogFilter: { severity?: string; category?: string; os?: string };
  onCatalogFilterChange: (v: { severity?: string; category?: string; os?: string }) => void;
  selected: Set<string>;
  onToggle: (id: string) => void;
}) {
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative flex-1 min-w-[14rem]" role="search">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400" aria-hidden="true" />
          <input
            type="search"
            role="searchbox"
            aria-label="Search patch catalog by title, KB, or CVE"
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder="Search by title, KB, or CVE…"
            className="w-full h-9 pl-9 pr-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
          />
        </div>
        <select
          value={catalogFilter.severity ?? ''}
          onChange={(e) =>
            onCatalogFilterChange({ ...catalogFilter, severity: e.target.value || undefined })
          }
          className="h-9 px-2 rounded-md bg-slate-800 border border-slate-700 text-sm text-white"
        >
          <option value="">All severities</option>
          <option value="critical">Critical</option>
          <option value="important">Important</option>
          <option value="moderate">Moderate</option>
          <option value="low">Low</option>
        </select>
        <select
          value={catalogFilter.category ?? ''}
          onChange={(e) =>
            onCatalogFilterChange({ ...catalogFilter, category: e.target.value || undefined })
          }
          className="h-9 px-2 rounded-md bg-slate-800 border border-slate-700 text-sm text-white"
        >
          <option value="">All categories</option>
          <option value="security">Security</option>
          <option value="os">OS</option>
          <option value="application">Application</option>
          <option value="driver">Driver</option>
          <option value="firmware">Firmware</option>
        </select>
      </div>

      <div className="rounded-md border border-slate-800 overflow-hidden">
        <div className="max-h-96 overflow-y-auto">
          <table role="table" aria-label="Patch catalog" className="w-full text-sm">
            <thead className="sticky top-0 bg-slate-900/90 z-10">
              <tr className="text-left text-xs uppercase tracking-wider text-gray-400 border-b border-slate-800">
                <th className="px-3 py-2 w-10" scope="col"></th>
                <th className="px-3 py-2" scope="col">Title</th>
                <th className="px-3 py-2 w-28" scope="col">KB / CVE</th>
                <th className="px-3 py-2 w-28" scope="col">Severity</th>
                <th className="px-3 py-2 w-24" scope="col">OS</th>
                <th className="px-3 py-2 w-20 text-right" scope="col">Affected</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {isLoading ? (
                <tr>
                  <td colSpan={6} className="px-4 py-8 text-center text-gray-400" role="status" aria-live="polite">
                    <Loader2 className="inline h-4 w-4 animate-spin mr-2" aria-hidden="true" />
                    Loading patch catalog…
                  </td>
                </tr>
              ) : catalog.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-8 text-center text-gray-400" role="status">
                    No patches match your filters.
                  </td>
                </tr>
              ) : (
                catalog.map((c) => {
                  const isSelected = selected.has(c.id);
                  return (
                    <tr
                      key={c.id}
                      onClick={() => onToggle(c.id)}
                      className={
                        'cursor-pointer transition-colors ' +
                        (isSelected ? 'bg-blue-600/10' : 'hover:bg-slate-800/40')
                      }
                    >
                      <td className="px-3 py-2">
                        <input
                          type="checkbox"
                          checked={isSelected}
                          onChange={() => onToggle(c.id)}
                          aria-label={`Select patch ${c.id}`}
                          className="h-4 w-4 rounded border-slate-700 bg-slate-800 text-blue-400 focus:ring-blue-500/40"
                        />
                      </td>
                      <td className="px-3 py-2 text-white">
                        <div className="flex flex-col">
                          <span className="truncate max-w-md">{c.title}</span>
                          {c.description && (
                            <span className="text-xs text-gray-400 truncate max-w-md">
                              {c.description}
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-3 py-2 text-gray-300 text-xs">
                        {c.kb_number ?? '—'}
                        {c.cve_ids && c.cve_ids.length > 0 && (
                          <span className="block text-gray-400">
                            {c.cve_ids.slice(0, 2).join(', ')}
                            {c.cve_ids.length > 2 ? ` +${c.cve_ids.length - 2}` : ''}
                          </span>
                        )}
                      </td>
                      <td className="px-3 py-2">
                        <SeverityBadge severity={c.severity} />
                      </td>
                      <td className="px-3 py-2 text-gray-300 text-xs">{c.os || '—'}</td>
                      <td className="px-3 py-2 text-right tabular-nums text-gray-300">
                        {c.affected_agent_count ?? 0}
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </div>
      <p className="text-xs text-gray-400">
        {selected.size} patch{selected.size === 1 ? '' : 'es'} selected.
      </p>
    </div>
  );
}

// ---------------------------------------------------------------------------
// TargetsStep — select target agents
// ---------------------------------------------------------------------------

export function TargetsStep({
  agents,
  isLoading,
  search,
  onSearchChange,
  selected,
  onToggle,
  onSelectAll,
  onClear,
}: {
  agents: Agent[];
  isLoading: boolean;
  search: string;
  onSearchChange: (v: string) => void;
  selected: Set<string>;
  onToggle: (id: string) => void;
  onSelectAll: () => void;
  onClear: () => void;
}) {
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative flex-1 min-w-[14rem]">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400" />
          <input
            type="search"
            value={search}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder="Search by hostname, ID, or OS…"
            className="w-full h-9 pl-9 pr-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500/40 focus:border-blue-500/40"
          />
        </div>
        <button
          type="button"
          onClick={onSelectAll}
          className="px-3 h-9 rounded-md bg-slate-800 border border-slate-700 text-gray-300 text-sm hover:bg-slate-700 transition-colors"
        >
          Select all visible
        </button>
        <button
          type="button"
          onClick={onClear}
          className="px-3 h-9 rounded-md bg-slate-800 border border-slate-700 text-gray-300 text-sm hover:bg-slate-700 transition-colors"
        >
          Clear
        </button>
      </div>
      <div className="rounded-md border border-slate-800 overflow-hidden">
        <div className="max-h-96 overflow-y-auto">
          <table className="w-full text-sm">
            <thead className="sticky top-0 bg-slate-900/90 z-10">
              <tr className="text-left text-xs uppercase tracking-wider text-gray-400 border-b border-slate-800">
                <th className="px-3 py-2 w-10"></th>
                <th className="px-3 py-2">Hostname</th>
                <th className="px-3 py-2 w-32">OS</th>
                <th className="px-3 py-2 w-20">Status</th>
                <th className="px-3 py-2 w-20 text-right">CPU%</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {isLoading ? (
                <tr>
                  <td colSpan={5} className="px-4 py-8 text-center text-gray-400">
                    <Loader2 className="inline h-4 w-4 animate-spin mr-2" />
                    Loading agents…
                  </td>
                </tr>
              ) : agents.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-4 py-8 text-center text-gray-400">
                    No agents match your search.
                  </td>
                </tr>
              ) : (
                agents.slice(0, 500).map((a) => {
                  const isSelected = selected.has(a.id);
                  return (
                    <tr
                      key={a.id}
                      onClick={() => onToggle(a.id)}
                      className={
                        'cursor-pointer transition-colors ' +
                        (isSelected ? 'bg-blue-600/10' : 'hover:bg-slate-800/40')
                      }
                    >
                      <td className="px-3 py-2">
                        <input
                          type="checkbox"
                          checked={isSelected}
                          onChange={() => onToggle(a.id)}
                          aria-label={`Select agent ${a.id}`}
                          className="h-4 w-4 rounded border-slate-700 bg-slate-800 text-blue-400 focus:ring-blue-500/40"
                        />
                      </td>
                      <td className="px-3 py-2 text-white">{a.hostname || a.id}</td>
                      <td className="px-3 py-2 text-gray-300 text-xs">{a.os || '—'}</td>
                      <td className="px-3 py-2 text-xs">
                        <span
                          className={
                            'inline-flex px-2 py-0.5 rounded-full border ' +
                            (a.status === 'online'
                              ? 'bg-green-500/10 text-green-400 border-green-800'
                              : 'bg-slate-700/30 text-gray-300 border-slate-700/30')
                          }
                        >
                          {a.status || 'unknown'}
                        </span>
                      </td>
                      <td className="px-3 py-2 text-right tabular-nums text-gray-300 text-xs">
                        {a.cpu_percent !== undefined ? `${a.cpu_percent.toFixed(0)}%` : '—'}
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </div>
      <p className="text-xs text-gray-400">
        {selected.size} agent{selected.size === 1 ? '' : 's'} selected.
      </p>
    </div>
  );
}
