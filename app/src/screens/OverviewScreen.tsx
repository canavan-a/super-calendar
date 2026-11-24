import { useCallback, useEffect, useLayoutEffect, useMemo, useState } from 'react';
import { RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';
import {
  Calendar,
  KIND_LABEL,
  KINDS,
  Kind,
  LAYERS,
  OverviewItem,
  UserSettings,
  api,
  FERTILE_COLOR,
  Flow,
  displayColor,
  kindOn,
  layerOn,
} from '../api/calendars';
import { Button } from '../components/ui';
import { Dot, Fab, HeaderButton, Muted, Row, SectionTitle, Sheet, ToggleRow } from '../components/kit';
import { DaySheet } from './CycleScreen';
import { DayMarks, MonthGrid } from '../components/MonthGrid';
import { useCalendars } from '../data/CalendarsContext';
import { DateStr, daysCovered, formatDay, localTime, monthGrid, addDays, today, startOfMonth } from '../dates';
import { SwitcherTitle } from '../navigation/Switcher';
import { ScreenProps } from '../navigation/types';
import { Theme, useThemedStyles } from '../theme';
import { activityGlyph, amount } from '../training';

// All-day items first, then by time (training sessions by their own time).
function sortItems(items: OverviewItem[]): OverviewItem[] {
  const key = (it: OverviewItem) =>
    it.allDay && !it.time ? `0 ${it.kind === 'training' ? 1 : 0}` : `1 ${it.allDay ? it.time : localTime(it.start)}`;
  return items.slice().sort((a, b) => key(a).localeCompare(key(b)));
}

// What a day cell says about an item: an event's title; a session's activity
// icon with its distance (or time, or failing both its title).
function chipLabel(it: OverviewItem): string {
  if (it.kind !== 'training') return it.title;
  const glyph = activityGlyph(it.icon);
  const amt = amount(it.distanceKm, it.durationMin);
  if (glyph) return `${glyph} ${amt || it.title}`;
  return amt ? `${it.title} ${amt}` : it.title;
}

const FLOW_STRENGTH: Record<string, number> = { spotting: 0.15, light: 0.4, medium: 0.7, heavy: 1 };

// The main calendar: every calendar you've switched on, overlaid. The Layers
// sheet switches whole types, single calendars, and layers within a type.
export default function OverviewScreen({ navigation }: ScreenProps<'Overview'>) {
  const styles = useThemedStyles(makeStyles);
  const { calendars, settings, setSettings, upsert, version, refresh, loaded, bump } = useCalendars();
  const [month, setMonth] = useState(startOfMonth(today()));
  const [selected, setSelected] = useState<DateStr | null>(today());
  const [items, setItems] = useState<OverviewItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [layersOpen, setLayersOpen] = useState(false);
  const [addOpen, setAddOpen] = useState(false);
  const [logging, setLogging] = useState<{ calendarId: string; date: DateStr } | null>(null);

  useLayoutEffect(() => {
    navigation.setOptions({
      headerTitle: () => <SwitcherTitle title="Overview" />,
      headerRight: () => <HeaderButton label="Layers" onPress={() => setLayersOpen(true)} />,
    });
  }, [navigation]);

  const grid = useMemo(() => monthGrid(month), [month]);
  const load = useCallback(async () => {
    setLoading(true);
    try {
      setItems(await api.overview(grid[0], addDays(grid[grid.length - 1], 1)));
      setError(null);
    } catch (e: any) {
      setError(e.message ?? "Couldn't load");
    } finally {
      setLoading(false);
    }
  }, [grid]);

  useEffect(() => {
    load();
  }, [load, version, calendars.length]);

  const visible = useMemo(
    () => items.filter((it) => kindOn(settings, it.kind) && layerOn(settings, it.kind, it.layer)),
    [items, settings]
  );

  // Days with a logged period: a prediction never draws over one.
  const loggedDays = useMemo(() => {
    const s = new Set<DateStr>();
    for (const it of visible) {
      if (it.kind === 'cycle' && it.layer === 'period') daysCovered(it.start, it.end, it.allDay).forEach((d) => s.add(d));
    }
    return s;
  }, [visible]);

  const marks = useMemo(() => {
    const m: Record<DateStr, DayMarks> = {};
    const at = (d: DateStr) => (m[d] ??= {});
    for (const it of sortItems(visible)) {
      for (const d of daysCovered(it.start, it.end, it.allDay)) {
        if (it.kind === 'cycle' && it.layer === 'predicted' && loggedDays.has(d)) continue;
        const mk = at(d);
        if (it.kind === 'cycle') {
          if (it.layer === 'period') {
            const s = FLOW_STRENGTH[it.flow ?? 'light'] ?? 0.4;
            if (!mk.tint || mk.tint.strength < s) mk.tint = { color: it.color, strength: s };
          } else if (it.layer === 'predicted') {
            // Ghosted: a faint fill and a dashed edge. A logged day there wins.
            mk.outline = { color: it.color, dashed: true };
            if (!mk.tint) mk.tint = { color: it.color, strength: 0 };
          } else if (it.layer === 'fertile' && !mk.outline) {
            // Light blue whatever the calendar's colour, so it never reads as a period.
            mk.outline = { color: FERTILE_COLOR };
            if (!mk.tint) mk.tint = { color: FERTILE_COLOR, strength: 0 };
          }
        } else {
          (mk.chips ??= []).push({ label: chipLabel(it), color: it.color, faded: it.layer === 'skipped' });
        }
      }
    }
    return m;
  }, [visible, loggedDays]);

  // What "+" can add to the selected day (today if none): an event on any
  // general calendar, or a period on a cycle calendar shown here that hasn't
  // logged that day yet.
  const addDay = selected ?? today();
  const addOptions = useMemo(() => {
    const logged = new Set(
      items
        .filter((it) => it.kind === 'cycle' && it.layer === 'period' && daysCovered(it.start, it.end, it.allDay).includes(addDay))
        .map((it) => it.calendarId)
    );
    return [
      ...calendars.filter((c) => c.kind === 'general').map((c) => ({ cal: c, cycle: false })),
      ...calendars
        .filter((c) => c.kind === 'cycle' && c.inOverview && kindOn(settings, 'cycle') && !logged.has(c.id))
        .map((c) => ({ cal: c, cycle: true })),
    ];
  }, [calendars, items, settings, addDay]);

  const addTo = (o: { cal: Calendar; cycle: boolean }) => {
    setAddOpen(false);
    if (o.cycle) setLogging({ calendarId: o.cal.id, date: addDay });
    else navigation.navigate('EventEditor', { calendarId: o.cal.id, date: addDay });
  };

  const logPeriod = async (calendarId: string, date: DateStr, flow: Flow, notes: string) => {
    setLogging(null);
    if (flow === 'none' && !notes) return;
    try {
      await api.putCycleDay(calendarId, date, flow, notes);
      bump(calendarId);
      load();
    } catch (e: any) {
      setError(e.message ?? "Couldn't log that day");
    }
  };

  const dayItems = useMemo(
    () =>
      selected ? sortItems(visible.filter((it) => daysCovered(it.start, it.end, it.allDay).includes(selected))) : [],
    [visible, selected]
  );

  const openItem = (it: OverviewItem) => {
    if ((it.kind === 'general' || it.kind === 'subscribed') && it.refId) {
      navigation.navigate('EventEditor', { calendarId: it.calendarId, eventId: it.refId, occurrence: it.occurrence || undefined });
    } else if (it.kind === 'training' && it.refId) {
      navigation.navigate('SessionEditor', { calendarId: it.calendarId, sessionId: it.refId });
    } else {
      navigation.navigate('Cycle', { id: it.calendarId });
    }
  };

  const describe = (it: OverviewItem) => {
    if (it.kind === 'cycle') return it.layer === 'period' ? `Logged: ${it.flow}` : it.layer === 'predicted' ? 'Predicted period' : 'Fertile window (estimate)';
    if (it.kind === 'training')
      return [it.time, amount(it.distanceKm, it.durationMin), it.layer].filter(Boolean).join(' · ');
    return it.allDay ? 'All day' : `${localTime(it.start)} – ${localTime(it.end)}`;
  };

  if (loaded && calendars.length === 0) {
    return (
      <View style={styles.empty}>
        <Text style={styles.emptyTitle}>No calendars yet</Text>
        <Muted style={{ textAlign: 'center', marginBottom: 20 }}>
          Make a general, training or cycle calendar, subscribe to a Google or Outlook one, or accept an invite from someone who shared one with you.
        </Muted>
        <Button label="New calendar" onPress={() => navigation.navigate('NewCalendar')} style={{ alignSelf: 'stretch' }} />
        <View style={{ height: 10 }} />
        <Button label="Inbox" variant="secondary" onPress={() => navigation.navigate('Inbox')} style={{ alignSelf: 'stretch' }} />
      </View>
    );
  }

  return (
    <View style={styles.root}>
      <ScrollView refreshControl={<RefreshControl refreshing={loading} onRefresh={() => { refresh(); load(); }} />}>
        <MonthGrid
          detailed
          swipeable
          month={month}
          selected={selected}
          marks={marks}
          onSelect={(d) => setSelected((cur) => (cur === d ? null : d))}
          onMonthChange={(m) => { setMonth(startOfMonth(m)); }}
        />
        {error ? <Muted style={{ padding: 16 }}>{error}</Muted> : null}
        {selected ? <SectionTitle>{formatDay(selected, true)}</SectionTitle> : null}
        {selected && dayItems.length === 0 ? <Muted style={{ paddingHorizontal: 16 }}>Nothing on this day.</Muted> : null}
        {dayItems.map((it, i) => (
          <Row key={i} label={it.title} detail={describe(it)} left={<Dot color={it.color} />} onPress={() => openItem(it)} />
        ))}
        <View style={{ height: 100 }} />
      </ScrollView>
      {addOptions.length > 0 ? (
        <Fab onPress={() => (addOptions.length === 1 ? addTo(addOptions[0]) : setAddOpen(true))} />
      ) : null}
      <Sheet visible={addOpen} onClose={() => setAddOpen(false)} title={`Add to ${formatDay(addDay)}`}>
        {addOptions.map((o) => (
          <Row
            key={o.cal.id}
            label={o.cycle ? `Log period · ${o.cal.name}` : o.cal.name}
            detail={o.cycle ? 'Cycle' : 'New event'}
            left={<Dot color={displayColor(o.cal)} />}
            onPress={() => addTo(o)}
          />
        ))}
      </Sheet>
      {logging ? (
        <DaySheet
          date={logging.date}
          onClose={() => setLogging(null)}
          onSave={(date, flow, notes) => logPeriod(logging.calendarId, date, flow, notes)}
        />
      ) : null}
      <LayersSheet
        visible={layersOpen}
        onClose={() => setLayersOpen(false)}
        calendars={calendars}
        settings={settings}
        setSettings={setSettings}
        onToggleCalendar={async (c, on) => {
          upsert({ ...c, inOverview: on });
          try {
            upsert(await api.updateMine(c.id, { inOverview: on }));
          } catch {
            upsert(c);
          }
        }}
      />
    </View>
  );
}

function LayersSheet({
  visible,
  onClose,
  calendars,
  settings,
  setSettings,
  onToggleCalendar,
}: {
  visible: boolean;
  onClose: () => void;
  calendars: Calendar[];
  settings: UserSettings;
  setSettings: (s: UserSettings) => void;
  onToggleCalendar: (c: Calendar, on: boolean) => void;
}) {
  const setKind = (k: Kind, on: boolean) =>
    setSettings({ ...settings, overview: { ...settings.overview, kinds: { ...settings.overview?.kinds, [k]: on } } });
  const setLayer = (k: Kind, layer: string, on: boolean) =>
    setSettings({
      ...settings,
      overview: { ...settings.overview, layers: { ...settings.overview?.layers, [`${k}/${layer}`]: on } },
    });
  return (
    <Sheet visible={visible} onClose={onClose} title="Layers">
      {KINDS.map((k) => {
        const list = calendars.filter((c) => c.kind === k);
        if (!list.length) return null;
        const on = kindOn(settings, k);
        return (
          <View key={k} style={{ marginBottom: 8 }}>
            <ToggleRow label={KIND_LABEL[k]} value={on} onChange={(v) => setKind(k, v)} />
            {on ? (
              <>
                {list.map((c) => (
                  <ToggleRow
                    key={c.id}
                    indent={1}
                    label={c.name}
                    left={<Dot color={displayColor(c)} />}
                    value={c.inOverview}
                    onChange={(v) => onToggleCalendar(c, v)}
                  />
                ))}
                {LAYERS[k].length > 1
                  ? LAYERS[k].map((l) => (
                      <ToggleRow
                        key={l.id}
                        indent={2}
                        label={l.label}
                        value={layerOn(settings, k, l.id)}
                        onChange={(v) => setLayer(k, l.id, v)}
                      />
                    ))
                  : null}
              </>
            ) : null}
          </View>
        );
      })}
    </Sheet>
  );
}

const makeStyles = (theme: Theme) =>
  StyleSheet.create({
    root: { flex: 1, backgroundColor: theme.bg },
    empty: { flex: 1, backgroundColor: theme.bg, padding: 24, justifyContent: 'center', alignItems: 'center' },
    emptyTitle: { color: theme.text, fontSize: 22, fontWeight: '700', marginBottom: 8 },
  });
