import AsyncStorage from '@react-native-async-storage/async-storage';
import React, { createContext, useContext, useEffect, useMemo, useState } from 'react';
import { useColorScheme } from 'react-native';
import { PALETTES, Palette, ThemeName } from './palettes';

// Theming: the five palettes in palettes.ts, picked in Settings, or "System"
// to follow the phone's light/dark setting (light or dark). Only colours come
// from here; each screen's StyleSheet keeps its own layout. The launcher icon
// follows the theme too (src/appIcon.ts).

export type Theme = Palette;
export type { ThemeName };
export type ThemeSetting = ThemeName | 'system';

const STORAGE_KEY = 'supercalendar.theme.v1';

type Ctx = { theme: Theme; setting: ThemeSetting; setSetting: (s: ThemeSetting) => void };

const ThemeContext = createContext<Ctx>({ theme: PALETTES.dark, setting: 'system', setSetting: () => {} });

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const scheme = useColorScheme();
  const [setting, setSettingState] = useState<ThemeSetting>('system');

  useEffect(() => {
    AsyncStorage.getItem(STORAGE_KEY)
      .then((v) => {
        if (v === 'system' || (v && v in PALETTES)) setSettingState(v as ThemeSetting);
      })
      .catch(() => {});
  }, []);

  const value = useMemo<Ctx>(() => {
    const name: ThemeName = setting === 'system' ? (scheme === 'light' ? 'light' : 'dark') : setting;
    return {
      theme: PALETTES[name],
      setting,
      setSetting: (s) => {
        setSettingState(s);
        AsyncStorage.setItem(STORAGE_KEY, s).catch(() => {});
      },
    };
  }, [setting, scheme]);

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): Theme {
  return useContext(ThemeContext).theme;
}

export function useThemeSetting() {
  return useContext(ThemeContext);
}

// Builds a StyleSheet from the current theme, rebuilt only when it changes.
export function useThemedStyles<T>(make: (theme: Theme) => T): T {
  const theme = useTheme();
  return useMemo(() => make(theme), [theme]); // eslint-disable-line react-hooks/exhaustive-deps
}
