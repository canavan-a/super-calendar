# Building this app

This app builds like ../super-atlas: a committed `android/` project edited by
hand, the React Native CLI and Gradle flavors (only `sideload` for now).
Don't run `expo prebuild`, add config plugins, or use EAS: there is no
`app.config.js` and the native folder is not generated.

It does use Expo SDK libraries (expo-secure-store, expo-status-bar). Expo HAS
CHANGED: read the exact versioned docs at
https://docs.expo.dev/versions/v57.0.0/ before writing code against them.
