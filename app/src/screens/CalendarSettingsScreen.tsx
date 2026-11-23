import { useCallback, useEffect, useState } from 'react';
import { Alert, ScrollView, StyleSheet, View } from 'react-native';
import { Feed, Invite, KIND_LABEL, Member, api } from '../api/calendars';
import { useAuth } from '../auth/AuthContext';
import { ColorPicker, ErrorText, Muted, Row, SectionTitle, Segmented, ToggleRow } from '../components/kit';
import { Button, TextField } from '../components/ui';
import { useCalendar, useCalendars } from '../data/CalendarsContext';
import { ScreenProps } from '../navigation/types';
import { FeedBar } from './SubscribedScreen';
import { Theme, useTheme, useThemedStyles } from '../theme';

// Name, colours, members, invites and leaving. Everything here except "My
// colour" and the Overview switch is shared with every member.
export default function CalendarSettingsScreen({ navigation, route }: ScreenProps<'CalendarSettings'>) {
  const styles = useThemedStyles(makeStyles);
  const theme = useTheme();
  const { id } = route.params;
  const cal = useCalendar(id);
  const { upsert, remove, bump, refresh } = useCalendars();
  const { user } = useAuth();
  const [name, setName] = useState(cal?.name ?? '');
  const [members, setMembers] = useState<Member[]>([]);
  const [sent, setSent] = useState<Invite[]>([]);
  const [email, setEmail] = useState('');
  const [inviting, setInviting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [feed, setFeed] = useState<Feed | null>(null);
  const [newLink, setNewLink] = useState('');
  const [replacing, setReplacing] = useState(false);
  const subscribed = cal?.kind === 'subscribed';

  const load = useCallback(async () => {
    try {
      const [m, s, f] = await Promise.all([api.members(id), api.sentInvites(id), subscribed ? api.feed(id) : Promise.resolve(null)]);
      setMembers(m);
      setSent(s);
      setFeed(f);
    } catch (e: any) {
      setError(e.message);
    }
  }, [id, subscribed]);
  useEffect(() => {
    load();
  }, [load]);

  // A new link is fetched and checked by the server before it replaces the old one.
  const replaceLink = async () => {
    setReplacing(true);
    setError(null);
    setNotice(null);
    try {
      setFeed(await api.replaceFeedLink(id, newLink));
      setNewLink('');
      setNotice('Link replaced and synced.');
      bump(id);
      refresh();
    } catch (e: any) {
      setError(e.message);
    } finally {
      setReplacing(false);
    }
  };

  if (!cal) return <View style={styles.root} />;

  const save = async (fn: () => Promise<any>) => {
    setError(null);
    try {
      const c = await fn();
      if (c) upsert(c);
    } catch (e: any) {
      setError(e.message);
    }
  };

  const invite = async () => {
    setInviting(true);
    setError(null);
    setNotice(null);
    try {
      const inv = await api.invite(id, email);
      setEmail('');
      setNotice(`Invited ${inv.email}. It waits in their Inbox (they get an email too).`);
      load();
    } catch (e: any) {
      setError(e.message);
    } finally {
      setInviting(false);
    }
  };

  const leave = () => {
    const last = members.length <= 1;
    Alert.alert(
      last ? 'Delete this calendar?' : 'Leave this calendar?',
      last
        ? "You're the only member, so leaving deletes it and everything in it."
        : 'The others keep it. You can only come back if someone invites you again.',
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: last ? 'Delete' : 'Leave',
          style: 'destructive',
          onPress: async () => {
            try {
              await api.leave(id);
              remove(id);
              navigation.reset({ index: 0, routes: [{ name: 'Overview' }] });
            } catch (e: any) {
              setError(e.message);
            }
          },
        },
      ]
    );
  };

  return (
    <ScrollView style={styles.root} contentContainerStyle={{ padding: 8, paddingBottom: 60 }} keyboardShouldPersistTaps="handled">
      <ErrorText error={error} />
      <SectionTitle>{KIND_LABEL[cal.kind]} calendar · shared</SectionTitle>
      <TextField
        value={name}
        onChangeText={setName}
        onEndEditing={() => name.trim() && name !== cal.name && save(() => api.updateCalendar(id, { name }))}
        style={{ marginHorizontal: 16 }}
        maxLength={80}
      />
      <Muted style={{ paddingHorizontal: 16, marginTop: 12 }}>Colour for everyone</Muted>
      <View style={{ paddingHorizontal: 16 }}>
        <ColorPicker value={cal.color} onChange={(c) => save(() => api.updateCalendar(id, { color: c }))} />
      </View>

      {cal.kind === 'training' ? (
        <>
          <Muted style={{ paddingHorizontal: 16, marginTop: 12, marginBottom: 8 }}>Weeks start on</Muted>
          <View style={{ paddingHorizontal: 16 }}>
            <Segmented
              options={[
                { id: '1', label: 'Monday' },
                { id: '0', label: 'Sunday' },
              ]}
              value={String(cal.settings.weekStart ?? 1)}
              onChange={(v) => save(() => api.updateCalendar(id, { settings: { weekStart: Number(v) } }))}
            />
          </View>
          <Row label="Activities" detail="The activity types sessions use" onPress={() => navigation.navigate('ActivityTypes', { calendarId: id })} />
        </>
      ) : null}

      {subscribed ? (
        <>
          <SectionTitle>Link</SectionTitle>
          <FeedBar id={id} feed={feed} onFeed={setFeed} />
          {feed ? (
            <Row
              label={feed.mine && feed.url ? feed.url : feed.host || 'A calendar link'}
              detail={
                feed.mine
                  ? 'Only you see the whole link. Treat it like a password.'
                  : `Added by ${feed.addedByName || 'someone who has left'}. Only they see the whole link.`
              }
            />
          ) : null}
          <Muted style={{ paddingHorizontal: 16, marginTop: 4 }}>
            Was the link reset, or should this follow another calendar? Paste the new link; it's checked before it replaces this one.
          </Muted>
          <View style={{ flexDirection: 'row', gap: 8, paddingHorizontal: 16, marginTop: 8 }}>
            <TextField
              value={newLink}
              onChangeText={setNewLink}
              placeholder="New link"
              autoCapitalize="none"
              autoCorrect={false}
              keyboardType="url"
              style={{ flex: 1 }}
            />
            <Button label="Replace" onPress={replaceLink} loading={replacing} disabled={!newLink.trim()} />
          </View>
        </>
      ) : null}

      <SectionTitle>Just for you</SectionTitle>
      <Muted style={{ paddingHorizontal: 16 }}>My colour</Muted>
      <View style={{ paddingHorizontal: 16 }}>
        <ColorPicker value={cal.myColor ?? ''} onChange={(c) => save(() => api.updateMine(id, { color: c }))} />
      </View>
      {cal.myColor ? <Row label="Use the shared colour" onPress={() => save(() => api.updateMine(id, { color: '' }))} /> : null}
      <ToggleRow label="Show on the Overview" value={cal.inOverview} onChange={(v) => save(() => api.updateMine(id, { inOverview: v }))} />

      <SectionTitle>Members</SectionTitle>
      {members.map((m) => (
        <Row key={m.userId} label={m.displayName + (m.userId === user?.id ? ' (you)' : '')} detail={m.email} />
      ))}
      {sent.map((inv) => (
        <Row
          key={inv.id}
          label={inv.email}
          detail={`Invited by ${inv.inviterName || 'someone'} · waiting`}
          right={<Muted style={{ color: theme.danger }}>Cancel</Muted>}
          onPress={() =>
            api
              .cancelInvite(id, inv.id)
              .then(load)
              .catch((e) => setError(e.message))
          }
        />
      ))}
      <View style={{ flexDirection: 'row', gap: 8, paddingHorizontal: 16, marginTop: 8 }}>
        <TextField
          value={email}
          onChangeText={setEmail}
          placeholder="Invite by email"
          autoCapitalize="none"
          keyboardType="email-address"
          style={{ flex: 1 }}
        />
        <Button label="Invite" onPress={invite} loading={inviting} disabled={!email.includes('@')} />
      </View>
      {notice ? <Muted style={{ paddingHorizontal: 16, marginTop: 8 }}>{notice}</Muted> : null}
      <Muted style={{ paddingHorizontal: 16, marginTop: 8, fontSize: 13 }}>
        Everyone in a calendar can change everything in it and invite others.
      </Muted>

      <View style={{ height: 24 }} />
      <Row label={members.length <= 1 ? 'Delete calendar' : 'Leave calendar'} danger onPress={leave} />
    </ScrollView>
  );
}

const makeStyles = (theme: Theme) => StyleSheet.create({ root: { flex: 1, backgroundColor: theme.bg } });
