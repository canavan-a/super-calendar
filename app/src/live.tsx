import React, { createContext, useContext, useEffect, useRef } from 'react';
import { AppState } from 'react-native';
import { LIVE_CLIENT_ID, WS_BASE_URL, getToken } from './api/client';
import { useCalendars } from './data/CalendarsContext';

// Live updates: while the app is in front it keeps one WebSocket open to the
// server (GET /live, server/internal/live). The server says *what* changed
// (never the data); this reloads it through the normal API:
//
//   hello      on every (re)connect: anything may have been missed, reload all
//   calendars  your calendar list or Overview settings changed: reload the list
//   inbox      your Inbox changed: reload it
//   changed    something in a calendar changed: reload that calendar's screens
//              and the Overview (editors also hear it; see useLiveChanges)
//
// Changes this app made itself come back tagged with its LIVE_CLIENT_ID and
// are skipped: the screen that made them has already reloaded.

export type LiveChange = {
  type: 'changed';
  calendarId: string;
  area: 'events' | 'training' | 'cycle' | 'calendar' | 'members' | 'invites';
  itemId?: string;
  by?: string;
  byName?: string;
  origin?: string;
};

type Listener = (c: LiveChange) => void;

const ListenersCtx = createContext<Set<Listener> | null>(null);

const MIN_RETRY_MS = 1000;
const MAX_RETRY_MS = 30000;
// Changes arriving within this long reload once (a copy-week sends one
// message, but an import or a busy evening of edits can send several).
const BATCH_MS = 300;

export function LiveProvider({ children }: { children: React.ReactNode }) {
  const { refresh, bump } = useCalendars();
  const listeners = useRef(new Set<Listener>()).current;
  // Latest functions, so the socket effect below runs once.
  const actions = useRef({ refresh, bump });
  actions.current = { refresh, bump };

  useEffect(() => {
    let socket: WebSocket | null = null;
    let stopped = false;
    let retryMs = MIN_RETRY_MS;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;
    let batchTimer: ReturnType<typeof setTimeout> | null = null;
    const pending = { refresh: false, all: false, calendars: new Set<string>() };

    const flush = () => {
      batchTimer = null;
      const { refresh: doRefresh, all, calendars } = pending;
      pending.refresh = false;
      pending.all = false;
      pending.calendars = new Set();
      if (doRefresh) actions.current.refresh(); // reloads the list and bumps everything
      else if (all) actions.current.bump();
      for (const id of calendars) actions.current.bump(id);
    };
    const later = () => {
      if (!batchTimer) batchTimer = setTimeout(flush, BATCH_MS);
    };

    const onMessage = (data: any) => {
      switch (data?.type) {
        case 'hello':
        case 'calendars':
        case 'inbox':
          pending.refresh = true;
          break;
        case 'changed': {
          const change = data as LiveChange;
          if (change.origin && change.origin === LIVE_CLIENT_ID) return;
          // Name, colour, members or invites show in the list and settings.
          if (change.area === 'calendar' || change.area === 'members' || change.area === 'invites') pending.refresh = true;
          pending.calendars.add(change.calendarId);
          listeners.forEach((l) => l(change));
          break;
        }
        default:
          return;
      }
      later();
    };

    const scheduleReconnect = () => {
      if (stopped || retryTimer || AppState.currentState !== 'active') return;
      retryTimer = setTimeout(() => {
        retryTimer = null;
        connect();
      }, retryMs);
      retryMs = Math.min(retryMs * 2, MAX_RETRY_MS);
    };

    const connect = async () => {
      if (stopped || socket) return;
      const token = await getToken().catch(() => null);
      if (!token || stopped || socket) return;
      // The token goes in a header, not the URL, so it stays out of logs.
      const ws = new (WebSocket as any)(`${WS_BASE_URL}/live`, null, {
        headers: { Authorization: `Bearer ${token}` },
      }) as WebSocket;
      socket = ws;
      ws.onopen = () => {
        retryMs = MIN_RETRY_MS;
      };
      ws.onmessage = (e) => {
        try {
          onMessage(JSON.parse(String(e.data)));
        } catch {
          // ignore anything malformed
        }
      };
      ws.onclose = () => {
        if (socket === ws) socket = null;
        scheduleReconnect();
      };
      ws.onerror = () => {
        // onclose follows and reconnects.
      };
    };

    const disconnect = () => {
      if (retryTimer) clearTimeout(retryTimer);
      retryTimer = null;
      const ws = socket;
      socket = null;
      ws?.close();
    };

    // Only while the app is in front; coming back reconnects, and the
    // server's hello on connect reloads whatever was missed meanwhile.
    const sub = AppState.addEventListener('change', (state) => {
      if (state === 'active') {
        retryMs = MIN_RETRY_MS;
        connect();
      } else {
        disconnect();
      }
    });
    connect();
    return () => {
      stopped = true;
      sub.remove();
      if (batchTimer) clearTimeout(batchTimer);
      disconnect();
    };
  }, [listeners]);

  return <ListenersCtx.Provider value={listeners}>{children}</ListenersCtx.Provider>;
}

/** Calls onChange for every change someone else makes to calendarId, while mounted. */
export function useLiveChanges(calendarId: string, onChange: Listener) {
  const listeners = useContext(ListenersCtx);
  const fn = useRef(onChange);
  fn.current = onChange;
  useEffect(() => {
    if (!listeners) return;
    const l: Listener = (c) => {
      if (c.calendarId === calendarId) fn.current(c);
    };
    listeners.add(l);
    return () => {
      listeners.delete(l);
    };
  }, [listeners, calendarId]);
}
