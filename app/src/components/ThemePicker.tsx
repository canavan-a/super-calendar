import { Pressable, Text, View } from 'react-native';
import { PALETTES, THEME_NAMES } from '../palettes';
import { ThemeSetting, useThemeSetting } from '../theme';
import { Logo } from './Logo';

// Pick a palette, or System to follow the phone's light/dark setting. Each
// choice shows its logo, which is also what the launcher icon becomes once
// you leave the app.
export default function ThemePicker() {
  const { theme, setting, setSetting } = useThemeSetting();
  const options: { id: ThemeSetting; label: string }[] = [
    { id: 'system', label: 'System' },
    ...THEME_NAMES.map((n) => ({ id: n as ThemeSetting, label: PALETTES[n].label })),
  ];
  return (
    <View>
      <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: 10 }}>
        {options.map((o) => {
          const on = o.id === setting;
          return (
            <Pressable
              key={o.id}
              onPress={() => setSetting(o.id)}
              style={{
                alignItems: 'center',
                padding: 6,
                borderRadius: 16,
                borderWidth: 2,
                borderColor: on ? theme.primary : 'transparent',
              }}
            >
              {o.id === 'system' ? (
                <View style={{ width: 52, height: 52, borderRadius: 12, overflow: 'hidden', flexDirection: 'row' }}>
                  <View style={{ flex: 1, overflow: 'hidden' }}>
                    <Logo size={52} palette={PALETTES.light} full />
                  </View>
                  <View style={{ flex: 1, overflow: 'hidden', alignItems: 'flex-end' }}>
                    <Logo size={52} palette={PALETTES.dark} full />
                  </View>
                </View>
              ) : (
                <Logo size={52} palette={PALETTES[o.id]} full />
              )}
              <Text style={{ color: on ? theme.primary : theme.text, fontSize: 12, fontWeight: '600', marginTop: 4 }}>{o.label}</Text>
            </Pressable>
          );
        })}
      </View>
      <Text style={{ color: theme.textMuted, fontSize: 13, marginTop: 8 }}>
        The app icon switches to match after you leave the app. On Android 13+, "Themed icons" in your wallpaper settings
        tints it to your wallpaper instead.
      </Text>
    </View>
  );
}
