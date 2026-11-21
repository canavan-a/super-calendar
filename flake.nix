{
  description = "Super Calendar: dev shell + NixOS module";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-26.05";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    let
      perSystem = flake-utils.lib.eachDefaultSystem (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};

          # $PWD is resolved at runtime, so these scripts must be run from the
          # repo root (always `cd` into super-calendar/ before `nix develop`).
          repoRoot = "$PWD";
          dataDir = "${repoRoot}/.data";
          # 8090, not 8080, so it can run next to super-atlas's dev server.
          listenAddr = "127.0.0.1:8090";

          # Dev mode: sign-in codes are printed to the log instead of emailed
          # (unless SMTP is set in server/.env).
          devEnv = ''
            export SUPERCALENDAR_LISTEN_ADDR="${listenAddr}"
            export SUPERCALENDAR_DATA_DIR="${dataDir}"
            export SUPERCALENDAR_DEV=1
          '';

          db-reset = pkgs.writeShellScriptBin "db-reset" ''
            set -e
            rm -rf "${dataDir}"
            echo "removed ${dataDir} (a fresh database is created on next start)"
          '';

          # foreground, hot-reload via air (see server/.air.toml)
          run-supercalendar = pkgs.writeShellScriptBin "run-supercalendar" ''
            set -e
            mkdir -p "${dataDir}"
            ${devEnv}
            cd "${repoRoot}/server"
            exec ${pkgs.air}/bin/air
          '';

          # The local server's cap on accounts, e.g. `supercalendar-admin cap 200`.
          supercalendar-admin = pkgs.writeShellScriptBin "supercalendar-admin" ''
            set -e
            export SUPERCALENDAR_DATA_DIR="${dataDir}"
            cd "${repoRoot}/server"
            exec ${pkgs.go}/bin/go run ./cmd/supercalendar-admin "$@"
          '';

          dev-up = pkgs.writeShellScriptBin "dev-up" ''
            set -e
            mkdir -p "${dataDir}/logs"
            ${devEnv}
            (cd "${repoRoot}/server" && \
              ${pkgs.go}/bin/go run ./cmd/supercalendar-server > "${dataDir}/logs/supercalendar.log" 2>&1 &)
            echo "supercalendar: http://${listenAddr} (log: ${dataDir}/logs/supercalendar.log)"
            echo "run 'dev-down' to stop it"
          '';

          dev-down = pkgs.writeShellScriptBin "dev-down" ''
            pkill -f "cmd/supercalendar-server" 2>/dev/null || true
            pkill -f "go-build.*supercalendar" 2>/dev/null || true
            pkill -f "tmp/supercalendar-server" 2>/dev/null || true
            echo "stopped supercalendar (best-effort)"
          '';

          # Needs the system-wide Android SDK already on this machine's PATH;
          # nothing Android-related is added to this flake on purpose (as in
          # super-atlas). An emulator reaches the dev server at 10.0.2.2:8090,
          # the build's default; for a phone, pass -PapiUrl (see app/README.md).
          run-app-android = pkgs.writeShellScriptBin "run-app-android" ''
            set -e
            cd "${repoRoot}/app"
            exec ${pkgs.nodejs_22}/bin/npm run android -- "$@"
          '';

          run-tests = pkgs.writeShellScriptBin "run-tests" ''
            set -e
            (cd "${repoRoot}/server" && ${pkgs.go}/bin/go test ./...)
            if [ -d "${repoRoot}/app/node_modules" ]; then
              (cd "${repoRoot}/app" && ${pkgs.nodejs_22}/bin/npx tsc --noEmit)
            fi
          '';

          printHelp = ''
            echo "super-calendar dev shell"
            echo "  run-supercalendar   # foreground: the server with hot reload (air) on ${listenAddr}"
            echo "  dev-up / dev-down   # start/stop the server headless (logs in .data/logs)"
            echo "  db-reset            # delete the local database"
            echo "  supercalendar-admin # cap [<n>] | list | remove <email>: the cap on accounts"
            echo "  run-app-android     # the app on a connected device/emulator (needs the system Android SDK)"
            echo "  run-tests           # server tests and the app's typecheck"
            echo "  dev-help            # re-print this list"
          '';

          dev-help = pkgs.writeShellScriptBin "dev-help" printHelp;
        in
        {
          devShells.default = pkgs.mkShell {
            buildInputs = [
              pkgs.go
              pkgs.gopls
              pkgs.air
              pkgs.sqlite
              pkgs.nodejs_22
              pkgs.watchman
              pkgs.gh
              db-reset
              supercalendar-admin
              run-supercalendar
              dev-up
              dev-down
              run-app-android
              run-tests
              dev-help
            ];
            shellHook = printHelp;
          };
        }
      );

      # No buildGoModule package on purpose, the same tradeoff as super-atlas:
      # the module builds the server from source in an activation script
      # instead of keeping a vendorHash in sync by hand.
      nixosModules.default = import ./module.nix { inherit self; };
    in
    perSystem // { inherit nixosModules; };
}
