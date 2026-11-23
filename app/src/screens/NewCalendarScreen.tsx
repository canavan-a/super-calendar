import { useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';
import { ImportPreview, Kind, PALETTE, api } from '../api/calendars';
import { ColorPicker, ErrorText, Muted, Row, SectionTitle } from '../components/kit';
import { Button, Card, TextField } from '../components/ui';
import { useCalendars } from '../data/CalendarsContext';
import { formatDay, localDate, localTime } from '../dates';
import { ScreenProps, routeForKind } from '../navigation/types';
import { Theme, useTheme, useThemedStyles } from '../theme';

const KINDS: { id: Kind; label: string; detail: string; icon: string }[] = [
  { id: 'general', label: 'General', detail: 'Events, all-day and repeating, and .ics imports', icon: '▦' },
  { id: 'subscribed', label: 'Subscribed', detail: 'Follows a Google, Outlook or iCal link and keeps up to date; read-only', icon: '⟳' },
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
  const [link, setLink] = useState(route.params?.url ?? '');
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [checking, setChecking] = useState(false);
  const [help, setHelp] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const subscribed = kind === 'subscribed';

  // A subscription is checked before it's made: the link is fetched and its
  // events previewed, and its own name fills in the name if that's empty.
  const check = async () => {
    setChecking(true);
    setError(null);
    setPreview(null);
    try {
      const p = await api.previewSubscription(link);
      setPreview(p);
      if (!name.trim() && p.name) setName(p.name.slice(0, 80));
    } catch (e: any) {
      setError(e.message);
    } finally {
      setChecking(false);
    }
  };

  const create = async () => {
    setSaving(true);
    setError(null);
    try {
      const c = subscribed ? await api.createSubscribed(name, color, link) : await api.createCalendar(kind, name, color);
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

      {subscribed ? (
        <>
          <SectionTitle>Link</SectionTitle>
          <View style={{ flexDirection: 'row', gap: 8, paddingHorizontal: 16 }}>
            <TextField
              value={link}
              onChangeText={(t) => {
                setLink(t);
                setPreview(null);
              }}
              placeholder="https://… or webcal://…"
              autoCapitalize="none"
              autoCorrect={false}
              keyboardType="url"
              style={{ flex: 1 }}
            />
            <Button label="Check" onPress={check} loading={checking} disabled={!link.trim()} />
          </View>
          <Muted style={{ paddingHorizontal: 16, marginTop: 8 }}>
            <Text style={{ color: theme.primary }} onPress={() => setHelp((h) => !h)}>
              {help ? 'Hide' : "Where's my link?"}
            </Text>
          </Muted>
          {help ? (
            <Card style={{ marginHorizontal: 16, marginTop: 8, gap: 8 }}>
              <Text style={styles.helpText}>
                <Text style={styles.bold}>Google Calendar</Text> (on a computer): Settings → pick the calendar under "Settings
                for my calendars" → Integrate calendar → copy "Secret address in iCal format".
              </Text>
              <Text style={styles.helpText}>
                <Text style={styles.bold}>Outlook.com</Text>: Settings → Calendar → Shared calendars → Publish a calendar →
                pick the calendar and "Can view all details" → Publish → copy the ICS link.
              </Text>
              <Muted style={{ fontSize: 13 }}>
                Treat the link like a password: anyone with it can read the calendar. Only you will see it here. If you
                reset it there, paste the new one in this calendar's settings.
              </Muted>
            </Card>
          ) : null}
          {preview ? (
            <Card style={{ marginHorizontal: 16, marginTop: 12 }}>
              <Text style={{ color: theme.text, fontSize: 16 }}>
                {preview.total} events{preview.name ? ` in "${preview.name}"` : ''}
              </Text>
              {preview.sample.slice(0, 3).map((e) => (
                <Muted key={e.uid + e.recurrenceId} style={{ marginTop: 4 }}>
                  {e.title} · {e.allDay ? formatDay(e.start, true) : `${formatDay(localDate(e.start), true)} ${localTime(e.start)}`}
                  {e.rrule ? ' · repeats' : ''}
                </Muted>
              ))}
              {preview.errors.length ? (
                <Muted style={{ marginTop: 6, fontSize: 13 }}>
                  {preview.errors.length} couldn't be read and will be left out.
                </Muted>
              ) : null}
            </Card>
          ) : null}
        </>
      ) : null}

      <SectionTitle>Name</SectionTitle>
      <TextField
        value={name}
        onChangeText={setName}
        placeholder={kind === 'training' ? 'Marathon block' : kind === 'cycle' ? 'My cycle' : subscribed ? 'Work' : 'Family'}
        style={{ marginHorizontal: 16 }}
        maxLength={80}
      />
      <SectionTitle>Colour</SectionTitle>
      <View style={{ paddingHorizontal: 16 }}>
        <ColorPicker value={color} onChange={setColor} />
      </View>
      <Muted style={{ paddingHorizontal: 16, marginTop: 8 }}>
        {subscribed
          ? 'It updates from the link every 15 minutes. You can share it like any calendar; the others never see the link.'
          : "You can invite people by email from the calendar's settings once it's made."}
      </Muted>
      <ErrorText error={error} />
      <Button
        label={subscribed ? 'Subscribe' : 'Create'}
        onPress={create}
        loading={saving}
        disabled={!name.trim() || (subscribed && !preview)}
        style={{ margin: 16 }}
      />
    </ScrollView>
  );
}

const makeStyles = (theme: Theme) =>
  StyleSheet.create({
    root: { flex: 1, backgroundColor: theme.bg },
    helpText: { color: theme.text, fontSize: 14 },
    bold: { fontWeight: '700' },
  });
