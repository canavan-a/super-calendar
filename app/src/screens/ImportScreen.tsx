import { useEffect, useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';
import { ImportPreview, ImportResult, ImportSource, api, displayColor } from '../api/calendars';
import { Dot, ErrorText, Muted, Row, SectionTitle } from '../components/kit';
import { Button, Card, TextField } from '../components/ui';
import { useCalendars } from '../data/CalendarsContext';
import { formatDay, localDate, localTime } from '../dates';
import { pickIcsFile } from '../icsIntents';
import { ScreenProps } from '../navigation/types';
import { Theme, useTheme, useThemedStyles } from '../theme';

// Every way in (a tapped link, the share sheet, the file picker, a pasted
// link) ends here: pick a general calendar, preview, import.
export default function ImportScreen({ navigation, route }: ScreenProps<'Import'>) {
  const styles = useThemedStyles(makeStyles);
  const theme = useTheme();
  const { calendars, bump } = useCalendars();
  const general = calendars.filter((c) => c.kind === 'general');
  const [source, setSource] = useState<ImportSource | undefined>(route.params.source);
  const [link, setLink] = useState('');
  const [calendarId, setCalendarId] = useState<string | undefined>(route.params.calendarId ?? (general.length === 1 ? general[0].id : undefined));
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [result, setResult] = useState<ImportResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setSource(route.params.source);
    setResult(null);
  }, [route.params.source]);

  useEffect(() => {
    if (!source || !calendarId) {
      setPreview(null);
      return;
    }
    setBusy(true);
    setError(null);
    api
      .previewImport(calendarId, source)
      .then(setPreview)
      .catch((e) => {
        setPreview(null);
        setError(e.message);
      })
      .finally(() => setBusy(false));
  }, [source, calendarId]);

  const doImport = async () => {
    if (!source || !calendarId) return;
    setBusy(true);
    setError(null);
    try {
      setResult(await api.importIcs(calendarId, source));
      bump();
    } catch (e: any) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  };

  if (result) {
    return (
      <View style={[styles.root, { padding: 24, justifyContent: 'center' }]}>
        <Text style={styles.big}>Imported</Text>
        <Muted style={{ marginBottom: 20 }}>
          {result.imported} new, {result.updated} updated
          {result.skipped ? `, ${result.skipped} skipped` : ''}.
        </Muted>
        <Button
          label="Open calendar"
          onPress={() => calendarId && navigation.reset({ index: 1, routes: [{ name: 'Overview' }, { name: 'General', params: { id: calendarId } }] })}
        />
      </View>
    );
  }

  return (
    <ScrollView style={styles.root} contentContainerStyle={{ padding: 8, paddingBottom: 60 }} keyboardShouldPersistTaps="handled">
      {!source ? (
        <>
          <SectionTitle>What to import</SectionTitle>
          <View style={{ paddingHorizontal: 16, gap: 10 }}>
            <Button
              label="Pick an .ics file"
              variant="secondary"
              onPress={async () => {
                try {
                  const p = await pickIcsFile();
                  if (p?.text) setSource({ ics: p.text });
                } catch (e: any) {
                  setError(e.message);
                }
              }}
            />
            <Muted>or paste a link to a calendar (https:// or webcal://)</Muted>
            <View style={{ flexDirection: 'row', gap: 8 }}>
              <TextField value={link} onChangeText={setLink} placeholder="webcal://…" autoCapitalize="none" keyboardType="url" style={{ flex: 1 }} />
              <Button label="Go" onPress={() => setSource({ url: link.trim() })} disabled={!link.trim()} />
            </View>
          </View>
        </>
      ) : (
        <Muted style={{ paddingHorizontal: 16, marginTop: 8 }}>
          {source.url ? `From ${source.url}` : 'From a calendar file'} ·{' '}
          <Text style={{ color: theme.primary }} onPress={() => setSource(undefined)}>
            change
          </Text>
        </Muted>
      )}

      <SectionTitle>Into</SectionTitle>
      {general.length === 0 ? (
        <Row label="+ Make a general calendar first" onPress={() => navigation.navigate('NewCalendar', { kind: 'general' })} />
      ) : null}
      {general.map((c) => (
        <Row
          key={c.id}
          label={c.name}
          left={<Dot color={displayColor(c)} />}
          right={<Text style={{ color: theme.primary }}>{calendarId === c.id ? '●' : '○'}</Text>}
          onPress={() => setCalendarId(c.id)}
        />
      ))}

      <ErrorText error={error} />
      {preview ? (
        <>
          <SectionTitle>{preview.name || 'Preview'}</SectionTitle>
          <Card style={{ marginHorizontal: 16 }}>
            <Text style={{ color: theme.text, fontSize: 16 }}>
              {preview.total} events: {preview.new} new, {preview.updates} already here (they'll be updated)
            </Text>
            {preview.errors.length ? (
              <Muted style={{ marginTop: 6 }}>
                {preview.errors.length} couldn't be read and will be skipped: {preview.errors.slice(0, 3).join('; ')}
                {preview.errors.length > 3 ? '…' : ''}
              </Muted>
            ) : null}
          </Card>
          {preview.sample.slice(0, 20).map((e) => (
            <Row
              key={e.uid + e.recurrenceId}
              label={e.title}
              detail={`${e.allDay ? formatDay(e.start, true) : `${formatDay(localDate(e.start), true)} ${localTime(e.start)}`}${e.rrule ? ' · repeats' : ''}`}
            />
          ))}
          {preview.total > 20 ? <Muted style={{ paddingHorizontal: 16 }}>…and {preview.total - 20} more</Muted> : null}
          <Button label={`Import ${preview.total} events`} onPress={doImport} loading={busy} disabled={!preview.total} style={{ margin: 16 }} />
        </>
      ) : busy ? (
        <Muted style={{ padding: 16 }}>Reading the calendar…</Muted>
      ) : null}
    </ScrollView>
  );
}

const makeStyles = (theme: Theme) =>
  StyleSheet.create({
    root: { flex: 1, backgroundColor: theme.bg },
    big: { color: theme.text, fontSize: 26, fontWeight: '700', marginBottom: 8 },
  });
