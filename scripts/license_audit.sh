#!/usr/bin/env sh
set -eu

test -f LICENSE
grep -q "MIT License" LICENSE

test -f internal/adapters/http/static/htmx.min.js
test -f internal/adapters/http/static/htmx.LICENSE.txt
grep -q "Zero-Clause BSD" internal/adapters/http/static/htmx.LICENSE.txt

test -f .context/specs/process/LICENSE_PROVENANCE_LEDGER.md
grep -q "htmx" .context/specs/process/LICENSE_PROVENANCE_LEDGER.md
grep -q "Claude Design handoff" .context/specs/process/LICENSE_PROVENANCE_LEDGER.md
grep -q "glitchtip-backend" .context/specs/process/LICENSE_PROVENANCE_LEDGER.md
grep -q "glitchtip-frontend" .context/specs/process/LICENSE_PROVENANCE_LEDGER.md

echo "license provenance audit ok"
