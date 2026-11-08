#!/usr/bin/env python3
"""Test hosted TUI and nested SSH locally using isolated throwaway keys."""
import os
import pathlib
import select
import shutil
import socket
import subprocess
import sys
import tempfile
import time


def main():
    binary = str(pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else "./wishport").resolve())
    ssh = shutil.which("ssh")
    keygen = shutil.which("ssh-keygen")
    assert ssh and keygen, "OpenSSH is required"
    with tempfile.TemporaryDirectory(prefix="wishport-hosted-") as directory:
        root = pathlib.Path(directory)
        client_key = root / "client"
        host_key = root / "host"
        subprocess.run([keygen, "-q", "-t", "ed25519", "-N", "", "-f", str(client_key)], check=True)
        config = root / "config.yaml"
        config.write_text("""version: 1
sources:
  ssh_config: false
  wishlist: false
forge:
  enabled: false
connections:
  - id: smoke
    name: hosted-smoke
    host: 192.0.2.1
""")
        fake = root / "ssh"
        fake.write_text(f"#!{sys.executable}\n" + "import os,sys\nprint('NESTED_SSH_TTY=' + str(int(all(os.isatty(n) for n in (0,1,2)))),flush=True)\nsys.exit(7)\n")
        fake.chmod(0o700)
        with socket.socket() as reservation:
            reservation.bind(("127.0.0.1", 0))
            port = reservation.getsockname()[1]
        env = dict(os.environ, HOME=str(root), XDG_CONFIG_HOME=str(root),
                   FORGE_API_TOKEN="", PATH=str(root) + os.pathsep + os.environ["PATH"])
        server = subprocess.Popen([binary, "serve", "--config", str(config),
            "--listen", f"127.0.0.1:{port}", "--host-key", str(host_key),
            "--authorized-keys", str(client_key) + ".pub"], env=env,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        client = None
        try:
            deadline = time.monotonic() + 5
            while not host_key.exists():
                if server.poll() is not None:
                    raise AssertionError("server startup failed: " + server.stderr.read().decode())
                assert time.monotonic() < deadline, "server startup timed out"
                time.sleep(0.02)
            public = subprocess.check_output([keygen, "-y", "-f", str(host_key)], text=True).strip()
            known_hosts = root / "known_hosts"
            known_hosts.write_text(f"[127.0.0.1]:{port} {public}\n")
            client = subprocess.Popen([ssh, "-F", "/dev/null", "-o", "StrictHostKeyChecking=yes",
                "-o", f"UserKnownHostsFile={known_hosts}", "-o", "BatchMode=yes",
                "-o", "IdentitiesOnly=yes", "-i", str(client_key), "-p", str(port),
                "-tt", "user@127.0.0.1"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT, env=dict(os.environ, TERM="xterm-256color"))
            output = bytearray()

            def until(fragment):
                deadline = time.monotonic() + 10
                while time.monotonic() < deadline:
                    if fragment in output:
                        return
                    if select.select([client.stdout], [], [], 0.1)[0]:
                        block = os.read(client.stdout.fileno(), 65536)
                        if not block:
                            break
                        output.extend(block)
                raise AssertionError(f"hosted terminal did not show {fragment!r}: {output!r}")

            until(b"hosted-smoke")
            client.stdin.write(b"t")
            client.stdin.flush()
            until(b"SSH target")
            client.stdin.write(b"\r")
            client.stdin.flush()
            until(b"NESTED_SSH_TTY=1")
            until(b"Connection ended")
            client.stdin.write(b"q")
            client.stdin.flush()
            assert client.wait(timeout=5) == 0, "hosted TUI did not exit cleanly"
            print("PASS: authenticated hosted TUI, strict host verification, table, nested SSH TTY, return and exit")
        finally:
            if client is not None and client.poll() is None:
                client.kill()
                client.wait()
            server.terminate()
            try:
                server.wait(timeout=5)
            except subprocess.TimeoutExpired:
                server.kill()
                server.wait()
                raise AssertionError("hosted server did not shut down")


if __name__ == "__main__":
    main()
