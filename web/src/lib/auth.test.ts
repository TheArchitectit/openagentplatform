import { describe, it, expect, beforeEach } from 'vitest';
import { getStoredUser, setStoredUser, clearStoredUser, isAuthenticated, type User } from './auth';

// Sanity test for the localStorage-backed user helpers. Also proves the
// vitest + jsdom toolchain runs (vitest.config.ts sets environment: 'jsdom'),
// which `make test`'s web step depends on.

describe('stored user helpers', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('returns null when nothing is stored', () => {
    expect(getStoredUser()).toBeNull();
    expect(isAuthenticated()).toBe(false);
  });

  it('round-trips a user through localStorage', () => {
    const user: User = { id: 'u1', email: 'a@example.com', role: 'admin' };
    setStoredUser(user);
    expect(isAuthenticated()).toBe(true);
    expect(getStoredUser()).toEqual(user);
  });

  it('clears the stored user', () => {
    setStoredUser({ id: 'u1', email: 'a@example.com' });
    clearStoredUser();
    expect(getStoredUser()).toBeNull();
    expect(isAuthenticated()).toBe(false);
  });

  it('returns null on corrupt JSON', () => {
    localStorage.setItem('oap_user', '{not valid json');
    expect(getStoredUser()).toBeNull();
  });
});
