#!/usr/bin/env bash
# Auto-release openfluke/octo for the current Welvet scorecard version.
#
# What it does:
#   1. Read version from ../../README.md (welvet scorecard)
#   2. Collect log assets from logs/ (suite.txt/pdf if present, else newest run)
#   3. Commit source if dirty (logs/ hub/ entities stay gitignored)
#   4. Push main → origin
#   5. Tag + GitHub Release with log assets
#
# Usage:
#   ./release.sh                 # full release
#   ./release.sh --dry-run       # check version / assets only
#   ./release.sh --no-push       # commit locally, skip push/release
#   ./release.sh --code-only     # release source tag, skip log assets
#
# Needs: git, and either gh (authenticated) or GITHUB_TOKEN.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

WELVET_README="$(cd "$ROOT/../.." && pwd)/README.md"
REPO_SLUG="openfluke/octo"
API="https://api.github.com/repos/${REPO_SLUG}"
LOG_DIR="$ROOT/logs"
DIST="$ROOT/dist"

DRY_RUN=0
NO_PUSH=0
CODE_ONLY=0

for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    --no-push) NO_PUSH=1 ;;
    --code-only) CODE_ONLY=1 ;;
    -h|--help)
      sed -n '2,20p' "$0"
      exit 0
      ;;
    *)
      echo "unknown flag: $arg" >&2
      exit 2
      ;;
  esac
done

read_version() {
  python3 - <<'PY'
from pathlib import Path
import re
text = Path("../../README.md").read_text(encoding="utf-8")
earned = None
m = re.search(r"\*\*(\d+(?:\.\d+)?)\s*/\s*100\*\*\s*pts", text)
if m:
    earned = float(m.group(1))
if earned is None:
    m = re.search(r"\|\s*\*\*Version\*\*\s*\|\s*\*\*(v[\d.]+)\*\*", text)
    if m:
        v = m.group(1)
        earned = 100.0 if v == "v1.0" else float(v[3:]) if v.startswith("v0.") else None
if earned is None:
    raise SystemExit("could not parse Welvet version from ../../README.md")
ver = "v1.0" if earned >= 100 else f"v0.{int(round(earned)):02d}"
print(f"{ver} {earned}")
PY
}

have_gh() { command -v gh >/dev/null 2>&1; }

token() {
  if [[ -n "${GITHUB_TOKEN:-}" ]]; then
    echo "$GITHUB_TOKEN"
  elif [[ -n "${GH_TOKEN:-}" ]]; then
    echo "$GH_TOKEN"
  else
    echo ""
  fi
}

need_publish_tools() {
  if have_gh; then
    return 0
  fi
  if [[ -n "$(token)" ]]; then
    return 0
  fi
  echo "ERROR: need GitHub CLI (gh) or GITHUB_TOKEN to publish a release." >&2
  echo "  install: https://cli.github.com/  then  gh auth login" >&2
  echo "  or:      export GITHUB_TOKEN=ghp_…" >&2
  exit 1
}

# Populate ASSETS array with paths under dist/ to upload.
collect_assets() {
  ASSETS=()
  mkdir -p "$DIST"
  local version="$1"

  if [[ "$CODE_ONLY" -eq 1 ]]; then
    echo "→ --code-only: no log assets"
    return 0
  fi

  # Prefer suite-style artifacts (same as w2a) if present.
  if [[ -f "$LOG_DIR/suite.txt" ]]; then
    local txt="$DIST/octo-suite-${version}.txt"
    cp -f "$LOG_DIR/suite.txt" "$txt"
    ASSETS+=("$txt")
    if [[ -f "$LOG_DIR/suite.pdf" ]]; then
      local pdf="$DIST/octo-suite-${version}.pdf"
      cp -f "$LOG_DIR/suite.pdf" "$pdf"
      ASSETS+=("$pdf")
    fi
    echo "→ using logs/suite.*"
    return 0
  fi

  # Else newest timestamped run log (*.txt) + matching *.json if any.
  if [[ ! -d "$LOG_DIR" ]]; then
    echo "→ no logs/ dir — code-only release"
    return 0
  fi
  local newest
  newest="$(find "$LOG_DIR" -maxdepth 1 -type f -name '*.txt' -printf '%T@ %p\n' 2>/dev/null | sort -nr | head -1 | cut -d' ' -f2- || true)"
  if [[ -z "$newest" ]]; then
    echo "→ no .txt logs — code-only release"
    return 0
  fi
  local base stem json
  base="$(basename "$newest")"
  stem="${base%.txt}"
  local out_txt="$DIST/octo-run-${version}.txt"
  cp -f "$newest" "$out_txt"
  ASSETS+=("$out_txt")
  echo "→ using latest log: $base"
  json="$LOG_DIR/${stem}.json"
  if [[ -f "$json" ]]; then
    local out_json="$DIST/octo-run-${version}.json"
    cp -f "$json" "$out_json"
    ASSETS+=("$out_json")
    echo "→ + matching json: $(basename "$json")"
  fi
}

release_exists() {
  local tag="$1"
  if have_gh; then
    gh release view "$tag" --repo "$REPO_SLUG" >/dev/null 2>&1
  else
    local code
    code=$(curl -sS -o /dev/null -w "%{http_code}" \
      -H "Authorization: Bearer $(token)" \
      -H "Accept: application/vnd.github+json" \
      "${API}/releases/tags/${tag}")
    [[ "$code" == "200" ]]
  fi
}

create_or_update_release() {
  local tag="$1"
  local earned="$2"
  shift 2
  local files=("$@")

  local asset_lines=""
  local f
  for f in "${files[@]+"${files[@]}"}"; do
    [[ -n "$f" ]] || continue
    asset_lines+="- \`$(basename "$f")\`"$'\n'
  done
  if [[ -z "$asset_lines" ]]; then
    asset_lines="- _(source tag only — no log assets)_"$'\n'
  fi

  local notes
  notes="$(cat <<EOF
## octo ${tag}

Welvet model shell release aligned to scorecard **${earned}/100** → **${tag}**.

### Assets
${asset_lines}
### Repo
https://github.com/${REPO_SLUG}

### Run
\`\`\`bash
cd welvet/apps/octo
go run .
./release.sh
\`\`\`
EOF
)"

  if have_gh; then
    if release_exists "$tag"; then
      echo "  updating existing release ${tag}…"
      if [[ ${#files[@]} -gt 0 ]]; then
        gh release upload "$tag" "${files[@]}" --repo "$REPO_SLUG" --clobber
      fi
      gh release edit "$tag" --repo "$REPO_SLUG" \
        --title "octo ${tag}" \
        --notes "$notes"
    else
      echo "  creating release ${tag}…"
      if [[ ${#files[@]} -gt 0 ]]; then
        gh release create "$tag" "${files[@]}" --repo "$REPO_SLUG" \
          --title "octo ${tag}" \
          --notes "$notes"
      else
        gh release create "$tag" --repo "$REPO_SLUG" \
          --title "octo ${tag}" \
          --notes "$notes"
      fi
    fi
    return
  fi

  # curl fallback
  local auth="Authorization: Bearer $(token)"
  local id upload
  if release_exists "$tag"; then
    id=$(curl -sS -H "$auth" -H "Accept: application/vnd.github+json" \
      "${API}/releases/tags/${tag}" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
  else
    local body
    body=$(VERSION="$tag" NOTES="$notes" python3 - <<'PY'
import json, os
print(json.dumps({
  "tag_name": os.environ["VERSION"],
  "name": f"octo {os.environ['VERSION']}",
  "body": os.environ["NOTES"],
  "draft": False,
  "prerelease": False,
}))
PY
)
    id=$(curl -sS -H "$auth" -H "Accept: application/vnd.github+json" \
      -H "Content-Type: application/json" \
      -d "$body" "${API}/releases" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
  fi

  if [[ ${#files[@]} -eq 0 ]]; then
    return 0
  fi

  upload=$(curl -sS -H "$auth" -H "Accept: application/vnd.github+json" \
    "${API}/releases/${id}" | python3 -c "import sys,json; print(json.load(sys.stdin)['upload_url'].split('{')[0])")

  for asset in "${files[@]}"; do
    local name ctype
    name="$(basename "$asset")"
    ctype="application/octet-stream"
    [[ "$asset" == *.txt ]] && ctype="text/plain"
    [[ "$asset" == *.json ]] && ctype="application/json"
    [[ "$asset" == *.pdf ]] && ctype="application/pdf"
    curl -sS -H "$auth" -H "Accept: application/vnd.github+json" \
      "${API}/releases/${id}/assets" \
      | NAME="$name" TOKEN="$(token)" API="$API" python3 -c '
import json,os,sys,urllib.request
name=os.environ["NAME"]; tok=os.environ["TOKEN"]; api=os.environ["API"]
for a in json.load(sys.stdin):
    if a.get("name")==name:
        req=urllib.request.Request(api+"/releases/assets/"+str(a["id"]), method="DELETE",
            headers={"Authorization":"Bearer "+tok,"Accept":"application/vnd.github+json"})
        try: urllib.request.urlopen(req)
        except Exception: pass
'
    echo "  uploading $name…"
    curl -sS -H "$auth" -H "Content-Type: $ctype" \
      --data-binary @"$asset" \
      "${upload}?name=${name}" >/dev/null
  done
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
if [[ ! -f "$WELVET_README" ]]; then
  echo "ERROR: welvet README not found at $WELVET_README" >&2
  exit 1
fi

read -r VERSION EARNED <<<"$(read_version)"

echo "════════════════════════════════════════"
echo " octo release"
echo " version:  ${VERSION}  (${EARNED}/100)"
echo " repo:     ${REPO_SLUG}"
echo "════════════════════════════════════════"

ASSETS=()
collect_assets "$VERSION"
for a in "${ASSETS[@]+"${ASSETS[@]}"}"; do
  [[ -n "$a" ]] || continue
  echo "  asset: $(basename "$a") ($(du -h "$a" | cut -f1))"
done

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo ""
  echo "dry-run: skipping commit / push / release"
  echo "would release: ${VERSION}"
  exit 0
fi

echo ""
echo "→ git status"
git status --short || true

if [[ -n "$(git status --porcelain)" ]]; then
  echo "→ committing source…"
  git add -A
  git reset -q -- logs/ dist/ octo_hub/ octo_entities/ octo_outputs/ octo 2>/dev/null || true
  if [[ -n "$(git diff --cached --name-only)" ]]; then
    git commit -m "$(cat <<EOF
Release octo ${VERSION}

Aligned to Welvet scorecard ${EARNED}/100.
Run logs published on GitHub Release (not committed).
EOF
)"
  else
    echo "→ nothing staged — skip commit"
  fi
else
  echo "→ working tree clean — nothing to commit"
fi

if [[ "$NO_PUSH" -eq 1 ]]; then
  echo "→ --no-push: skipping push + GitHub release"
  exit 0
fi

need_publish_tools

echo "→ pushing main…"
git push origin HEAD

echo "→ publishing GitHub Release ${VERSION}…"
if git rev-parse "$VERSION" >/dev/null 2>&1; then
  echo "  tag ${VERSION} already exists locally"
else
  git tag -a "$VERSION" -m "octo ${VERSION} (Welvet scorecard ${EARNED}/100)"
fi
git push origin "$VERSION" 2>/dev/null || git push origin "refs/tags/${VERSION}"

create_or_update_release "$VERSION" "$EARNED" "${ASSETS[@]+"${ASSETS[@]}"}"

echo ""
echo "════════════════════════════════════════"
echo " Done · ${VERSION}"
echo " Repo:    https://github.com/${REPO_SLUG}"
echo " Release: https://github.com/${REPO_SLUG}/releases/tag/${VERSION}"
echo "════════════════════════════════════════"
