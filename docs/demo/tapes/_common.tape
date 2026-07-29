# Shared VHS settings for Deployah demos.
# Sourced by docs/demo/tapes/*.tape (files starting with _ are not rendered).
# Run demos from the repo root: nix run .#demo

Set Shell "bash"
Set FontSize 16
Set Margin 10
Set Padding 10
Set BorderRadius 15
Set Width 1400
Set Height 700
Set Theme "Dracula"
Set WindowBar Colorful
Set TypingSpeed 55ms
Set CursorBlink false
Set Framerate 12
# Kind create + first deploy can take several minutes (default WaitTimeout is 15s).
Set WaitTimeout 15m
