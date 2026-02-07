{ lib, pkgs, config, ... }:

let
  cfg = config.services.tailgate;

  defaultPackage = pkgs.callPackage ./package.nix { };

  mkService = name: path: {
    description = "Tailgate (CoreDNS-based) instance ${name}";
    wantedBy = [ "multi-user.target" ];
    after = [ "network-online.target" ];
    wants = [ "network-online.target" ];
    serviceConfig = {
      ExecStart = "${cfg.package}/bin/tailgate -conf ${path}";
      Restart = "on-failure";
      User = "tailgate";
      Group = "tailgate";
      AmbientCapabilities = [ "CAP_NET_BIND_SERVICE" ];
      CapabilityBoundingSet = [ "CAP_NET_BIND_SERVICE" ];
      NoNewPrivileges = true;
      PrivateDevices = true;
      PrivateTmp = true;
      ProtectSystem = "strict";
      ProtectHome = "read-only";
      ProtectControlGroups = true;
      ProtectKernelTunables = true;
      ProtectKernelModules = true;
      ProtectClock = true;
      ProtectHostname = true;
      LockPersonality = true;
      MemoryDenyWriteExecute = true;
      RestrictSUIDSGID = true;
      RestrictRealtime = true;
      RestrictNamespaces = true;
      RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
      ProcSubset = "pid";
      ProtectProc = "invisible";
      UMask = "0077";
      ReadWritePaths = [ "/run/tailscale" ];
    };
  };

  instances = lib.mapAttrs' (name: path: {
    name = "tailgate-${name}";
    value = mkService name path;
  }) cfg.configFiles;
in
{
  options.services.tailgate = {
    enable = lib.mkEnableOption "Tailgate (CoreDNS-based DNS server)";

    package = lib.mkOption {
      type = lib.types.package;
      default = defaultPackage;
      defaultText = "pkgs.callPackage ./nix/package.nix { }";
      description = "Tailgate package to use.";
    };

    configFiles = lib.mkOption {
      type = lib.types.attrsOf lib.types.path;
      default = { };
      example = lib.literalExpression ''
        {
          proxy = pkgs.writeText "Corefile.proxy" "...";
          normal = pkgs.writeText "Corefile.normal" "...";
        }
      '';
      description = ''
        Attribute set mapping instance names to Corefile paths.
        Each entry produces a systemd service named `tailgate-<name>.service`.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.configFiles != { };
        message = "services.tailgate.configFiles must contain at least one entry.";
      }
    ];

    systemd.services = instances;

    users.users.tailgate = {
      isSystemUser = true;
      group = "tailgate";
      description = "Tailgate (CoreDNS) service user";
    };
    users.groups.tailgate = { };
  };
}
