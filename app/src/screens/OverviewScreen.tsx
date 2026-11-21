import { useCallback, useEffect, useLayoutEffect, useMemo, useState } from 'react';
import { RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';
import {
  Calendar,
  KIND_LABEL,
  Kind,
  LAYERS,
  OverviewItem,
  UserSettings,
  api,
  displayColor,
  kindOn,
  layerOn,
} from '../api/calendars';
import { Button } from '../components/ui';
import { Dot, HeaderButton, Muted, Row, SectionTitle, Sheet, ToggleRow } from '../components/kit';
import { DayMarks, MonthGrid } from '../components/MonthGrid';
import { useCalendars } from '../data/CalendarsContext';
import { DateStr, daysCovered, formatDay, localTime, monthGrid, addDays, today, startOfMonth } from '../dates';
import { SwitcherTitle } from '../navigation/Switcher';
import { ScreenProps } from '../navigation/types';
import { Theme, useThemedStyles } from '../theme';

const FLOW_STRENGTH: Record<string, number> = { spotting: 0.15, light: 0.4, medium: 0.7, heavy: 1 };

// The main calendar: every calendar you've switched on, overlaid. The Layers
// sheet switches whole types, single calendars, and layers within a type.
export default function OverviewScreen({ navigation }: ScreenProps<'Overview'>) {
  const styles = useThemedStyles(makeStyles);
  const { calendars, settings, setSettings, upsert, version, refresh, loaded } = useCalendars();
  const [month, setMonth] = useState(startOfMonth(today()));
  const [selected, setSelected] = useState<DateStr>(today());
  const [items, setItems] = useState<OverviewItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [layersOpen, setLayersOpen] = useState(false);

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

  const marks = useMemo(() => {
    const m: Record<DateStr, DayMarks> = {};
    const at = (d: DateStr) => (m[d] ??= {});
    for (const it of visible) {
      for (const d of daysCovered(it.start, it.end, it.allDay)) {
        const mk = at(d);
        if (it.kind === 'cycle') {
          if (it.layer === 'period') {
            const s = FLOW_STRENGTH[it.flow ?? 'light'] ?? 0.4;
            if (!mk.tint || mk.tint.strength < s) mk.tint = { color: it.color, strength: s };
          } else if (it.layer === 'predicted') {
            mk.outline = { color: it.color, dashed: true };
          } else if (it.layer === 'fertile' && !mk.outline) {
            mk.outline = { color: it.color + '88' };
          }
        } else {
          (mk.dots ??= []).push(it.color);
        }
      }
    }
    return m;
  }, [visible]);

  const dayItems = useMemo(
    () =>
      visible
        .filter((it) => daysCovered(it.start, it.end, it.allDay).includes(selected))
        .sort((a, b) => (a.allDay === b.allDay ? a.start.localeCompare(b.start) : a.allDay ? -1 : 1)),
    [visible, selected]
  );

  const openItem = (it: OverviewItem) => {
    if (it.kind === 'general' && it.refId) {
      navigation.navigate('EventEditor', { calendarId: it.calendarId, eventId: it.refId, occurrence: it.occurrence || undefined });
    } else if (it.kind === 'training' && it.refId) {
      navigation.navigate('SessionEditor', { calendarId: it.calendarId, sessionId: it.refId });
    } else {
      navigation.navigate('Cycle', { id: it.calendarId });
    }
  };

  const describe = (it: OverviewItem) => {
    if (it.kind === 'cycle') return it.layer === 'period' ? `Logged: ${it.flow}` : it.layer === 'predicted' ? 'Predicted period' : 'Fertile window (estimate)';
    if (it.kind === 'training') return `${it.time ? it.time + ' · ' : ''}${it.layer}`;
    return it.allDay ? 'All day' : `${localTime(it.start)} – ${localTime(it.end)}`;
  };

  if (loaded && calendars.length === 0) {
    return (
      <View style={styles.empty}>
        <Text style={styles.emptyTitle}>No calendars yet</Text>
        <Muted style={{ textAlign: 'center', marginBottom: 20 }}>
          Make a general, training or cycle calendar, or accept an invite from someone who shared one with you.
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
        <MonthGrid month={month} selected={selected} marks={marks} onSelect={setSelected} onMonthChange={(m) => { setMonth(startOfMonth(m)); }} />
        {error ? <Muted style={{ padding: 16 }}>{error}</Muted> : null}
        <SectionTitle>{formatDay(selected, true)}</SectionTitle>
        {dayItems.length === 0 ? <Muted style={{ paddingHorizontal: 16 }}>Nothing on this day.</Muted> : null}
        {dayItems.map((it, i) => (
          <Row key={i} label={it.title} detail={describe(it)} left={<Dot color={it.color} />} onPress={() => openItem(it)} />
        ))}
        <View style={{ height: 40 }} />
      </ScrollView>
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
  const kinds: Kind[] = ['general', 'training', 'cycle'];
  return (
    <Sheet visible={visible} onClose={onClose} title="Layers">
      {kinds.map((k) => {
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
