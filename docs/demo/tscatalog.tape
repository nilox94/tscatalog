# From the repo root:
#   docs/demo/record tscatalog
#
# record installs docs/demo/redact.py as bash and runs vhs. That bash deletes
# the live MagicDNS suffix from the shell output before ttyd draws a frame.
# Do not run vhs on this file directly.
#
# Hidden setup copies docs/demo/tailscale onto PATH ahead of the real CLI.
# That stub returns an empty MagicDNS suffix, so the program prints <tailnet>.
# The bash filter is what keeps a real suffix out of the GIF. Do not remove it.

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
Type@0 'test -n "$TAPE_REDACT_FILE" || exit 1' Enter
Type@0 "unset NO_COLOR TS_TAILNET" Enter
Type@0 "export TERM=xterm-256color COLORTERM=truecolor" Enter
Type@0 "export PS1='$ '" Enter
Type@0 "mkdir -p /tmp/tscat-demo-bin" Enter
Type@0 "cp $PWD/docs/demo/tailscale /tmp/tscat-demo-bin/tailscale && chmod +x /tmp/tscat-demo-bin/tailscale || exit 1" Enter
Type@0 "export PATH=/tmp/tscat-demo-bin:$PWD/docs/demo/bin:$PATH" Enter
Type@0 "command -v tailscale | grep -qx /tmp/tscat-demo-bin/tailscale || exit 1" Enter
Type@0 "command -v tscatalog | grep -qx $PWD/docs/demo/bin/tscatalog || exit 1" Enter
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
