import AsyncStorage from '@react-native-async-storage/async-storage';
import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { AppState } from 'react-native';
import { Calendar, Invite, UserSettings, api } from '../api/calendars';
import { useAuth } from '../auth/AuthContext';
import { calendarCacheKey } from './calendarCache';

// The signed-in user's calendars, Inbox and Overview switches: everything the
// switcher and Overview need. Refreshed on launch and whenever the app comes
// back to the front; the last copy is cached so the list shows offline.

type State = {
  calendars: Calendar[];
  invites: Invite[];
  settings: UserSettings;
  loaded: boolean;
  refresh: () => Promise<void>;
  /** Replaces a calendar in the list after an edit. */
  upsert: (c: Calendar) => void;
  remove: (id: string) => void;
  setSettings: (s: UserSettings) => void;
  /** Bumped whenever any calendar's contents may have changed (the Overview reloads). */
  version: number;
  /** Per calendar: bumped when that one changed, or on a full refresh (its screen reloads). */
  calendarVersions: Record<string, number>;
  epoch: number;
  /** Something changed: in calendarId, or (without one) possibly anywhere. */
  bump: (calendarId?: string) => void;
};

const Ctx = createContext<State | null>(null);

export function CalendarsProvider({ children }: { children: React.ReactNode }) {
  const { user } = useAuth();
  const cacheKey = user ? calendarCacheKey(user.id) : null;
  // Set once the server has answered, so a slower cache read can't then
  // replace what it sent.
  const fresh = useRef(false);
  const [calendars, setCalendars] = useState<Calendar[]>([]);
  const [invites, setInvites] = useState<Invite[]>([]);
  const [settings, setSettingsState] = useState<UserSettings>({});
  const [loaded, setLoaded] = useState(false);
  const [version, setVersion] = useState(0);
  const [epoch, setEpoch] = useState(0);
  const [calendarVersions, setCalendarVersions] = useState<Record<string, number>>({});
  const bump = useCallback((calendarId?: string) => {
    setVersion((v) => v + 1);
    if (calendarId) setCalendarVersions((m) => ({ ...m, [calendarId]: (m[calendarId] ?? 0) + 1 }));
    else setEpoch((e) => e + 1);
  }, []);

  const refresh = useCallback(async () => {
    try {
      const [cals, inv, st] = await Promise.all([api.listCalendars(), api.inbox(), api.getSettings()]);
      setCalendars(cals);
      setInvites(inv);
      setSettingsState(st ?? {});
      setLoaded(true);
      fresh.current = true;
      bump();
      if (cacheKey) AsyncStorage.setItem(cacheKey, JSON.stringify({ cals, st })).catch(() => {});
    } catch {
      // Offline: keep what we have.
    }
  }, [bump, cacheKey]);

  useEffect(() => {
    (cacheKey ? AsyncStorage.getItem(cacheKey) : Promise.resolve(null))
      .then((v) => {
        if (!v || fresh.current) return;
        const { cals, st } = JSON.parse(v);
        setCalendars(cals);
        setSettingsState(st ?? {});
        setLoaded(true);
      })
      .catch(() => {});
    refresh();
    const sub = AppState.addEventListener('change', (s) => {
      if (s === 'active') refresh();
    });
    return () => sub.remove();
  }, [refresh, cacheKey]);

  const upsert = useCallback((c: Calendar) => {
    setCalendars((cur) => {
      const i = cur.findIndex((x) => x.id === c.id);
      if (i < 0) return [...cur, c];
      const next = cur.slice();
      next[i] = c;
      return next;
    });
  }, []);

  const remove = useCallback((id: string) => setCalendars((cur) => cur.filter((c) => c.id !== id)), []);

  // Saved in the background; a failed save is retried with the next change.
  const pending = useRef<UserSettings | null>(null);
  const setSettings = useCallback((s: UserSettings) => {
    setSettingsState(s);
    pending.current = s;
    api
      .putSettings(s)
      .then(() => {
        if (pending.current === s) pending.current = null;
      })
      .catch(() => {});
  }, []);

  const value = useMemo(
    () => ({ calendars, invites, settings, loaded, refresh, upsert, remove, setSettings, version, calendarVersions, epoch, bump }),
    [calendars, invites, settings, loaded, refresh, upsert, remove, setSettings, version, calendarVersions, epoch, bump]
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useCalendars() {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error('useCalendars must be used within CalendarsProvider');
  return ctx;
}

export function useCalendar(id: string): Calendar | undefined {
  return useCalendars().calendars.find((c) => c.id === id);
}

/** Changes when calendarId's contents may have changed: its screen reloads on it. */
export function useCalendarVersion(calendarId: string): string {
  const { calendarVersions, epoch } = useCalendars();
  return `${epoch}.${calendarVersions[calendarId] ?? 0}`;
}
