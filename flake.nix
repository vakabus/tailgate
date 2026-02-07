{
  description = "Tailgate (CoreDNS-based DNS server and Tailscale proxy)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [ self.overlays.default ];
        };
      in
      {
        packages.tailgate = pkgs.tailgate;
        packages.default = pkgs.tailgate;

        devShells.default = pkgs.mkShell {
          buildInputs = [ pkgs.go ];
        };
      }
    ) // {
      overlays.default = final: prev: {
        tailgate = final.callPackage ./nix/package.nix { };
      };

      nixosModules.tailgate = import ./nix/module.nix;
    };
}
