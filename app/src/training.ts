import { ActivityType, Plan } from './api/calendars';
import { DateStr, diffDays, startOfWeek } from './dates';

/** "8 km · 45 min · easy" or "3 exercises". */
export function planSummary(p: Plan | undefined): string {
  if (!p) return '';
  const parts: string[] = [];
  if (p.distanceKm) parts.push(`${p.distanceKm} km`);
  if (p.durationMin) parts.push(`${p.durationMin} min`);
  if (p.intensity) parts.push(p.intensity);
  if (p.exercises?.length) parts.push(p.exercises.length === 1 ? p.exercises[0].name : `${p.exercises.length} exercises`);
  return parts.join(' · ');
}

/** "Week 3" counted from the plan start, if the calendar has one. */
export function weekLabel(weekStart: DateStr, planStart: DateStr | undefined, ws: 0 | 1): string | null {
  if (!planStart) return null;
  const n = Math.floor(diffDays(startOfWeek(planStart, ws), weekStart) / 7) + 1;
  return n >= 1 ? `Week ${n}` : null;
}

export const typeById = (types: ActivityType[], id: string | null) => types.find((t) => t.id === id);

/** Icons an activity type can have (its `icon`), shown on cards and the Overview. */
export const ACTIVITY_ICONS: { id: string; glyph: string; label: string }[] = [
  { id: 'run', glyph: '🏃', label: 'Run' },
  { id: 'bike', glyph: '🚴', label: 'Ride' },
  { id: 'swim', glyph: '🏊', label: 'Swim' },
  { id: 'lift', glyph: '🏋️', label: 'Strength' },
  { id: 'stretch', glyph: '🧘', label: 'Mobility' },
  { id: 'walk', glyph: '🚶', label: 'Walk' },
  { id: 'hike', glyph: '🥾', label: 'Hike' },
  { id: 'row', glyph: '🚣', label: 'Row' },
  { id: 'climb', glyph: '🧗', label: 'Climb' },
  { id: 'ball', glyph: '⚽', label: 'Sport' },
  { id: 'rest', glyph: '💤', label: 'Rest' },
];

export const activityGlyph = (icon: string | undefined) => ACTIVITY_ICONS.find((i) => i.id === icon)?.glyph ?? '';

/** A session's amount in a few characters: distance if there is one, else time. */
export function amount(distanceKm?: number, durationMin?: number): string {
  if (distanceKm) return `${Math.round(distanceKm * 10) / 10} km`;
  if (durationMin) return durationMin >= 60 && durationMin % 60 === 0 ? `${durationMin / 60} h` : `${durationMin} min`;
  return '';
}
