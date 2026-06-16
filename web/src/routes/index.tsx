import { createFileRoute, redirect } from '@tanstack/react-router';
import { isAuthenticated } from '@/lib/auth';

export const Route = createFileRoute('/')({
  beforeLoad: () => {
    throw redirect({
      to: isAuthenticated() ? '/dashboard' : '/login',
    });
  },
});
