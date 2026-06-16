import { apiFetch } from './api';

export interface User {
  id: string;
  email: string;
  name?: string;
  role?: string;
  avatarUrl?: string;
}

const USER_KEY = 'oap_user';

export function getStoredUser(): User | null {
  if (typeof window === 'undefined') return null;
  const raw = localStorage.getItem(USER_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as User;
  } catch {
    return null;
  }
}

export function setStoredUser(user: User): void {
  localStorage.setItem(USER_KEY, JSON.stringify(user));
}

export function clearStoredUser(): void {
  localStorage.removeItem(USER_KEY);
}

export function isAuthenticated(): boolean {
  return getStoredUser() !== null;
}

export async function getUser(): Promise<User | null> {
  try {
    const user = await apiFetch<User>('/auth/me');
    setStoredUser(user);
    return user;
  } catch (err) {
    clearStoredUser();
    return null;
  }
}

export function logout(): void {
  clearStoredUser();
  // Full-window redirect so server-side cookies/session are cleared
  window.location.href = '/auth/logout';
}
