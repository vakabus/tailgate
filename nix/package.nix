{ lib, buildGoModule }:

buildGoModule {
  pname = "tailgate";
  version = "unstable-2026-06-23.1";

  src = lib.cleanSource ../.;
  subPackages = [ "." ];

  vendorHash = "sha256-qnm6Zrp2AQJOUFmtMnIf06Q9KzZaneiNZqsXFIP+870=";

  # Run the tests for our custom plugins as part of the build. Upstream CoreDNS
  # components are assumed tested upstream, so we only check our additions.
  # -race needs cgo; the stdenv already provides a C compiler.
  doCheck = true;
  env.CGO_ENABLED = "1";
  checkPhase = ''
    runHook preCheck
    go test -race -count=1 \
      ./plugin/tsproxy/... \
      ./plugin/tailscale/... \
      ./plugin/tsnames/...
    runHook postCheck
  '';

  postInstall = ''
    if [ -e "$out/bin/coredns" ] && [ ! -e "$out/bin/tailgate" ]; then
      mv "$out/bin/coredns" "$out/bin/tailgate"
      ln -s "$out/bin/tailgate" "$out/bin/coredns"
    fi
  '';
}
