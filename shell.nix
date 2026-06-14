{ pkgs ? import <nixpkgs> { } }:

# Dev shell: provides Go (build/test) and mpv (playback).
pkgs.mkShell {
  buildInputs = [
    pkgs.go
    pkgs.mpv
  ];
}
