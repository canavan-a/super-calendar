import { NativeEventEmitter, NativeModules } from 'react-native';

// Calendar files and links handed to the app by Android (IcsIntentModule.kt):
// a tapped .ics or webcal:// link, "Share → Super Calendar", or the file
// picker. Each arrives as {text} or {url}, both of which the server imports.

// text: the file's contents; url: a link for the server to fetch.
export type IcsPayload = { text?: string; url?: string; error?: string };
type Payload = IcsPayload;

const native = NativeModules.IcsIntent as
  | { getInitial(): Promise<Payload | null>; pickFile(): Promise<Payload | null> }
  | undefined;

/** The intent the app was opened with, once. */
export async function initialIcs(): Promise<Payload | null> {
  return native ? native.getInitial().catch(() => null) : null;
}

/** Calendar files and links shared while the app is running. */
export function onIcs(handler: (p: Payload) => void): () => void {
  if (!native) return () => {};
  const emitter = new NativeEventEmitter(NativeModules.IcsIntent);
  const sub = emitter.addListener('SuperCalendarIcs', handler);
  return () => sub.remove();
}

/** Opens the system file picker; null if nothing was picked. */
export async function pickIcsFile(): Promise<Payload | null> {
  if (!native) throw new Error("This build can't open files");
  return native.pickFile();
}
