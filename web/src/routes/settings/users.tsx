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

export const Route = createFileRoute('/settings/users')({
  component: UsersPage,
});

const PAGE_SIZE = 20;

const ROLES: UserRole[] = ['admin', 'operator', 'engineer', 'viewer'];

function roleBadgeClasses(role: UserRole): string {
  switch (role) {
    case 'admin':
      return 'bg-red-500/10 text-red-400 border-red-500/20';
    case 'operator':
      return 'bg-blue-500/10 text-blue-400 border-blue-500/20';
    case 'engineer':
      return 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20';
    case 'viewer':
      return 'bg-slate-500/10 text-gray-300 border-slate-500/20';
    default:
      return 'bg-slate-500/10 text-gray-300 border-slate-500/20';
  }
}

function statusBadgeClasses(status: User['status']): string {
  switch (status) {
    case 'active':
      return 'bg-green-500/10 text-green-400 border-green-500/20';
    case 'inactive':
      return 'bg-slate-500/10 text-gray-300 border-slate-500/20';
    case 'pending':
      return 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20';
    default:
      return 'bg-slate-500/10 text-gray-300 border-slate-500/20';
  }
}

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
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div>
          <h1 className="text-2xl font-bold text-white">Users</h1>
          <p className="text-gray-300 text-sm mt-0.5">
            Invite, manage, and remove members of your organization.
          </p>
        </div>
        <button
          type="button"
          onClick={() => setShowInvite(true)}
          className="inline-flex items-center gap-1.5 px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 transition-colors"
        >
          <UserPlus className="h-4 w-4" />
          Invite User
        </button>
      </div>

      {/* Table card */}
      <div className="rounded-xl border border-slate-800 bg-slate-900 overflow-hidden">
        {/* Search bar */}
        <div className="flex items-center gap-2 px-4 py-3 border-b border-slate-800">
          <Search className="h-4 w-4 text-gray-400" aria-hidden="true" />
          <input
            type="search"
            role="searchbox"
            aria-label="Search users"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search by name, email, or role..."
            className="flex-1 h-8 px-2 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
          />
          <span className="text-xs text-gray-400">
            {filtered.length} user{filtered.length === 1 ? '' : 's'}
          </span>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="bg-slate-800 text-left text-xs uppercase tracking-wider text-gray-300">
                <th className="px-4 py-2.5 font-medium">Name</th>
                <th className="px-4 py-2.5 font-medium">Email</th>
                <th className="px-4 py-2.5 font-medium">Role</th>
                <th className="px-4 py-2.5 font-medium">Status</th>
                <th className="px-4 py-2.5 font-medium">Last Login</th>
                <th className="px-4 py-2.5 font-medium text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800">
              {isLoadingUsers ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center text-gray-400" role="status">
                    Loading users...
                  </td>
                </tr>
              ) : pageItems.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center text-gray-400" role="status">
                    {search.trim() ? 'No users match your search.' : 'No users yet. Invite your first team member.'}
                  </td>
                </tr>
              ) : (
                pageItems.map((u) => (
                  <tr key={u.id} className="hover:bg-slate-800/40 transition-colors">
                    <td className="px-4 py-2.5 text-white font-medium">{u.name}</td>
                    <td className="px-4 py-2.5 text-gray-300">{u.email}</td>
                    <td className="px-4 py-2.5">
                      <span className={`inline-flex items-center px-2 py-0.5 text-[10px] font-medium rounded-full border ${roleBadgeClasses(u.role)}`}>
                        {u.role}
                      </span>
                    </td>
                    <td className="px-4 py-2.5">
                      <span className={`inline-flex items-center px-2 py-0.5 text-[10px] font-medium rounded-full border ${statusBadgeClasses(u.status)}`}>
                        {u.status}
                      </span>
                    </td>
                    <td className="px-4 py-2.5 text-gray-300 text-xs">
                      {u.last_login
                        ? new Date(u.last_login).toLocaleString()
                        : 'Never'}
                    </td>
                    <td className="px-4 py-2.5 text-right">
                      <div className="flex items-center justify-end gap-1.5">
                        <button
                          type="button"
                          onClick={() => setEditingUser(u)}
                          className="inline-flex items-center gap-1 h-7 px-2 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-xs text-gray-300 hover:text-white transition-colors"
                          title="Edit user"
                        >
                          <ShieldCheck className="h-3 w-3" /> Edit
                        </button>
                        <button
                          type="button"
                          onClick={() => setResetPasswordUser(u)}
                          className="inline-flex items-center justify-center h-7 w-7 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-gray-300 hover:text-white transition-colors"
                          title="Reset password"
                        >
                          <KeyRound className="h-3 w-3" />
                        </button>
                        {u.status === 'active' && (
                          <button
                            type="button"
                            onClick={() => handleDeactivate(u.id)}
                            className="inline-flex items-center justify-center h-7 w-7 rounded-md bg-slate-800 hover:bg-red-600 border border-slate-700 text-red-400 hover:text-white transition-colors"
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
        </div>

        {totalPages > 1 && (
          <div className="flex items-center justify-between px-4 py-2.5 border-t border-slate-800 text-xs text-gray-300">
            <span>Page {page + 1} of {totalPages}</span>
            <div className="flex items-center gap-1">
              <button
                type="button"
                onClick={() => setPage((p) => Math.max(0, p - 1))}
                disabled={page === 0}
                className="inline-flex items-center justify-center h-7 w-7 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-gray-300 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
                aria-label="Previous page"
              >
                <ChevronLeft className="h-3 w-3" />
              </button>
              <button
                type="button"
                onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
                disabled={page >= totalPages - 1}
                className="inline-flex items-center justify-center h-7 w-7 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-gray-300 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
                aria-label="Next page"
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
    </div>
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
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60" onClick={onClose}>
      <div
        className="rounded-xl border border-slate-800 bg-slate-900 p-5 w-full max-w-md mx-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-white">Invite User</h2>
          <button
            type="button"
            onClick={onClose}
            className="inline-flex items-center justify-center h-7 w-7 rounded-md text-gray-300 hover:bg-slate-800 hover:text-white transition-colors"
            aria-label="Close"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <form onSubmit={handleSubmit} className="space-y-3">
          <div>
            <label htmlFor="invite-email" className="block text-xs text-gray-300 mb-1">
              Email
            </label>
            <input
              id="invite-email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="user@example.com"
              required
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
            />
          </div>
          <div>
            <label htmlFor="invite-name" className="block text-xs text-gray-300 mb-1">
              Name
            </label>
            <input
              id="invite-name"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Full name"
              required
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white placeholder:text-gray-400 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
            />
          </div>
          <div>
            <label htmlFor="invite-role" className="block text-xs text-gray-300 mb-1">
              Role
            </label>
            <select
              id="invite-role"
              value={role}
              onChange={(e) => setRole(e.target.value as UserRole)}
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
            >
              {ROLES.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
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
              disabled={busy}
              className="inline-flex items-center px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 text-sm text-white disabled:opacity-50 transition-colors"
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
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60" onClick={onClose}>
      <div
        className="rounded-xl border border-slate-800 bg-slate-900 p-5 w-full max-w-md mx-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-white">Edit {user.name}</h2>
          <button
            type="button"
            onClick={onClose}
            className="inline-flex items-center justify-center h-7 w-7 rounded-md text-gray-300 hover:bg-slate-800 hover:text-white transition-colors"
            aria-label="Close"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <form onSubmit={handleSubmit} className="space-y-3">
          <div>
            <label className="block text-xs text-gray-300 mb-1">Email</label>
            <input
              readOnly
              value={user.email}
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-gray-300 cursor-not-allowed"
            />
          </div>
          <div>
            <label htmlFor="edit-role" className="block text-xs text-gray-300 mb-1">
              Role
            </label>
            <select
              id="edit-role"
              value={role}
              onChange={(e) => setRole(e.target.value as UserRole)}
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
            >
              {ROLES.map((r) => (
                <option key={r} value={r}>
                  {r}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label htmlFor="edit-status" className="block text-xs text-gray-300 mb-1">
              Status
            </label>
            <select
              id="edit-status"
              value={status}
              onChange={(e) => setStatus(e.target.value as User['status'])}
              className="w-full h-9 px-3 rounded-md bg-slate-800/60 border border-slate-700 text-sm text-white focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus:border-blue-500"
            >
              <option value="active">active</option>
              <option value="inactive">inactive</option>
              <option value="pending">pending</option>
            </select>
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
              disabled={busy}
              className="inline-flex items-center px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 text-sm text-white disabled:opacity-50 transition-colors"
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
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60" onClick={onClose}>
      <div
        className="rounded-xl border border-slate-800 bg-slate-900 p-5 w-full max-w-md mx-4"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-white">Reset Password</h2>
          <button
            type="button"
            onClick={onClose}
            className="inline-flex items-center justify-center h-7 w-7 rounded-md text-gray-300 hover:bg-slate-800 hover:text-white transition-colors"
            aria-label="Close"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        {!sent ? (
          <>
            <p className="text-sm text-gray-300 mb-4">
              Send a password reset link to <strong className="text-white">{user.email}</strong>?
            </p>
            <div className="flex items-center justify-end gap-2">
              <button
                type="button"
                onClick={onClose}
                className="inline-flex items-center px-3 h-9 rounded-md bg-slate-800 hover:bg-slate-700 border border-slate-700 text-sm text-white transition-colors"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => setSent(true)}
                className="inline-flex items-center px-3 h-9 rounded-md bg-blue-600 hover:bg-blue-500 text-sm text-white transition-colors"
              >
                Send Reset Link
              </button>
            </div>
          </>
        ) : (
          <p className="text-sm text-green-400">Reset link sent to {user.email}.</p>
        )}
      </div>
    </div>
  );
}
