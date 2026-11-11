# From the repo root:
#   go build -o docs/demo/bin/tscatalog ./cmd/tscatalog
#   PATH="$PWD/docs/demo/bin:$PATH" vhs docs/demo/tscatalog.tape

Output docs/demo/tscatalog.gif

Require tscatalog

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

Type "tscatalog -f demo.yaml list"
Sleep 250ms
Enter
Sleep 2s

Type "tscatalog -f demo.yaml describe web"
Sleep 250ms
Enter
Sleep 2s

Type "tscatalog -f demo.yaml ps"
Sleep 250ms
Enter
Sleep 2s

Type "tscatalog -f demo.yaml health"
Sleep 250ms
Enter
Sleep 3s
