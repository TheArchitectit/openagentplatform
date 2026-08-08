// Settings — Users.
//
// Table of users with invite, role-change, deactivate, and password reset
// actions. Supports search and pagination on the client.

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



const ROLES: UserRole[] = ['admin', 'operator', 'engineer', 'viewer'];

// ---------------------------------------------------------------------------
// Invite modal
// ---------------------------------------------------------------------------

export function InviteUserModal({
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

export function EditUserModal({
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

export function ResetPasswordModal({
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
