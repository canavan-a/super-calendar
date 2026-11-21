import * as SecureStore from 'expo-secure-store';

const SESSION_TOKEN_KEY = 'sc_session_token';

// The session token lives in the Android Keystore-backed secure store.
export const tokenStore = {
  get: (): Promise<string | null> => SecureStore.getItemAsync(SESSION_TOKEN_KEY),
  set: (token: string): Promise<void> => SecureStore.setItemAsync(SESSION_TOKEN_KEY, token),
  clear: (): Promise<void> => SecureStore.deleteItemAsync(SESSION_TOKEN_KEY),
};
