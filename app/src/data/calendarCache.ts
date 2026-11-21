import AsyncStorage from '@react-native-async-storage/async-storage';

// The cached calendar list is kept per account and wiped on sign-out, so a
// shared phone never shows one person's calendars to the next.
const PREFIX = 'supercalendar.calendars.v1.';

export const calendarCacheKey = (userId: string) => PREFIX + userId;

export async function clearCalendarCache() {
  const keys = await AsyncStorage.getAllKeys().catch(() => [] as readonly string[]);
  await AsyncStorage.multiRemove(keys.filter((k) => k.startsWith(PREFIX))).catch(() => {});
}
