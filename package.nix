# Copyright 2026 Roxy Light and Benjamin Pollack
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is furnished
# to do so, subject to the following conditions:
# 
# The above copyright notice and this permission notice (including the next
# paragraph) shall be included in all copies or substantial portions of the
# Software.
# 
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
# FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS
# OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
# WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF
# OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
#
# SPDX-License-Identifier: MIT

{ buildGoModule
, nix-gitignore
, lib
}:

let
  version = "0.1.0";

  root = ./.;
  patterns = nix-gitignore.withGitignoreFile extraIgnores root;
  extraIgnores = [
    "*.nix"
    "flake.lock"
    "/.github/"
    ".vscode/"
    ".jj/"
    "result"
    "result-*"
  ];
  src = builtins.path {
    name = "jj-domino";
    path = root;
    filter = nix-gitignore.gitignoreFilterPure (_: _: true) patterns root;
  };
in
  buildGoModule {
    pname = "jj-domino";
    inherit version;

    inherit src;

    vendorHash = "sha256-TOIZ4WX8bZIZtXnH21zU6wB+e29HAn4f4oqdb1/MoLc=";

    subPackages = ["."];
    goSum = builtins.readFile ./go.sum;
    ldflags = ["-s" "-w" "-X=main.jjDominoVersion=${version}"];

    meta = {
      description = "Pull request stack manager for Jujutsu";
      homepage = "https://github.com/zombiezen/jj-domino";
      license = lib.licenses.mit;
    };
  }
