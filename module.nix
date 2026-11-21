# NixOS module for the Super Calendar server, following super-atlas's.
#
# The server is built by an activation script rather than a buildGoModule
# derivation: keeping a vendorHash in sync by hand on every go.sum change
# costs more than it's worth for a binary with no reproducibility
# requirement. It rebuilds from ${self}/server on every activation; goCache
# keeps that fast after the first run.
{ self }:
{ config, lib, pkgs, ... }:
let
  cfg = config.services.supercalendar;
  stateDir = "/var/lib/supercalendar";
  serverBin = "${stateDir}/bin/supercalendar-server";
  adminBin = "${stateDir}/bin/supercalendar-admin";
  goCache = "/var/cache/supercalendar-go";

  # `supercalendar-admin cap <n>` etc. against the running service's database.
  adminWrapper = pkgs.writeShellScriptBin "supercalendar-admin" ''
    export SUPERCALENDAR_DATA_DIR="${cfg.dataDir}"
    exec ${pkgs.sudo}/bin/sudo -u supercalendar -E ${adminBin} "$@"
  '';
in
{
  options.services.supercalendar = {
    enable = lib.mkEnableOption "the Super Calendar server";

    listenAddr = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:8090";
      description = ''
        Address for the API to listen on. Loopback by default: put a tunnel
        (cloudflared) or reverse proxy in front of it for TLS.
      '';
    };

    dataDir = lib.mkOption {
      type = lib.types.str;
      default = "${stateDir}/data";
      description = "Where the SQLite database lives.";
    };

    trustProxy = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = ''
        Rate-limit by the client address a local tunnel reports: the
        rightmost X-Forwarded-For entry, believed only on loopback
        connections, so clients can't forge it. Turn off if clients connect
        to the server directly.
      '';
    };

    environmentFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "/run/secrets/supercalendar.env";
      description = ''
        File with the SMTP account sign-in codes are sent from:
        SUPERCALENDAR_SMTP_EMAIL=… and SUPERCALENDAR_SMTP_APP_PASSWORD=… (an
        app password), plus SUPERCALENDAR_SMTP_HOST=… and
        SUPERCALENDAR_SMTP_PORT=… for anything but Gmail (default
        smtp.gmail.com, 587). Without it nobody can sign in.

        SUPERCALENDAR_INVITE_CODE=… makes it invite-only: a new account must
        give that code (case, spaces and dashes don't matter) before a code is
        emailed. Existing accounts, and anyone with a pending calendar invite,
        don't need it. Unset, anyone can sign up until there are 50 accounts;
        `supercalendar-admin cap <n>` changes that cap either way. Give a runtime path as a string, not a Nix path literal (which
        would copy it into the store).
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    users.users.supercalendar = {
      isSystemUser = true;
      group = "supercalendar";
      home = stateDir;
    };
    users.groups.supercalendar = { };

    environment.systemPackages = [ adminWrapper ];

    systemd.tmpfiles.rules = [
      "d ${stateDir} 0750 supercalendar supercalendar -"
      "d ${stateDir}/bin 0755 root root -"
      "d ${cfg.dataDir} 0700 supercalendar supercalendar -"
      "d ${goCache} 0755 root root -"
    ];

    system.activationScripts.supercalendarBuild = {
      deps = [ ];
      text = ''
        export PATH=${pkgs.go}/bin:${pkgs.bash}/bin:$PATH
        export HOME=${goCache}
        export GOCACHE=${goCache}/build
        export GOPATH=${goCache}/path
        # modernc sqlite is pure Go; no C toolchain needed.
        export CGO_ENABLED=0
        mkdir -p ${stateDir}/bin ${goCache}
        cd ${self}/server
        ${pkgs.go}/bin/go build -buildvcs=false -o ${serverBin}.new ./cmd/supercalendar-server
        mv -f ${serverBin}.new ${serverBin}
        ${pkgs.go}/bin/go build -buildvcs=false -o ${adminBin}.new ./cmd/supercalendar-admin
        mv -f ${adminBin}.new ${adminBin}
        ${pkgs.systemd}/bin/systemctl try-restart supercalendar.service || true
      '';
    };

    systemd.services.supercalendar = {
      description = "Super Calendar server";
      after = [ "network.target" ];
      wantedBy = [ "multi-user.target" ];
      environment = {
        SUPERCALENDAR_LISTEN_ADDR = cfg.listenAddr;
        SUPERCALENDAR_DATA_DIR = cfg.dataDir;
        SUPERCALENDAR_TRUST_PROXY = if cfg.trustProxy then "1" else "0";
      };
      serviceConfig = {
        ExecStart = serverBin;
        Restart = "always";
        RestartSec = 2;
        User = "supercalendar";
        Group = "supercalendar";
        WorkingDirectory = cfg.dataDir;
        NoNewPrivileges = true;
        PrivateTmp = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        ReadWritePaths = [ cfg.dataDir ];
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectControlGroups = true;
        RestrictSUIDSGID = true;
        LockPersonality = true;
        MemoryDenyWriteExecute = true;
        RestrictAddressFamilies = [ "AF_INET" "AF_INET6" "AF_UNIX" ];
        CapabilityBoundingSet = "";
        UMask = "0077";
      } // lib.optionalAttrs (cfg.environmentFile != null) {
        EnvironmentFile = cfg.environmentFile;
      };
    };
  };
}
