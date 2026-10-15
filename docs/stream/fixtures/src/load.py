#!/usr/bin/env python3
"""A small load generator for the `command` load tool, used only to record fixtures.

Sends GET requests to the chaos proxy for DURATION seconds from a few threads and
prints the normalized series on stdout, as the `command` tool expects:

    {"time_series":[{"offset_s":0,"rps":10,"error_rate":0.01}]}
"""
import json
import sys
import threading
import time
import urllib.request

URL = "http://127.0.0.1:18080/api/launches"
# The generator starts a little after the fault does, so it stops before the
# fault window (12s) ends. A generator that outlives the proxy only records
# refused connections.
DURATION = 11
WORKERS = 6

lock = threading.Lock()
buckets = {}


def worker(start):
    while time.time() - start < DURATION:
        ok = True
        try:
            with urllib.request.urlopen(URL, timeout=3) as resp:
                resp.read()
                ok = resp.status == 200
        except Exception:
            ok = False
        sec = int(time.time() - start)
        with lock:
            total, failed = buckets.get(sec, (0, 0))
            buckets[sec] = (total + 1, failed + (0 if ok else 1))


def main():
    start = time.time()
    threads = [threading.Thread(target=worker, args=(start,)) for _ in range(WORKERS)]
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    series = []
    for sec in sorted(buckets):
        total, failed = buckets[sec]
        series.append({"offset_s": sec, "rps": total, "error_rate": round(failed / total, 4)})
    json.dump({"time_series": series}, sys.stdout)


if __name__ == "__main__":
    main()
