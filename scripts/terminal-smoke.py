#!/usr/bin/env python3
"""Exercise real terminal handoff with a fake ssh; never contacts a server."""
import fcntl
import json
import os
import pathlib
import pty
import select
import signal
import struct
import subprocess
import sys
import tempfile
import termios
import time


def main():
    binary = str(pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else "./wishport").resolve())
    with tempfile.TemporaryDirectory(prefix="wishport-terminal-") as directory:
        root = pathlib.Path(directory)
        config = root / "config.yaml"
        config.write_text("""version: 1
forge:
  enabled: false
sources:
  ssh_config: false
  wishlist: false
connections:
  - id: smoke
    name: smoke-target
    host: 192.0.2.1
    user: deploy
    directory: /srv/application's root
""")
        executable = root / "ssh"
        executable.write_text(f"#!{sys.executable}\n" + """import json, os, sys
print('FAKE_SSH_TTY=' + str(int(all(os.isatty(n) for n in (0,1,2)))), flush=True)
with open(os.environ['WISHPORT_SMOKE_ARGS'], 'w') as f: json.dump(sys.argv[1:], f)
sys.exit(7)
""")
        executable.chmod(0o755)
        master, slave = pty.openpty()
        before = termios.tcgetattr(slave)
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 100, 0, 0))
        env = dict(os.environ, PATH=str(root), HOME=str(root), TERM="xterm-256color",
                   WISHPORT_SMOKE_ARGS=str(root / "args.json"), FORGE_API_TOKEN="")
        proc = subprocess.Popen([binary, "--config", str(config)], stdin=slave,
                                stdout=slave, stderr=slave, env=env, start_new_session=True)
        output = bytearray()

        def until(fragment, timeout=8):
            deadline = time.monotonic() + timeout
            while time.monotonic() < deadline:
                if fragment in output:
                    return
                if select.select([master], [], [], 0.1)[0]:
                    output.extend(os.read(master, 65536))
                if proc.poll() is not None:
                    break
            raise AssertionError(f"terminal did not show {fragment!r}; output={output!r}")

        try:
            until(b"smoke-target")
            # Resize down and back, exercising SIGWINCH and list layout.
            for rows, cols in [(8, 35), (24, 100)]:
                fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
                os.kill(proc.pid, signal.SIGWINCH)
            os.write(master, b"t")
            until(b"SSH target")
            os.write(master, b"/smk\r")
            until(b"FAKE_SSH_TTY=1")
            until(b"Connection ended")
            os.write(master, b"q")
            assert proc.wait(timeout=8) == 0
            while select.select([master], [], [], 0.1)[0]:
                output.extend(os.read(master, 65536))
            after = termios.tcgetattr(slave)
            assert before == after, "terminal attributes were not restored"
            assert b"\x1b[?1049l" in output, "alternate screen was not left"
            args = json.loads((root / "args.json").read_text())
            assert args[:2] == ["-l", "deploy"] and "-t" in args
            assert args[-2] == "192.0.2.1" and "'\\''" in args[-1]
            print("PASS: table toggle, search, resize, SSH TTY handoff, exit-code display, return and terminal restoration")
        finally:
            if proc.poll() is None:
                proc.kill()
                proc.wait()
            os.close(master)
            os.close(slave)


if __name__ == "__main__":
    main()
