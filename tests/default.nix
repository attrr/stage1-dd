# tests/default.nix
{
  pkgs,
  lib,
  mkRescue,
  failoverPkg,
}:
let
  runEvalTest =
    name: testSuite:
    pkgs.runCommand "test-${name}"
      {
        isPassed = testSuite == [ ];
        results = builtins.toJSON testSuite;
        passAsFile = [ "results" ];
      }
      ''
        if [ -n "$isPassed" ]; then
          touch $out
        else
          echo "Test failures in ${name}:"
          cat $resultsPath
          exit 1
        fi
      '';

  stage0-eval = runEvalTest "stage0" (import ./stage0-test.nix { inherit pkgs lib; });
  stage1-eval = runEvalTest "stage1" (import ./stage1-test.nix { inherit pkgs lib; });
  mkrescue-eval = runEvalTest "mkrescue" (
    import ./mkrescue-test.nix {
      inherit pkgs lib mkRescue;
    }
  );

  # ── Generate test SSH keys at build time ───────────────────────
  # Host key: single-file derivation ($out IS the key file, not a directory).
  # This keeps it as a Nix path type, which boot.initrd.secrets accepts.
  testHostKey =
    pkgs.runCommand "test-host-ed25519-key"
      {
        nativeBuildInputs = [ pkgs.openssh ];
      }
      ''
        ssh-keygen -t ed25519 -N "" -f key -C "test-host-key"
        cp key $out
      '';

  # User key: we only need the .pub for authorizedKeys
  testUserPub =
    pkgs.runCommand "test-user-ed25519-pub"
      {
        nativeBuildInputs = [ pkgs.openssh ];
      }
      ''
        ssh-keygen -t ed25519 -N "" -f key -C "test-user-key"
        cp key.pub $out
      '';

  # ── Integration tests with real SSH config ────────────────────
  defaultRescue = mkRescue {
    system = pkgs.stdenv.hostPlatform.system;
    ssh = {
      authorizedKeys = [ (builtins.readFile testUserPub) ];
      hostKeys = [ testHostKey ];
    };
    extraModules = [
      {
        stage1.debug = true;
      }
    ];
  };
  integrationTests = import ./integration-test.nix {
    inherit pkgs;
    rescueSystem = defaultRescue;
  };

  # Split Failover Tests
  failover-tests = import ./failover-test.nix {
    inherit pkgs lib failoverPkg;
    failoverModule = ../modules/failover;
  };
in
{
  inherit
    stage0-eval
    stage1-eval
    mkrescue-eval
    ;

  # Embeded tests
  integration-256mb = integrationTests.ram-256mb;
  integration-512mb = integrationTests.ram-512mb;
  integration-1gb = integrationTests.ram-1gb;
  integration-2gb = integrationTests.ram-2gb;

  # Split (separated) initrd/squashfs tests
  integration-separated-128mb = integrationTests.ram-separated-128mb;
  integration-separated-256mb = integrationTests.ram-separated-256mb;
  integration-separated-512mb = integrationTests.ram-separated-512mb;

  # Failover Trigger Test (Main -> Rescue)
  failover-trigger = failover-tests.failover-trigger;
  failover-grub-trigger = failover-tests.failover-grub-trigger;
  # Failover Recovery Test (Rescue -> Main)
  failover-recovery = failover-tests.failover-recovery;
  failover-grub-recovery = failover-tests.failover-grub-recovery;
}
