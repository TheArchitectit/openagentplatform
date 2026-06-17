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
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-surface-primary via-surface-secondary to-surface-primary p-4">
      <div className="w-full max-w-md">
        <div className="rounded-xl border border-border-subtle bg-surface-secondary/80 backdrop-blur p-8 shadow-2xl">
          <div className="flex items-center justify-center mb-6" aria-hidden="true">
            <div className="h-12 w-12 rounded-xl bg-accent/20 border border-accent/30 flex items-center justify-center">
              <ShieldCheck className="h-6 w-6 text-accent" />
            </div>
          </div>
          <h1 className="text-2xl font-bold text-center text-text-primary">
            OpenAgentPlatform
          </h1>
          <p className="text-text-secondary text-center mt-2 mb-8">
            Sign in to manage your endpoints, agents, and alerts.
          </p>
          <button
            type="button"
            onClick={handleLogin}
            autoFocus
            className="w-full py-3 px-4 bg-accent hover:bg-accent-hover active:bg-accent-hover text-white rounded-md font-medium transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-surface-secondary"
          >
            Sign in with OIDC
          </button>
          <p className="text-xs text-text-muted text-center mt-6">
            You will be redirected to your identity provider to authenticate.
          </p>
        </div>
      </div>
    </div>
  );
}
