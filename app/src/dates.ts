// Dates are passed around as local 'YYYY-MM-DD' strings, which sort and
// compare as text and match what the server uses. Only the few helpers the
// screens need, rather than a date library.

export type DateStr = string;

const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

const pad = (n: number) => String(n).padStart(2, '0');

export function fmt(d: Date): DateStr {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export function parse(s: DateStr): Date {
  const [y, m, d] = s.split('-').map(Number);
  return new Date(y, m - 1, d);
}

export const today = () => fmt(new Date());

export function addDays(s: DateStr, n: number): DateStr {
  const d = parse(s);
  d.setDate(d.getDate() + n);
  return fmt(d);
}

/** Whole days from a to b. */
export function diffDays(a: DateStr, b: DateStr): number {
  return Math.round((Date.UTC(...ymd(b)) - Date.UTC(...ymd(a))) / 86400000);
}

function ymd(s: DateStr): [number, number, number] {
  const [y, m, d] = s.split('-').map(Number);
  return [y, m - 1, d];
}

export function startOfMonth(s: DateStr): DateStr {
  return s.slice(0, 8) + '01';
}

export function addMonths(s: DateStr, n: number): DateStr {
  const d = parse(startOfMonth(s));
  d.setMonth(d.getMonth() + n);
  return fmt(d);
}

export const weekday = (s: DateStr) => parse(s).getDay();

/** The first day of s's week; weekStart 1 = Monday, 0 = Sunday. */
export function startOfWeek(s: DateStr, weekStart = 1): DateStr {
  return addDays(s, -((weekday(s) - weekStart + 7) % 7));
}

/** Six weeks of days covering s's month, starting on weekStart. */
export function monthGrid(month: DateStr, weekStart = 1): DateStr[] {
  const first = startOfWeek(startOfMonth(month), weekStart);
  return Array.from({ length: 42 }, (_, i) => addDays(first, i));
}

export function weekdayLabels(weekStart = 1): string[] {
  return Array.from({ length: 7 }, (_, i) => DAYS[(i + weekStart) % 7]);
}

export const formatMonth = (s: DateStr) => `${MONTHS[parse(s).getMonth()]} ${parse(s).getFullYear()}`;

export function formatDay(s: DateStr, withYear = false): string {
  const d = parse(s);
  const base = `${DAYS[d.getDay()]} ${d.getDate()} ${MONTHS[d.getMonth()].slice(0, 3)}`;
  return withYear ? `${base} ${d.getFullYear()}` : base;
}

export function formatShort(s: DateStr): string {
  const d = parse(s);
  return `${d.getDate()} ${MONTHS[d.getMonth()].slice(0, 3)}`;
}

export const dayOfMonth = (s: DateStr) => parse(s).getDate();

/** The phone's IANA zone, sent with ranges so the server cuts days right. */
export function timeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
}

/** Local 'YYYY-MM-DD' of an RFC3339 instant. */
export const localDate = (iso: string): DateStr => fmt(new Date(iso));

/** Local 'HH:MM' of an RFC3339 instant. */
export function localTime(iso: string): string {
  const d = new Date(iso);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** RFC3339 (with the local offset) for a local date and 'HH:MM'. */
export function toInstant(date: DateStr, time: string): string {
  const [h, m] = time.split(':').map(Number);
  const d = parse(date);
  d.setHours(h || 0, m || 0, 0, 0);
  return d.toISOString();
}

export const validTime = (t: string) => /^([01]\d|2[0-3]):[0-5]\d$/.test(t);

/** Each local date an item covers, for an end that's exclusive. */
export function daysCovered(start: string, end: string, allDay: boolean): DateStr[] {
  const s = allDay ? start : localDate(start);
  let e: DateStr;
  if (allDay) {
    e = addDays(end, -1);
  } else {
    // A timed item ending exactly at midnight doesn't cover the next day.
    const endD = new Date(end);
    e = localDate(new Date(Math.max(new Date(start).getTime(), endD.getTime() - 1)).toISOString());
  }
  const out: DateStr[] = [];
  for (let d = s; d <= e && out.length < 62; d = addDays(d, 1)) out.push(d);
  return out.length ? out : [s];
}
