import { useState } from 'react';
import { Pressable, StyleSheet, Text } from 'react-native';
import { DateStr, formatDay, startOfMonth } from '../dates';
import { Theme, useThemedStyles } from '../theme';
import { Sheet } from './kit';
import { MonthGrid } from './MonthGrid';

/** A date that opens a month grid to change it. */
export function DateField({ value, onChange, label }: { value: DateStr; onChange: (d: DateStr) => void; label?: string }) {
  const styles = useThemedStyles(makeStyles);
  const [open, setOpen] = useState(false);
  const [month, setMonth] = useState(startOfMonth(value));
  return (
    <>
      <Pressable
        style={styles.field}
        onPress={() => {
          setMonth(startOfMonth(value));
          setOpen(true);
        }}
      >
        <Text style={styles.text}>{formatDay(value, true)}</Text>
      </Pressable>
      <Sheet visible={open} onClose={() => setOpen(false)} title={label}>
        <MonthGrid
          month={month}
          selected={value}
          marks={{}}
          onSelect={(d) => {
            onChange(d);
            setOpen(false);
          }}
          onMonthChange={setMonth}
        />
      </Sheet>
    </>
  );
}

const makeStyles = (theme: Theme) =>
  StyleSheet.create({
    field: {
      backgroundColor: theme.surface,
      borderColor: theme.border,
      borderWidth: 1,
      borderRadius: 12,
      paddingHorizontal: 16,
      paddingVertical: 14,
      flex: 1,
    },
    text: { color: theme.text, fontSize: 16 },
  });
