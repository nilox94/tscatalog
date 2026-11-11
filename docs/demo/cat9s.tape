# From the repo root:
#   go build -o docs/demo/bin/cat9s ./cmd/cat9s
#   PATH="$PWD/docs/demo/bin:$PATH" vhs docs/demo/cat9s.tape
# docs/demo/bin must precede any other cat9s on PATH.
#
# Every row is unpublished, so a restart cannot republish.

Output docs/demo/cat9s.gif

Require cat9s

Set Shell "bash"
Set FontSize 18
Set Width 1100
Set Height 640
Set Padding 20
Set TypingSpeed 35ms
Set Framerate 24
Set Theme "Catppuccin Mocha"

# vhs inherits NO_COLOR and TERM=dumb from the agent shell. Those strip ANSI.
Hide
Type@0 "unset NO_COLOR" Enter
Type@0 "export TERM=xterm-256color COLORTERM=truecolor" Enter
Type@0 "export PS1='$ '" Enter
Type@0 "python3 -m http.server 59991 --bind 127.0.0.1 >/tmp/tscat-demo-http.log 2>&1 &" Enter
Sleep 400ms
Type@0 "cd docs/demo/fixture" Enter
Type@0 "clear" Enter
Show

Type "cat9s -f demo.yaml"
Sleep 200ms
Enter
Sleep 2s

Type "j"
Sleep 700ms
Enter
Sleep 1200ms
Escape
Sleep 600ms

Type "/"
Sleep 300ms
Type "web"
Sleep 500ms
Enter
Sleep 700ms

Type "l"
Sleep 1200ms
Escape
Sleep 600ms

Type "q"
Sleep 1s
