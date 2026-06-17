// Settings — Roles (RBAC).
//
// Table of roles with create/edit modal containing a permission grid
// grouped by category. Built-in roles are shown but cannot be edited.

import { createFileRoute } from '@tanstack/react-router';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Plus, X, Lock, ShieldCheck } from 'lucide-react';
import {
  useSettings,
  PERMISSION_CATEGORIES,
  type CreateRoleInput,
  type Role,
  type UpdateRoleInput,
} from '@/lib/useSettings';

export const Route = createFileRoute('/settings/roles')({
  component: RolesPage,
});

function RolesPage() {
  const { roles, isLoadingRoles, fetchRoles, createRole, updateRole, deleteRole } =
    useSettings();

  const [editing, setEditing] = useState<Role | null>(null);
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    fetchRoles();
  }, [fetchRoles]);

  const handleCreate = useCallback(
    async (input: CreateRoleInput) => {
      await createRole(input);
      setCreating(false);
    },
    [createRole]
  );

  const handleUpdate = useCallback(
    async (id: string, input: UpdateRoleInput) => {
      await updateRole(id, input);
      setEditing(null);
    },
    [updateRole]
  );

  const handleDelete = useCallback(
    async (id: string, name: string) => {
      if (
        confirm(
          `Delete role "${name}"? Users with this role will lose their assigned permissions.`
        )
      ) {
        await deleteRole(id);
      }
    },
    [deleteRole]
  );

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div>
          <h1 className="text-2xl font-bold text-white">Roles</h1>
          <p className="text-gray-300 text-sm mt-0.5">
            Manage role-based access control and assign permissions per category.
          </p>
        </div>
        <button
          type="button"
          onClick={() => setCreating(true)}
          className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
        >
          <Plus className="h-4 w-4" />
          Create Role
        </button>
      </div>

      {/* Table */}
      <div className="rounded-xl border border-slate-800 bg-slate-900 overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="bg-slate-800 text-left text-xs uppercase tracking-wider text-gray-300">
                <th className="px-4 py-2.5 font-medium">Role Name</th>
                <th className="px-4 py-2.5 font-medium">Description</th>
                <th className="px-4 py-2.5 font-medium text-right">Users</th>
                <th className="px-4 py-2.5 font-medium text-right">Permissions</th>
                <th className="px-4 py-2.5 font-medium text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {isLoadingRoles ? (
                <tr>
                  <td colSpan={5} className="px-4 py-12 text-center text-gray-400" role="status">
                    Loading roles...
                  </td>
                </tr>
              ) : roles.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-4 py-12 text-center text-gray-400" role="status">
                    No roles defined.
                  </td>
                </tr>
              ) : (
                roles.map((r) => (
                  <tr key={r.id} className="hover:bg-slate-800/40 transition-colors">
                    <td className="px-4 py-2.5 text-white font-medium">
                      <div className="flex items-center gap-2">
                        <span className="inline-flex items-center gap-1.5">
                          {r.built_in && <Lock className="h-3 w-3 text-gray-400" />}
                          {r.name}
                        </span>
                        {r.built_in && (
                          <span className="inline-flex items-center px-1.5 py-0.5 text-[10px] font-medium rounded-full border bg-slate-500/10 text-gray-300 border-slate-500/20">
                            built-in
                          </span>
                        )}
                      </div>
                    </td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs">{r.description}</td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs text-right">{r.user_count}</td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs text-right">
                      {r.permission_count || r.permissions.length}
                    </td>
                    <td className="px-4 py-2.5 text-right">
                      <div className="flex items-center justify-end gap-1.5">
                        <button
                          type="button"
                          onClick={() => !r.built_in && setEditing(r)}
                          disabled={r.built_in}
                          title={r.built_in ? 'Built-in roles cannot be edited' : 'Edit role'}
                          className="inline-flex items-center gap-1 h-7 px-2 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-xs text-gray-300 hover:text-white disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
                        >
                          <ShieldCheck className="h-3 w-3" /> Edit
                        </button>
                        {!r.built_in && (
                          <button
                            type="button"
                            onClick={() => handleDelete(r.id, r.name)}
                            title="Delete role"
                            className="inline-flex items-center h-7 px-2 rounded-md bg-slate-800 hover:bg-red-600 border border-slate-700 text-xs text-red-400 hover:text-white transition-colors"
                          >
                            Delete
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      {creating && (
        <RoleModal
          onClose={() => setCreating(false)}
          onSubmit={handleCreate}
        />
      )}

      {editing && (
        <RoleModal
          role={editing}
          onClose={() => setEditing(null)}
          onSubmit={(input) => handleUpdate(editing.id, input)}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Role create / edit modal
// ---------------------------------------------------------------------------

function RoleModal({
  role,
  onClose,
  onSubmit,
}: {
  role?: Role;
  onClose: () => void;
  onSubmit: (input: CreateRoleInput | UpdateRoleInput) => Promise<void>;
}) {
  const isEdit = !!role;
  const [name, setName] = useState(role?.name ?? '');
  const [description, setDescription] = useState(role?.description ?? '');
  const [permissions, setPermissions] = useState<Set<string>>(
    () => new Set(role?.permissions ?? [])
  );
  const [busy, setBusy] = useState(false);

  const togglePerm = useCallback((perm: string, checked: boolean) => {
    setPermissions((prev) => {
      const next = new Set(prev);
      if (checked) next.add(perm);
      else next.delete(perm);
      return next;
    });
  }, []);

  const permCount = permissions.size;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    setBusy(true);
    try {
      if (isEdit) {
        await onSubmit({
          name: name.trim(),
          description: description.trim(),
          permissions: Array.from(permissions),
        } as UpdateRoleInput);
      } else {
        await onSubmit({
          name: name.trim(),
          description: description.trim(),
          permissions: Array.from(permissions),
        } as CreateRoleInput);
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60" onClick={onClose}>
      <div
        className="rounded-xl border border-slate-800 bg-slate-900 p-5 w-full max-w-2xl mx-4 max-h-[85vh] overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-white">
            {isEdit ? `Edit Role: ${role!.name}` : 'Create Role'}
          </h2>
          <button
            type="button"
            onClick={onClose}
            className="inline-flex items-center justify-center h-7 w-7 rounded-md text-gray-300 hover:bg-slate-800 hover:text-white transition-colors"
            aria-label="Close"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label htmlFor="role-name" className="block text-xs text-gray-300 mb-1">
              Role Name
            </label>
            <input
              id="role-name"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              readOnly={isEdit}
              placeholder="e.g. auditor"
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500 readOnly:cursor-not-allowed"
            />
          </div>
          <div>
            <label htmlFor="role-desc" className="block text-xs text-gray-300 mb-1">
              Description
            </label>
            <textarea
              id="role-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What can this role do?"
              rows={3}
              className="w-full px-3 py-2 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
            />
          </div>

          <div>
            <label className="block text-xs text-gray-300 mb-2">
              Permissions ({permCount} selected)
            </label>
            <div className="space-y-3">
              {PERMISSION_CATEGORIES.map((cat) => (
                <div key={cat.key} className="rounded-md border border-slate-800 bg-slate-800/40 p-3">
                  <div className="text-xs font-semibold text-white uppercase tracking-wider mb-2">
                    {cat.label}
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {cat.actions.map((action) => {
                      const perm = `${cat.key}:${action}`;
                      const checked = permissions.has(perm);
                      return (
                        <label
                          key={perm}
                          className="inline-flex items-center gap-1.5 px-2 py-1 rounded-md border border-slate-700 bg-slate-800 text-xs text-gray-300 cursor-pointer hover:border-slate-600 transition-colors"
                        >
                          <input
                            type="checkbox"
                            checked={checked}
                            onChange={(e) => togglePerm(perm, e.target.checked)}
                            className="h-3 w-3 rounded border-slate-600 text-blue-500 focus:ring-blue-500 focus:ring-offset-0"
                          />
                          {action}
                        </label>
                      );
                    })}
                  </div>
                </div>
              ))}
            </div>
          </div>

          <div className="flex items-center justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="inline-flex items-center px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-white transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={busy || !name.trim()}
              className="inline-flex items-center px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 text-sm text-white disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {busy ? 'Saving...' : isEdit ? 'Save Changes' : 'Create Role'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
