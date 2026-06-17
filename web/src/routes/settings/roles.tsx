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
import '../settings.css';

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
    <>
      <div className="settings-page-header">
        <div>
          <h1>Roles</h1>
          <p>Manage role-based access control and assign permissions per category.</p>
        </div>
        <button
          type="button"
          className="settings-input"
          style={{
            width: 'auto',
            height: '2.25rem',
            padding: '0 0.75rem',
            cursor: 'pointer',
            display: 'inline-flex',
            alignItems: 'center',
            gap: '0.375rem',
            background: 'rgb(99 102 241)',
            color: 'white',
            border: 'none',
            fontWeight: 500,
          }}
          onClick={() => setCreating(true)}
        >
          <Plus className="h-4 w-4" />
          Create Role
        </button>
      </div>

      <div className="settings-table-wrap">
        <table className="settings-table">
          <thead>
            <tr>
              <th>Role Name</th>
              <th>Description</th>
              <th>Users</th>
              <th>Permissions</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {isLoadingRoles ? (
              <tr className="empty-row">
                <td colSpan={5}>Loading roles...</td>
              </tr>
            ) : roles.length === 0 ? (
              <tr className="empty-row">
                <td colSpan={5}>No roles defined.</td>
              </tr>
            ) : (
              roles.map((r) => (
                <tr key={r.id}>
                  <td style={{ color: 'rgb(241 245 249)', fontWeight: 500 }}>
                    <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.375rem' }}>
                      {r.built_in && <Lock className="h-3 w-3 text-text-muted" />}
                      {r.name}
                    </span>
                    {r.built_in && (
                      <span className="settings-badge settings-badge--built-in" style={{ marginLeft: '0.5rem' }}>
                        built-in
                      </span>
                    )}
                  </td>
                  <td style={{ color: 'rgb(148 163 184)' }}>{r.description}</td>
                  <td>{r.user_count}</td>
                  <td>{r.permission_count || r.permissions.length}</td>
                  <td>
                    <div style={{ display: 'flex', gap: '0.375rem' }}>
                      <button
                        type="button"
                        className="settings-input"
                        style={{
                          width: 'auto',
                          height: '1.75rem',
                          padding: '0 0.5rem',
                          cursor: r.built_in ? 'not-allowed' : 'pointer',
                          display: 'inline-flex',
                          alignItems: 'center',
                          gap: '0.25rem',
                          fontSize: '0.75rem',
                          opacity: r.built_in ? 0.4 : 1,
                        }}
                        onClick={() => !r.built_in && setEditing(r)}
                        disabled={r.built_in}
                        title={r.built_in ? 'Built-in roles cannot be edited' : 'Edit role'}
                      >
                        <ShieldCheck className="h-3 w-3" /> Edit
                      </button>
                      {!r.built_in && (
                        <button
                          type="button"
                          className="settings-input"
                          style={{
                            width: 'auto',
                            height: '1.75rem',
                            padding: '0 0.5rem',
                            cursor: 'pointer',
                            display: 'inline-flex',
                            alignItems: 'center',
                            fontSize: '0.75rem',
                            color: 'rgb(252 165 165)',
                          }}
                          onClick={() => handleDelete(r.id, r.name)}
                          title="Delete role"
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
    </>
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
    <div className="settings-modal-backdrop" onClick={onClose}>
      <div className="settings-modal settings-modal--wide" onClick={(e) => e.stopPropagation()}>
        <div className="settings-modal-header">
          <h2>{isEdit ? `Edit Role: ${role!.name}` : 'Create Role'}</h2>
          <button type="button" className="settings-modal-close" onClick={onClose}>
            <X className="h-4 w-4" />
          </button>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="settings-form-group">
            <label className="settings-form-label" htmlFor="role-name">
              Role Name
            </label>
            <input
              id="role-name"
              type="text"
              className="settings-input"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              readOnly={isEdit}
              placeholder="e.g. auditor"
            />
          </div>
          <div className="settings-form-group">
            <label className="settings-form-label" htmlFor="role-desc">
              Description
            </label>
            <textarea
              id="role-desc"
              className="settings-textarea"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What can this role do?"
            />
          </div>

          <div className="settings-form-group">
            <label className="settings-form-label">
              Permissions ({permCount} selected)
            </label>
            {PERMISSION_CATEGORIES.map((cat) => (
              <div key={cat.key} className="settings-perm-category">
                <div className="settings-perm-category-header">
                  <span className="settings-perm-category-label">{cat.label}</span>
                </div>
                <div className="settings-perm-row">
                  {cat.actions.map((action) => {
                    const perm = `${cat.key}:${action}`;
                    return (
                      <label key={perm} className="settings-perm-check">
                        <input
                          type="checkbox"
                          checked={permissions.has(perm)}
                          onChange={(e) => togglePerm(perm, e.target.checked)}
                        />
                        {action}
                      </label>
                    );
                  })}
                </div>
              </div>
            ))}
          </div>

          <div className="settings-form-actions">
            <button
              type="button"
              className="settings-input"
              style={{ width: 'auto', height: '2.25rem', padding: '0 0.75rem', cursor: 'pointer' }}
              onClick={onClose}
            >
              Cancel
            </button>
            <button
              type="submit"
              className="settings-input"
              style={{
                width: 'auto',
                height: '2.25rem',
                padding: '0 0.75rem',
                cursor: 'pointer',
                background: 'rgb(99 102 241)',
                color: 'white',
                border: 'none',
                fontWeight: 500,
              }}
              disabled={busy || !name.trim()}
            >
              {busy ? 'Saving...' : isEdit ? 'Save Changes' : 'Create Role'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
