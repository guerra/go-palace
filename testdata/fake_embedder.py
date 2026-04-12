"""Fake embedder for Go unit tests.

Reads newline-delimited JSON arrays of strings from stdin and emits
newline-delimited JSON arrays of 4-dim float vectors to stdout. The vectors
are deterministic (first four bytes of sha256 normalised to [0,1]) so tests
can assert exact output.

Does NOT import chromadb or any heavy dependencies — this is the test
double, NOT the production helper.
"""

import json
import sys
import hashlib

sys.stdout.write(json.dumps({"ready": True, "dim": 4}) + "\n")
sys.stdout.flush()

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    texts = json.loads(line)
    out = []
    for t in texts:
        h = hashlib.sha256(t.encode()).digest()
        out.append([h[0] / 255.0, h[1] / 255.0, h[2] / 255.0, h[3] / 255.0])
    sys.stdout.write(json.dumps(out) + "\n")
    sys.stdout.flush()
