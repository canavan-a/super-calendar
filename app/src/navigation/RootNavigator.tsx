import { createNavigationContainerRef } from '@react-navigation/native';
import { createNativeStackNavigator } from '@react-navigation/native-stack';
import { useEffect } from 'react';
import { ActivityIndicator, Alert, View } from 'react-native';
import { useAuth } from '../auth/AuthContext';
import { CalendarsProvider } from '../data/CalendarsContext';
import { IcsPayload, initialIcs, onIcs } from '../icsIntents';
import ActivityTypesScreen from '../screens/ActivityTypesScreen';
import CalendarSettingsScreen from '../screens/CalendarSettingsScreen';
import CycleScreen from '../screens/CycleScreen';
import EventEditorScreen from '../screens/EventEditorScreen';
import GeneralScreen from '../screens/GeneralScreen';
import ImportScreen from '../screens/ImportScreen';
import InboxScreen from '../screens/InboxScreen';
import NewCalendarScreen from '../screens/NewCalendarScreen';
import OverviewScreen from '../screens/OverviewScreen';
import SessionEditorScreen from '../screens/SessionEditorScreen';
import SettingsScreen from '../screens/SettingsScreen';
import SignInScreen from '../screens/SignInScreen';
import TrainingScreen from '../screens/TrainingScreen';
import { useTheme } from '../theme';
import { RootStackParamList } from './types';

const Stack = createNativeStackNavigator<RootStackParamList>();

export const navigationRef = createNavigationContainerRef<RootStackParamList>();

// Calendar files and links from Android (a tapped .ics or webcal:// link, the
// share sheet) open the import screen. Only while signed in: a file shared
// before signing in is dropped, as there's nowhere to put it yet.
function useIcsIntents() {
  useEffect(() => {
    const open = (p: IcsPayload | null) => {
      if (!p) return;
      if (p.error) {
        Alert.alert("Couldn't open that calendar", p.error);
        return;
      }
      const source = p.url ? { url: p.url } : { ics: p.text };
      const go = () => navigationRef.navigate('Import', { source });
      if (navigationRef.isReady()) go();
      else setTimeout(go, 500);
    };
    initialIcs().then(open);
    return onIcs(open);
  }, []);
}

function SignedIn() {
  useIcsIntents();
  return (
    <CalendarsProvider>
      <Stack.Navigator>
        <Stack.Screen name="Overview" component={OverviewScreen} />
        <Stack.Screen name="General" component={GeneralScreen} />
        <Stack.Screen name="Training" component={TrainingScreen} />
        <Stack.Screen name="Cycle" component={CycleScreen} />
        <Stack.Screen name="Inbox" component={InboxScreen} />
        <Stack.Screen name="NewCalendar" component={NewCalendarScreen} options={{ title: 'New calendar' }} />
        <Stack.Screen name="CalendarSettings" component={CalendarSettingsScreen} options={{ title: 'Calendar settings' }} />
        <Stack.Screen name="EventEditor" component={EventEditorScreen} />
        <Stack.Screen name="Import" component={ImportScreen} options={{ title: 'Import calendar' }} />
        <Stack.Screen name="SessionEditor" component={SessionEditorScreen} />
        <Stack.Screen name="ActivityTypes" component={ActivityTypesScreen} options={{ title: 'Activities' }} />
        <Stack.Screen name="Settings" component={SettingsScreen} />
      </Stack.Navigator>
    </CalendarsProvider>
  );
}

export default function RootNavigator() {
  const theme = useTheme();
  const { status } = useAuth();

  if (status === 'loading') {
    return (
      <View style={{ flex: 1, alignItems: 'center', justifyContent: 'center', backgroundColor: theme.bg }}>
        <ActivityIndicator color={theme.primary} />
      </View>
    );
  }

  if (status === 'signedIn') return <SignedIn />;
  return (
    <Stack.Navigator>
      <Stack.Screen name={'SignIn' as any} component={SignInScreen} options={{ headerShown: false }} />
    </Stack.Navigator>
  );
}
