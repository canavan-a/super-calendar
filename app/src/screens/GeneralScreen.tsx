import React, { useCallback, useEffect, useLayoutEffect, useMemo, useState } from 'react';
import { RefreshControl, ScrollView, StyleSheet, View } from 'react-native';
import { Occurrence, api, displayColor } from '../api/calendars';
import { Dot, Fab, HeaderButton, Muted, Row, SectionTitle, Sheet } from '../components/kit';
import { DayMarks, MonthGrid } from '../components/MonthGrid';
import { useCalendar, useCalendarVersion } from '../data/CalendarsContext';
import { DateStr, addDays, daysCovered, formatDay, localTime, monthGrid, startOfMonth, today } from '../dates';
import { pickIcsFile } from '../icsIntents';
import { SwitcherTitle } from '../navigation/Switcher';
import { ScreenProps } from '../navigation/types';
import { Theme, useThemedStyles } from '../theme';

// A general calendar: the month, the selected day's events, and import.
export default function GeneralScreen({ navigation, route }: ScreenProps<'General'>) {
  return <EventsCalendar navigation={navigation as any} id={route.params.id} />;
}

/**
 * The month and the selected day's events, for the kinds made of events.
 * readOnly (a subscribed calendar, whose events are its link's) drops adding
 * and importing; top goes above the month (the sync status).
 */
export function EventsCalendar({
  navigation,
  id,
  readOnly = false,
  top,
  onReload,
}: {
  navigation: ScreenProps<'General'>['navigation'];
  id: string;
  readOnly?: boolean;
  top?: React.ReactNode;
  onReload?: () => void;
}) {
  const styles = useThemedStyles(makeStyles);
  const cal = useCalendar(id);
  const version = useCalendarVersion(id);
  const [month, setMonth] = useState(startOfMonth(today()));
  const [selected, setSelected] = useState(today());
  const [occ, setOcc] = useState<Occurrence[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [menu, setMenu] = useState(false);
  const color = cal ? displayColor(cal) : '#888';

  useLayoutEffect(() => {
    navigation.setOptions({
      headerTitle: () => <SwitcherTitle title={cal?.name ?? ''} color={color} />,
      headerRight: () => <HeaderButton label="⋯" onPress={() => setMenu(true)} />,
    });
  }, [navigation, cal?.name, color]);

  const grid = useMemo(() => monthGrid(month), [month]);
  const load = useCallback(async () => {
    setLoading(true);
    try {
      setOcc(await api.occurrences(id, grid[0], addDays(grid[41], 1)));
      setError(null);
    } catch (e: any) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [id, grid]);

  useEffect(() => {
    const unsub = navigation.addListener('focus', load);
    return unsub;
  }, [navigation, load]);
  useEffect(() => {
    load();
  }, [load, version]);
  const reload = () => {
    load();
    onReload?.();
  };

  // All-day events first, then by start time.
  const sorted = useMemo(
    () => occ.slice().sort((a, b) => (a.allDay === b.allDay ? a.start.localeCompare(b.start) : a.allDay ? -1 : 1)),
    [occ]
  );

  // Each day lists its events' titles, in each event's own colour.
  const marks = useMemo(() => {
    const m: Record<DateStr, DayMarks> = {};
    for (const o of sorted)
      for (const d of daysCovered(o.start, o.end, o.allDay)) ((m[d] ??= {}).chips ??= []).push({ label: o.title, color: o.color || color });
    return m;
  }, [sorted, color]);

  const day = sorted.filter((o) => daysCovered(o.start, o.end, o.allDay).includes(selected));

  const importFile = async () => {
    setMenu(false);
    try {
      const picked = await pickIcsFile();
      if (picked?.text) navigation.navigate('Import', { source: { ics: picked.text }, calendarId: id });
    } catch (e: any) {
      setError(e.message);
    }
  };

  return (
    <View style={styles.root}>
      <ScrollView refreshControl={<RefreshControl refreshing={loading} onRefresh={reload} />}>
        {top}
        <MonthGrid detailed month={month} selected={selected} marks={marks} onSelect={setSelected} onMonthChange={(m) => setMonth(startOfMonth(m))} />
        {error ? <Muted style={{ padding: 16 }}>{error}</Muted> : null}
        <SectionTitle>{formatDay(selected, true)}</SectionTitle>
        {day.length === 0 ? (
          <Muted style={{ paddingHorizontal: 16 }}>{readOnly ? 'No events.' : 'No events. Tap + to add one.'}</Muted>
        ) : null}
        {day.map((o) => (
          <Row
            key={o.eventId + o.occurrence}
            label={o.title}
            left={<Dot color={o.color || color} />}
            detail={[o.allDay ? 'All day' : `${localTime(o.start)} – ${localTime(o.end)}`, o.location, o.recurring ? 'repeats' : '']
              .filter(Boolean)
              .join(' · ')}
            onPress={() =>
              navigation.navigate('EventEditor', { calendarId: id, eventId: o.eventId, occurrence: o.occurrence || undefined })
            }
          />
        ))}
        <View style={{ height: 100 }} />
      </ScrollView>
      {readOnly ? null : <Fab onPress={() => navigation.navigate('EventEditor', { calendarId: id, date: selected })} />}
      <Sheet visible={menu} onClose={() => setMenu(false)}>
        {readOnly ? null : (
          <>
            <Row label="Import .ics file" detail="From your phone's files" onPress={importFile} />
            <Row
              label="Import from a link"
              detail="An .ics or webcal:// address"
              onPress={() => {
                setMenu(false);
                navigation.navigate('Import', { calendarId: id });
              }}
            />
          </>
        )}
        <Row
          label="Calendar settings"
          detail={readOnly ? 'Name, colour, the link, members and invites' : 'Name, colour, members and invites'}
          onPress={() => {
            setMenu(false);
            navigation.navigate('CalendarSettings', { id });
          }}
        />
      </Sheet>
    </View>
  );
}

const makeStyles = (theme: Theme) => StyleSheet.create({ root: { flex: 1, backgroundColor: theme.bg } });
