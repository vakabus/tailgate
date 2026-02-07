{ lib, buildGoModule }:

buildGoModule {
  pname = "tailgate";
  version = "unstable-2026-02-07";

  src = lib.cleanSource ../.;
  subPackages = [ "." ];

  vendorHash = "sha256-ZdrtsSsMXDO+AJKOudKUkJ1SiMl9QFhdMOElcrB6ztw=";

  postInstall = ''
    if [ -e "$out/bin/coredns" ] && [ ! -e "$out/bin/tailgate" ]; then
      mv "$out/bin/coredns" "$out/bin/tailgate"
      ln -s "$out/bin/tailgate" "$out/bin/coredns"
    fi
  '';
}
