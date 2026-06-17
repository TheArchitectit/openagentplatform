// Settings — Users.
//
// Table of users with invite, role-change, deactivate, and password reset
// actions. Supports search and pagination on the client.

import { createFileRoute } from '@tanstack/react-router';
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Search,
  UserPlus,
  ChevronLeft,
  ChevronRight,
  X,
  ShieldCheck,
  KeyRound,
  PowerOff,
} from 'lucide-react';
import {
  useSettings,
  type InviteUserInput,
  type UpdateUserInput,
  type User,
  type UserRole,
} from '@/lib/useSettings';
import './settings.css';

export const Route = createFileRoute('/settings/users')({
  component: UsersPage,
});

const PAGE_SIZE = 20;

const ROLES: UserRole[] = ['admin', 'operator', 'engineer', 'viewer'];

function UsersPage() {
  const {
    users,
    isLoadingUsers,
    fetchUsers,
    inviteUser,
    updateUser,
    deactivateUser,
  } = useSettings();

  const [search, setSearch] = useState('');
  const [page, setPage] = useState(0);

  const [showInvite, setShowInvite] = useState(false);
  const [editingUser, setEditingUser] = useState<User | null>(null);
  const [resetPasswordUser, setResetPasswordUser] = useState<User | null>(null);

  useEffect(() => {
    fetchUsers();
  }, [fetchUsers]);

  const filtered = useMemo(() => {
    if (!search.trim()) return users;
    const q = search.toLowerCase();
    return users.filter(
      (u) =>
        u.name.toLowerCase().includes(q) ||
        u.email.toLowerCase().includes(q) ||
        u.role.toLowerCase().includes(q)
    );
  }, [users, search]);

  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const pageItems = filtered.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE);

  useEffect(() => {
    if (page > totalPages - 1) setPage(0);
  }, [page, totalPages]);

  const handleInvite = useCallback(
    async (input: InviteUserInput) => {
      await inviteUser(input);
      setShowInvite(false);
    },
    [inviteUser]
  );

  const handleUpdate = useCallback(
    async (id: string, input: UpdateUserInput) => {
      await updateUser(id, input);
      setEditingUser(null);
    },
    [updateUser]
  );

  const handleDeactivate = useCallback(
    async (id: string) => {
      if (confirm('Deactivate this user? They will no longer be able to sign in.')) {
        await deactivateUser(id);
      }
    },
    [deactivateUser]
  );

  return (
    <>
      <div className="settings-page-header">
        <div>
          <h1>Users</h1>
          <p>Invite, manage, and remove members of your organization.</p>
        </div>
        <button
          type="button"
          className="settings-input"
          style={{ width: 'auto', height: '2.25rem', padding: '0 0.75rem', cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: '0.375rem', background: 'rgb(99 102 241)', color: 'white', border: 'none', fontWeight: 500 }}
          onClick={() => setShowInvite(true)}
        >
          <UserPlus className="h-4 w-4" />
          Invite User
        </button>
      </div>

      <div className="settings-table-wrap">
        <div className="settings-filter-bar">
          <Search className="h-4 w-4 text-text-muted" />
          <input
            type="text"
            className="settings-input"
            placeholder="Search by name, email, or role..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <div style={{ marginLeft: 'auto', fontSize: '0.8125rem', color: 'rgb(100 116 139)' }}>
            {filtered.length} user{filtered.length === 1 ? '' : 's'}
          </div>
        </div>

        <table className="settings-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Email</th>
              <th>Role</th>
              <th>Status</th>
              <th>Last Login</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {isLoadingUsers ? (
              <tr className="empty-row">
                <td colSpan={6}>Loading users...</td>
              </tr>
            ) : pageItems.length === 0 ? (
              <tr className="empty-row">
                <td colSpan={6}>
                  {search.trim() ? 'No users match your search.' : 'No users yet. Invite your first team member.'}
                </td>
              </tr>
            ) : (
              pageItems.map((u) => (
                <tr key={u.id}>
                  <td style={{ color: 'rgb(241 245 249)', fontWeight: 500 }}>{u.name}</td>
                  <td>{u.email}</td>
                  <td>
                    <span className={`settings-badge settings-badge--role-${u.role}`}>
                      {u.role}
                    </span>
                  </td>
                  <td>
                    <span className={`settings-badge settings-badge--${u.status}`}>
                      {u.status}
                    </span>
                  </td>
                  <td>
                    {u.last_login
                      ? new Date(u.last_login).toLocaleString()
                      : 'Never'}
                  </td>
                  <td>
                    <div style={{ display: 'flex', gap: '0.375rem' }}>
                      <button
                        type="button"
                        className="settings-input"
                        style={{ width: 'auto', height: '1.75rem', padding: '0 0.5rem', cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: '0.25rem', fontSize: '0.75rem' }}
                        onClick={() => setEditingUser(u)}
                        title="Edit user"
                      >
                        <ShieldCheck className="h-3 w-3" /> Edit
                      </button>
                      <button
                        type="button"
                        className="settings-input"
                        style={{ width: 'auto', height: '1.75rem', padding: '0 0.5rem', cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: '0.25rem', fontSize: '0.75rem' }}
                        onClick={() => setResetPasswordUser(u)}
                        title="Reset password"
                      >
                        <KeyRound className="h-3 w-3" />
                      </button>
                      {u.status === 'active' && (
                        <button
                          type="button"
                          className="settings-input"
                          style={{ width: 'auto', height: '1.75rem', padding: '0 0.5rem', cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: '0.25rem', fontSize: '0.75rem', color: 'rgb(252 165 165)' }}
                          onClick={() => handleDeactivate(u.id)}
                          title="Deactivate user"
                        >
                          <PowerOff className="h-3 w-3" />
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>

        {totalPages > 1 && (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '0.5rem 0.75rem', borderTop: '1px solid rgb(30 41 59)', fontSize: '0.8125rem', color: 'rgb(148 163 184)' }}>
            <span>
              Page {page + 1} of {totalPages}
            </span>
            <div style={{ display: 'flex', gap: '0.25rem' }}>
              <button
                type="button"
                className="settings-input"
                style={{ width: 'auto', height: '1.75rem', padding: '0 0.5rem', cursor: 'pointer', display: 'inline-flex', alignItems: 'center' }}
                onClick={() => setPage((p) => Math.max(0, p - 1))}
                disabled={page === 0}
              >
                <ChevronLeft className="h-3 w-3" />
              </button>
              <button
                type="button"
                className="settings-input"
                style={{ width: 'auto', height: '1.75rem', padding: '0 0.5rem', cursor: 'pointer', display: 'inline-flex', alignItems: 'center' }}
                onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
                disabled={page >= totalPages - 1}
              >
                <ChevronRight className="h-3 w-3" />
              </button>
            </div>
          </div>
        )}
      </div>

      {showInvite && (
        <InviteUserModal
          onClose={() => setShowInvite(false)}
          onSubmit={handleInvite}
        />
      )}

      {editingUser && (
        <EditUserModal
          user={editingUser}
          onClose={() => setEditingUser(null)}
          onSubmit={(input) => handleUpdate(editingUser.id, input)}
        />
      )}

      {resetPasswordUser && (
        <ResetPasswordModal
          user={resetPasswordUser}
          onClose={() => setResetPasswordUser(null)}
        />
      )}
    </>
  );
}

// ---------------------------------------------------------------------------
// Invite modal
// ---------------------------------------------------------------------------

function InviteUserModal({
  onClose,
  onSubmit,
}: {
  onClose: () => void;
  onSubmit: (input: InviteUserInput) => Promise<void>;
}) {
  const [email, setEmail] = useState('');
  const [name, setName] = useState('');
  const [role, setRole] = useState<UserRole>('viewer');
  const [busy, setBusy] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!email.trim() || !name.trim()) return;
    setBusy(true);
    try {
      await onSubmit({ email: email.trim(), name: name.trim(), role });
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="settings-modal-backdrop" onClick={onClose}>
      <div className="settings-modal" onClick={(e) => e.stopPropagation()}>
        <div className="settings-modal-header">
          <h2>Invite User</h2>
          <button type="button" className="settings-modal-close" onClick={onClose}>
            <X className="h-4 w-4" />
          </button>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="settings-form-group">
            <label className="settings-form-label" htmlFor="invite-email">
              Email
            </label>
            <input
              id="invite-email"
              type="email"
              className="settings-input"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="user@example.com"
              required
            />
          </div>
          <div className="settings-form-group">
            <label className="settings-form-label" htmlFor="invite-name">
              Name
            </label>
            <input
              id="invite-name"
              type="text"
              className="settings-input"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Full name"
              required
            />
          </div>
          <div className="settings-form-group">
            <label className="settings-form-label" htmlFor="invite-role">
              Role
            </label>
            <select
              id="invite-role"
              className="settings-select"
              value={role}
              onChange={(e) => setRole(e.target.value as UserRole)}
            >
              {ROLES.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
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
              style={{ width: 'auto', height: '2.25rem', padding: '0 0.75rem', cursor: 'pointer', background: 'rgb(99 102 241)', color: 'white', border: 'none', fontWeight: 500 }}
              disabled={busy}
            >
              {busy ? 'Sending...' : 'Send Invite'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Edit modal
// ---------------------------------------------------------------------------

function EditUserModal({
  user,
  onClose,
  onSubmit,
}: {
  user: User;
  onClose: () => void;
  onSubmit: (input: UpdateUserInput) => Promise<void>;
}) {
  const [role, setRole] = useState<UserRole>(user.role);
  const [status, setStatus] = useState(user.status);
  const [busy, setBusy] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await onSubmit({ role, status });
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="settings-modal-backdrop" onClick={onClose}>
      <div className="settings-modal" onClick={(e) => e.stopPropagation()}>
        <div className="settings-modal-header">
          <h2>Edit {user.name}</h2>
          <button type="button" className="settings-modal-close" onClick={onClose}>
            <X className="h-4 w-4" />
          </button>
        </div>
        <form onSubmit={handleSubmit}>
          <div className="settings-form-group">
            <label className="settings-form-label">Email</label>
            <input className="settings-input" value={user.email} readOnly />
          </div>
          <div className="settings-form-group">
            <label className="settings-form-label" htmlFor="edit-role">
              Role
            </label>
            <select
              id="edit-role"
              className="settings-select"
              value={role}
              onChange={(e) => setRole(e.target.value as UserRole)}
            >
              {ROLES.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
          </div>
          <div className="settings-form-group">
            <label className="settings-form-label" htmlFor="edit-status">
              Status
            </label>
            <select
              id="edit-status"
              className="settings-select"
              value={status}
              onChange={(e) => setStatus(e.target.value as User['status'])}
            >
              <option value="active">active</option>
              <option value="inactive">inactive</option>
              <option value="pending">pending</option>
            </select>
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
              style={{ width: 'auto', height: '2.25rem', padding: '0 0.75rem', cursor: 'pointer', background: 'rgb(99 102 241)', color: 'white', border: 'none', fontWeight: 500 }}
              disabled={busy}
            >
              {busy ? 'Saving...' : 'Save'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Reset password modal (info only — sends reset email)
// ---------------------------------------------------------------------------

function ResetPasswordModal({
  user,
  onClose,
}: {
  user: User;
  onClose: () => void;
}) {
  const [sent, setSent] = useState(false);

  return (
    <div className="settings-modal-backdrop" onClick={onClose}>
      <div className="settings-modal" onClick={(e) => e.stopPropagation()}>
        <div className="settings-modal-header">
          <h2>Reset Password</h2>
          <button type="button" className="settings-modal-close" onClick={onClose}>
            <X className="h-4 w-4" />
          </button>
        </div>
        {!sent ? (
          <>
            <p style={{ fontSize: '0.875rem', color: 'rgb(148 163 184)', marginBottom: '1rem' }}>
              Send a password reset link to <strong style={{ color: 'rgb(226 232 240)' }}>{user.email}</strong>?
            </p>
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
                type="button"
                className="settings-input"
                style={{ width: 'auto', height: '2.25rem', padding: '0 0.75rem', cursor: 'pointer', background: 'rgb(99 102 241)', color: 'white', border: 'none', fontWeight: 500 }}
                onClick={() => setSent(true)}
              >
                Send Reset Link
              </button>
            </div>
          </>
        ) : (
          <p style={{ fontSize: '0.875rem', color: 'rgb(110 231 183)' }}>
            Reset link sent to {user.email}.
          </p>
        )}
      </div>
    </div>
  );
}
