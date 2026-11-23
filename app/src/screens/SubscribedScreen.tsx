import { useCallback, useEffect, useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { Feed, api } from '../api/calendars';
import { ApiError } from '../api/client';
import { useCalendar, useCalendarVersion, useCalendars } from '../data/CalendarsContext';
import { ago } from '../dates';
import { ScreenProps } from '../navigation/types';
import { Theme, useThemedStyles } from '../theme';
import { EventsCalendar } from './GeneralScreen';

// A subscribed calendar: a general calendar's month and day list, read-only
// (its events are its link's), under how syncing the link is going.
export default function SubscribedScreen({ navigation, route }: ScreenProps<'Subscribed'>) {
  const { id } = route.params;
  const [feed, setFeed] = useState<Feed | null>(null);
  const version = useCalendarVersion(id);
  const loadFeed = useCallback(() => {
    api.feed(id).then(setFeed).catch(() => {});
  }, [id]);
  useEffect(() => {
    loadFeed();
  }, [loadFeed, version]);

  return (
    <EventsCalendar
      navigation={navigation as any}
      id={id}
      readOnly
      onReload={loadFeed}
      top={<FeedBar id={id} feed={feed} onFeed={setFeed} onFix={() => navigation.navigate('CalendarSettings', { id })} />}
    />
  );
}

/** "Synced 5 min ago · Refresh", or what's wrong with the link. */
export function FeedBar({ id, feed, onFeed, onFix }: { id: string; feed: Feed | null; onFeed: (f: Feed) => void; onFix?: () => void }) {
  const styles = useThemedStyles(makeStyles);
  const cal = useCalendar(id);
  const { refresh, bump } = useCalendars();
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const status = feed?.status ?? cal?.feed?.status ?? 'ok';
  const synced = feed?.lastSyncedAt ?? cal?.feed?.lastSyncedAt ?? null;

  const refreshNow = async () => {
    setBusy(true);
    setNote(null);
    try {
      onFeed(await api.refreshFeed(id));
      bump(id);
      refresh(); // the list shows the status
    } catch (e: any) {
      setNote(e instanceof ApiError && e.status === 429 ? 'Just refreshed. Try again in a minute.' : e.message);
    } finally {
      setBusy(false);
    }
  };

  const syncedText = synced ? `Synced ${ago(synced)}` : 'Not synced yet';
  return (
    <View style={[styles.bar, status !== 'ok' && styles.trouble]}>
      {status === 'broken' ? (
        <Text style={styles.text}>
          This link stopped working{feed?.lastError ? ` (${feed.lastError})` : ''}. It may have been reset: paste the new one.
          {synced ? ` Showing what it had ${ago(synced)}.` : ''}
        </Text>
      ) : status === 'failing' ? (
        <Text style={styles.text}>
          Couldn't sync lately{feed?.lastError ? `: ${feed.lastError}` : ''}. It keeps trying. {syncedText}.
        </Text>
      ) : (
        <Text style={styles.muted}>
          {syncedText}
          {feed?.host ? ` from ${feed.host}` : ''} · updates every 15 min
        </Text>
      )}
      {note ? <Text style={styles.muted}>{note}</Text> : null}
      <View style={styles.actions}>
        {status === 'broken' && onFix ? (
          <Pressable onPress={onFix} hitSlop={8}>
            <Text style={styles.action}>Paste new link</Text>
          </Pressable>
        ) : null}
        <Pressable onPress={refreshNow} disabled={busy} hitSlop={8}>
          <Text style={[styles.action, busy && { opacity: 0.5 }]}>{busy ? 'Refreshing…' : 'Refresh now'}</Text>
        </Pressable>
      </View>
    </View>
  );
}

const makeStyles = (theme: Theme) =>
  StyleSheet.create({
    bar: {
      margin: 12,
      marginBottom: 4,
      padding: 12,
      borderRadius: 10,
      backgroundColor: theme.surfaceAlt,
      gap: 6,
    },
    trouble: { backgroundColor: theme.dangerBg, borderColor: theme.danger, borderWidth: 1 },
    text: { color: theme.text },
    muted: { color: theme.textMuted, fontSize: 13 },
    actions: { flexDirection: 'row', gap: 20, justifyContent: 'flex-end' },
    action: { color: theme.primary, fontWeight: '700' },
  });
