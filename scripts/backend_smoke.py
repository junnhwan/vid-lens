#!/usr/bin/env python3
"""Exercise the real HTTP workflow. Writes private results only to the chosen output."""
import argparse
import http.cookiejar
import json
import os
import pathlib
import time
import urllib.error
import urllib.request
import uuid


class HTTPProblem(RuntimeError):
    def __init__(self, status, body):
        super().__init__(f"HTTP {status}: {body}")
        self.status = status


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--origin", default="http://127.0.0.1:8086")
    parser.add_argument("--video", required=True)
    parser.add_argument("--ask-frame",action="store_true")
    parser.add_argument("--question", default="请结合视频画面解释这段视频的主要内容，并说明哪些内容不能确定。")
    parser.add_argument("--output", default=".local/smoke-result.json")
    parser.add_argument("--timeout", type=int, default=900)
    args = parser.parse_args()
    output = pathlib.Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)

    def save(result):
        temporary = output.with_name(output.name + "." + uuid.uuid4().hex + ".tmp")
        descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, "w") as file:
            json.dump(result, file, ensure_ascii=False, indent=2)
            file.write("\n")
        os.replace(temporary, output)
    jar = http.cookiejar.CookieJar()
    client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    csrf = ""

    def call(method, path, body=None, key=None):
        headers = {"Origin": args.origin, "Content-Type": "application/json"}
        if csrf:
            headers["X-CSRF-Token"] = csrf
        if key:
            headers["Idempotency-Key"] = key
        request = urllib.request.Request(args.origin + path, None if body is None else json.dumps(body).encode(), headers, method=method)
        try:
            with client.open(request, timeout=30) as response:
                return json.load(response)
        except urllib.error.HTTPError as exc:
            raise HTTPProblem(exc.code, exc.read().decode()) from None

    def wait(path, field):
        end = time.monotonic() + args.timeout
        previous = None
        while time.monotonic() < end:
            try:
                view = call("GET", path)
            except (urllib.error.URLError, TimeoutError):
                time.sleep(3)
                continue
            except HTTPProblem as exc:
                if exc.status not in (429, 502, 503, 504):
                    raise
                time.sleep(3)
                continue
            execution = view["execution"] or {}
            activity = view["activity"]
            state = (view[field]["status"], execution.get("phase"), tuple(s["title"] for s in activity["recent_steps"][-1:]))
            if state != previous:
                print(json.dumps({"resource": field, "state": state}, ensure_ascii=False), flush=True)
                previous = state
            if view[field]["status"] in ("completed", "failed", "canceled"):
                return view
            time.sleep(2)
        raise TimeoutError("Smoke test timed out; the persisted workflow remains available.")

    session = call("POST", "/api/v1/session", {})
    csrf = session["csrf_token"]
    key = uuid.uuid4().hex
    created = call("POST", "/api/v1/requests", {"video_url": args.video}, key)
    replay = call("POST", "/api/v1/requests", {"video_url": args.video}, key)
    assert created["request"]["id"] == replay["request"]["id"]
    request_path = "/api/v1/requests/" + created["request"]["id"]
    print(json.dumps({"request_url": args.origin + request_path}), flush=True)
    result = {"request": wait(request_path, "request"), "idempotent_replay": True}
    save(result)
    if result["request"]["request"]["status"] == "completed":
        result["article"] = call("GET", request_path + "/article")
        with client.open(args.origin+request_path+"/events",timeout=30) as response:
            stream=response.read().decode()
            assert "event: view.snapshot" in stream and '"status":"completed"' in stream
            result["terminal_sse_snapshot"]=True
        result["activity"] = call("GET", request_path + "/activity")
        save(result)
        for frame in result["article"]["frames"]:
            with client.open(args.origin + frame["urls"]["full"], timeout=30) as response:
                assert response.headers["Content-Type"] == "image/jpeg"
                assert len(response.read()) > 100
        conversation = call("POST", request_path + "/conversation", {})["conversation"]
        path = "/api/v1/conversations/" + conversation["id"] + "/turns"
        turn = call("POST", path, {"question": args.question, "context": {"frame_id": result["article"]["frames"][0]["id"] if args.ask_frame and result["article"]["frames"] else None, "passage": None}, "expected_last_turn_id": conversation["last_turn_id"]}, uuid.uuid4().hex)
        result["turn"] = turn
        save(result)
        result["turn"] = wait(path + "/" + turn["turn"]["id"], "turn")
    save(result)
    print(json.dumps({"output": str(output), "request_status": result["request"]["request"]["status"], "turn_status": result.get("turn", {}).get("turn", {}).get("status")}), flush=True)
    if result["request"]["request"]["status"] != "completed" or result.get("turn", {}).get("turn", {}).get("status") != "completed":
        raise SystemExit(1)


if __name__ == "__main__":
    main()
