#!/usr/bin/env python3
"""Synthetic T04 CLI/curl smoke test; all state/credentials are temporary.

Run from any directory: python3 scripts/e2e/archive-intake.py
Requires Go, curl and Python 3. No installed Core, IM or NAS configuration is used.
"""
import datetime
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import time

REPO = Path(__file__).resolve().parents[2]


def main():
    with tempfile.TemporaryDirectory(prefix="atrium-intake-e2e-") as directory:
        root = Path(directory)
        # Ignore production-related environment overrides entirely.
        env = {k: v for k, v in os.environ.items() if not k.startswith("ATRIUM_")}
        binary, data, config = root / "atrium", root / "data", root / "config.yaml"
        subprocess.run(["go", "build", "-o", str(binary), "./cmd/atrium"], cwd=REPO, env=env, check=True)
        config.write_text(
            "home:\n  timezone: UTC\nserver:\n  listen: '127.0.0.1:0'\n"
            "  tls:\n    mode: 'off'\nstorage:\n  data_dir: " + json.dumps(str(data)) +
            "\n  min_free_bytes: 0\narchives:\n  enabled: true\n"
        )
        config.chmod(0o600)
        # init prints its one-time token; suppress output and use its 0600 file.
        subprocess.run([str(binary), "init", "--config", str(config)], env=env, check=True,
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        process = subprocess.Popen([str(binary), "serve", "--config", str(config)], env=env,
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            address = None
            for _ in range(200):
                if process.poll() is not None:
                    raise RuntimeError("temporary Core exited before listening")
                logfile = data / "logs" / "atrium.log"
                if logfile.exists():
                    for line in logfile.read_text().splitlines():
                        try:
                            record = json.loads(line)
                        except json.JSONDecodeError:
                            continue
                        if record.get("event") == "listening":
                            address = "http://" + record["addr"]
                if address:
                    break
                time.sleep(0.05)
            if address is None:
                raise RuntimeError("temporary Core did not start")
            admin = [str(binary), "admin", "--url", address, "--data-dir", str(data)]
            household = json.loads(subprocess.check_output(admin + ["household", "show"], env=env))["id"]
            tokenfile = root / "integration.token"
            expiry = (datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(hours=1)).isoformat()
            subprocess.run(admin + ["integrations", "issue", "--label", "synthetic T04 smoke",
                           "--permissions", "archive.ingest,archive.read", "--households", household,
                           "--expires-at", expiry, "--out", str(tokenfile)], env=env, check=True,
                           stdout=subprocess.DEVNULL)
            # Keep tokens out of process arguments and command output.
            headers = root / "headers"
            headers.write_text("Authorization: Bearer " + tokenfile.read_text().strip() + "\n")
            headers.chmod(0o600)
            source = dict(kind="text", channel="cli", account="synthetic", conversation="test",
                          sender="synthetic", provider_message_id="t04-smoke", slot_index=0)
            metadata, lookup, original = root / "metadata.json", root / "lookup.json", root / "original.txt"
            metadata.write_text(json.dumps(dict(source, original_name="synthetic.txt")))
            lookup.write_text(json.dumps(source))
            content = "  synthetic 原文\r\n70 ml\n".encode()
            original.write_bytes(content)
            statuses = []

            def curl(path, args):
                output = root / "response.json"
                status = subprocess.check_output(["curl", "--silent", "--show-error", "--max-time", "10",
                         "--header", "@" + str(headers), "--output", str(output), "--write-out", "%{http_code}",
                         address + path] + args, env=env).decode()
                statuses.append(int(status))
                if status != "200":
                    raise RuntimeError("synthetic request failed with HTTP " + status)
                return json.loads(output.read_text())["data"]

            prefix = "/api/v1/integrations/archives"
            receipt = curl(prefix + "/intakes", ["--form", "metadata=@" + str(metadata) + ";type=application/json",
                           "--form", "content=@" + str(original) + ";type=application/octet-stream"])
            recovered = curl(prefix + "/intakes/lookup", ["--header", "Content-Type: application/json",
                             "--data-binary", "@" + str(lookup)])
            assert receipt == recovered, "lost-reply recovery changed the receipt"
            task = curl(prefix + "/tasks/" + receipt["task_id"], [])
            expected = hashlib.sha256(content).hexdigest()
            assert receipt["sha256"] == expected and receipt["size_bytes"] == len(content)
            assert task["status"] == "queued" and task["original_status"] == "saved" and task["nas_status"] == "pending"
            print(json.dumps(dict(http_statuses=statuses, size_bytes=len(content), sha256=expected,
                                  original_status="saved", processing=task["status"], nas=task["nas_status"],
                                  lookup_same_receipt=True, real_im_binding_tested=False)))
        finally:
            if process.poll() is None:
                process.send_signal(signal.SIGTERM)
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


if __name__ == "__main__":
    main()
