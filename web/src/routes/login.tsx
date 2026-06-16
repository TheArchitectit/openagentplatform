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
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-slate-950 via-slate-900 to-slate-950 p-4">
      <div className="w-full max-w-md">
        <div className="rounded-xl border border-slate-800 bg-slate-900/80 backdrop-blur p-8 shadow-2xl">
          <div className="flex items-center justify-center mb-6">
            <div className="h-12 w-12 rounded-xl bg-indigo-600/20 border border-indigo-500/30 flex items-center justify-center">
              <ShieldCheck className="h-6 w-6 text-indigo-400" />
            </div>
          </div>
          <h1 className="text-2xl font-bold text-center text-slate-100">
            OpenAgentPlatform
          </h1>
          <p className="text-slate-400 text-center mt-2 mb-8">
            Sign in to manage your endpoints, agents, and alerts.
          </p>
          <button
            type="button"
            onClick={handleLogin}
            className="w-full py-3 px-4 bg-indigo-600 hover:bg-indigo-500 active:bg-indigo-700 text-white rounded-md font-medium transition-colors focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2 focus:ring-offset-slate-900"
          >
            Sign in with OIDC
          </button>
          <p className="text-xs text-slate-500 text-center mt-6">
            You will be redirected to your identity provider to authenticate.
          </p>
        </div>
      </div>
    </div>
  );
}
