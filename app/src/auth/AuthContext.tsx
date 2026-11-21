import AsyncStorage from '@react-native-async-storage/async-storage';
import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { ApiError, apiJson, clearToken, getToken, setSignedOutHandler, setToken } from '../api/client';
import { clearCalendarCache } from '../data/calendarCache';

export type User = { id: string; email: string; displayName: string };

type VerifyResult = 'signedIn' | 'needsName';

type AuthState = {
  status: 'loading' | 'signedOut' | 'signedIn';
  user: User | null;
  /** inviteCode is only needed for a new account, when the server has one. */
  requestCode: (email: string, inviteCode?: string) => Promise<void>;
  verify: (email: string, code: string, displayName?: string) => Promise<VerifyResult>;
  logout: () => Promise<void>;
  /** Deletes the account and all its data on the server, then signs out here. */
  deleteAccount: () => Promise<void>;
};

const AuthContext = createContext<AuthState | null>(null);

// The signed-in user is cached so a launch without network still opens the
// app; /me refreshes it whenever the server is reachable.
const USER_CACHE_KEY = 'supercalendar.user.v1';

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [status, setStatus] = useState<AuthState['status']>('loading');
  const [user, setUser] = useState<User | null>(null);

  const signOutLocally = useCallback(async () => {
    await clearToken();
    await AsyncStorage.removeItem(USER_CACHE_KEY).catch(() => {});
    await clearCalendarCache();
    setUser(null);
    setStatus('signedOut');
  }, []);

  const refreshMe = useCallback(async () => {
    try {
      const me = await apiJson<{ user: User }>('/me');
      setUser(me.user);
      await AsyncStorage.setItem(USER_CACHE_KEY, JSON.stringify(me.user)).catch(() => {});
    } catch {
      // Offline or the server is down: keep the cached user. A 401 has
      // already signed out through the handler below.
    }
  }, []);

  useEffect(() => {
    setSignedOutHandler(() => {
      signOutLocally();
    });
    (async () => {
      const token = await getToken().catch(() => null);
      if (!token) {
        setStatus('signedOut');
        return;
      }
      const cached = await AsyncStorage.getItem(USER_CACHE_KEY).catch(() => null);
      if (cached) setUser(JSON.parse(cached));
      setStatus('signedIn');
      refreshMe();
    })();
    return () => setSignedOutHandler(null);
  }, [refreshMe, signOutLocally]);

  const requestCode = useCallback(async (email: string, inviteCode?: string) => {
    await apiJson('/auth/code', { method: 'POST', body: JSON.stringify({ email, inviteCode }) });
  }, []);

  const verify = useCallback(async (email: string, code: string, displayName?: string): Promise<VerifyResult> => {
    try {
      const data = await apiJson<{ user: User; token: string }>('/auth/verify', {
        method: 'POST',
        body: JSON.stringify({ email, code, displayName }),
      });
      await setToken(data.token);
      await AsyncStorage.setItem(USER_CACHE_KEY, JSON.stringify(data.user)).catch(() => {});
      setUser(data.user);
      setStatus('signedIn');
      return 'signedIn';
    } catch (e) {
      if (e instanceof ApiError && e.body?.needsName) return 'needsName';
      throw e;
    }
  }, []);

  const logout = useCallback(async () => {
    await apiJson('/auth/logout', { method: 'POST' }).catch(() => {});
    await signOutLocally();
  }, [signOutLocally]);

  // Unlike logout, a failure here is thrown: the account must really be gone
  // before this device forgets it.
  const deleteAccount = useCallback(async () => {
    await apiJson('/me', { method: 'DELETE' });
    await signOutLocally();
  }, [signOutLocally]);

  const value = useMemo(
    () => ({ status, user, requestCode, verify, logout, deleteAccount }),
    [status, user, requestCode, verify, logout, deleteAccount]
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}
