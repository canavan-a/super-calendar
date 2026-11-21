import { useCallback, useEffect, useState } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';
import { ActivityField, ActivityType, PALETTE, api } from '../api/calendars';
import { ColorPicker, Dot, ErrorText, Muted, Row, Sheet, ToggleRow } from '../components/kit';
import { Button, TextField } from '../components/ui';
import { ScreenProps } from '../navigation/types';
import { Theme, useThemedStyles } from '../theme';

const FIELDS: { id: ActivityField; label: string }[] = [
  { id: 'duration', label: 'Duration (minutes)' },
  { id: 'distance', label: 'Distance (km)' },
  { id: 'intensity', label: 'Intensity (easy → max)' },
  { id: 'exercises', label: 'Exercises (sets × reps × kg)' },
];

// The calendar's activity library: what sessions can be, and which plan
// fields each asks for. Shared by everyone in the calendar.
export default function ActivityTypesScreen({ route }: ScreenProps<'ActivityTypes'>) {
  const styles = useThemedStyles(makeStyles);
  const { calendarId } = route.params;
  const [types, setTypes] = useState<ActivityType[]>([]);
  const [editing, setEditing] = useState<Partial<ActivityType> | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    api.activityTypes(calendarId).then(setTypes).catch((e) => setError(e.message));
  }, [calendarId]);
  useEffect(load, [load]);

  const save = async () => {
    if (!editing) return;
    setError(null);
    try {
      const body = { name: editing.name, color: editing.color, fields: editing.fields, archived: editing.archived };
      if (editing.id) await api.updateActivityType(calendarId, editing.id, body);
      else await api.createActivityType(calendarId, body);
      setEditing(null);
      load();
    } catch (e: any) {
      setError(e.message);
    }
  };

  const active = types.filter((t) => !t.archived);
  const archived = types.filter((t) => t.archived);

  return (
    <View style={styles.root}>
      <ScrollView contentContainerStyle={{ padding: 8, paddingBottom: 60 }}>
        {!editing ? <ErrorText error={error} /> : null}
        {active.map((t) => (
          <Row
            key={t.id}
            label={t.name}
            detail={t.fields.length ? FIELDS.filter((f) => t.fields.includes(f.id)).map((f) => f.label.split(' (')[0]).join(', ') : 'Nothing to plan'}
            left={<Dot color={t.color} />}
            onPress={() => setEditing(t)}
          />
        ))}
        <Row label="+ New activity" onPress={() => setEditing({ name: '', color: PALETTE[0], fields: ['duration'], archived: false })} />
        {archived.length ? <Muted style={{ padding: 16 }}>Archived (kept on old sessions, hidden when planning)</Muted> : null}
        {archived.map((t) => (
          <Row key={t.id} label={t.name} left={<Dot color={t.color} />} onPress={() => setEditing(t)} />
        ))}
      </ScrollView>
      <Sheet visible={!!editing} onClose={() => setEditing(null)} title={editing?.id ? 'Edit activity' : 'New activity'}>
        {editing ? (
          <View style={{ paddingHorizontal: 8 }}>
            <ErrorText error={error} />
            <TextField value={editing.name} onChangeText={(name) => setEditing({ ...editing, name })} placeholder="Name" style={{ marginHorizontal: 8 }} />
            <View style={{ paddingHorizontal: 8 }}>
              <ColorPicker value={editing.color ?? ''} onChange={(color) => setEditing({ ...editing, color })} />
            </View>
            {FIELDS.map((f) => (
              <ToggleRow
                key={f.id}
                label={f.label}
                value={!!editing.fields?.includes(f.id)}
                onChange={(on) =>
                  setEditing({
                    ...editing,
                    fields: on ? [...(editing.fields ?? []), f.id] : (editing.fields ?? []).filter((x) => x !== f.id),
                  })
                }
              />
            ))}
            {editing.id ? (
              <ToggleRow label="Archived" value={!!editing.archived} onChange={(archived) => setEditing({ ...editing, archived })} />
            ) : null}
            <Button label="Save" onPress={save} disabled={!editing.name?.trim()} style={{ margin: 8 }} />
          </View>
        ) : null}
      </Sheet>
    </View>
  );
}

const makeStyles = (theme: Theme) => StyleSheet.create({ root: { flex: 1, backgroundColor: theme.bg } });
