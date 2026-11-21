import { useCallback, useEffect, useLayoutEffect, useMemo, useState } from 'react';
import { RefreshControl, ScrollView, StyleSheet, View } from 'react-native';
import { Occurrence, api, displayColor } from '../api/calendars';
import { Fab, HeaderButton, Muted, Row, SectionTitle, Sheet } from '../components/kit';
import { DayMarks, MonthGrid } from '../components/MonthGrid';
import { useCalendar, useCalendars } from '../data/CalendarsContext';
import { DateStr, addDays, daysCovered, formatDay, localTime, monthGrid, startOfMonth, today } from '../dates';
import { pickIcsFile } from '../icsIntents';
import { SwitcherTitle } from '../navigation/Switcher';
import { ScreenProps } from '../navigation/types';
import { Theme, useThemedStyles } from '../theme';

// A general calendar: the month, the selected day's events, and import.
export default function GeneralScreen({ navigation, route }: ScreenProps<'General'>) {
  const styles = useThemedStyles(makeStyles);
  const { id } = route.params;
  const cal = useCalendar(id);
  const { version } = useCalendars();
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

  const marks = useMemo(() => {
    const m: Record<DateStr, DayMarks> = {};
    for (const o of occ) for (const d of daysCovered(o.start, o.end, o.allDay)) ((m[d] ??= {}).dots ??= []).push(color);
    return m;
  }, [occ, color]);

  const day = occ.filter((o) => daysCovered(o.start, o.end, o.allDay).includes(selected));

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
      <ScrollView refreshControl={<RefreshControl refreshing={loading} onRefresh={load} />}>
        <MonthGrid month={month} selected={selected} marks={marks} onSelect={setSelected} onMonthChange={(m) => setMonth(startOfMonth(m))} />
        {error ? <Muted style={{ padding: 16 }}>{error}</Muted> : null}
        <SectionTitle>{formatDay(selected, true)}</SectionTitle>
        {day.length === 0 ? <Muted style={{ paddingHorizontal: 16 }}>No events. Tap + to add one.</Muted> : null}
        {day.map((o) => (
          <Row
            key={o.eventId + o.occurrence}
            label={o.title}
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
      <Fab onPress={() => navigation.navigate('EventEditor', { calendarId: id, date: selected })} />
      <Sheet visible={menu} onClose={() => setMenu(false)}>
        <Row label="Import .ics file" detail="From your phone's files" onPress={importFile} />
        <Row
          label="Import from a link"
          detail="An .ics or webcal:// address"
          onPress={() => {
            setMenu(false);
            navigation.navigate('Import', { calendarId: id });
          }}
        />
        <Row
          label="Calendar settings"
          detail="Name, colour, members and invites"
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
