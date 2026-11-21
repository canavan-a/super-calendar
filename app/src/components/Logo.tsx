import { View } from 'react-native';
import { LOGO, Palette, logoColors } from '../palettes';
import { useTheme } from '../theme';

// The app's logo drawn with plain Views (no SVG library), from the same
// geometry and colours as the launcher icons (palettes.ts), so it changes with
// the theme. `size` is the width of the cropped artwork, or of the whole
// 108-unit icon canvas (on its background) with `full`.
//
// The stripes are horizontal bands in a square rotated 45° behind the page,
// which clips them: a band edge at y' (from the square's centre) lands on the
// line x + y = (page centre x + y) + y'·√2.

export function Logo({ size = 96, palette, full }: { size?: number; palette?: Palette; full?: boolean }) {
  const theme = useTheme();
  const c = logoColors(palette ?? theme);
  const crop = full ? { x: 0, y: 0, w: 108, h: 108 } : { x: 26, y: 24, w: 56, h: 60 };
  const k = size / crop.w;
  const pg = LOGO.page;
  const cx = pg.x + pg.w / 2;
  const cy = pg.y + pg.h / 2;
  const D = 100; // the rotated square's side, enough to cover the page
  const edge = (cut: number) => D / 2 + (cut - (cx + cy)) / Math.SQRT2;
  const [e0, e1] = LOGO.cuts.map(edge);
  const bands = [
    { top: 0, h: e0 },
    { top: e0, h: e1 - e0 },
    { top: e1, h: D - e1 },
  ];
  return (
    <View
      style={{
        width: crop.w * k,
        height: crop.h * k,
        backgroundColor: full ? c.bg : undefined,
        borderRadius: full ? 24 * k : 0,
        overflow: 'hidden',
      }}
    >
      <View
        style={{
          position: 'absolute',
          left: (pg.x - crop.x) * k,
          top: (pg.y - crop.y) * k,
          width: pg.w * k,
          height: pg.h * k,
          borderRadius: pg.r * k,
          overflow: 'hidden',
        }}
      >
        <View
          style={{
            position: 'absolute',
            left: (cx - D / 2 - pg.x) * k,
            top: (cy - D / 2 - pg.y) * k,
            width: D * k,
            height: D * k,
            transform: [{ rotate: '-45deg' }],
          }}
        >
          {bands.map((b, i) => (
            <View
              key={i}
              style={{ position: 'absolute', left: 0, right: 0, top: b.top * k, height: b.h * k, backgroundColor: c.stripes[i] }}
            />
          ))}
        </View>
      </View>
      {LOGO.rings.map((r, i) => (
        <View
          key={i}
          style={{
            position: 'absolute',
            left: (r.x - crop.x) * k,
            top: (r.y - crop.y) * k,
            width: r.w * k,
            height: r.h * k,
            borderRadius: r.r * k,
            backgroundColor: c.rings,
          }}
        />
      ))}
    </View>
  );
}
