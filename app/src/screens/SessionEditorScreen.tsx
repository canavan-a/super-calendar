import { useEffect, useLayoutEffect, useState } from 'react';
import { ActivityIndicator, Alert, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { ActivityField, ActivityType, Exercise, Plan, Session, SessionStatus, api } from '../api/calendars';
import { DateField } from '../components/DateField';
import { ErrorText, HeaderButton, Muted, Row, SectionTitle, Segmented } from '../components/kit';
import { TextField } from '../components/ui';
import { useCalendars } from '../data/CalendarsContext';
import { DateStr, today, validTime } from '../dates';
import { ScreenProps } from '../navigation/types';
import { Theme, useTheme, useThemedStyles } from '../theme';

const INTENSITIES = [
  { id: 'easy', label: 'Easy' },
  { id: 'moderate', label: 'Moderate' },
  { id: 'hard', label: 'Hard' },
  { id: 'max', label: 'Max' },
] as const;

const num = (s: string) => {
  const n = parseFloat(s.replace(',', '.'));
  return Number.isFinite(n) ? n : undefined;
};

// Only the keys a type uses, without empties, so the plan stays tidy.
function clean(p: Plan, fields: ActivityField[]): Plan {
  const out: Plan = {};
  if (fields.includes('duration') && p.durationMin) out.durationMin = p.durationMin;
  if (fields.includes('distance') && p.distanceKm) out.distanceKm = p.distanceKm;
  if (fields.includes('intensity') && p.intensity) out.intensity = p.intensity;
  if (fields.includes('exercises') && p.exercises?.length) out.exercises = p.exercises.filter((e) => e.name.trim());
  return out;
}

export default function SessionEditorScreen({ navigation, route }: ScreenProps<'SessionEditor'>) {
  const styles = useThemedStyles(makeStyles);
  const theme = useTheme();
  const { calendarId, sessionId } = route.params;
  const { bump } = useCalendars();
  const [types, setTypes] = useState<ActivityType[]>([]);
  const [session, setSession] = useState<Session | null>(null);
  const [loaded, setLoaded] = useState(!sessionId);
  const [date, setDate] = useState<DateStr>(route.params.date ?? today());
  const [time, setTime] = useState('');
  const [typeId, setTypeId] = useState<string | null>(null);
  const [title, setTitle] = useState('');
  const [plan, setPlan] = useState<Plan>({});
  const [result, setResult] = useState<Plan>({});
  const [status, setStatus] = useState<SessionStatus>('planned');
  const [notes, setNotes] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .activityTypes(calendarId)
      .then((t) => {
        setTypes(t);
        if (!sessionId) setTypeId((cur) => cur ?? t.find((x) => !x.archived)?.id ?? null);
      })
      .catch((e) => setError(e.message));
  }, [calendarId, sessionId]);

  useEffect(() => {
    if (!sessionId) return;
    (async () => {
      try {
        const found = await api.getSession(calendarId, sessionId);
        setSession(found);
        setDate(found.date);
        setTime(found.time);
        setTypeId(found.activityTypeId);
        setTitle(found.title);
        setPlan(found.plan ?? {});
        setResult(found.result ?? {});
        setStatus(found.status);
        setNotes(found.notes);
        setLoaded(true);
      } catch (e: any) {
        setError(e.message);
      }
    })();
  }, [calendarId, sessionId]);

  const type = types.find((t) => t.id === typeId);
  const fields = type?.fields ?? ['duration', 'distance', 'intensity'];

  const save = async () => {
    if (time && !validTime(time)) return setError('Time looks like 07:30');
    setSaving(true);
    setError(null);
    const input = {
      date,
      time,
      activityTypeId: typeId ?? '',
      title: title.trim(),
      plan: clean(plan, fields),
      result: status === 'done' ? clean(result, fields) : {},
      status,
      notes,
    };
    try {
      if (sessionId) await api.updateSession(calendarId, sessionId, input);
      else await api.createSession(calendarId, input);
      bump();
      navigation.goBack();
    } catch (e: any) {
      setError(e.message);
      setSaving(false);
    }
  };

  const remove = () =>
    Alert.alert('Delete this session?', undefined, [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Delete',
        style: 'destructive',
        onPress: async () => {
          try {
            await api.deleteSession(calendarId, sessionId!);
            bump();
            navigation.goBack();
          } catch (e: any) {
            setError(e.message);
          }
        },
      },
    ]);

  useLayoutEffect(() => {
    navigation.setOptions({
      title: sessionId ? 'Session' : 'New session',
      headerRight: () => (saving ? <ActivityIndicator color={theme.primary} /> : <HeaderButton label="Save" onPress={save} />),
    });
  });

  if (!loaded) {
    return (
      <View style={[styles.root, { justifyContent: 'center' }]}>
        {error ? <ErrorText error={error} /> : <ActivityIndicator color={theme.primary} />}
      </View>
    );
  }

  return (
    <ScrollView style={styles.root} contentContainerStyle={{ padding: 16, paddingBottom: 60, gap: 10 }} keyboardShouldPersistTaps="handled">
      <ErrorText error={error} />
      <Text style={styles.label}>Activity</Text>
      <View style={styles.chips}>
        {types
          .filter((t) => !t.archived || t.id === typeId)
          .map((t) => (
            <Pressable
              key={t.id}
              onPress={() => setTypeId(t.id)}
              style={[styles.chip, { borderColor: t.color }, t.id === typeId && { backgroundColor: t.color }]}
            >
              <Text style={{ color: t.id === typeId ? '#fff' : theme.text, fontWeight: '600' }}>{t.name}</Text>
            </Pressable>
          ))}
        <Pressable onPress={() => navigation.navigate('ActivityTypes', { calendarId })} style={[styles.chip, { borderColor: theme.border }]}>
          <Text style={{ color: theme.primary, fontWeight: '600' }}>Edit…</Text>
        </Pressable>
      </View>
      <TextField value={title} onChangeText={setTitle} placeholder={type ? `Title (default: ${type.name})` : 'Title'} />
      <View style={{ flexDirection: 'row', gap: 8 }}>
        <DateField value={date} onChange={setDate} />
        <TextField value={time} onChangeText={setTime} placeholder="Time" style={{ width: 96, textAlign: 'center' }} keyboardType="numbers-and-punctuation" maxLength={5} />
      </View>

      <SectionTitle>Plan</SectionTitle>
      <PlanFields fields={fields} value={plan} onChange={setPlan} />

      <SectionTitle>Status</SectionTitle>
      <Segmented
        options={[
          { id: 'planned', label: 'Planned' },
          { id: 'done', label: 'Done' },
          { id: 'skipped', label: 'Skipped' },
        ]}
        value={status}
        onChange={(s) => {
          if (s === 'done' && !Object.keys(result).length) setResult(plan);
          setStatus(s);
        }}
      />
      {status === 'done' ? (
        <>
          <SectionTitle>What you actually did</SectionTitle>
          <PlanFields fields={fields} value={result} onChange={setResult} />
        </>
      ) : null}

      <TextField value={notes} onChangeText={setNotes} placeholder="Notes" multiline style={{ minHeight: 80, textAlignVertical: 'top', marginTop: 10 }} />
      {session?.updatedBy ? <Muted style={{ fontSize: 13 }}>Last edited by {session.updatedBy}.</Muted> : null}
      {sessionId ? <Row label="Delete session" danger onPress={remove} /> : null}
    </ScrollView>
  );
}

function PlanFields({ fields, value, onChange }: { fields: ActivityField[]; value: Plan; onChange: (p: Plan) => void }) {
  const styles = useThemedStyles(makeStyles);
  const theme = useTheme();
  if (!fields.length) return <Muted>This activity has nothing to plan.</Muted>;
  const setEx = (i: number, patch: Partial<Exercise>) => {
    const ex = (value.exercises ?? []).slice();
    ex[i] = { ...ex[i], ...patch };
    onChange({ ...value, exercises: ex });
  };
  return (
    <View style={{ gap: 10 }}>
      <View style={{ flexDirection: 'row', gap: 8 }}>
        {fields.includes('duration') ? (
          <NumberField label="Minutes" value={value.durationMin} onChange={(n) => onChange({ ...value, durationMin: n })} />
        ) : null}
        {fields.includes('distance') ? (
          <NumberField label="Kilometres" value={value.distanceKm} onChange={(n) => onChange({ ...value, distanceKm: n })} />
        ) : null}
      </View>
      {fields.includes('intensity') ? (
        <Segmented
          options={INTENSITIES.map((i) => ({ id: i.id, label: i.label }))}
          value={value.intensity ?? ('' as any)}
          onChange={(v) => onChange({ ...value, intensity: v === value.intensity ? undefined : (v as Plan['intensity']) })}
        />
      ) : null}
      {fields.includes('exercises') ? (
        <View style={{ gap: 8 }}>
          {(value.exercises ?? []).map((e, i) => (
            <View key={i} style={styles.exercise}>
              <TextField value={e.name} onChangeText={(t) => setEx(i, { name: t })} placeholder="Exercise" style={{ flex: 1, paddingVertical: 10 }} />
              <SmallNum value={e.sets} placeholder="sets" onChange={(n) => setEx(i, { sets: n })} />
              <SmallNum value={e.reps} placeholder="reps" onChange={(n) => setEx(i, { reps: n })} />
              <SmallNum value={e.weight} placeholder="kg" onChange={(n) => setEx(i, { weight: n })} />
              <Pressable hitSlop={8} onPress={() => onChange({ ...value, exercises: (value.exercises ?? []).filter((_, j) => j !== i) })}>
                <Text style={{ color: theme.danger, fontSize: 18 }}>×</Text>
              </Pressable>
            </View>
          ))}
          <Pressable onPress={() => onChange({ ...value, exercises: [...(value.exercises ?? []), { name: '' }] })}>
            <Text style={{ color: theme.primary, fontWeight: '600', paddingVertical: 6 }}>+ Add exercise</Text>
          </Pressable>
        </View>
      ) : null}
    </View>
  );
}

function NumberField({ label, value, onChange }: { label: string; value?: number; onChange: (n?: number) => void }) {
  const [text, setText] = useState(value != null ? String(value) : '');
  useEffect(() => {
    if (num(text) !== value) setText(value != null ? String(value) : '');
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value]);
  return (
    <TextField
      value={text}
      onChangeText={(t) => {
        setText(t);
        onChange(num(t));
      }}
      placeholder={label}
      keyboardType="decimal-pad"
      style={{ flex: 1 }}
    />
  );
}

function SmallNum({ value, placeholder, onChange }: { value?: number; placeholder: string; onChange: (n?: number) => void }) {
  return (
    <TextField
      defaultValue={value != null ? String(value) : ''}
      onChangeText={(t) => onChange(num(t))}
      placeholder={placeholder}
      keyboardType="decimal-pad"
      style={{ width: 58, paddingHorizontal: 6, paddingVertical: 10, textAlign: 'center' }}
    />
  );
}

const makeStyles = (theme: Theme) =>
  StyleSheet.create({
    root: { flex: 1, backgroundColor: theme.bg },
    label: { color: theme.textMuted, fontSize: 13, fontWeight: '600' },
    chips: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
    chip: { borderWidth: 2, borderRadius: 18, paddingHorizontal: 14, paddingVertical: 7 },
    exercise: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  });
