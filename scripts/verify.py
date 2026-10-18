#!/usr/bin/env python3
"""Repeatable verification; never starts services or changes their configuration.

python scripts/verify.py --profile unit
python scripts/verify.py --profile full

Full verification needs isolated VIDLENS_TEST_DATABASE_URL and
VIDLENS_TEST_REDIS_URL, FFmpeg/ffprobe, and the existing private capability image.
Use --scope frontend/backend when the two toolchains run on different hosts.
--go-exec forwards Go's standard -exec option to an existing Linux test runtime.
"""

import argparse
import collections
import datetime
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]
PRIVATE_TEST = "TestCapabilityProbeUsesOwnerSuppliedScreenshotAndChecksItsDetails/private_screenshot"
DEPENDENCIES = ("VIDLENS_TEST_DATABASE_URL", "VIDLENS_TEST_REDIS_URL")


def go_results(path, profile, expected_packages):
    outputs = collections.defaultdict(str)
    terminal = {}
    started = set()
    packages = set()
    for line in path.read_text(encoding="utf-8").splitlines():
        event = json.loads(line)
        package, test = event.get("Package", ""), event.get("Test", "")
        action = event.get("Action")
        if test:
            key = (package, test)
            if action == "run":
                started.add(key)
            elif action == "output":
                outputs[key] += event.get("Output", "")
            elif action in ("pass", "fail", "skip"):
                terminal[key] = action
        elif action == "pass":
            packages.add(package)
        elif action == "fail":
            raise RuntimeError(f"Go package failed: {package}")
    missing = set(expected_packages) - packages
    if missing:
        raise RuntimeError("Go packages did not complete: " + ", ".join(sorted(missing)))
    if started - terminal.keys():
        raise RuntimeError("Go tests did not reach a terminal result")
    if not terminal or not any(action == "pass" for action in terminal.values()):
        raise RuntimeError("Go verification did not execute any passing tests")
    failures = [f"{p}/{t}" for (p, t), action in terminal.items() if action == "fail"]
    if failures:
        raise RuntimeError("Go tests failed: " + ", ".join(failures))
    skips = []
    for (package, test), action in terminal.items():
        if action != "skip":
            continue
        reason = outputs[(package, test)].strip()
        allowed = profile == "unit" and (
            "integration: excluded by -short" in reason
            or test == PRIVATE_TEST
        )
        skips.append({"package": package, "test": test, "reason": reason, "expected": allowed})
    unexpected = [s for s in skips if not s["expected"]]
    result = {
        "passed_test_events_including_subtests": sum(a == "pass" for a in terminal.values()),
        "passed_top_level_tests": sum(a == "pass" and "/" not in t for (_, t), a in terminal.items()),
        "completed_test_packages": sorted(set(expected_packages) & packages),
        "skipped": skips,
    }
    if unexpected:
        raise RuntimeError("Unexpected Go skips: " + ", ".join(s["test"] for s in unexpected))
    return result


def frontend_results(path):
    report = json.loads(path.read_text(encoding="utf-8"))
    total, passed = report["numTotalTests"], report["numPassedTests"]
    if (not report.get("success") or total == 0 or passed != total
            or report.get("numPendingTests", 0) or report.get("numTodoTests", 0)
            or report.get("numFailedTests", 0)):
        raise RuntimeError("Frontend suite failed, was empty, or skipped tests")
    return {"passed_tests": passed, "total_tests": total, "skipped_tests": report.get("numPendingTests", 0)}


class Verification:
    def __init__(self, args):
        self.args = args
        self.output = Path(args.report_dir).resolve()
        self.output.mkdir(parents=True, exist_ok=False)
        self.env = os.environ.copy()
        self.env["GOMAXPROCS"] = self.env.get("GOMAXPROCS", "2")
        self.env["VIDLENS_TEST_STRICT"] = "1"
        if args.profile == "unit":
            for name in DEPENDENCIES:
                self.env.pop(name, None)
        self.report = {"profile": args.profile, "scope": args.scope, "steps": [], "success": False}

    def run(self, name, command, cwd=ROOT):
        print(f"Running {name}", flush=True)
        started = time.monotonic()
        stdout_path = self.output / f"{name}.log"
        stderr_path = self.output / f"{name}.stderr.log"
        with stdout_path.open("w", encoding="utf-8") as stdout, stderr_path.open("w", encoding="utf-8") as stderr:
            completed = subprocess.run(command, cwd=cwd, env=self.env, stdout=stdout, stderr=stderr, check=False)
        self.report["steps"].append({"name": name, "exit_code": completed.returncode,
                                     "elapsed_seconds": round(time.monotonic() - started, 3)})
        # Do not echo service URLs from failures into the terminal or summary.
        for path in (stdout_path, stderr_path):
            content = path.read_text(encoding="utf-8", errors="replace")
            for name in DEPENDENCIES:
                if value := os.environ.get(name):
                    content = content.replace(value, "<REDACTED>")
            path.write_text(content, encoding="utf-8")
        if completed.returncode:
            raise RuntimeError(f"{name} failed (exit {completed.returncode}); see {stdout_path.name} and {stderr_path.name}")
        return stdout_path

    def backend(self):
        if not shutil.which("go"):
            raise RuntimeError("Go is required; use a Linux toolchain/runtime for the backend")
        goos = self.run("go-platform", ["go", "env", "GOOS"]).read_text().strip()
        if goos != "linux":
            raise RuntimeError("Backend needs Linux: run in Linux/WSL, or set GOOS=linux with --go-exec")
        if self.args.profile == "full":
            missing = [name for name in DEPENDENCIES if not self.env.get(name)]
            if missing:
                raise RuntimeError("Full verification requires isolated dependencies: " + ", ".join(missing))
            if not (ROOT / "internal/investigator/testdata/model_capability.png").is_file():
                raise RuntimeError("Full verification requires the existing private capability image")
            if not self.args.go_exec:
                for tool in ("ffmpeg", "ffprobe"):
                    if not shutil.which(tool):
                        raise RuntimeError(f"Full verification requires {tool}")
                    self.run(f"{tool}-runtime", [tool, "-version"])
        packages = self.run("go-packages", ["go", "list", "-f", "{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}",
                                             "./cmd/...", "./internal/..."]).read_text().split()
        if not packages:
            raise RuntimeError("No backend test packages discovered")
        command = ["go", "test", "-json", "-count=1", "-p=1", "-timeout=10m"]
        if self.args.go_exec:
            command.extend(["-exec", self.args.go_exec])
        if self.args.profile == "unit":
            command.extend(["-short", "-skip", "^" + PRIVATE_TEST + "$"])
            self.report["exclusions"] = ["integration dependencies (-short)", PRIVATE_TEST]
        command.extend(["./cmd/...", "./internal/..."])
        path = self.run("backend-tests", command)
        self.report["backend"] = go_results(path, self.args.profile, packages)
        self.run("backend-vet", ["go", "vet", "-p=1", "./cmd/...", "./internal/..."])
        self.run("backend-build", ["go", "build", "-p=1", "-o", str(self.output / "vidlens"), "./cmd/vidlens"])

    def frontend(self):
        npm = shutil.which("npm.cmd" if os.name == "nt" else "npm")
        if not npm:
            raise RuntimeError("Node/npm is required for frontend verification")
        path = self.output / "frontend-tests.json"
        self.run("frontend-tests", [npm, "test", "--", "--reporter=json", "--outputFile=" + str(path)], ROOT / "frontend")
        self.report["frontend"] = frontend_results(path)
        self.run("frontend-build", [npm, "run", "build"], ROOT / "frontend")

    def execute(self):
        try:
            self.run("verification-tests", [sys.executable, "-m", "unittest", "discover", "-s", "scripts", "-p", "test_verify.py"])
            if self.args.scope in ("all", "backend"):
                self.backend()
            if self.args.scope in ("all", "frontend"):
                self.frontend()
            self.report["success"] = True
        except (RuntimeError, OSError, ValueError, KeyError) as error:
            self.report["error"] = str(error)
        finally:
            (self.output / "summary.json").write_text(json.dumps(self.report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
        print(f"{'PASS' if self.report['success'] else 'FAIL'}: {self.output / 'summary.json'}", flush=True)
        if not self.report["success"]:
            print(self.report["error"], file=sys.stderr)
        return 0 if self.report["success"] else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--profile", choices=("unit", "full"), default="unit")
    parser.add_argument("--scope", choices=("all", "frontend", "backend"), default="all")
    parser.add_argument("--go-exec", help="Go -exec command for an existing Linux runtime; it must preserve test flags and exit status")
    stamp = datetime.datetime.now().strftime("%Y%m%d-%H%M%S")
    parser.add_argument("--report-dir", default=str(ROOT / ".local/verification" / f"{stamp}-{os.getpid()}"))
    args = parser.parse_args()
    return Verification(args).execute()


if __name__ == "__main__":
    sys.exit(main())
