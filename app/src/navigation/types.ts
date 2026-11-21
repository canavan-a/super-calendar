import { NativeStackScreenProps } from '@react-navigation/native-stack';
import { ImportSource, Kind } from '../api/calendars';
import { DateStr } from '../dates';

export type RootStackParamList = {
  Overview: undefined;
  General: { id: string };
  Training: { id: string };
  Cycle: { id: string };
  Inbox: undefined;
  NewCalendar: { kind?: Kind } | undefined;
  CalendarSettings: { id: string };
  EventEditor: { calendarId: string; eventId?: string; occurrence?: string; date?: DateStr };
  Import: { source?: ImportSource; calendarId?: string };
  SessionEditor: { calendarId: string; sessionId?: string; date?: DateStr };
  ActivityTypes: { calendarId: string };
  Settings: undefined;
};

export type ScreenProps<T extends keyof RootStackParamList> = NativeStackScreenProps<RootStackParamList, T>;

export const routeForKind = (k: Kind) => (k === 'general' ? 'General' : k === 'training' ? 'Training' : 'Cycle') as
  | 'General'
  | 'Training'
  | 'Cycle';
