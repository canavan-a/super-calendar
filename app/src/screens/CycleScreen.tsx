import { useCallback, useEffect, useLayoutEffect, useMemo, useState } from 'react';
import { Pressable, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';
import { CycleDay, CycleSummary, FLOWS, Flow, api, displayColor, layerOn } from '../api/calendars';
import { ErrorText, HeaderButton, Muted, Row, SectionTitle, Sheet, ToggleRow } from '../components/kit';
import { DayMarks, MonthGrid, withAlpha } from '../components/MonthGrid';
import { Button, Card, TextField } from '../components/ui';
import { useCalendar, useCalendars } from '../data/CalendarsContext';
import { DateStr, addDays, diffDays, formatDay, formatShort, startOfMonth, today } from '../dates';
import { SwitcherTitle } from '../navigation/Switcher';
import { ScreenProps } from '../navigation/types';
import { Theme, useTheme, useThemedStyles } from '../theme';

const STRENGTH: Record<Flow, number> = { none: 0, spotting: 0.15, light: 0.4, medium: 0.7, heavy: 1 };

// A cycle calendar: mostly blank days, with flow logged when a period comes.
// Once there's enough history the server predicts the next ones.
export default function CycleScreen({ navigation, route }: ScreenProps<'Cycle'>) {
  const styles = useThemedStyles(makeStyles);
  const theme = useTheme();
  const { id } = route.params;
  const cal = useCalendar(id);
  const { version, bump, settings, setSettings } = useCalendars();
  const [month, setMonth] = useState(startOfMonth(today()));
  const [days, setDays] = useState<CycleDay[]>([]);
  const [summary, setSummary] = useState<CycleSummary | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [editing, setEditing] = useState<DateStr | null>(null);
  const [menu, setMenu] = useState(false);
  const color = cal ? displayColor(cal) : '#d6336c';
  const showFertile = layerOn(settings, 'cycle', 'fertile');

  useLayoutEffect(() => {
    navigation.setOptions({
      headerTitle: () => <SwitcherTitle title={cal?.name ?? ''} color={color} />,
      headerRight: () => <HeaderButton label="⋯" onPress={() => setMenu(true)} />,
    });
  }, [navigation, cal?.name, color]);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [d, s] = await Promise.all([api.cycleDays(id), api.cycleSummary(id)]);
      setDays(d);
      setSummary(s);
      setError(null);
    } catch (e: any) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [id]);
  useEffect(() => {
    load();
  }, [load, version]);

  const byDate = useMemo(() => Object.fromEntries(days.map((d) => [d.date, d])), [days]);

  const marks = useMemo(() => {
    const m: Record<DateStr, DayMarks> = {};
    if (summary) {
      for (const p of summary.predictions) {
        if (showFertile) {
          for (let d = p.fertileStart; d <= p.fertileEnd; d = addDays(d, 1)) {
            if (d >= summary.today) (m[d] ??= {}).outline = { color: withAlpha(color, 0.5) };
          }
        }
        for (let d = p.start; d <= p.end; d = addDays(d, 1)) {
          // Ghosted: a faint fill and a dashed edge (a logged day below wins).
          if (d >= summary.today) m[d] = { ...m[d], outline: { color, dashed: true }, tint: { color, strength: 0 } };
        }
      }
    }
    for (const d of days) {
      if (d.flow !== 'none') (m[d.date] ??= {}).tint = { color, strength: STRENGTH[d.flow] };
      if (d.notes) (m[d.date] ??= {}).label = '•';
    }
    return m;
  }, [days, summary, color, showFertile]);

  const log = async (date: DateStr, flow: Flow, notes: string) => {
    setEditing(null);
    // Show it straight away; the reload confirms.
    setDays((cur) => [...cur.filter((d) => d.date !== date), ...(flow === 'none' && !notes ? [] : [{ date, flow, notes, updatedBy: '', updatedAt: '' }])]);
    try {
      await api.putCycleDay(id, date, flow, notes);
      bump();
    } catch (e: any) {
      setError(e.message);
      load();
    }
  };

  const now = today();
  const ongoing = summary?.periods.length ? diffDays(summary.periods[summary.periods.length - 1].end, now) <= 1 : false;

  return (
    <View style={styles.root}>
      <ScrollView refreshControl={<RefreshControl refreshing={loading} onRefresh={load} />} contentContainerStyle={{ paddingBottom: 60 }}>
        <ErrorText error={error} />
        {summary ? <SummaryCard summary={summary} color={color} /> : null}
        <View style={{ flexDirection: 'row', gap: 10, paddingHorizontal: 16, marginTop: 12 }}>
          {!byDate[now] || byDate[now].flow === 'none' ? (
            <Button label={ongoing ? 'Log today' : 'Period started today'} onPress={() => log(now, 'medium', byDate[now]?.notes ?? '')} style={{ flex: 1 }} />
          ) : (
            <Button label={`Today: ${byDate[now].flow}`} variant="secondary" onPress={() => setEditing(now)} style={{ flex: 1 }} />
          )}
        </View>
        <MonthGrid month={month} selected={editing} marks={marks} onSelect={setEditing} onMonthChange={(m) => setMonth(startOfMonth(m))} />
        <View style={styles.legend}>
          <View style={[styles.swatch, { backgroundColor: withAlpha(color, 0.7) }]} />
          <Muted style={{ fontSize: 12 }}>Logged</Muted>
          <View style={[styles.swatch, { borderColor: color, borderWidth: 2, borderStyle: 'dashed', backgroundColor: withAlpha(color, 0.15) }]} />
          <Muted style={{ fontSize: 12 }}>Predicted</Muted>
          {showFertile ? (
            <>
              <View style={[styles.swatch, { borderColor: withAlpha(color, 0.5), borderWidth: 2 }]} />
              <Muted style={{ fontSize: 12 }}>Fertile (estimate)</Muted>
            </>
          ) : null}
        </View>

        {summary?.cycles.length ? (
          <>
            <SectionTitle>History</SectionTitle>
            {summary.cycles
              .slice()
              .reverse()
              .slice(0, 12)
              .map((c) => (
                <Row
                  key={c.start}
                  label={`${formatShort(c.start)} · ${c.length} days`}
                  detail={c.excluded ? 'Left out of predictions (probably a missed log)' : undefined}
                />
              ))}
          </>
        ) : null}
      </ScrollView>

      <DaySheet date={editing} day={editing ? byDate[editing] : undefined} onClose={() => setEditing(null)} onSave={log} />
      <Sheet visible={menu} onClose={() => setMenu(false)}>
        <ToggleRow
          label="Show fertile window"
          detail="An estimate from the calendar method: not contraception"
          value={showFertile}
          onChange={(v) =>
            setSettings({ ...settings, overview: { ...settings.overview, layers: { ...settings.overview?.layers, 'cycle/fertile': v } } })
          }
        />
        <Row
          label="Calendar settings"
          detail="Name, colour, members and invites"
          onPress={() => {
            setMenu(false);
            navigation.navigate('CalendarSettings', { id });
          }}
        />
        <Muted style={{ padding: 16, fontSize: 12 }}>
          Predictions use the calendar method: a weighted average of your recent cycle lengths, with a range from how much
          they vary. Cycles under 15 or over 60 days are left out. It's a guide, not medical advice.
        </Muted>
      </Sheet>
    </View>
  );
}

function SummaryCard({ summary, color }: { summary: CycleSummary; color: string }) {
  const theme = useTheme();
  const next = summary.predictions[0];
  return (
    <Card style={{ margin: 16, marginBottom: 0, borderLeftWidth: 4, borderLeftColor: color }}>
      {!summary.enough ? (
        <>
          <Text style={{ color: theme.text, fontSize: 17, fontWeight: '700' }}>No prediction yet</Text>
          <Muted style={{ marginTop: 4 }}>
            Log {summary.needPeriods} more period{summary.needPeriods === 1 ? '' : 's'} to see when the next one might come.
          </Muted>
        </>
      ) : summary.lateDays > 0 ? (
        <>
          <Text style={{ color: theme.text, fontSize: 17, fontWeight: '700' }}>{summary.lateDays} days late</Text>
          <Muted style={{ marginTop: 4 }}>It was expected around {formatDay(next.start)}.</Muted>
        </>
      ) : (
        <>
          <Muted>Next period</Muted>
          <Text style={{ color: theme.text, fontSize: 20, fontWeight: '700', marginTop: 2 }}>
            {formatDay(next.start)} <Text style={{ color: theme.textMuted, fontSize: 15 }}>± {next.rangeDays} day{next.rangeDays === 1 ? '' : 's'}</Text>
          </Text>
          <Muted style={{ marginTop: 2 }}>
            in {Math.max(0, diffDays(summary.today, next.start))} days
          </Muted>
        </>
      )}
      {summary.enough ? (
        <Muted style={{ marginTop: 10, fontSize: 13 }}>
          Cycles average {summary.meanCycle} days (±{summary.sdCycle}) · periods about {summary.periodLength} days
          {summary.irregular ? '\nYour cycles vary a lot, so this is a rough guess.' : ''}
        </Muted>
      ) : null}
    </Card>
  );
}

function DaySheet({
  date,
  day,
  onClose,
  onSave,
}: {
  date: DateStr | null;
  day?: CycleDay;
  onClose: () => void;
  onSave: (date: DateStr, flow: Flow, notes: string) => void;
}) {
  const theme = useTheme();
  const [flow, setFlow] = useState<Flow>('none');
  const [notes, setNotes] = useState('');
  useEffect(() => {
    setFlow(day?.flow ?? 'none');
    setNotes(day?.notes ?? '');
  }, [date, day]);
  if (!date) return null;
  return (
    <Sheet visible onClose={onClose} title={formatDay(date, true)}>
      <View style={{ flexDirection: 'row', gap: 6, paddingHorizontal: 12, marginBottom: 12 }}>
        {FLOWS.map((f) => {
          const on = f.id === flow;
          return (
            <Pressable
              key={f.id}
              onPress={() => setFlow(f.id)}
              style={{
                flex: 1,
                alignItems: 'center',
                paddingVertical: 12,
                borderRadius: 10,
                backgroundColor: on ? theme.primary : theme.surfaceAlt,
              }}
            >
              <View
                style={{
                  width: 8 + STRENGTH[f.id] * 14,
                  height: 8 + STRENGTH[f.id] * 14,
                  borderRadius: 12,
                  backgroundColor: f.id === 'none' ? 'transparent' : '#d6336c',
                  borderWidth: f.id === 'none' ? 1 : 0,
                  borderColor: theme.textMuted,
                  marginBottom: 6,
                }}
              />
              <Text style={{ color: on ? theme.primaryText : theme.text, fontSize: 12, fontWeight: '600' }}>{f.label}</Text>
            </Pressable>
          );
        })}
      </View>
      <View style={{ paddingHorizontal: 12, gap: 10 }}>
        <TextField value={notes} onChangeText={setNotes} placeholder="Notes (optional)" multiline style={{ minHeight: 70, textAlignVertical: 'top' }} />
        {day?.updatedBy ? <Muted style={{ fontSize: 12 }}>Logged by {day.updatedBy}</Muted> : null}
        <Button label="Save" onPress={() => onSave(date, flow, notes)} />
        {day ? <Button label="Clear this day" variant="secondary" onPress={() => onSave(date, 'none', '')} /> : null}
      </View>
    </Sheet>
  );
}

const makeStyles = (theme: Theme) =>
  StyleSheet.create({
    root: { flex: 1, backgroundColor: theme.bg },
    legend: { flexDirection: 'row', alignItems: 'center', gap: 6, paddingHorizontal: 16, paddingVertical: 8, flexWrap: 'wrap' },
    swatch: { width: 14, height: 14, borderRadius: 4, marginLeft: 6 },
  });
