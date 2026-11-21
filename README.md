# super-calendar

A calendar app: a Go server (`server/`) and a React Native Android app
(`app/`), distributed by sideloading (`release/`). Set up the same way as
super-atlas.

```sh
nix develop          # prints the dev commands
run-supercalendar    # the server on 127.0.0.1:8090 with hot reload
run-app-android      # the app on an emulator (talks to 10.0.2.2:8090)
run-tests
```

In dev mode, sign-in codes are printed to the server log instead of emailed.

- `server/` — Go, chi, SQLite (modernc, migrations embedded), email-code sign-in.
- `app/` — see `app/README.md`.
- `release/` — signing and the guided APK release; see `release/README.md`.
- `module.nix` — the NixOS module (`services.supercalendar`).
- `PLAN.md` — the scaffolding plan and what comes next.
