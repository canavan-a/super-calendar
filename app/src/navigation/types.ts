import { NativeStackScreenProps } from '@react-navigation/native-stack';
import { ImportSource, Kind } from '../api/calendars';
import { DateStr } from '../dates';

export type RootStackParamList = {
  Overview: undefined;
  General: { id: string };
  Subscribed: { id: string };
  Training: { id: string };
  Cycle: { id: string };
  Inbox: undefined;
  /** url: a link to subscribe to (from Import's "Subscribe instead"). */
  NewCalendar: { kind?: Kind; url?: string } | undefined;
  CalendarSettings: { id: string };
  EventEditor: { calendarId: string; eventId?: string; occurrence?: string; date?: DateStr };
  Import: { source?: ImportSource; calendarId?: string };
  SessionEditor: { calendarId: string; sessionId?: string; date?: DateStr };
  ActivityTypes: { calendarId: string };
  Settings: undefined;
};

export type ScreenProps<T extends keyof RootStackParamList> = NativeStackScreenProps<RootStackParamList, T>;

const ROUTES = { general: 'General', subscribed: 'Subscribed', training: 'Training', cycle: 'Cycle' } as const;

export const routeForKind = (k: Kind) => ROUTES[k];
