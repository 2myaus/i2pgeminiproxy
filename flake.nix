{
  outputs = inputs @ {
    flake-parts,
    nixpkgs,
    ...
  }:
    flake-parts.lib.mkFlake {inherit inputs;} {
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];

      perSystem = {pkgs, ...}: {
        devShells.default = pkgs.mkShell {
          buildInputs = [
            pkgs.openssl
            pkgs.go
            pkgs.gopls
          ];
        };
      };
    };
}
