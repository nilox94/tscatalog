#!/usr/bin/env python3
"""Filter bash output for VHS.

ttyd looks up `bash` on PATH and draws whatever that process prints. `record`
installs this file on PATH as `bash`. It reads the live MagicDNS suffix from
TAPE_REDACT_FILE, deletes those bytes from the shell's output, and only then
writes the stream ttyd turns into pixels.

The needle list never includes a replacement that still contains the needle.
A trailing chunk is held back only while it is a prefix of a needle, so a
needle split across reads is removed before it is forwarded.
"""

import os
import select
import signal
import subprocess
import sys
import tempfile
import tty
from pathlib import Path

PLACEHOLDER = "<tailnet>"
SUFFIX_PLACEHOLDER = "<tailnet>.ts.net"


class Redactor:
    def __init__(self, pairs):
        self.pairs = tuple(sorted(pairs, key=lambda item: len(item[0]), reverse=True))
        self.buf = b""

    def feed(self, data):
        if not data:
            return b""
        self.buf += data
        self._replace()
        keep = self._hold()
        if keep <= 0:
            out, self.buf = self.buf, b""
            return out
        if keep >= len(self.buf):
            return b""
        out, self.buf = self.buf[:-keep], self.buf[-keep:]
        return out

    def flush(self):
        self._replace()
        out, self.buf = self.buf, b""
        return out

    def _replace(self):
        for src, dst in self.pairs:
            self.buf = self.buf.replace(src, dst)

    def _hold(self):
        hold = 0
        for src, _dst in self.pairs:
            limit = min(len(self.buf), len(src) - 1)
            n = hold + 1
            while n <= limit:
                if src.startswith(self.buf[-n:]):
                    hold = n
                n += 1
        return hold


def replacement(src):
    if src.endswith(".ts.net"):
        return SUFFIX_PLACEHOLDER
    return PLACEHOLDER


def needle_pairs(text, required):
    pairs = []
    seen = set()
    for raw in text.splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if "\t" in line:
            src, dst = line.split("\t", 1)
        else:
            src, dst = line, replacement(line)
        src = src.strip()
        dst = dst.strip()
        if src in seen:
            continue
        if len(src) < 4:
            raise SystemExit("docs/demo/redact.py: refusing a needle shorter than 4 characters")
        if src in dst:
            raise SystemExit("docs/demo/redact.py: replacement still contains the needle")
        seen.add(src)
        pairs.append((src.encode(), dst.encode()))
    if required and not pairs:
        raise SystemExit("docs/demo/redact.py: no needles")
    return pairs


def parse_needles(text):
    return needle_pairs(text, required=True)


def load_needles(path):
    try:
        text = Path(path).read_text(encoding="utf-8")
    except OSError:
        raise SystemExit("docs/demo/redact.py: cannot read TAPE_REDACT_FILE") from None
    return parse_needles(text)


def add_pair(lines, seen, src, dst):
    src = src.strip().strip(".")
    if len(src) < 4 or src in seen or src in dst:
        return
    seen.add(src)
    lines.append(f"{src}\t{dst}")


def pairs_from_status(data, extra_text=""):
    lines = []
    seen = set()
    current = data.get("CurrentTailnet") or {}
    suffixes = []
    for value in (data.get("MagicDNSSuffix"), current.get("MagicDNSSuffix")):
        if isinstance(value, str) and value.strip("."):
            suffixes.append(value.strip())
    for suffix in suffixes:
        suffix = suffix.strip(".")
        add_pair(lines, seen, suffix, SUFFIX_PLACEHOLDER)
        if suffix.endswith(".ts.net"):
            add_pair(lines, seen, suffix[: -len(".ts.net")], PLACEHOLDER)
    name = current.get("Name")
    if isinstance(name, str):
        name = name.strip()
        if name and name != "-":
            add_pair(lines, seen, name, replacement(name))
            if name.endswith(".ts.net"):
                add_pair(lines, seen, name[: -len(".ts.net")], PLACEHOLDER)
    for src, dst in needle_pairs(extra_text, required=False):
        add_pair(lines, seen, src.decode(), dst.decode())
    if not lines:
        raise SystemExit(
            "docs/demo/record: tailscale returned no MagicDNS suffix; refusing to draw a tape"
        )
    return "\n".join(lines) + "\n"


def discover():
    import json

    try:
        raw = subprocess.check_output(["tailscale", "status", "--json"], stderr=subprocess.DEVNULL)
    except (OSError, subprocess.CalledProcessError):
        raise SystemExit(
            "docs/demo/record: tailscale status --json failed; refusing to draw a tape"
        ) from None
    try:
        data = json.loads(raw)
    except json.JSONDecodeError:
        raise SystemExit(
            "docs/demo/record: tailscale status --json was not JSON; refusing to draw a tape"
        ) from None
    extra = ""
    local = Path(__file__).with_name("redact.local")
    if local.is_file():
        extra = local.read_text(encoding="utf-8")
    sys.stdout.write(pairs_from_status(data, extra))


def real_bash():
    wrapper_dir = Path(sys.argv[0]).resolve().parent
    for directory in os.environ.get("PATH", "").split(":"):
        if not directory:
            continue
        try:
            if Path(directory).resolve() == wrapper_dir:
                continue
        except OSError:
            continue
        candidate = Path(directory, "bash")
        if candidate.is_file() and os.access(candidate, os.X_OK):
            return str(candidate)
    return "/bin/bash"


def write_all(fd, data):
    view = memoryview(data)
    while view:
        try:
            n = os.write(fd, view)
        except InterruptedError:
            continue
        view = view[n:]


def copy_winsize(src, dst):
    try:
        size = fcntl_winsize(src)
        import fcntl
        import termios

        fcntl.ioctl(dst, termios.TIOCSWINSZ, size)
    except OSError:
        return


def fcntl_winsize(fd):
    import array
    import fcntl
    import termios

    buf = array.array("h", [0, 0, 0, 0])
    fcntl.ioctl(fd, termios.TIOCGWINSZ, buf)
    return buf


def proxy(pairs):
    import pty

    bash = real_bash()
    pid, master = pty.fork()
    if pid == 0:
        os.execv(bash, [bash, *sys.argv[1:]])
    stdin_fd = sys.stdin.fileno()
    stdout_fd = sys.stdout.fileno()
    if os.isatty(stdin_fd):
        tty.setraw(stdin_fd)
        copy_winsize(stdin_fd, master)

    def on_winch(_signum, _frame):
        copy_winsize(stdin_fd, master)
        try:
            os.kill(pid, signal.SIGWINCH)
        except OSError:
            pass

    def forward(signum, _frame):
        try:
            os.kill(pid, signum)
        except OSError:
            pass

    signal.signal(signal.SIGWINCH, on_winch)
    signal.signal(signal.SIGTERM, forward)
    signal.signal(signal.SIGHUP, forward)
    redactor = Redactor(pairs)
    watch = [stdin_fd, master]
    while watch:
        try:
            ready, _, _ = select.select(watch, [], [])
        except InterruptedError:
            continue
        if master in ready:
            try:
                data = os.read(master, 65536)
            except OSError:
                data = b""
            if not data:
                write_all(stdout_fd, redactor.flush())
                break
            write_all(stdout_fd, redactor.feed(data))
        if stdin_fd in watch and stdin_fd in ready:
            try:
                data = os.read(stdin_fd, 65536)
            except OSError:
                data = b""
            if not data:
                watch.remove(stdin_fd)
                continue
            try:
                write_all(master, data)
            except OSError:
                break
    _, status = os.waitpid(pid, 0)
    raise SystemExit(os.waitstatus_to_exitcode(status))


def self_test():
    secret = b"zzexample-tailnet.ts.net"
    label = b"zzexample-tailnet"
    pairs = [(secret, SUFFIX_PLACEHOLDER.encode()), (label, PLACEHOLDER.encode())]
    redactor = Redactor(pairs)
    raw = b"https://web." + secret
    out = b""
    for index in range(len(raw)):
        out += redactor.feed(raw[index : index + 1])
    out += redactor.flush()
    if out != b"https://web.<tailnet>.ts.net" or secret in out or label in out:
        raise SystemExit("redactor self-test failed")

    redactor = Redactor(pairs)
    out = redactor.feed(b"https://web." + secret + b"\n")
    if out != b"https://web.<tailnet>.ts.net\n" or redactor.flush():
        raise SystemExit("redactor self-test failed to flush a finished line")

    redactor = Redactor(pairs)
    first = redactor.feed(b"see zzexample-tail")
    rest = redactor.feed(b"net.ts.net") + redactor.flush()
    if secret in first + rest or b"<tailnet>.ts.net" not in first + rest:
        raise SystemExit("redactor self-test failed across a split read")

    text = pairs_from_status(
        {
            "MagicDNSSuffix": "zzexample-tailnet.ts.net.",
            "CurrentTailnet": {"Name": "zzexample-tailnet.ts.net", "MagicDNSSuffix": ""},
        },
        "other-secret.example\t<tailnet>\n",
    )
    parsed = {src for src, _dst in parse_needles(text)}
    if secret not in parsed or label not in parsed or b"other-secret.example" not in parsed:
        raise SystemExit("redactor self-test failed to read a status payload")
    if b"placeholder-needle" in parsed:
        raise SystemExit("redactor self-test invented a needle")

    with tempfile.TemporaryDirectory() as directory:
        needle = Path(directory, "needles")
        needle.write_text(text, encoding="utf-8")
        env = os.environ.copy()
        env["TAPE_REDACT_FILE"] = str(needle)
        env.setdefault("PATH", "/usr/bin:/bin")
        proc = subprocess.run(
            [
                sys.executable,
                str(Path(__file__).resolve()),
                "--noprofile",
                "--norc",
                "-c",
                "printf '%s\\n' 'https://web.zzexample-tailnet.ts.net'",
            ],
            env=env,
            stdin=subprocess.DEVNULL,
            capture_output=True,
            timeout=10,
            check=False,
        )
    leaked = secret in proc.stdout or secret in proc.stderr or label in proc.stdout
    if leaked or b"https://web.<tailnet>.ts.net" not in proc.stdout or proc.returncode != 0:
        raise SystemExit("proxy self-test failed")


def main():
    if len(sys.argv) > 1 and sys.argv[1] == "--self-test":
        self_test()
        return
    if len(sys.argv) > 1 and sys.argv[1] == "--discover":
        discover()
        return
    path = os.environ.get("TAPE_REDACT_FILE", "")
    if not path:
        raise SystemExit("docs/demo/redact.py: TAPE_REDACT_FILE is unset; refusing to draw a tape")
    proxy(load_needles(path))


if __name__ == "__main__":
    main()
