import React, { useEffect } from 'react';
import {
  KeyboardAvoidingView,
  Modal,
  Pressable,
  ScrollView,
  StyleSheet,
  Switch,
  Text,
  View,
  ViewStyle,
} from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { PALETTE } from '../api/calendars';
import { Theme, useTheme, useThemedStyles } from '../theme';

// Building blocks the calendar screens share, on top of ui.tsx.

/** A sheet that slides up from the bottom, closed by tapping outside. */
export function Sheet({
  visible,
  onClose,
  title,
  children,
}: {
  visible: boolean;
  onClose: () => void;
  title?: string;
  children: React.ReactNode;
}) {
  const styles = useThemedStyles(makeStyles);
  const insets = useSafeAreaInsets();
  return (
    <Modal visible={visible} transparent animationType="slide" onRequestClose={onClose} statusBarTranslucent>
      <KeyboardAvoidingView style={{ flex: 1 }} behavior="height">
        <Pressable style={styles.backdrop} onPress={onClose} />
        <View style={[styles.sheet, { paddingBottom: insets.bottom + 16 }]}>
          <View style={styles.handle} />
          {title ? <Text style={styles.sheetTitle}>{title}</Text> : null}
          <ScrollView keyboardShouldPersistTaps="handled" style={{ maxHeight: 560 }}>
            {children}
          </ScrollView>
        </View>
      </KeyboardAvoidingView>
    </Modal>
  );
}

/** A tappable list row. */
export function Row({
  label,
  detail,
  onPress,
  left,
  right,
  danger,
  style,
}: {
  label: string;
  detail?: string;
  onPress?: () => void;
  left?: React.ReactNode;
  right?: React.ReactNode;
  danger?: boolean;
  style?: ViewStyle;
}) {
  const styles = useThemedStyles(makeStyles);
  const theme = useTheme();
  return (
    <Pressable
      onPress={onPress}
      disabled={!onPress}
      style={({ pressed }) => [styles.row, pressed && { backgroundColor: theme.surfaceAlt }, style]}
    >
      {left}
      <View style={{ flex: 1 }}>
        <Text style={[styles.rowLabel, danger && { color: theme.danger }]} numberOfLines={1}>
          {label}
        </Text>
        {detail ? (
          <Text style={styles.rowDetail} numberOfLines={2}>
            {detail}
          </Text>
        ) : null}
      </View>
      {right}
    </Pressable>
  );
}

export function ToggleRow({
  label,
  detail,
  value,
  onChange,
  left,
  indent,
}: {
  label: string;
  detail?: string;
  value: boolean;
  onChange: (v: boolean) => void;
  left?: React.ReactNode;
  indent?: number;
}) {
  const theme = useTheme();
  return (
    <Row
      label={label}
      detail={detail}
      left={left}
      onPress={() => onChange(!value)}
      style={indent ? { paddingLeft: 16 + indent * 20 } : undefined}
      right={
        <Switch
          value={value}
          onValueChange={onChange}
          trackColor={{ true: theme.primary, false: theme.border }}
          thumbColor="#ffffff"
        />
      }
    />
  );
}

export function Dot({ color, size = 12 }: { color: string; size?: number }) {
  return <View style={{ width: size, height: size, borderRadius: size / 2, backgroundColor: color }} />;
}

export function SectionTitle({ children }: { children: React.ReactNode }) {
  const styles = useThemedStyles(makeStyles);
  return <Text style={styles.section}>{children}</Text>;
}

export function Muted({ children, style }: { children: React.ReactNode; style?: any }) {
  const styles = useThemedStyles(makeStyles);
  return <Text style={[styles.muted, style]}>{children}</Text>;
}

export function ErrorText({ error }: { error: string | null | undefined }) {
  const theme = useTheme();
  if (!error) return null;
  return <Text style={{ color: theme.danger, marginVertical: 8 }}>{error}</Text>;
}

export function ColorPicker({ value, onChange }: { value: string; onChange: (c: string) => void }) {
  const theme = useTheme();
  return (
    <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: 10, marginVertical: 8 }}>
      {PALETTE.map((c) => (
        <Pressable
          key={c}
          onPress={() => onChange(c)}
          hitSlop={4}
          style={{
            width: 34,
            height: 34,
            borderRadius: 17,
            backgroundColor: c,
            borderWidth: 3,
            borderColor: value.toLowerCase() === c ? theme.text : 'transparent',
          }}
        />
      ))}
    </View>
  );
}

/** A row of mutually exclusive choices. */
export function Segmented<T extends string>({
  options,
  value,
  onChange,
}: {
  options: { id: T; label: string }[];
  value: T;
  onChange: (v: T) => void;
}) {
  const styles = useThemedStyles(makeStyles);
  const theme = useTheme();
  return (
    <View style={styles.segmented}>
      {options.map((o) => {
        const on = o.id === value;
        return (
          <Pressable
            key={o.id}
            onPress={() => onChange(o.id)}
            style={[styles.segment, on && { backgroundColor: theme.primary }]}
          >
            <Text style={{ color: on ? theme.primaryText : theme.text, fontWeight: '600', fontSize: 13 }} numberOfLines={1}>
              {o.label}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
}

/** The round "+" button in the corner. */
export function Fab({ onPress, label = '+' }: { onPress: () => void; label?: string }) {
  const styles = useThemedStyles(makeStyles);
  const insets = useSafeAreaInsets();
  return (
    <Pressable onPress={onPress} style={[styles.fab, { bottom: insets.bottom + 20 }]}>
      <Text style={styles.fabText}>{label}</Text>
    </Pressable>
  );
}

/** A message at the bottom with an optional action (Undo), gone after a while. */
export function Snackbar({
  message,
  action,
  onAction,
  onDismiss,
}: {
  message: string | null;
  action?: string;
  onAction?: () => void;
  onDismiss: () => void;
}) {
  const styles = useThemedStyles(makeStyles);
  const insets = useSafeAreaInsets();
  useEffect(() => {
    if (!message) return;
    const t = setTimeout(onDismiss, 8000);
    return () => clearTimeout(t);
  }, [message, onDismiss]);
  if (!message) return null;
  return (
    <View style={[styles.snack, { bottom: insets.bottom + 90 }]}>
      <Text style={styles.snackText}>{message}</Text>
      {action ? (
        <Pressable onPress={onAction} hitSlop={8}>
          <Text style={styles.snackAction}>{action}</Text>
        </Pressable>
      ) : null}
    </View>
  );
}

export function HeaderButton({ label, onPress }: { label: string; onPress: () => void }) {
  const theme = useTheme();
  return (
    <Pressable onPress={onPress} hitSlop={10} style={{ paddingHorizontal: 4 }}>
      <Text style={{ color: theme.primary, fontSize: 16, fontWeight: '600' }}>{label}</Text>
    </Pressable>
  );
}

export function Badge({ count }: { count: number }) {
  const theme = useTheme();
  if (count <= 0) return null;
  return (
    <View style={{ backgroundColor: theme.danger, borderRadius: 10, minWidth: 20, paddingHorizontal: 6, height: 20, alignItems: 'center', justifyContent: 'center' }}>
      <Text style={{ color: '#fff', fontSize: 12, fontWeight: '700' }}>{count}</Text>
    </View>
  );
}

const makeStyles = (theme: Theme) =>
  StyleSheet.create({
    backdrop: { flex: 1, backgroundColor: 'rgba(0,0,0,0.45)' },
    sheet: {
      backgroundColor: theme.bg,
      borderTopLeftRadius: 20,
      borderTopRightRadius: 20,
      paddingTop: 8,
      paddingHorizontal: 8,
    },
    handle: { alignSelf: 'center', width: 40, height: 4, borderRadius: 2, backgroundColor: theme.border, marginBottom: 8 },
    sheetTitle: { color: theme.text, fontSize: 18, fontWeight: '700', paddingHorizontal: 12, marginBottom: 8 },
    row: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 12, paddingHorizontal: 16, borderRadius: 10 },
    rowLabel: { color: theme.text, fontSize: 16 },
    rowDetail: { color: theme.textMuted, fontSize: 13, marginTop: 2 },
    section: {
      color: theme.textMuted,
      fontSize: 13,
      fontWeight: '700',
      textTransform: 'uppercase',
      letterSpacing: 0.5,
      marginTop: 20,
      marginBottom: 6,
      paddingHorizontal: 16,
    },
    muted: { color: theme.textMuted, fontSize: 14 },
    segmented: { flexDirection: 'row', backgroundColor: theme.surfaceAlt, borderRadius: 10, padding: 3, gap: 3 },
    segment: { flex: 1, paddingVertical: 8, paddingHorizontal: 4, borderRadius: 8, alignItems: 'center' },
    fab: {
      position: 'absolute',
      right: 20,
      width: 58,
      height: 58,
      borderRadius: 29,
      backgroundColor: theme.primary,
      alignItems: 'center',
      justifyContent: 'center',
      elevation: 4,
    },
    fabText: { color: theme.primaryText, fontSize: 30, lineHeight: 34 },
    snack: {
      position: 'absolute',
      left: 16,
      right: 16,
      backgroundColor: theme.isDark ? '#e6edf3' : '#1f2328',
      borderRadius: 10,
      paddingVertical: 12,
      paddingHorizontal: 16,
      flexDirection: 'row',
      alignItems: 'center',
      gap: 16,
      elevation: 6,
    },
    snackText: { flex: 1, color: theme.isDark ? '#1f2328' : '#ffffff' },
    snackAction: { color: theme.isDark ? '#0969da' : '#58a6ff', fontWeight: '700' },
  });
