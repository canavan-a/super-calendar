# release/

Local signing material and the guided release tool for the Android app,
ported from `../super-atlas/release/`. Only the **sideload** build exists for
now (the `sideload` Gradle flavor in `app/android/app/build.gradle`): a
signed APK published as a GitHub Release, which the app's Settings → Check
for updates installs.

Tracked in git: `release.sh`, `gen-keystore.sh`, `README.md`, `.gitignore`.
**Never** committed: `release.keystore`, `keystore.env`, `app.env`, or any
built `.apk`.

## First-time setup

```
./release/gen-keystore.sh
```

Generates `release.keystore` (a real Android signing key, valid ~27 years)
and `keystore.env` (its passwords). Refuses to run again once these exist —
regenerating would produce a *different* key, and Android won't install an
update signed by a different key over an app installed from the old one, so
losing/replacing this keystore means every existing install can never again
be updated in place. **Back up `release.keystore` and `keystore.env`
somewhere durable outside this checkout.**

The server URL the app is built for goes in `app.env`
(`SUPERCALENDAR_API_URL=https://calendar.example.com`); `release.sh` asks for
it the first time. It must be https.

## Cutting a release

```
./release/release.sh
```

(or `npm run release` from `app/`)

The script checks the toolchain, sources `keystore.env` and `app.env`, lists
existing `app-v*` tags, prompts for the new tag (e.g. `app-v0.1.0`) and a
`versionCode`, runs `./gradlew assembleSideloadRelease`, then — after a
single confirmation — creates and pushes the tag and publishes a GitHub
Release with the signed APK attached (named `supercalendar-<tag>.apk`,
matching `app/src/appUpdater.ts`'s `APK_NAME` regex so the in-app updater
finds it). The signed APK is also left at `release/supercalendar-<tag>.apk`.

The in-app updater reads releases from the public GitHub API, so the repo
must be public (or the updater taught to authenticate) for it to work.

Assumes `git`, `java`, `node`, the Android SDK, and `gh` (authenticated).
`nix develop` provides node and gh; the JDK and Android SDK come from the
system, as for super-atlas.
