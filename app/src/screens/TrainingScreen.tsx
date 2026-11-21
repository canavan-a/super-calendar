import { useCallback, useEffect, useLayoutEffect, useMemo, useState } from 'react';
import { Alert, Pressable, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';
import { ActivityType, CopyResult, Session, api, displayColor } from '../api/calendars';
import { DateField } from '../components/DateField';
import { ErrorText, HeaderButton, Muted, Row, Segmented, Sheet, Snackbar } from '../components/kit';
import { Button } from '../components/ui';
import { useCalendar, useCalendars } from '../data/CalendarsContext';
import { DateStr, addDays, formatDay, formatShort, startOfWeek, today } from '../dates';
import { SwitcherTitle } from '../navigation/Switcher';
import { ScreenProps } from '../navigation/types';
import { Theme, useTheme, useThemedStyles } from '../theme';
import { planSummary, typeById, weekLabel } from '../training';

type CopyTarget = { kind: 'week' | 'day'; from: DateStr };

// A training calendar: weeks stacked top to bottom, each day's sessions as
// cards. A week (or a day) can be copied onto later ones and then tweaked;
// every copy can be undone straight away.
export default function TrainingScreen({ navigation, route }: ScreenProps<'Training'>) {
  const styles = useThemedStyles(makeStyles);
  const theme = useTheme();
  const { id } = route.params;
  const cal = useCalendar(id);
  const { version } = useCalendars();
  const ws = (cal?.settings.weekStart ?? 1) as 0 | 1;
  const [first, setFirst] = useState(() => addDays(startOfWeek(today(), ws), -7));
  const [weeks, setWeeks] = useState(6);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [types, setTypes] = useState<ActivityType[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [menu, setMenu] = useState(false);
  const [planOpen, setPlanOpen] = useState(false);
  const [copy, setCopy] = useState<CopyTarget | null>(null);
  const [snack, setSnack] = useState<{ msg: string; undo?: CopyResult } | null>(null);
  const color = cal ? displayColor(cal) : '#888';

  useLayoutEffect(() => {
    navigation.setOptions({
      headerTitle: () => <SwitcherTitle title={cal?.name ?? ''} color={color} />,
      headerRight: () => <HeaderButton label="⋯" onPress={() => setMenu(true)} />,
    });
  }, [navigation, cal?.name, color]);

  const last = addDays(first, weeks * 7);
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [s, t] = await Promise.all([api.sessions(id, first, last), api.activityTypes(id)]);
      setSessions(s);
      setTypes(t);
      setError(null);
    } catch (e: any) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [id, first, last]);

  useEffect(() => navigation.addListener('focus', load), [navigation, load]);
  useEffect(() => {
    load();
  }, [load, version]);

  const byDay = useMemo(() => {
    const m: Record<DateStr, Session[]> = {};
    for (const s of sessions) (m[s.date] ??= []).push(s);
    return m;
  }, [sessions]);

  const runCopy = async (from: DateStr, to: DateStr[], days: number, mode: 'merge' | 'replace') => {
    setCopy(null);
    try {
      const res = await api.copySessions(id, { from, to, days, mode });
      await load();
      setSnack({
        msg: `Copied ${res.created.length} session${res.created.length === 1 ? '' : 's'}${res.removed.length ? `, replaced ${res.removed.length}` : ''}`,
        undo: res,
      });
    } catch (e: any) {
      setError(e.message);
    }
  };

  const clearWeek = (start: DateStr) => {
    const list = sessions.filter((s) => s.date >= start && s.date < addDays(start, 7));
    if (!list.length) return;
    Alert.alert('Clear this week?', `${list.length} sessions will be deleted.`, [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Clear',
        style: 'destructive',
        onPress: async () => {
          try {
            for (const s of list) await api.deleteSession(id, s.id);
            await load();
            setSnack({ msg: `Cleared ${list.length} sessions`, undo: { created: [], removed: list } });
          } catch (e: any) {
            setError(e.message);
          }
        },
      },
    ]);
  };

  const undo = async () => {
    const u = snack?.undo;
    setSnack(null);
    if (!u) return;
    try {
      await api.restoreSessions(id, u);
      await load();
    } catch (e: any) {
      setError(e.message);
    }
  };

  const toggleDone = async (s: Session) => {
    try {
      await api.updateSession(id, s.id, { status: s.status === 'done' ? 'planned' : 'done' });
      load();
    } catch (e: any) {
      setError(e.message);
    }
  };

  const weekStarts = Array.from({ length: weeks }, (_, i) => addDays(first, i * 7));
  const now = today();

  return (
    <View style={styles.root}>
      <ScrollView refreshControl={<RefreshControl refreshing={loading} onRefresh={load} />} contentContainerStyle={{ paddingBottom: 140 }}>
        <ErrorText error={error} />
        <Pressable onPress={() => {
          setFirst(addDays(first, -28));
          setWeeks(weeks + 4);
        }} style={styles.more}>
          <Text style={styles.moreText}>Show earlier weeks</Text>
        </Pressable>
        {weekStarts.map((w) => {
          const label = weekLabel(w, cal?.settings.planStart, ws);
          const isNow = now >= w && now < addDays(w, 7);
          return (
            <View key={w} style={[styles.week, isNow && { borderColor: theme.primary }]}>
              <View style={styles.weekHeader}>
                <Text style={styles.weekTitle}>
                  {label ? `${label} · ` : ''}
                  {formatShort(w)} – {formatShort(addDays(w, 6))}
                </Text>
                <Pressable hitSlop={10} onPress={() => setCopy({ kind: 'week', from: w })}>
                  <Text style={styles.weekAction}>Copy</Text>
                </Pressable>
                <Pressable hitSlop={10} onPress={() => clearWeek(w)}>
                  <Text style={[styles.weekAction, { color: theme.textMuted }]}>Clear</Text>
                </Pressable>
              </View>
              {Array.from({ length: 7 }, (_, i) => addDays(w, i)).map((d) => (
                <View key={d} style={styles.day}>
                  <Pressable
                    style={styles.dayLabel}
                    onPress={() => navigation.navigate('SessionEditor', { calendarId: id, date: d })}
                    onLongPress={() => setCopy({ kind: 'day', from: d })}
                  >
                    <Text style={[styles.dayName, d === now && { color: theme.primary }]}>{formatDay(d).split(' ')[0]}</Text>
                    <Text style={styles.dayNum}>{d.slice(8)}</Text>
                  </Pressable>
                  <View style={{ flex: 1, gap: 6 }}>
                    {(byDay[d] ?? []).map((s) => {
                      const t = typeById(types, s.activityTypeId);
                      return (
                        <Pressable
                          key={s.id}
                          onPress={() => navigation.navigate('SessionEditor', { calendarId: id, sessionId: s.id })}
                          onLongPress={() => toggleDone(s)}
                          style={[styles.card, { borderLeftColor: t?.color ?? color }, s.status === 'skipped' && { opacity: 0.5 }]}
                        >
                          <View style={{ flex: 1 }}>
                            <Text style={[styles.cardTitle, s.status === 'skipped' && { textDecorationLine: 'line-through' }]} numberOfLines={1}>
                              {s.time ? `${s.time} ` : ''}
                              {s.title}
                            </Text>
                            {planSummary(s.status === 'done' && Object.keys(s.result).length ? s.result : s.plan) ? (
                              <Text style={styles.cardDetail} numberOfLines={1}>
                                {planSummary(s.status === 'done' && Object.keys(s.result).length ? s.result : s.plan)}
                              </Text>
                            ) : null}
                          </View>
                          <Text style={{ color: s.status === 'done' ? theme.success : theme.textMuted, fontSize: 18 }}>
                            {s.status === 'done' ? '✓' : s.status === 'skipped' ? '–' : '○'}
                          </Text>
                        </Pressable>
                      );
                    })}
                  </View>
                </View>
              ))}
            </View>
          );
        })}
        <Pressable onPress={() => setWeeks(weeks + 4)} style={styles.more}>
          <Text style={styles.moreText}>Show later weeks</Text>
        </Pressable>
        <Muted style={{ textAlign: 'center', fontSize: 12, paddingHorizontal: 24 }}>
          Tap a day to add a session. Long-press a day to copy it, or a session to mark it done.
        </Muted>
      </ScrollView>

      <CopySheet target={copy} ws={ws} onClose={() => setCopy(null)} onCopy={runCopy} />
      <Snackbar message={snack?.msg ?? null} action={snack?.undo ? 'Undo' : undefined} onAction={undo} onDismiss={() => setSnack(null)} />
      <Sheet visible={menu} onClose={() => setMenu(false)}>
        <Row
          label="Activities"
          detail="Run, ride, strength… and which plan fields each uses"
          onPress={() => {
            setMenu(false);
            navigation.navigate('ActivityTypes', { calendarId: id });
          }}
        />
        <Row
          label={cal?.settings.planStart ? `Plan starts ${formatDay(cal.settings.planStart, true)}` : 'Number weeks from a start date'}
          detail="Weeks show as Week 1, Week 2… of the plan"
          onPress={() => {
            setMenu(false);
            setPlanOpen(true);
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
      <PlanStartSheet calendarId={id} open={planOpen} onClose={() => setPlanOpen(false)} current={cal?.settings.planStart} ws={ws} />
    </View>
  );
}

function PlanStartSheet({ calendarId, open, onClose, current, ws }: { calendarId: string; open: boolean; onClose: () => void; current?: DateStr; ws: 0 | 1 }) {
  const { upsert } = useCalendars();
  const [date, setDate] = useState(current ?? startOfWeek(today(), ws));
  const [error, setError] = useState<string | null>(null);
  const save = async (planStart: DateStr | null) => {
    try {
      upsert(await api.updateCalendar(calendarId, { settings: { planStart } }));
      onClose();
    } catch (e: any) {
      setError(e.message);
    }
  };
  return (
    <Sheet visible={open} onClose={onClose} title="Plan start">
      <View style={{ paddingHorizontal: 16, gap: 12 }}>
        <Muted>Week 1 is the week this date falls in. Shared with everyone in the calendar.</Muted>
        <View style={{ flexDirection: 'row' }}>
          <DateField value={date} onChange={setDate} />
        </View>
        <ErrorText error={error} />
        <Button label="Save" onPress={() => save(date)} />
        {current ? <Button label="Stop numbering weeks" variant="secondary" onPress={() => save(null)} /> : null}
      </View>
    </Sheet>
  );
}

function CopySheet({
  target,
  ws,
  onClose,
  onCopy,
}: {
  target: CopyTarget | null;
  ws: 0 | 1;
  onClose: () => void;
  onCopy: (from: DateStr, to: DateStr[], days: number, mode: 'merge' | 'replace') => void;
}) {
  const [mode, setMode] = useState<'merge' | 'replace'>('merge');
  const [custom, setCustom] = useState<DateStr | null>(null);
  useEffect(() => setCustom(null), [target]);
  if (!target) return null;
  const isWeek = target.kind === 'week';
  const days = isWeek ? 7 : 1;
  const step = isWeek ? 7 : 1;
  const next = (n: number) => Array.from({ length: n }, (_, i) => addDays(target.from, step * (i + 1)));
  const sameDayNextWeek = addDays(target.from, 7);
  return (
    <Sheet visible onClose={onClose} title={isWeek ? `Copy week of ${formatShort(target.from)}` : `Copy ${formatDay(target.from)}`}>
      <View style={{ paddingHorizontal: 16, marginBottom: 8 }}>
        <Segmented
          options={[
            { id: 'merge', label: 'Add to what’s there' },
            { id: 'replace', label: 'Replace' },
          ]}
          value={mode}
          onChange={setMode}
        />
      </View>
      {isWeek ? (
        <>
          <Row label="To next week" onPress={() => onCopy(target.from, next(1), days, mode)} />
          <Row label="To each of the next 2 weeks" onPress={() => onCopy(target.from, next(2), days, mode)} />
          <Row label="To each of the next 3 weeks" onPress={() => onCopy(target.from, next(3), days, mode)} />
          <Row label="To each of the next 4 weeks" onPress={() => onCopy(target.from, next(4), days, mode)} />
        </>
      ) : (
        <>
          <Row label="To the next day" onPress={() => onCopy(target.from, next(1), days, mode)} />
          <Row label={`To ${formatDay(sameDayNextWeek)}`} onPress={() => onCopy(target.from, [sameDayNextWeek], days, mode)} />
        </>
      )}
      <Row label={isWeek ? 'To another week…' : 'To another day…'} onPress={() => setCustom(addDays(target.from, step))} />
      {custom ? (
        <View style={{ paddingHorizontal: 16, gap: 10 }}>
          <View style={{ flexDirection: 'row' }}>
            <DateField value={custom} onChange={(d) => setCustom(isWeek ? startOfWeek(d, ws) : d)} />
          </View>
          <Button label="Copy" onPress={() => onCopy(target.from, [custom], days, mode)} />
        </View>
      ) : null}
    </Sheet>
  );
}

const makeStyles = (theme: Theme) =>
  StyleSheet.create({
    root: { flex: 1, backgroundColor: theme.bg },
    more: { padding: 14, alignItems: 'center' },
    moreText: { color: theme.primary, fontWeight: '600' },
    week: { marginHorizontal: 10, marginBottom: 12, borderRadius: 14, borderWidth: 1, borderColor: theme.border, backgroundColor: theme.surface, paddingBottom: 6 },
    weekHeader: { flexDirection: 'row', alignItems: 'center', gap: 16, paddingHorizontal: 14, paddingVertical: 10, borderBottomWidth: 1, borderBottomColor: theme.border },
    weekTitle: { flex: 1, color: theme.text, fontWeight: '700', fontSize: 15 },
    weekAction: { color: theme.primary, fontWeight: '600' },
    day: { flexDirection: 'row', paddingHorizontal: 10, paddingVertical: 5, gap: 10, minHeight: 40 },
    dayLabel: { width: 40, alignItems: 'center', paddingTop: 2 },
    dayName: { color: theme.textMuted, fontSize: 12, fontWeight: '600' },
    dayNum: { color: theme.text, fontSize: 16, fontWeight: '600' },
    card: { flexDirection: 'row', alignItems: 'center', gap: 8, backgroundColor: theme.bg, borderRadius: 8, borderLeftWidth: 4, paddingVertical: 8, paddingHorizontal: 10 },
    cardTitle: { color: theme.text, fontSize: 15, fontWeight: '600' },
    cardDetail: { color: theme.textMuted, fontSize: 13, marginTop: 1 },
  });
