import type { ReactNode } from 'react';
import { Lock } from 'lucide-react';
import { usePermission, type Permission } from '@/lib/org';

export interface RequirePermissionProps {
  /** The permission action required to view the children */
  action: Permission;
  /** Content to render when the user has the required permission */
  children: ReactNode;
  /**
   * Optional custom fallback rendered instead of the default "Access Denied" card.
   * If not provided, a default card is shown.
   */
  fallback?: ReactNode;
}

/**
 * Conditionally renders children only if the current user has the required permission.
 * When the user lacks permission, shows an "Access Denied" card by default,
 * or a custom fallback if one is provided.
 */
export function RequirePermission({
  action,
  children,
  fallback,
}: RequirePermissionProps) {
  const hasPermission = usePermission(action);

  if (hasPermission) {
    return <>{children}</>;
  }

  if (fallback) {
    return <>{fallback}</>;
  }

  return <AccessDeniedCard permission={action} />;
}

// ---------------------------------------------------------------------------
// Default "Access Denied" card
// ---------------------------------------------------------------------------

function AccessDeniedCard({ permission }: { permission: string }) {
  return (
    <div
      className="rounded-xl border border-slate-800 bg-slate-900 p-8 max-w-lg mx-auto mt-12"
      role="alert"
      aria-live="polite"
    >
      <div className="flex flex-col items-center text-center gap-4">
        <div
          className="h-12 w-12 rounded-full bg-slate-800 flex items-center justify-center"
          aria-hidden="true"
        >
          <Lock className="w-6 h-6 text-gray-500" />
        </div>
        <div>
          <h2 className="text-lg font-semibold text-white">Access Denied</h2>
          <p className="text-sm text-gray-400 mt-1">
            You do not have permission to view this page.
          </p>
          <p className="text-xs text-gray-500 mt-2 font-mono">
            Required: <span className="text-gray-400">{permission}</span>
          </p>
        </div>
        <p className="text-xs text-gray-500">
          Contact your organization administrator to request access.
        </p>
      </div>
    </div>
  );
}
