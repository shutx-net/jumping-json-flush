{
  description = "jjf: manage database design specifications as JSON and generate Excel design documents";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      inherit (nixpkgs) lib;

      # The platforms the release workflow builds, minus windows/amd64, which nix
      # has no host platform for. Nothing below is platform specific: jjf is pure
      # Go and is linked without cgo.
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];

      forAllSystems = f: lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      # The go directive of go.mod is the single source of the Go version, the
      # same contract CI relies on through actions/setup-go's go-version-file.
      # Nix cannot honour it by fetching a toolchain, because the toolchain is
      # whatever the locked nixpkgs ships, so the directive is asserted instead: a
      # nixpkgs bump that regresses below it then fails during evaluation, with a
      # message, rather than somewhere inside the compiler.
      goDirective =
        let
          directives = builtins.filter (line: lib.hasPrefix "go " line) (
            lib.splitString "\n" (builtins.readFile ./go.mod)
          );
        in
        lib.removePrefix "go " (builtins.head directives);

      goSatisfiesGoMod =
        pkgs:
        let
          complaint = "go.mod requires go >= ${goDirective}, but this nixpkgs provides go ${pkgs.go.version}";
        in
        lib.assertMsg (lib.versionAtLeast pkgs.go.version goDirective) complaint;

      # jjf keeps no VERSION file. The tool version lives in the plugin manifest,
      # which the release workflow gates against the tag it publishes, so reading
      # it here avoids inventing a second answer.
      toolVersion = (builtins.fromJSON (builtins.readFile ./.claude-plugin/plugin.json)).version;

      # A nix build sees no VCS metadata, because its source is a store path and
      # not a clone, so the version the go command stamps from git is unavailable
      # and -ldflags is the only way in. The revision is part of the string
      # because a flake is built from any commit, tagged or not, and a bare
      # "v0.1.0" would claim to be that release.
      buildVersion = "v${toolVersion}+nix.${self.shortRev or self.dirtyShortRev or "unknown"}";

      # One definition, shared by packages and checks. Calling it twice yields the
      # same derivation, so `nix flake check` verifies exactly what `nix build`
      # produces.
      jjfFor =
        pkgs:
        assert goSatisfiesGoMod pkgs;
        pkgs.buildGoModule {
          pname = "jjf";
          version = toolVersion;

          # The whole tree, deliberately unfiltered. The test suite reaches
          # outside the packages it covers: skills/ reads ../README.md,
          # ../README.ja.md and ../docs/db-design-guide.ja.md, and internal/schema
          # reads ../../examples/db-design.example.json. A source filter trimmed
          # to what the compiler needs would leave those tests failing.
          src = ./.;

          vendorHash = "sha256-0Nkz66ahGO/kChkCc/e63mkx6EhzMNFKji1uq00Y1fY=";

          # subPackages is deliberately unset. cmd/jjf is the only main package,
          # so the build produces the same single binary either way, while the
          # check phase gets to run the whole `go test ./...` suite instead of one
          # package's tests. skills/ holds tests and no non-test sources, which
          # the go builder tolerates.

          # The distributed binaries are static and so is this one. The race
          # detector is what that costs, since it requires cgo, and CI runs it
          # separately.
          env.CGO_ENABLED = 0;

          # -trimpath and -buildid= are added by buildGoModule itself.
          ldflags = [
            "-s"
            "-w"
            "-X main.version=${buildVersion}"
          ];

          meta = {
            description = "Manage database design specifications as JSON and generate Excel design documents";
            homepage = "https://github.com/shutx-net/jumping-json-flush";
            license = lib.licenses.mit;
            mainProgram = "jjf";
          };
        };
    in
    {
      packages = forAllSystems (pkgs: rec {
        jjf = jjfFor pkgs;
        default = jjf;
      });

      devShells = forAllSystems (pkgs: {
        default =
          assert goSatisfiesGoMod pkgs;
          pkgs.mkShell {
            packages = [
              pkgs.go
              # The language server the devcontainer's Go extension installs.
              pkgs.gopls
              # staticcheck. nixpkgs ships the same 2026.1 release CI pins, which
              # is what makes `staticcheck ./...` here and the `go run
              # honnef.co/go/tools/cmd/staticcheck@2026.1` line in ci.yml agree.
              pkgs.go-tools
              pkgs.gh
              # The release workflow checks the plugin manifests with jq.
              pkgs.jq
              # install.sh is the only shell this repository ships as a product.
              # CI lints it with the shellcheck that comes on the runner; this is
              # the same linter available locally.
              pkgs.shellcheck
            ];

            # Nix pins the toolchain, so the go command must not quietly download
            # a different one. Prefix GOTOOLCHAIN=auto for the one command that
            # needs the opposite: `go run honnef.co/go/tools/cmd/...@version`,
            # whose module may require a newer go than jjf targets.
            GOTOOLCHAIN = "local";

            # CGO_ENABLED is left alone on purpose: `go test -race` requires cgo,
            # and a shell that pins it to 0 takes the race detector away.
          };
      });

      # `nix flake check` builds the package, whose check phase is the test suite.
      checks = forAllSystems (pkgs: {
        jjf = jjfFor pkgs;
      });

      # nixfmt-tree rather than nixfmt itself: `nix fmt` with no arguments passes
      # no path either, and bare nixfmt then reads its empty stdin and fails.
      formatter = forAllSystems (pkgs: pkgs.nixfmt-tree);
    };
}
