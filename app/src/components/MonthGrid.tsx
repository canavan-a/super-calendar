import React, { memo } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { DateStr, addMonths, dayOfMonth, formatMonth, monthGrid, today, weekdayLabels } from '../dates';
import { Theme, useTheme, useThemedStyles } from '../theme';

// A hand-built month grid: every calendar kind draws onto it. Dots are
// general events and training sessions; a tint fills the cell (logged cycle
// days); an outline marks a span (predicted period, fertile window).

export type DayMarks = {
  dots?: string[]; // colours, at most 4 shown
  tint?: { color: string; strength: number }; // strength 0..1
  outline?: { color: string; dashed?: boolean };
  label?: string; // a tiny line under the number
};

export const MonthGrid = memo(function MonthGrid({
  month,
  weekStart = 1,
  selected,
  marks,
  onSelect,
  onMonthChange,
}: {
  month: DateStr;
  weekStart?: 0 | 1;
  selected?: DateStr | null;
  marks: Record<DateStr, DayMarks>;
  onSelect: (d: DateStr) => void;
  onMonthChange: (m: DateStr) => void;
}) {
  const styles = useThemedStyles(makeStyles);
  const theme = useTheme();
  const days = monthGrid(month, weekStart);
  const thisMonth = month.slice(0, 7);
  const now = today();
  return (
    <View>
      <View style={styles.header}>
        <Pressable hitSlop={12} onPress={() => onMonthChange(addMonths(month, -1))} style={styles.arrow}>
          <Text style={styles.arrowText}>‹</Text>
        </Pressable>
        <Pressable onPress={() => onMonthChange(now)}>
          <Text style={styles.month}>{formatMonth(month)}</Text>
        </Pressable>
        <Pressable hitSlop={12} onPress={() => onMonthChange(addMonths(month, 1))} style={styles.arrow}>
          <Text style={styles.arrowText}>›</Text>
        </Pressable>
      </View>
      <View style={styles.week}>
        {weekdayLabels(weekStart).map((l) => (
          <Text key={l} style={styles.weekday}>
            {l.slice(0, 2)}
          </Text>
        ))}
      </View>
      {[0, 1, 2, 3, 4, 5].map((w) => (
        <View key={w} style={styles.week}>
          {days.slice(w * 7, w * 7 + 7).map((d) => {
            const m = marks[d];
            const inMonth = d.slice(0, 7) === thisMonth;
            const isSel = d === selected;
            const isToday = d === now;
            return (
              <Pressable key={d} style={styles.cell} onPress={() => onSelect(d)}>
                <View
                  style={[
                    styles.inner,
                    m?.tint && { backgroundColor: withAlpha(m.tint.color, 0.15 + 0.55 * m.tint.strength) },
                    m?.outline && {
                      borderColor: m.outline.color,
                      borderWidth: 2,
                      borderStyle: m.outline.dashed ? 'dashed' : 'solid',
                    },
                    isSel && { borderColor: theme.primary, borderWidth: 2, borderStyle: 'solid' },
                  ]}
                >
                  <Text
                    style={[
                      styles.num,
                      !inMonth && { color: theme.textMuted, opacity: 0.5 },
                      isToday && styles.today,
                    ]}
                  >
                    {dayOfMonth(d)}
                  </Text>
                  <View style={styles.dots}>
                    {(m?.dots ?? []).slice(0, 4).map((c, i) => (
                      <View key={i} style={[styles.dot, { backgroundColor: c }]} />
                    ))}
                  </View>
                  {m?.label ? (
                    <Text style={styles.label} numberOfLines={1}>
                      {m.label}
                    </Text>
                  ) : null}
                </View>
              </Pressable>
            );
          })}
        </View>
      ))}
    </View>
  );
});

/** '#rrggbb' with an alpha, as '#rrggbbaa'. */
export function withAlpha(hex: string, a: number): string {
  const v = Math.round(Math.max(0, Math.min(1, a)) * 255)
    .toString(16)
    .padStart(2, '0');
  return hex.length === 7 ? hex + v : hex;
}

const makeStyles = (theme: Theme) =>
  StyleSheet.create({
    header: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', paddingHorizontal: 8, paddingVertical: 8 },
    arrow: { paddingHorizontal: 16, paddingVertical: 4 },
    arrowText: { color: theme.primary, fontSize: 28, fontWeight: '300' },
    month: { color: theme.text, fontSize: 18, fontWeight: '700' },
    week: { flexDirection: 'row', paddingHorizontal: 4 },
    weekday: { flex: 1, textAlign: 'center', color: theme.textMuted, fontSize: 12, fontWeight: '600', paddingVertical: 4 },
    cell: { flex: 1, aspectRatio: 0.9, padding: 2 },
    inner: { flex: 1, borderRadius: 10, alignItems: 'center', paddingTop: 4, borderWidth: 2, borderColor: 'transparent' },
    num: { color: theme.text, fontSize: 15, fontWeight: '500' },
    today: { color: theme.primary, fontWeight: '800' },
    dots: { flexDirection: 'row', gap: 3, marginTop: 4 },
    dot: { width: 6, height: 6, borderRadius: 3 },
    label: { color: theme.textMuted, fontSize: 9, marginTop: 2 },
  });
