import { Pressable, StyleSheet, Text, View } from 'react-native';
import { Theme, useThemedStyles } from '../theme';

/**
 * Shown in an editor when someone else saves the same item while it's open
 * (live updates). Saving anyway asks first (the server refuses a save based on
 * an older version), so nothing is lost silently.
 */
export function ChangedBanner({ name, onLoad }: { name: string; onLoad: () => void }) {
  const styles = useThemedStyles(makeStyles);
  return (
    <View style={styles.banner}>
      <Text style={styles.text}>{name} just changed this.</Text>
      <Pressable onPress={onLoad} hitSlop={8}>
        <Text style={styles.action}>Load theirs</Text>
      </Pressable>
    </View>
  );
}

const makeStyles = (theme: Theme) =>
  StyleSheet.create({
    banner: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 12,
      backgroundColor: theme.surfaceAlt,
      borderColor: theme.primary,
      borderWidth: 1,
      borderRadius: 10,
      paddingVertical: 10,
      paddingHorizontal: 14,
    },
    text: { flex: 1, color: theme.text },
    action: { color: theme.primary, fontWeight: '700' },
  });
