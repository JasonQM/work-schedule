#!/usr/bin/env python3
"""Optional durable write queue. No third-party packages, model calls or project scanning."""
import argparse
import contextlib
import hashlib
import json
import os
from pathlib import Path
import random
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

MAX_BYTES = 10 * 1024 * 1024
MAX_JOBS = 1000
MAX_PAYLOAD = 128 * 1024


@contextlib.contextmanager
def lock(path, timeout=0.2):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    handle = open(path, "a+b")
    os.chmod(path, 0o600)
    if handle.seek(0, 2) == 0:
        handle.write(b"0")
        handle.flush()
    deadline = time.monotonic() + timeout
    acquired = False
    try:
        while True:
            try:
                handle.seek(0)
                if os.name == "nt":
                    import msvcrt
                    msvcrt.locking(handle.fileno(), msvcrt.LK_NBLCK, 1)
                else:
                    import fcntl
                    fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
                acquired = True
                break
            except (BlockingIOError, OSError):
                if time.monotonic() >= deadline:
                    raise RuntimeError("local_queue_busy")
                time.sleep(0.01)
        yield
    finally:
        if acquired:
            handle.seek(0)
            if os.name == "nt":
                import msvcrt
                msvcrt.locking(handle.fileno(), msvcrt.LK_UNLCK, 1)
            else:
                import fcntl
                fcntl.flock(handle.fileno(), fcntl.LOCK_UN)
        handle.close()


def atomic_write(path, data):
    temporary = path.parent / (".tmp-" + uuid.uuid4().hex)
    try:
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump(data, stream, ensure_ascii=False, separators=(",", ":"))
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        if hasattr(os, "O_DIRECTORY"):
            directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(directory)
            finally:
                os.close(directory)
    finally:
        temporary.unlink(missing_ok=True)


def jobs(root):
    result = []
    for path in root.glob("job-*.json"):
        try:
            result.append((path, json.loads(path.read_text(encoding="utf-8"))))
        except (ValueError, OSError):
            raise RuntimeError("queue_file_invalid: " + path.name)
    return sorted(result, key=lambda row: (row[1]["created_at"], row[0].name))


def is_running(root):
    try:
        with lock(root / "worker.lock", 0):
            return False
    except RuntimeError:
        return True


def credentials():
    url = os.environ.get("WORK_SCHEDULE_URL", "").rstrip("/")
    token = os.environ.get("WORK_SCHEDULE_TOKEN", "")
    parsed = urllib.parse.urlsplit(url)
    if parsed.scheme not in ("http", "https") or not parsed.hostname or parsed.username or parsed.password or parsed.path or parsed.query or parsed.fragment or not token:
        raise RuntimeError("configure WORK_SCHEDULE_URL (origin only) and WORK_SCHEDULE_TOKEN")
    return url, token


def enqueue(root, payload):
    if not isinstance(payload, dict) or set(payload) - {"request_id", "project_id", "changes"}:
        raise RuntimeError("invalid_batch")
    payload = dict(payload)
    payload.setdefault("request_id", str(uuid.uuid4()))
    if not isinstance(payload.get("request_id"), str) or not 1 <= len(payload["request_id"]) <= 100 or not isinstance(payload.get("project_id"), str) or not 1 <= len(payload["project_id"]) <= 36 or not isinstance(payload.get("changes"), list) or not 1 <= len(payload["changes"]) <= 20:
        raise RuntimeError("invalid_batch")
    encoded = json.dumps(payload, sort_keys=True, ensure_ascii=False, separators=(",", ":")).encode()
    if len(encoded) > MAX_PAYLOAD:
        raise RuntimeError("batch_too_large")
    identity = hashlib.sha256(payload["request_id"].encode()).hexdigest()
    fingerprint = hashlib.sha256(encoded).hexdigest()
    target = root / ("job-" + identity + ".json")
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    with lock(root / "queue.lock"):
        if target.exists():
            old = json.loads(target.read_text(encoding="utf-8"))
            if old["fingerprint"] != fingerprint:
                raise RuntimeError("request_id_reused_with_different_payload")
            return receipt(old, root)
        existing = jobs(root)
        synced = [row for row in existing if row[1]["state"] in ("synced", "superseded")]
        for path, _ in synced[:-100]:
            path.unlink(missing_ok=True)
        existing = jobs(root)
        if len(existing) >= MAX_JOBS or sum(path.stat().st_size for path, _ in existing) + len(encoded) + 1024 > MAX_BYTES:
            raise RuntimeError("local_queue_full: existing records retained")
        job = {"request_id": payload["request_id"], "project_id": payload["project_id"], "fingerprint": fingerprint, "payload": payload, "created_at": time.time(), "state": "pending", "attempts": 0, "next_attempt_at": 0}
        atomic_write(target, job)
    return receipt(job, root)


def receipt(job, root):
    result = {"request_id": job["request_id"], "state": job["state"], "queued": job["state"] in ("pending", "retry"), "synced": job["state"] == "synced", "worker_running": is_running(root)}
    if job.get("receipt"):
        result["receipt"] = job["receipt"]
    if job.get("error"):
        result["error"] = job["error"]
    return result


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise urllib.error.HTTPError(req.full_url, code, "redirect refused", headers, fp)


def send(payload):
    url, token = credentials()
    body = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode()
    request = urllib.request.Request(url + "/api/agent/sync", data=body, headers={"Content-Type": "application/json", "Authorization": "Bearer " + token}, method="POST")
    try:
        with urllib.request.build_opener(NoRedirect()).open(request, timeout=2) as response:
            data = response.read(65537)
            if len(data) > 65536:
                raise RuntimeError("response_too_large")
            result = json.loads(data)
            if result.get("request_id") != payload["request_id"] or result.get("project_id") != payload["project_id"] or not isinstance(result.get("results"), list):
                raise RuntimeError("invalid_sync_receipt")
            return result, None
    except urllib.error.HTTPError as error:
        info = {}
        try:
            info = json.loads(error.read(65536)).get("error", {})
        except (ValueError, OSError):
            pass
        if not isinstance(info, dict):
            info = {}
        return None, {"code": info.get("code", "http_" + str(error.code)), "message": info.get("message", "sync request failed"), "retryable": error.code in (408, 429) or error.code >= 500, "action": info.get("action", "check_credentials" if error.code in (401, 403) else "review_input")}
    except (OSError, ValueError, RuntimeError) as error:
        return None, {"code": "network_error", "message": type(error).__name__, "retryable": True, "action": "save_pending_continue"}


def process_once(root, sender=send):
    blocked = set()
    try:
        with lock(root / "queue.lock", 0.02):
            work = jobs(root)
    except RuntimeError:
        return
    for path, job in work:
        if job["state"] in ("synced", "superseded"):
            continue
        project = job["project_id"]
        if project in blocked:
            continue
        if job["state"] not in ("pending", "retry") or job["next_attempt_at"] > time.time():
            blocked.add(project)
            continue
        job["attempts"] += 1
        # Persist the exact immutable payload before sending; unknown outcomes replay the same ID/body.
        atomic_write(path, job)
        result, error = sender(job["payload"])
        if error is None:
            job["state"] = "synced"
            job["receipt"] = result
            job.pop("payload", None)
            job.pop("error", None)
        else:
            job["error"] = error
            job["state"] = "retry" if error["retryable"] and job["attempts"] < 10 else "conflict" if error["code"] == "conflict" else "attention"
            job["next_attempt_at"] = time.time() + min(300, 2 ** min(job["attempts"], 8)) + random.uniform(0, 2)
            blocked.add(project)
        atomic_write(path, job)


def status(root):
    all_jobs = jobs(root)
    counts = {}
    for _, job in all_jobs:
        counts[job["state"]] = counts.get(job["state"], 0) + 1
    recent = [dict(request_id=j["request_id"], project_id=j["project_id"], state=j["state"], attempts=j["attempts"], **({"error": j["error"]} if j.get("error") else {})) for _, j in all_jobs[-20:]]
    return {"worker_running": is_running(root), "counts": counts, "recent": recent}


def resolve(root, request_id, replacement):
    old_path=root / ("job-"+hashlib.sha256(request_id.encode()).hexdigest()+".json")
    new_path=root / ("job-"+hashlib.sha256(replacement.encode()).hexdigest()+".json")
    with lock(root / "queue.lock"):
        old=json.loads(old_path.read_text(encoding="utf-8"))
        new=json.loads(new_path.read_text(encoding="utf-8"))
        if request_id==replacement or old["state"] not in ("conflict","attention") or new["state"] not in ("pending","retry") or old["project_id"]!=new["project_id"]:
            raise RuntimeError("invalid_replacement: refresh and merge before replacing a failed batch")
        new["created_at"]=old["created_at"]
        atomic_write(new_path,new)
        old["state"]="superseded"
        old["replacement_request_id"]=replacement
        old.pop("payload",None)
        atomic_write(old_path,old)
    return {"resolved":request_id,"replacement":replacement,"synced":False}


def next_due(root):
    seen=set()
    due=float("inf")
    for _, job in jobs(root):
        if job["state"] in ("synced","superseded") or job["project_id"] in seen:
            continue
        seen.add(job["project_id"])
        if job["state"] in ("pending","retry"):
            due=min(due,job["next_attempt_at"])
    return due

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("enqueue", "status", "start", "stop", "worker", "resolve"))
    parser.add_argument("--queue-dir", default=os.environ.get("WORK_SCHEDULE_QUEUE_DIR", str(Path.home() / ".local/state/work-schedule/outbox")))
    parser.add_argument("--request-id", default="")
    parser.add_argument("--replacement", default="")
    parser.add_argument("--once", action="store_true", help="worker: process currently eligible jobs once")
    args = parser.parse_args()
    root = Path(args.queue_dir).expanduser().resolve()
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    try:
        if args.command == "enqueue":
            data = sys.stdin.buffer.read(MAX_PAYLOAD + 1)
            if len(data) > MAX_PAYLOAD:
                raise RuntimeError("batch_too_large")
            result = enqueue(root, json.loads(data))
        elif args.command == "status":
            result = status(root)
        elif args.command == "resolve":
            result=resolve(root,args.request_id,args.replacement)
        elif args.command == "stop":
            atomic_write(root / "stop.json", {"stop": True})
            result = {"stop_requested": True}
        elif args.command == "start":
            credentials()
            root.joinpath("stop.json").unlink(missing_ok=True)
            if is_running(root):
                result = {"worker_running": True}
            else:
                subprocess.Popen([sys.executable, str(Path(__file__).resolve()), "worker", "--queue-dir", str(root)], stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True)
                result = {"worker_starting": True}
        else:
            credentials()
            try:
                with lock(root / "worker.lock", 0):
                    last_change=-1
                    due=0
                    while not root.joinpath("stop.json").exists():
                        change=root.stat().st_mtime_ns
                        if change!=last_change or time.time()>=due or args.once:
                            process_once(root)
                            due=next_due(root)
                            last_change=change
                        if args.once:
                            break
                        time.sleep(1)
            except RuntimeError as error:
                if str(error) != "local_queue_busy":
                    raise
            result = {"worker_exited": True}
        print(json.dumps(result, ensure_ascii=False))
        return 0
    except (ValueError, OSError, RuntimeError) as error:
        print(json.dumps({"error": str(error), "synced": False, "action": "continue_development_review_queue"}, ensure_ascii=False))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
