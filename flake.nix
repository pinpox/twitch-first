{
  description = "Twitch 'first' channel point redemption tracker";

  inputs.nixpkgs.url = "nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      lastModifiedDate = self.lastModifiedDate or self.lastModified or "19700101";
      version = builtins.substring 0 8 lastModifiedDate;
      supportedSystems = [ "x86_64-linux" "x86_64-darwin" "aarch64-linux" "aarch64-darwin" ];
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;
      nixpkgsFor = forAllSystems (system: import nixpkgs { inherit system; });
    in
    {
      packages = forAllSystems (system:
        let
          pkgs = nixpkgsFor.${system};
        in
        {
          twitch-first = pkgs.buildGoModule {
            pname = "twitch-first";
            inherit version;
            src = ./.;
            vendorHash = "sha256-TK1bWff8mAymsEh1+oBtlA6SysBcUvmHD45pIFDkrEg=";
          };
        });

      defaultPackage = forAllSystems (system: self.packages.${system}.twitch-first);
    };
}
