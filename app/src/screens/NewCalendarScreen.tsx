import { useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';
import { Kind, PALETTE, api } from '../api/calendars';
import { ColorPicker, ErrorText, Muted, Row, SectionTitle } from '../components/kit';
import { Button, TextField } from '../components/ui';
import { useCalendars } from '../data/CalendarsContext';
import { ScreenProps, routeForKind } from '../navigation/types';
import { Theme, useTheme, useThemedStyles } from '../theme';

const KINDS: { id: Kind; label: string; detail: string; icon: string }[] = [
  { id: 'general', label: 'General', detail: 'Events, all-day and repeating, and .ics imports', icon: '▦' },
  { id: 'training', label: 'Training', detail: 'Sessions with activities and plans; copy weeks and tweak', icon: '⚑' },
  { id: 'cycle', label: 'Cycle', detail: 'Log period days and flow; see the next one predicted', icon: '◐' },
];

export default function NewCalendarScreen({ navigation, route }: ScreenProps<'NewCalendar'>) {
  const styles = useThemedStyles(makeStyles);
  const theme = useTheme();
  const { upsert } = useCalendars();
  const [kind, setKind] = useState<Kind>(route.params?.kind ?? 'general');
  const [name, setName] = useState('');
  const [color, setColor] = useState(PALETTE[0]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const create = async () => {
    setSaving(true);
    setError(null);
    try {
      const c = await api.createCalendar(kind, name, color);
      upsert(c);
      navigation.reset({ index: 1, routes: [{ name: 'Overview' }, { name: routeForKind(c.kind), params: { id: c.id } }] });
    } catch (e: any) {
      setError(e.message);
      setSaving(false);
    }
  };

  return (
    <ScrollView style={styles.root} contentContainerStyle={{ padding: 8, paddingBottom: 40 }} keyboardShouldPersistTaps="handled">
      <SectionTitle>Type</SectionTitle>
      {KINDS.map((k) => (
        <Row
          key={k.id}
          label={k.label}
          detail={k.detail}
          left={<Text style={{ fontSize: 20, width: 24, color: theme.text }}>{k.icon}</Text>}
          right={<Text style={{ color: theme.primary, fontSize: 18 }}>{kind === k.id ? '●' : '○'}</Text>}
          onPress={() => setKind(k.id)}
        />
      ))}
      <SectionTitle>Name</SectionTitle>
      <TextField
        value={name}
        onChangeText={setName}
        placeholder={kind === 'training' ? 'Marathon block' : kind === 'cycle' ? 'My cycle' : 'Family'}
        style={{ marginHorizontal: 16 }}
        maxLength={80}
      />
      <SectionTitle>Colour</SectionTitle>
      <View style={{ paddingHorizontal: 16 }}>
        <ColorPicker value={color} onChange={setColor} />
      </View>
      <Muted style={{ paddingHorizontal: 16, marginTop: 8 }}>
        You can invite people by email from the calendar's settings once it's made.
      </Muted>
      <ErrorText error={error} />
      <Button label="Create" onPress={create} loading={saving} disabled={!name.trim()} style={{ margin: 16 }} />
    </ScrollView>
  );
}

const makeStyles = (theme: Theme) => StyleSheet.create({ root: { flex: 1, backgroundColor: theme.bg } });
