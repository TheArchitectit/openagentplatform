import { createFileRoute } from '@tanstack/react-router';
import { ShieldCheck } from 'lucide-react';
import { toast } from 'sonner';

export const Route = createFileRoute('/login')({
  component: LoginPage,
});

function LoginPage() {
  const handleLogin = () => {
    const apiBase = import.meta.env.VITE_API_URL ?? '';
    const loginUrl = `${apiBase}/auth/login`;

    toast.info('Redirecting to identity provider…');
    // Full-window redirect so the OIDC server can set session cookies
    window.location.href = loginUrl;
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-slate-950 p-4 relative overflow-hidden">
      {/* Subtle radial gradient background */}
      <div
        className="absolute inset-0 opacity-30 pointer-events-none"
        style={{
          background:
            'radial-gradient(ellipse at top, rgba(59,130,246,0.15) 0%, transparent 60%), radial-gradient(ellipse at bottom right, rgba(99,102,241,0.1) 0%, transparent 50%)',
        }}
        aria-hidden="true"
      />
      {/* Grid pattern overlay */}
      <div
        className="absolute inset-0 opacity-[0.03] pointer-events-none"
        style={{
          backgroundImage:
            'linear-gradient(rgba(255,255,255,.06) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,.06) 1px, transparent 1px)',
          backgroundSize: '32px 32px',
        }}
        aria-hidden="true"
      />

      <div className="relative w-full max-w-md">
        <div className="rounded-2xl border border-slate-800 bg-slate-900 p-8 shadow-2xl">
          <div className="flex items-center justify-center mb-6" aria-hidden="true">
            <div className="h-12 w-12 rounded-xl bg-blue-600 flex items-center justify-center">
              <ShieldCheck className="h-6 w-6 text-white" />
            </div>
          </div>
          <h1 className="text-2xl font-bold text-center text-white">
            OpenAgentPlatform
          </h1>
          <p className="text-gray-400 text-sm text-center mt-2 mb-6">
            Sign in to manage your endpoints, agents, and alerts.
          </p>
          <button
            type="button"
            onClick={handleLogin}
            autoFocus
            className="w-full px-6 py-3 bg-blue-600 hover:bg-blue-500 text-white rounded-lg font-medium transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2 focus-visible:ring-offset-slate-900"
          >
            Sign in with OIDC
          </button>
          <p className="text-xs text-gray-600 text-center mt-6">
            You will be redirected to your identity provider to authenticate.
          </p>
        </div>
      </div>
    </div>
  );
}
