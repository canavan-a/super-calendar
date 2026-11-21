import { useState } from 'react';
import { RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';
import { Invite, KIND_LABEL, api } from '../api/calendars';
import { Dot, ErrorText, Muted } from '../components/kit';
import { Button, Card } from '../components/ui';
import { useCalendars } from '../data/CalendarsContext';
import { ScreenProps, routeForKind } from '../navigation/types';
import { Theme, useThemedStyles } from '../theme';

// Invites to shared calendars, waiting for a yes or no.
export default function InboxScreen({ navigation }: ScreenProps<'Inbox'>) {
  const styles = useThemedStyles(makeStyles);
  const { invites, refresh, upsert } = useCalendars();
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  const respond = async (inv: Invite, accept: boolean) => {
    setBusy(inv.id + accept);
    setError(null);
    try {
      const cal = await api.respond(inv.id, accept);
      if (cal) upsert(cal);
      await refresh();
      if (cal) navigation.navigate(routeForKind(cal.kind), { id: cal.id });
    } catch (e: any) {
      setError(e.message);
    } finally {
      setBusy(null);
    }
  };

  return (
    <ScrollView
      style={styles.root}
      contentContainerStyle={{ padding: 16, gap: 12 }}
      refreshControl={
        <RefreshControl
          refreshing={refreshing}
          onRefresh={async () => {
            setRefreshing(true);
            await refresh();
            setRefreshing(false);
          }}
        />
      }
    >
      <ErrorText error={error} />
      {invites.length === 0 ? (
        <Muted style={{ textAlign: 'center', marginTop: 40 }}>
          No invites. When someone shares a calendar with your email, it shows up here.
        </Muted>
      ) : null}
      {invites.map((inv) => (
        <Card key={inv.id}>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: 10, marginBottom: 4 }}>
            <Dot color={inv.calendarColor} />
            <Text style={styles.name}>{inv.calendarName}</Text>
          </View>
          <Muted>
            {KIND_LABEL[inv.calendarKind]} calendar · from {inv.inviterName || 'someone who has left'}
          </Muted>
          <Muted style={{ marginTop: 6, fontSize: 13 }}>
            Joining lets you see and change everything in it, and invite others. You can leave any time.
          </Muted>
          <View style={{ flexDirection: 'row', gap: 10, marginTop: 14 }}>
            <Button label="Accept" onPress={() => respond(inv, true)} loading={busy === inv.id + true} style={{ flex: 1 }} />
            <Button
              label="Decline"
              variant="secondary"
              onPress={() => respond(inv, false)}
              loading={busy === inv.id + false}
              style={{ flex: 1 }}
            />
          </View>
        </Card>
      ))}
    </ScrollView>
  );
}

const makeStyles = (theme: Theme) =>
  StyleSheet.create({
    root: { flex: 1, backgroundColor: theme.bg },
    name: { color: theme.text, fontSize: 18, fontWeight: '700' },
  });
