// The app's colour palettes: the same five as super-atlas (light, dark,
// slate, sepia, ember), switchable in Settings. Plain data with no imports,
// so tools/gen-icons.ts can read it too: the launcher icon for each palette is
// generated from these same colours (and the logo in src/components/Logo.tsx).

export type ThemeName = 'light' | 'dark' | 'slate' | 'sepia' | 'ember';

export type Palette = {
  name: ThemeName;
  label: string;
  // Whether the palette is dark, for the status bar and native controls.
  isDark: boolean;
  bg: string;
  surface: string;
  surfaceAlt: string;
  border: string;
  text: string;
  textMuted: string;
  primary: string;
  primaryText: string;
  danger: string;
  dangerBg: string;
  success: string;
  // The logo's three stripes, one per calendar kind.
  logo: [string, string, string];
};

export const PALETTES: Record<ThemeName, Palette> = {
  light: {
    name: 'light',
    label: 'Light',
    isDark: false,
    bg: '#ffffff',
    surface: '#f6f8fa',
    surfaceAlt: '#eaeef2',
    border: '#d0d7de',
    text: '#1f2328',
    textMuted: '#59636e',
    primary: '#0969da',
    primaryText: '#ffffff',
    danger: '#cf222e',
    dangerBg: '#ffebe9',
    success: '#1a7f37',
    logo: ['#0969da', '#1a7f37', '#bf3989'],
  },
  dark: {
    name: 'dark',
    label: 'Dark',
    isDark: true,
    bg: '#0d1117',
    surface: '#161b22',
    surfaceAlt: '#21262d',
    border: '#30363d',
    text: '#e6edf3',
    textMuted: '#8b949e',
    primary: '#2f81f7',
    primaryText: '#ffffff',
    danger: '#f85149',
    dangerBg: '#3d1418',
    success: '#3fb950',
    logo: ['#2f81f7', '#3fb950', '#db61a2'],
  },
  slate: {
    name: 'slate',
    label: 'Slate',
    isDark: true,
    bg: '#1e2530',
    surface: '#262e3b',
    surfaceAlt: '#2f3947',
    border: '#3c4757',
    text: '#dde3ea',
    textMuted: '#93a0b3',
    primary: '#5aa9e6',
    primaryText: '#0d1117',
    danger: '#ef6a6a',
    dangerBg: '#3a2222',
    success: '#6fcf97',
    logo: ['#5aa9e6', '#6fcf97', '#e88bb8'],
  },
  sepia: {
    name: 'sepia',
    label: 'Sepia',
    isDark: false,
    bg: '#f4ecd8',
    surface: '#ece0c6',
    surfaceAlt: '#e5d7b8',
    border: '#d8c6a0',
    text: '#3b2f1e',
    textMuted: '#7a6a4d',
    primary: '#a0651b',
    primaryText: '#fdf6e7',
    danger: '#a13c2c',
    dangerBg: '#f0dcd2',
    success: '#4e7c3a',
    logo: ['#a0651b', '#4e7c3a', '#a13c5c'],
  },
  ember: {
    name: 'ember',
    label: 'Ember',
    isDark: true,
    bg: '#050403',
    surface: '#0c0908',
    surfaceAlt: '#150f0d',
    border: '#241815',
    text: '#e8d9d0',
    textMuted: '#8a6c5f',
    primary: '#b04a2c',
    primaryText: '#f1e2d9',
    danger: '#c93a3a',
    dangerBg: '#1c0d0c',
    success: '#4f9e70',
    logo: ['#c8562f', '#d9973a', '#8a3620'],
  },
};

export const THEME_NAMES: ThemeName[] = ['light', 'dark', 'slate', 'sepia', 'ember'];

/**
 * The logo, on the 108-unit canvas of an Android adaptive icon (artwork kept
 * inside its 66-unit safe zone): a calendar page split diagonally into three
 * stripes, one colour per calendar kind, with binding rings on top. The
 * stripes meet along the lines x + y = cuts[0] and x + y = cuts[1]. Shared by
 * the launcher icons (tools/gen-icons.mts) and Logo.tsx.
 */
export const LOGO = {
  page: { x: 30, y: 34, w: 48, h: 46, r: 7 },
  rings: [
    { x: 40, y: 28, w: 5, h: 12, r: 2.5 },
    { x: 63, y: 28, w: 5, h: 12, r: 2.5 },
  ],
  cuts: [110, 130] as [number, number],
};

/** The colours a palette's logo uses: background, the three stripes, rings. */
export function logoColors(p: Palette) {
  return { bg: p.bg, stripes: p.logo, rings: p.text };
}
