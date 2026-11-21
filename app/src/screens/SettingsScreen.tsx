import { useEffect, useState } from 'react';
import { Alert, AppState, ScrollView, StyleSheet, Text } from 'react-native';
import {
  Release,
  canInstallUpdates,
  compareVersions,
  fetchLatestRelease,
  getVersionInfo,
  installRelease,
  onDownloadProgress,
  openInstallSettings,
  updatesSupported,
} from '../appUpdater';
import { useAuth } from '../auth/AuthContext';
import { Button } from '../components/ui';
import ThemePicker from '../components/ThemePicker';
import { Theme, useThemedStyles } from '../theme';

type UpdateState =
  | { kind: 'idle' }
  | { kind: 'checking' }
  | { kind: 'upToDate' }
  | { kind: 'available'; release: Release }
  // Sent to Android's "Install unknown apps" page; carries on by itself once
  // it's allowed and the app is back in front.
  | { kind: 'needsPermission'; release: Release }
  | { kind: 'downloading'; release: Release; percent: number | null }
  | { kind: 'error'; message: string };

// Updates (sideload), the account, sign out and deleting the account.
export default function SettingsScreen() {
  const styles = useThemedStyles(makeStyles);
  const { user, logout, deleteAccount } = useAuth();
  const [version, setVersion] = useState<string | null>(null);
  const [update, setUpdate] = useState<UpdateState>({ kind: 'idle' });
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  useEffect(() => {
    if (!updatesSupported) return;
    getVersionInfo()
      .then((v) => setVersion(v.versionName))
      .catch(() => {});
  }, []);

  const checkForUpdates = async () => {
    setUpdate({ kind: 'checking' });
    try {
      // The version may not have loaded yet if this is tapped right away.
      const current = version ?? (await getVersionInfo()).versionName;
      const latest = await fetchLatestRelease();
      if (latest && compareVersions(latest.version, current) > 0) {
        setUpdate({ kind: 'available', release: latest });
      } else {
        setUpdate({ kind: 'upToDate' });
      }
    } catch (e: any) {
      setUpdate({ kind: 'error', message: e.message ?? 'Could not check for updates' });
    }
  };

  const installUpdate = async (release: Release) => {
    if (!(await canInstallUpdates().catch(() => false))) {
      setUpdate({ kind: 'needsPermission', release });
      await openInstallSettings().catch(() => {});
      return;
    }
    setUpdate({ kind: 'downloading', release, percent: null });
    const stop = onDownloadProgress((p) => {
      if (p.bytesTotal > 0) {
        setUpdate({ kind: 'downloading', release, percent: Math.round((100 * p.bytesDownloaded) / p.bytesTotal) });
      }
    });
    try {
      await installRelease(release);
      setUpdate({ kind: 'available', release });
    } catch (e: any) {
      setUpdate({ kind: 'error', message: e.message ?? 'Update failed' });
    } finally {
      stop();
    }
  };

  // Back from "Install unknown apps" with it allowed: carry on installing.
  useEffect(() => {
    if (update.kind !== 'needsPermission') return;
    const { release } = update;
    const sub = AppState.addEventListener('change', async (state) => {
      if (state === 'active' && (await canInstallUpdates().catch(() => false))) installUpdate(release);
    });
    return () => sub.remove();
  }, [update]); // eslint-disable-line react-hooks/exhaustive-deps

  const confirmDelete = () =>
    Alert.alert('Delete your account?', 'Your account and everything in it are deleted for good.', [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Delete',
        style: 'destructive',
        onPress: async () => {
          setDeleting(true);
          setDeleteError(null);
          try {
            await deleteAccount();
          } catch (e: any) {
            setDeleteError(e.message ?? 'Could not delete your account');
            setDeleting(false);
          }
        },
      },
    ]);

  return (
    <ScrollView style={styles.root} contentContainerStyle={styles.content}>
      <Text style={styles.section}>Theme</Text>
      <ThemePicker />
      {updatesSupported && (
        <>
          <Text style={styles.section}>Updates</Text>
          <Text style={styles.text}>
            Version {version ?? '…'}
            {update.kind === 'upToDate' && ' · up to date'}
          </Text>
          {update.kind === 'available' || update.kind === 'needsPermission' ? (
            <Button label={`Install ${update.release.version}`} onPress={() => installUpdate(update.release)} style={styles.gap} />
          ) : update.kind === 'downloading' ? (
            <Button
              label={update.percent === null ? 'Downloading…' : `Downloading ${update.percent}%`}
              onPress={() => {}}
              disabled
              style={styles.gap}
            />
          ) : (
            <Button
              label="Check for updates"
              onPress={checkForUpdates}
              loading={update.kind === 'checking'}
              variant="secondary"
              style={styles.gap}
            />
          )}
          {update.kind === 'needsPermission' && (
            <Text style={[styles.hint, styles.gap]}>
              Allow "Install unknown apps" for Super Calendar, then come back. The update carries on by itself.
            </Text>
          )}
          {update.kind === 'error' && <Text style={styles.error}>{update.message}</Text>}
        </>
      )}

      <Text style={styles.section}>Account</Text>
      {user && (
        <>
          <Text style={styles.text}>{user.displayName}</Text>
          <Text style={styles.hint}>{user.email}</Text>
        </>
      )}
      <Button label="Sign out" onPress={logout} variant="secondary" style={styles.gap} />
      <Button label="Delete account" onPress={confirmDelete} loading={deleting} variant="danger" style={styles.gap} />
      {deleteError && <Text style={styles.error}>{deleteError}</Text>}
    </ScrollView>
  );
}

const makeStyles = (theme: Theme) =>
  StyleSheet.create({
    root: { flex: 1, backgroundColor: theme.bg },
    content: { padding: 20, paddingBottom: 40 },
    section: {
      color: theme.textMuted,
      fontSize: 13,
      fontWeight: '600',
      textTransform: 'uppercase',
      letterSpacing: 0.5,
      marginTop: 24,
      marginBottom: 8,
    },
    text: { color: theme.text, fontSize: 16 },
    hint: { color: theme.textMuted, fontSize: 14 },
    gap: { marginTop: 12 },
    error: { color: theme.danger, marginTop: 12 },
  });
