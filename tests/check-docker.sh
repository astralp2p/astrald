#!/usr/bin/env bash
# check-docker.sh — the no-netsim acceptance checks, in order, with one
# report to send back.
#
#   ./tests/check-docker.sh [--agent]
#
# Runs, from the repo root: the selftests, the live Docker selftests,
# main.suite (local worlds), integration.suite --docker, a check that no
# container or network of the harness is left, and with --agent the plan
# and a run of agent.suite --driver agent, then docker-agent.suite --driver
# agent --docker. Every step runs even when one
# before it failed; the summary says which passed.
#
# Everything lands in tests/results/check-docker-<stamp>/: summary.txt, one
# log per step, the first lines of each run's report.md, every failing
# entry's driver.log and verify.log, and the image build log.
#
# why a script: the first Docker run is the first time this path meets a
# real engine, and one directory of evidence is easier to read than a
# terminal scrollback
set -u

cd "$(dirname "$0")/.." || exit 2
TESTS=$PWD/tests
AGENT=0
[ "${1:-}" = "--agent" ] && AGENT=1

PY=${PYTHON:-python3}
ASTRAL_PY=$("$PY" -c 'import os, tomllib
cfg = tomllib.load(open("tests/config.toml", "rb"))
print(os.path.expanduser(cfg["astral_py"]["path"]))') || exit 2
export PYTHONPATH=".:$ASTRAL_PY/src"

STAMP=$(date -u +%Y%m%dT%H%M%SZ)
OUT=$TESTS/results/check-docker-$STAMP
mkdir -p "$OUT"
SUMMARY=$OUT/summary.txt
{
    echo "check-docker $STAMP"
    echo "host: $(uname -srm)"
    echo "branch: $(git rev-parse --abbrev-ref HEAD) $(git rev-parse --short HEAD)"
    echo "python: $("$PY" --version 2>&1)"
    echo "docker: $(docker version --format '{{.Server.Version}} {{.Server.Arch}}' 2>&1 | head -1)"
    echo
} > "$SUMMARY"

# step NAME CMD...: run CMD, log it, and record PASS or FAIL
step() {
    local name=$1; shift
    echo "== $name: $*"
    if "$@" > "$OUT/$name.log" 2>&1; then
        echo "PASS  $name" | tee -a "$SUMMARY"
    else
        echo "FAIL  $name (exit $?, see $name.log)" | tee -a "$SUMMARY"
    fi
}

newest() { ls -td "$TESTS"/results/2*/ 2>/dev/null | head -1; }

# collect NAME BEFORE: the verdict of the run NAME made, and the logs of its
# failing entries; nothing when NAME made no run (BEFORE is still newest)
collect() {
    local name=$1 before=$2 run
    run=$(newest)
    if [ -z "$run" ] || [ "$run" = "$before" ]; then
        echo "   $name: no run (refused before its results directory)"
        return 0
    fi
    echo "   $name: $(sed -n 3p "$run/report.md") [$(basename "$run")]"
    "$PY" - "$run" "$OUT/$name-failures" <<'PY'
import json, shutil, sys
from pathlib import Path
run, out = Path(sys.argv[1]), Path(sys.argv[2])
doc = json.loads((run / "results.json").read_text())
for e in doc["entries"]:
    if e["status"] == "pass":
        continue
    print(f"   {e['world']} {e['test']}: {e['status']} "
          f"{e.get('failure_kind') or ''} {e.get('phase') or ''}".rstrip())
    art = run / e["artifacts"]
    for log in ("driver.log", "verify.log"):
        if (art / log).is_file():
            dest = out / e["artifacts"].rstrip("/")
            dest.mkdir(parents=True, exist_ok=True)
            shutil.copy(art / log, dest / log)
PY
}

step selftests sh -c "cd tests && $PY -m unittest discover -s selftest -t ."
# why first: without an engine the live tests skip and exit 0, which would
# read as a pass
step docker-engine docker info --format '{{.ServerVersion}}'
step docker-live sh -c "cd tests && ASTRAL_TESTS_DOCKER=1 $PY -m unittest selftest.test_docker_live -v"
cp "$TESTS/.cache/docker-build.log" "$OUT/" 2>/dev/null

before=$(newest)
step main-suite ./tests/run main.suite
collect main-suite "$before" | tee -a "$SUMMARY"

before=$(newest)
step integration-docker ./tests/run integration.suite --docker
collect integration-docker "$before" | tee -a "$SUMMARY"

left=$(docker ps -aq --filter label=astral.tests.run 2>/dev/null)
nets=$(docker network ls -q --filter label=astral.tests.run 2>/dev/null)
if ! docker info >/dev/null 2>&1; then
    echo "FAIL  nothing-left: no Docker engine to ask" | tee -a "$SUMMARY"
elif [ -z "$left$nets" ]; then
    echo "PASS  nothing-left" | tee -a "$SUMMARY"
else
    echo "FAIL  nothing-left: containers [$left] networks [$nets]" \
        | tee -a "$SUMMARY"
fi

if [ "$AGENT" = 1 ]; then
    step agent-plan sh -c "./tests/run --plan agent.suite --driver agent \
        && ./tests/run --plan agent.suite --driver agent | grep -q '^w0 docker'"
    before=$(newest)
    step agent-suite ./tests/run agent.suite --driver agent
    collect agent-suite "$before" | tee -a "$SUMMARY"
    before=$(newest)
    step docker-agent-suite ./tests/run docker-agent.suite --driver agent \
        --docker
    collect docker-agent-suite "$before" | tee -a "$SUMMARY"
fi

echo
echo "summary: $SUMMARY"
cat "$SUMMARY"
! grep -q '^FAIL' "$SUMMARY"
