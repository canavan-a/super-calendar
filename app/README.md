# Super Calendar app

Bare React Native (Android), built the way super-atlas is: a committed
`android/` project, the React Native CLI and Gradle, no Expo build tooling.
It does use Expo SDK libraries (`expo-secure-store`, `expo-status-bar`),
linked through Expo's autolinking in `android/settings.gradle`.

```sh
npm ci
npm start                  # Metro
npm run android            # sideload flavor on a device/emulator (system Android SDK)
npm run typecheck
```

The Android build talks to the server it was built for: `-PapiUrl`, by default
`http://10.0.2.2:8090` (the dev server, seen from an emulator). For a phone on
your network: `npm run android -- --extra-params -PapiUrl=http://192.168.1.20:8090`.

## Builds

Only **sideload** for now (`android/app/src/sideload`): the in-app updater
(`AppUpdaterModule.kt`, GitHub Releases, `src/appUpdater.ts`). The
`distribution` flavor dimension is kept so a Play build can be added beside
it later, as in super-atlas.

## Layout

- `src/screens/` — sign in (email code), calendar (placeholder), settings.
- `src/auth/` — the session: request a code, verify it, sign out, delete account.
- `src/api/` — requests with the session token (kept in the secure store).
- `src/palettes.ts`, `src/theme.tsx` — five palettes (light, dark, slate, sepia,
  ember) or System, picked in Settings.

The logo and launcher icons follow the theme. `src/palettes.ts` holds the
logo's shapes and each palette's colours; `npm run icons`
(`tools/gen-icons.mts`) regenerates the per-theme adaptive icons, the
monochrome glyph for Android 13+ themed icons and the PNG fallbacks. Commit
what it writes. The launcher has one `<activity-alias>` per theme and
`AppIconModule.kt` swaps them as the app goes to the background
(`src/appIcon.ts`), as in super-atlas.
