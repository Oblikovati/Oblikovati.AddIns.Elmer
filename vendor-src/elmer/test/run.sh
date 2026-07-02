#!/usr/bin/env bash
# Smoke: solve the hand-written 5-tet cube; assert clean exit + a VTU appears +
# the max w-displacement equals sigma*L/E = 1e-3 m (nu=0 => exact on any mesh).
set -euo pipefail
cd "$(dirname "$0")"
BIN="${OBK_ELMER_BIN:-../install/bin/ElmerSolver}"
# ResultOutputSolver writes case_t0001.vtu under mesh/ (the "Mesh DB" directory from
# case.sif), not next to case.sif -- verified against the actual solver run, not assumed.
rm -f mesh/case*.vtu
echo "case.sif" > ELMERSOLVER_STARTINFO
"$BIN" | tee solve.log
grep -q "ALL DONE" solve.log
VTU=$(ls mesh/case*.vtu | head -1)
python3 - "$VTU" <<'EOF'
import re, sys
txt = open(sys.argv[1]).read()
m = re.search(r'Name="displacement"[^>]*>([^<]*)<', txt, re.S)
vals = [float(v) for v in m.group(1).split()]
w = vals[2::3]
peak = max(abs(v) for v in w)
assert abs(peak - 1.0e-3) < 1.0e-5, f"peak w {peak} != 1e-3"
print(f"smoke OK: peak displacement {peak}")
EOF
