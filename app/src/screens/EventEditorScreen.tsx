import { useEffect, useLayoutEffect, useState } from 'react';
import { ActivityIndicator, Alert, Linking, ScrollView, StyleSheet, Text, View } from 'react-native';
import { CalEvent, EventInput, api, displayColor } from '../api/calendars';
import { DateField } from '../components/DateField';
import { ColorPicker, Dot, ErrorText, HeaderButton, Muted, Row, SectionTitle, ToggleRow } from '../components/kit';
import { TextField } from '../components/ui';
import { useCalendar, useCalendars } from '../data/CalendarsContext';
import { DateStr, addDays, diffDays, formatDay, localDate, localTime, timeZone, toInstant, today, validTime } from '../dates';
import { ScreenProps } from '../navigation/types';
import { ChangedBanner } from '../components/ChangedBanner';
import { useLiveChanges } from '../live';
import { ApiError } from '../api/client';
import { Theme, useTheme, useThemedStyles } from '../theme';

const REPEATS = [
  { id: '', label: 'Never' },
  { id: 'FREQ=DAILY', label: 'Every day' },
  { id: 'FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR', label: 'Every weekday' },
  { id: 'FREQ=WEEKLY', label: 'Every week' },
  { id: 'FREQ=WEEKLY;INTERVAL=2', label: 'Every 2 weeks' },
  { id: 'FREQ=MONTHLY', label: 'Every month' },
  { id: 'FREQ=YEARLY', label: 'Every year' },
];

type Form = {
  title: string;
  allDay: boolean;
  startDate: DateStr;
  startTime: string;
  endDate: DateStr;
  endTime: string;
  rrule: string;
  location: string;
  description: string;
  color: string; // '' = the calendar's
};

function formFromEvent(e: CalEvent, occurrence?: string): Form {
  let start = e.start;
  let end = e.end;
  if (occurrence && e.rrule) {
    // Show the occurrence that was tapped, not the series' first one.
    if (e.allDay) {
      end = addDays(occurrence, diffDays(e.start, e.end));
    } else {
      end = new Date(new Date(occurrence).getTime() + (new Date(e.end).getTime() - new Date(e.start).getTime())).toISOString();
    }
    start = occurrence;
  }
  return {
    title: e.title,
    allDay: e.allDay,
    startDate: e.allDay ? start : localDate(start),
    startTime: e.allDay ? '09:00' : localTime(start),
    endDate: e.allDay ? addDays(end, -1) : localDate(end),
    endTime: e.allDay ? '10:00' : localTime(end),
    rrule: e.rrule,
    location: e.location,
    description: e.description,
    color: e.color ?? '',
  };
}

export default function EventEditorScreen({ navigation, route }: ScreenProps<'EventEditor'>) {
  const styles = useThemedStyles(makeStyles);
  const theme = useTheme();
  const { calendarId, eventId, occurrence } = route.params;
  const { bump } = useCalendars();
  const cal = useCalendar(calendarId);
  // A subscribed calendar's events are its link's: shown, never edited.
  const readOnly = cal?.kind === 'subscribed';
  const [event, setEvent] = useState<CalEvent | null>(null);
  const [form, setForm] = useState<Form | null>(
    eventId
      ? null
      : {
          title: '',
          allDay: false,
          startDate: route.params.date ?? today(),
          startTime: '09:00',
          endDate: route.params.date ?? today(),
          endTime: '10:00',
          rrule: '',
          location: '',
          description: '',
          color: '',
        }
  );
  const [custom, setCustom] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!eventId) return;
    api
      .getEvent(calendarId, eventId)
      .then((e) => {
        setEvent(e);
        setForm(formFromEvent(e, occurrence));
        setCustom(!!e.rrule && !REPEATS.some((r) => r.id === e.rrule));
      })
      .catch((e) => setError(e.message));
  }, [calendarId, eventId, occurrence]);

  const set = (patch: Partial<Form>) => setForm((f) => (f ? { ...f, ...patch } : f));
  const series = !!event?.rrule && !!occurrence;

  const input = (f: Form): EventInput | string => {
    if (!f.allDay && (!validTime(f.startTime) || !validTime(f.endTime))) return 'Times look like 09:30';
    if (f.allDay) {
      if (f.endDate < f.startDate) return 'It ends before it starts';
      return { title: f.title, allDay: true, start: f.startDate, end: addDays(f.endDate, 1), location: f.location, description: f.description, color: f.color, rrule: f.rrule, tzid: timeZone() };
    }
    const start = toInstant(f.startDate, f.startTime);
    const end = toInstant(f.endDate, f.endTime);
    if (end < start) return 'It ends before it starts';
    return { title: f.title, allDay: false, start, end, location: f.location, description: f.description, color: f.color, rrule: f.rrule, tzid: event?.tzid && event.tzid !== 'UTC' ? event.tzid : timeZone() };
  };

  // "All events": keep the series' first date, take the new times and length.
  const rebaseOnSeries = (f: Form, e: CalEvent): Form => {
    const first = e.allDay ? e.start : localDate(e.start);
    return { ...f, startDate: first, endDate: addDays(first, diffDays(f.startDate, f.endDate)) };
  };

  // Saves send the version this editor loaded (base); if someone saved in
  // between, the server refuses with their version, and this asks whether to
  // keep theirs or overwrite. null base overwrites.
  const run = async (fn: (base?: string) => Promise<unknown>, base: string | null = event?.updatedAt ?? null) => {
    setSaving(true);
    setError(null);
    try {
      await fn(base ?? undefined);
      bump(calendarId);
      navigation.goBack();
    } catch (e: any) {
      setSaving(false);
      if (e instanceof ApiError && e.status === 409 && e.body?.conflict) {
        const current = e.body.current as CalEvent;
        Alert.alert(`${e.body.by || 'Someone'} changed this`, 'They saved it while you were editing. Keep their version, or overwrite it with yours?', [
          { text: 'Keep theirs', onPress: () => loadTheirs(current) },
          { text: 'Overwrite', style: 'destructive', onPress: () => run(fn, null) },
        ]);
        return;
      }
      setError(e.message);
    }
  };

  // Someone else saved this event while it's open here (live updates).
  const [remote, setRemote] = useState<string | null>(null);
  useLiveChanges(calendarId, (c) => {
    // Read-only: there's nothing of yours to lose, so just show the new one.
    if (readOnly) loadTheirs();
    else if (eventId && c.itemId === eventId) setRemote(c.byName || 'Someone');
  });
  const loadTheirs = (current?: CalEvent) => {
    setRemote(null);
    const apply = (e: CalEvent) => {
      setEvent(e);
      setForm(formFromEvent(e, occurrence));
    };
    if (current) apply(current);
    else if (eventId) api.getEvent(calendarId, eventId).then(apply).catch((e) => setError(e.message));
  };

  const save = () => {
    if (!form) return;
    const body = input(form);
    if (typeof body === 'string') return setError(body);
    if (!eventId) return run(() => api.createEvent(calendarId, body));
    if (!series || !event) return run((base) => api.updateEvent(calendarId, eventId, { ...body, baseUpdatedAt: base }));
    Alert.alert('Change which?', 'This is a repeating event.', [
      { text: 'Cancel', style: 'cancel' },
      { text: 'Only this one', onPress: () => run((base) => api.updateEvent(calendarId, eventId, { ...body, rrule: undefined, baseUpdatedAt: base }, occurrence)) },
      {
        text: 'All of them',
        onPress: () => {
          const all = input(rebaseOnSeries(form, event));
          if (typeof all !== 'string') run((base) => api.updateEvent(calendarId, eventId, { ...all, baseUpdatedAt: base }));
        },
      },
    ]);
  };

  const remove = () => {
    if (!eventId) return;
    if (series) {
      Alert.alert('Delete which?', 'This is a repeating event.', [
        { text: 'Cancel', style: 'cancel' },
        { text: 'Only this one', onPress: () => run(() => api.deleteEvent(calendarId, eventId, occurrence)) },
        { text: 'All of them', style: 'destructive', onPress: () => run(() => api.deleteEvent(calendarId, eventId)) },
      ]);
    } else {
      Alert.alert('Delete this event?', undefined, [
        { text: 'Cancel', style: 'cancel' },
        { text: 'Delete', style: 'destructive', onPress: () => run(() => api.deleteEvent(calendarId, eventId)) },
      ]);
    }
  };

  useLayoutEffect(() => {
    navigation.setOptions({
      title: eventId ? 'Event' : 'New event',
      headerRight: readOnly
        ? () => null
        : () => (saving ? <ActivityIndicator color={theme.primary} /> : <HeaderButton label="Save" onPress={save} />),
    });
  });

  if (!form) {
    return (
      <View style={[styles.root, { justifyContent: 'center' }]}>
        {error ? <ErrorText error={error} /> : <ActivityIndicator color={theme.primary} />}
      </View>
    );
  }

  if (readOnly && event) {
    const repeat = event.rrule ? REPEATS.find((r) => r.id === event.rrule)?.label ?? 'Repeats' : '';
    const sameDay = form.startDate === form.endDate;
    const when = form.allDay
      ? sameDay
        ? `${formatDay(form.startDate, true)} · all day`
        : `${formatDay(form.startDate, true)} – ${formatDay(form.endDate, true)}`
      : sameDay
        ? `${formatDay(form.startDate, true)} · ${form.startTime} – ${form.endTime}`
        : `${formatDay(form.startDate, true)} ${form.startTime} – ${formatDay(form.endDate, true)} ${form.endTime}`;
    return (
      <ScrollView style={styles.root} contentContainerStyle={{ padding: 16, paddingBottom: 60, gap: 12 }}>
        <ErrorText error={error} />
        <View style={styles.pair}>
          <Dot color={event.color || (cal ? displayColor(cal) : theme.textMuted)} />
          <Text style={styles.viewTitle}>{event.title || '(no title)'}</Text>
        </View>
        <Text style={styles.viewText}>{when}</Text>
        {repeat ? <Text style={styles.viewText}>{repeat}</Text> : null}
        {event.location ? <Text style={styles.viewText}>{event.location}</Text> : null}
        {event.url ? (
          <Text style={[styles.viewText, { color: theme.primary }]} onPress={() => Linking.openURL(event.url).catch(() => {})}>
            {event.url}
          </Text>
        ) : null}
        {event.description ? <Text style={styles.viewText}>{event.description}</Text> : null}
        <Muted style={{ fontSize: 13, marginTop: 8 }}>
          From {cal?.name ?? 'a subscribed calendar'}, which follows a link, so its events change there and can't be edited here.
        </Muted>
      </ScrollView>
    );
  }

  return (
    <ScrollView style={styles.root} contentContainerStyle={{ padding: 16, paddingBottom: 60, gap: 10 }} keyboardShouldPersistTaps="handled">
      <ErrorText error={error} />
      {remote ? <ChangedBanner name={remote} onLoad={() => loadTheirs()} /> : null}
      <TextField value={form.title} onChangeText={(t) => set({ title: t })} placeholder="Title" autoFocus={!eventId} />
      <ToggleRow label="All day" value={form.allDay} onChange={(v) => set({ allDay: v })} />
      <Text style={styles.label}>Starts</Text>
      <View style={styles.pair}>
        <DateField
          value={form.startDate}
          label="Starts"
          onChange={(d) => set({ startDate: d, endDate: d > form.endDate ? d : addDays(d, diffDays(form.startDate, form.endDate)) })}
        />
        {!form.allDay ? (
          <TextField value={form.startTime} onChangeText={(t) => set({ startTime: t })} style={styles.time} keyboardType="numbers-and-punctuation" maxLength={5} />
        ) : null}
      </View>
      <Text style={styles.label}>Ends</Text>
      <View style={styles.pair}>
        <DateField value={form.endDate} label="Ends" onChange={(d) => set({ endDate: d })} />
        {!form.allDay ? (
          <TextField value={form.endTime} onChangeText={(t) => set({ endTime: t })} style={styles.time} keyboardType="numbers-and-punctuation" maxLength={5} />
        ) : null}
      </View>

      <SectionTitle>Repeats</SectionTitle>
      {REPEATS.map((r) => (
        <Row
          key={r.id}
          label={r.label}
          right={<Text style={{ color: theme.primary }}>{!custom && form.rrule === r.id ? '●' : '○'}</Text>}
          onPress={() => {
            setCustom(false);
            set({ rrule: r.id });
          }}
        />
      ))}
      <Row label="Custom rule" right={<Text style={{ color: theme.primary }}>{custom ? '●' : '○'}</Text>} onPress={() => setCustom(true)} />
      {custom ? (
        <>
          <TextField value={form.rrule} onChangeText={(t) => set({ rrule: t })} placeholder="FREQ=MONTHLY;BYDAY=1MO" autoCapitalize="characters" />
          <Muted style={{ fontSize: 13 }}>An iCalendar RRULE, as calendar files use.</Muted>
        </>
      ) : null}

      <SectionTitle>Colour</SectionTitle>
      <Row
        label={`Calendar's colour${cal ? ` (${cal.name})` : ''}`}
        left={<Dot color={cal ? displayColor(cal) : theme.textMuted} />}
        right={<Text style={{ color: theme.primary }}>{form.color ? '○' : '●'}</Text>}
        onPress={() => set({ color: '' })}
      />
      <ColorPicker value={form.color} onChange={(c) => set({ color: c })} />

      <SectionTitle>Details</SectionTitle>
      <TextField value={form.location} onChangeText={(t) => set({ location: t })} placeholder="Location" />
      <TextField value={form.description} onChangeText={(t) => set({ description: t })} placeholder="Notes" multiline style={{ minHeight: 90, textAlignVertical: 'top' }} />
      {event ? (
        <Muted style={{ fontSize: 13 }}>
          {event.source === 'ics' ? 'Imported from a calendar file. ' : ''}
          {event.updatedBy ? `Last edited by ${event.updatedBy}.` : ''}
        </Muted>
      ) : null}
      {eventId ? <Row label="Delete event" danger onPress={remove} /> : null}
    </ScrollView>
  );
}

const makeStyles = (theme: Theme) =>
  StyleSheet.create({
    root: { flex: 1, backgroundColor: theme.bg },
    label: { color: theme.textMuted, fontSize: 13, fontWeight: '600', marginTop: 4 },
    pair: { flexDirection: 'row', gap: 8 },
    time: { width: 96, textAlign: 'center' },
    viewTitle: { color: theme.text, fontSize: 22, fontWeight: '700', flex: 1 },
    viewText: { color: theme.text, fontSize: 16 },
  });
