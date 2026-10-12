{ pkgs, lib, config, inputs, ... }:

let
  # Devenv's rolling Nixpkgs provides Go 1.27.1, which is too old for this
  # module. Overriding only the compiler to 1.27.2 also makes its older go-tools
  # fail on incompatible export data, so pin Go and its tools as one package set.
  goPkgs = inputs.go-nixpkgs.legacyPackages.${pkgs.stdenv.hostPlatform.system};
in {
  # https://devenv.sh/basics/
  env.GREET = "devenv";

  # https://devenv.sh/packages/
  packages = [
    pkgs.git
    pkgs.git-absorb
	  goPkgs.go_1_27
	  goPkgs.delve
	  goPkgs.gopls
	  goPkgs.gotools
	  goPkgs.gomodifytags
	  goPkgs.impl
	  goPkgs.go-tools
	  goPkgs.gotests
	  goPkgs.iferr
  ];

  # https://devenv.sh/languages/
  # languages.rust.enable = true;
  env.GOROOT = "${goPkgs.go_1_27}/share/go";
  env.GOPATH = config.env.DEVENV_STATE + "/go";
  env.GOTOOLCHAIN = "local";
  hardeningDisable = [ "fortify" ];

  # https://devenv.sh/processes/
  # processes.dev.exec = "${lib.getExe pkgs.watchexec} -n -- ls -la";

  # https://devenv.sh/services/
  # services.postgres.enable = true;

  # https://devenv.sh/scripts/
  scripts.hello.exec = ''
    echo hello from $GREET
  '';

  # https://devenv.sh/basics/
  enterShell = ''
    export PATH=$GOPATH/bin:$PATH
    hello         # Run scripts directly
    git --version # Use packages
  '';

  # https://devenv.sh/tasks/
  # tasks = {
  #   "myproj:setup".exec = "mytool build";
  #   "devenv:enterShell".after = [ "myproj:setup" ];
  # };

  # https://devenv.sh/tests/
  enterTest = ''
    echo "Running tests"
    git --version | grep --color=auto "${pkgs.git.version}"
  '';

  # https://devenv.sh/git-hooks/
  # git-hooks.hooks.shellcheck.enable = true;

  # See full reference at https://devenv.sh/reference/options/
}
