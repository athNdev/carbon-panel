# SPDX-License-Identifier: MIT AND MPL-2.0
# This file includes DiscoPanel material under the MIT License,
# Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
# and still applies to that inherited material.
# Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
# If a copy of the MPL was not distributed with this file, You can obtain
# one at https://mozilla.org/MPL/2.0/.


{
  description = "MineServer - The Minecraft server manager that works";
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs =
    { nixpkgs, ... }:
    let
      system = "x86_64-linux";
      pkgs = nixpkgs.legacyPackages.${system};
    in
    {
      devShells.${system}.default = pkgs.mkShell {
        buildInputs = with pkgs; [
          go
          nodejs
          buf
          gnumake
        ];
      };
    };
}
