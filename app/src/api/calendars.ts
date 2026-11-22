import { DateStr, timeZone } from '../dates';
import { apiJson } from './client';

// Typed wrappers for the server's calendar API (server/internal/calendars).

export type Kind = 'general' | 'training' | 'cycle';

export type Calendar = {
  id: string;
  kind: Kind;
  name: string;
  color: string;
  myColor: string | null;
  inOverview: boolean;
  position: number;
  settings: { weekStart?: 0 | 1; planStart?: DateStr };
  memberCount: number;
  createdAt: string;
};

export const displayColor = (c: Calendar) => c.myColor ?? c.color;

export type Member = { userId: string; displayName: string; email: string; joinedAt: string };

export type Invite = {
  id: string;
  calendarId: string;
  calendarName: string;
  calendarKind: Kind;
  calendarColor: string;
  email: string;
  inviterName: string;
  status: string;
  createdAt: string;
};

const json = (method: string, body?: unknown): RequestInit => ({
  method,
  body: body === undefined ? undefined : JSON.stringify(body),
});

const range = (from: DateStr, to: DateStr) =>
  `from=${from}&to=${to}&tz=${encodeURIComponent(timeZone())}`;

const cal = (id: string) => `/calendars/${encodeURIComponent(id)}`;

export const api = {
  // --- Calendars and sharing ---
  listCalendars: () => apiJson<{ calendars: Calendar[] }>('/calendars').then((r) => r.calendars),
  createCalendar: (kind: Kind, name: string, color: string) =>
    apiJson<{ calendar: Calendar }>('/calendars', json('POST', { kind, name, color })).then((r) => r.calendar),
  reorder: (ids: string[]) => apiJson<void>('/calendars/order', json('PUT', { ids })),
  updateCalendar: (id: string, patch: { name?: string; color?: string; settings?: Record<string, unknown> }) =>
    apiJson<{ calendar: Calendar }>(cal(id), json('PATCH', patch)).then((r) => r.calendar),
  updateMine: (id: string, patch: { color?: string; inOverview?: boolean; position?: number }) =>
    apiJson<{ calendar: Calendar }>(`${cal(id)}/me`, json('PATCH', patch)).then((r) => r.calendar),
  leave: (id: string) => apiJson<void>(`${cal(id)}/me`, json('DELETE')),
  members: (id: string) => apiJson<{ members: Member[] }>(`${cal(id)}/members`).then((r) => r.members),
  sentInvites: (id: string) => apiJson<{ invites: Invite[] }>(`${cal(id)}/invites`).then((r) => r.invites),
  invite: (id: string, email: string) =>
    apiJson<{ invite: Invite }>(`${cal(id)}/invites`, json('POST', { email })).then((r) => r.invite),
  cancelInvite: (id: string, inviteId: string) => apiJson<void>(`${cal(id)}/invites/${inviteId}`, json('DELETE')),
  inbox: () => apiJson<{ invites: Invite[] }>('/invites').then((r) => r.invites),
  respond: (inviteId: string, accept: boolean) =>
    apiJson<{ calendar: Calendar | null }>(`/invites/${inviteId}/${accept ? 'accept' : 'decline'}`, json('POST')).then(
      (r) => r.calendar
    ),
  getSettings: () => apiJson<{ settings: UserSettings }>('/me/settings').then((r) => r.settings),
  putSettings: (settings: UserSettings) => apiJson<void>('/me/settings', json('PUT', { settings })),
  overview: (from: DateStr, to: DateStr) =>
    apiJson<{ items: OverviewItem[] }>(`/overview?${range(from, to)}`).then((r) => r.items),

  // --- General ---
  occurrences: (id: string, from: DateStr, to: DateStr) =>
    apiJson<{ occurrences: Occurrence[] }>(`${cal(id)}/events?${range(from, to)}`).then((r) => r.occurrences),
  getEvent: (id: string, eventId: string) =>
    apiJson<{ event: CalEvent }>(`${cal(id)}/events/${eventId}`).then((r) => r.event),
  createEvent: (id: string, input: EventInput) =>
    apiJson<{ event: CalEvent }>(`${cal(id)}/events`, json('POST', input)).then((r) => r.event),
  updateEvent: (id: string, eventId: string, input: EventInput, occurrence?: string) =>
    apiJson<{ event: CalEvent }>(
      `${cal(id)}/events/${eventId}${occurrence ? `?occurrence=${encodeURIComponent(occurrence)}` : ''}`,
      json('PATCH', input)
    ).then((r) => r.event),
  deleteEvent: (id: string, eventId: string, occurrence?: string) =>
    apiJson<void>(
      `${cal(id)}/events/${eventId}${occurrence ? `?occurrence=${encodeURIComponent(occurrence)}` : ''}`,
      json('DELETE')
    ),
  previewImport: (id: string, source: ImportSource) =>
    apiJson<ImportPreview>(`${cal(id)}/import/preview`, json('POST', { ...source, tz: timeZone() })),
  importIcs: (id: string, source: ImportSource) =>
    apiJson<ImportResult>(`${cal(id)}/import`, json('POST', { ...source, tz: timeZone() })),

  // --- Training ---
  activityTypes: (id: string) =>
    apiJson<{ activityTypes: ActivityType[] }>(`${cal(id)}/activity-types`).then((r) => r.activityTypes),
  createActivityType: (id: string, input: Partial<ActivityType>) =>
    apiJson<{ activityType: ActivityType }>(`${cal(id)}/activity-types`, json('POST', input)).then((r) => r.activityType),
  updateActivityType: (id: string, typeId: string, input: Partial<ActivityType>) =>
    apiJson<{ activityType: ActivityType }>(`${cal(id)}/activity-types/${typeId}`, json('PATCH', input)).then(
      (r) => r.activityType
    ),
  sessions: (id: string, from: DateStr, to: DateStr) =>
    apiJson<{ sessions: Session[] }>(`${cal(id)}/sessions?${range(from, to)}`).then((r) => r.sessions),
  getSession: (id: string, sessionId: string) =>
    apiJson<{ session: Session }>(`${cal(id)}/sessions/${sessionId}`).then((r) => r.session),
  createSession: (id: string, input: SessionInput) =>
    apiJson<{ session: Session }>(`${cal(id)}/sessions`, json('POST', input)).then((r) => r.session),
  updateSession: (id: string, sessionId: string, input: SessionInput) =>
    apiJson<{ session: Session }>(`${cal(id)}/sessions/${sessionId}`, json('PATCH', input)).then((r) => r.session),
  deleteSession: (id: string, sessionId: string) => apiJson<void>(`${cal(id)}/sessions/${sessionId}`, json('DELETE')),
  copySessions: (id: string, req: { from: DateStr; to: DateStr[]; days: number; mode: 'merge' | 'replace' }) =>
    apiJson<CopyResult>(`${cal(id)}/sessions/copy`, json('POST', req)),
  restoreSessions: (id: string, undo: CopyResult) =>
    apiJson<void>(`${cal(id)}/sessions/restore`, json('POST', { delete: undo.created, restore: undo.removed })),

  // --- Cycle ---
  cycleDays: (id: string, from?: DateStr, to?: DateStr) =>
    apiJson<{ days: CycleDay[] }>(`${cal(id)}/cycle/days${from && to ? `?from=${from}&to=${to}` : ''}`).then((r) => r.days),
  putCycleDay: (id: string, date: DateStr, flow: Flow, notes: string) =>
    apiJson<void>(`${cal(id)}/cycle/days/${date}`, json('PUT', { flow, notes })),
  cycleSummary: (id: string) =>
    apiJson<CycleSummary>(`${cal(id)}/cycle/summary?tz=${encodeURIComponent(timeZone())}`),
};

// --- Overview ---

export type OverviewItem = {
  calendarId: string;
  kind: Kind;
  layer: string;
  allDay: boolean;
  start: string;
  end: string;
  title: string;
  color: string;
  refId?: string;
  occurrence?: string;
  flow?: Flow;
  time?: string;
  // Training: the activity's icon and colour, and what was planned (or done).
  icon?: string;
  activityColor?: string;
  distanceKm?: number;
  durationMin?: number;
};

/** The Overview's switches by type and by layer within a type. Missing means on. */
export type UserSettings = {
  overview?: {
    kinds?: Partial<Record<Kind, boolean>>;
    layers?: Record<string, boolean>; // "training/done", "cycle/fertile", …
  };
};

/** Layers each kind offers in the Overview, and whether they start on. */
export const LAYERS: Record<Kind, { id: string; label: string; defaultOn: boolean }[]> = {
  general: [{ id: 'event', label: 'Events', defaultOn: true }],
  training: [
    { id: 'planned', label: 'Planned', defaultOn: true },
    { id: 'done', label: 'Done', defaultOn: true },
    { id: 'skipped', label: 'Skipped', defaultOn: false },
  ],
  cycle: [
    { id: 'period', label: 'Logged period', defaultOn: true },
    { id: 'predicted', label: 'Predicted period', defaultOn: true },
    { id: 'fertile', label: 'Fertile window (estimate)', defaultOn: false },
  ],
};

export function layerOn(settings: UserSettings, kind: Kind, layer: string): boolean {
  const v = settings.overview?.layers?.[`${kind}/${layer}`];
  return v ?? LAYERS[kind].find((l) => l.id === layer)?.defaultOn ?? true;
}

export const kindOn = (settings: UserSettings, kind: Kind) => settings.overview?.kinds?.[kind] ?? true;

// --- General ---

export type Occurrence = {
  eventId: string;
  calendarId: string;
  occurrence: string;
  recurring: boolean;
  title: string;
  location: string;
  color: string; // the event's own, or ''
  allDay: boolean;
  start: string;
  end: string;
};

export type CalEvent = {
  id: string;
  calendarId: string;
  uid: string;
  recurrenceId: string;
  title: string;
  description: string;
  location: string;
  url: string;
  /** The event's own colour, or '' for its calendar's. */
  color: string;
  allDay: boolean;
  start: string;
  end: string;
  tzid: string;
  rrule: string;
  exdates: string[];
  source: 'manual' | 'ics';
  updatedBy: string;
  updatedAt: string;
};

export type EventInput = Partial<
  Pick<CalEvent, 'title' | 'description' | 'location' | 'url' | 'color' | 'allDay' | 'start' | 'end' | 'tzid' | 'rrule'>
> & {
  /** The updatedAt the editor loaded; the server refuses (409) if it's been saved since. */
  baseUpdatedAt?: string;
};

export type ImportSource = { ics?: string; url?: string };

export type ImportPreview = {
  name: string;
  total: number;
  new: number;
  updates: number;
  errors: string[];
  sample: CalEvent[];
};

export type ImportResult = { imported: number; updated: number; skipped: number; errors: string[] };

// --- Training ---

export type ActivityField = 'duration' | 'distance' | 'intensity' | 'exercises';

export type ActivityType = {
  id: string;
  name: string;
  color: string;
  icon: string;
  fields: ActivityField[];
  archived: boolean;
  position: number;
};

export type Exercise = { name: string; sets?: number; reps?: number; weight?: number };

export type Plan = {
  durationMin?: number;
  distanceKm?: number;
  intensity?: 'easy' | 'moderate' | 'hard' | 'max';
  exercises?: Exercise[];
};

export type SessionStatus = 'planned' | 'done' | 'skipped';

export type Session = {
  id: string;
  calendarId: string;
  date: DateStr;
  time: string;
  position: number;
  activityTypeId: string | null;
  title: string;
  plan: Plan;
  notes: string;
  status: SessionStatus;
  result: Plan;
  updatedBy: string;
  updatedAt: string;
};

export type SessionInput = Partial<Omit<Session, 'id' | 'calendarId' | 'updatedBy' | 'updatedAt' | 'activityTypeId'>> & {
  activityTypeId?: string; // '' clears it
  /** The updatedAt the editor loaded; the server refuses (409) if it's been saved since. */
  baseUpdatedAt?: string;
};

export type CopyResult = { created: string[]; removed: Session[] };

// --- Cycle ---

export type Flow = 'none' | 'spotting' | 'light' | 'medium' | 'heavy';

export const FLOWS: { id: Flow; label: string }[] = [
  { id: 'none', label: 'None' },
  { id: 'spotting', label: 'Spotting' },
  { id: 'light', label: 'Light' },
  { id: 'medium', label: 'Medium' },
  { id: 'heavy', label: 'Heavy' },
];

export type CycleDay = { date: DateStr; flow: Flow; notes: string; updatedBy: string; updatedAt: string };

export type CycleSummary = {
  today: DateStr;
  periods: { start: DateStr; end: DateStr; length: number }[];
  cycles: { start: DateStr; length: number; excluded: boolean }[];
  enough: boolean;
  needPeriods: number;
  meanCycle: number;
  sdCycle: number;
  periodLength: number;
  irregular: boolean;
  predictions: {
    start: DateStr;
    end: DateStr;
    rangeDays: number;
    ovulation: DateStr;
    fertileStart: DateStr;
    fertileEnd: DateStr;
  }[];
  lateDays: number;
  /** The rest of a period under way, as expected (both ends included). */
  ongoing: { start: DateStr; end: DateStr } | null;
};

/** The fertile window's colour, whatever the calendar's. */
export const FERTILE_COLOR = '#74c0fc';

export const KIND_LABEL: Record<Kind, string> = { general: 'General', training: 'Training', cycle: 'Cycle' };

export const PALETTE = ['#2f81f7', '#1f9d55', '#e8590c', '#d6336c', '#7048e8', '#0c8599', '#f08c00', '#495057'];
