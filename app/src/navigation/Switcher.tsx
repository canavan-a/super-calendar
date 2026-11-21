import { useNavigation } from '@react-navigation/native';
import { NativeStackNavigationProp } from '@react-navigation/native-stack';
import { useState } from 'react';
import { Pressable, Text, View } from 'react-native';
import { KIND_LABEL, Kind, displayColor } from '../api/calendars';
import { Badge, Dot, Row, SectionTitle, Sheet } from '../components/kit';
import { useCalendars } from '../data/CalendarsContext';
import { useTheme } from '../theme';
import { RootStackParamList, routeForKind } from './types';

const KINDS: Kind[] = ['general', 'training', 'cycle'];

// Switching modes: the header title opens one list with the Overview, every
// calendar grouped by type, "+ New calendar" and the Inbox.
export function SwitcherTitle({ title, color }: { title: string; color?: string }) {
  const theme = useTheme();
  const [open, setOpen] = useState(false);
  const { calendars, invites } = useCalendars();
  const navigation = useNavigation<NativeStackNavigationProp<RootStackParamList>>();

  const go = (fn: () => void) => {
    setOpen(false);
    fn();
  };
  const openCalendar = (id: string, kind: Kind) =>
    go(() =>
      navigation.reset({ index: 1, routes: [{ name: 'Overview' }, { name: routeForKind(kind), params: { id } }] })
    );

  return (
    <>
      <Pressable onPress={() => setOpen(true)} hitSlop={8} style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
        {color ? <Dot color={color} /> : null}
        <Text style={{ color: theme.text, fontSize: 19, fontWeight: '700', maxWidth: 210 }} numberOfLines={1}>
          {title}
        </Text>
        <Text style={{ color: theme.textMuted, fontSize: 14 }}>▾</Text>
        {invites.length > 0 ? <Badge count={invites.length} /> : null}
      </Pressable>
      <Sheet visible={open} onClose={() => setOpen(false)}>
        <Row
          label="Overview"
          detail="Everything you've switched on, together"
          left={<Text style={{ fontSize: 18 }}>◎</Text>}
          onPress={() => go(() => navigation.reset({ index: 0, routes: [{ name: 'Overview' }] }))}
        />
        {KINDS.map((k) => {
          const list = calendars.filter((c) => c.kind === k);
          if (!list.length) return null;
          return (
            <View key={k}>
              <SectionTitle>{KIND_LABEL[k]}</SectionTitle>
              {list.map((c) => (
                <Row
                  key={c.id}
                  label={c.name}
                  detail={c.memberCount > 1 ? `Shared with ${c.memberCount - 1}` : undefined}
                  left={<Dot color={displayColor(c)} />}
                  onPress={() => openCalendar(c.id, c.kind)}
                />
              ))}
            </View>
          );
        })}
        <View style={{ height: 12 }} />
        <Row label="+ New calendar" onPress={() => go(() => navigation.navigate('NewCalendar'))} />
        <Row
          label="Inbox"
          detail={invites.length ? `${invites.length} waiting for you` : 'Invites to shared calendars'}
          right={<Badge count={invites.length} />}
          onPress={() => go(() => navigation.navigate('Inbox'))}
        />
        <Row label="Settings" onPress={() => go(() => navigation.navigate('Settings'))} />
      </Sheet>
    </>
  );
}
