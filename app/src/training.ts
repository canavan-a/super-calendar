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
