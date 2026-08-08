#!/usr/bin/env bash
# Instal driver playwright-go + chromium langsung dari internet di VPS.
# Tanpa Go, tanpa npm, tanpa scp dari mesin lokal.
# Hanya untuk linux amd64. Cukup sekali per VPS (user yang menjalankan bot).
set -euo pipefail

PW_VERSION="1.61.1"      # versi driver yang dipakai playwright-go v0.6100.0
NODE_VERSION="24.18.0"   # node runtime yang di-bundle driver

case "$(uname -m)" in
  x86_64) SUFFIX="linux-x64" ;;
  *) echo "❌ Script ini hanya untuk linux amd64 (x86_64). Ditemukan: $(uname -m)" >&2; exit 1 ;;
esac

DRIVER="$HOME/.cache/ms-playwright-go/$PW_VERSION"
mkdir -p "$DRIVER/package"

if [ -x "$DRIVER/node" ] && [ -f "$DRIVER/package/cli.js" ]; then
  echo "✅ Driver sudah terpasang di $DRIVER"
  exit 0
fi

echo "→ Download playwright-core $PW_VERSION (npm registry)..."
curl -fsSL "https://registry.npmjs.org/playwright-core/-/playwright-core-$PW_VERSION.tgz" -o /tmp/pw-core.tgz
tar -xzf /tmp/pw-core.tgz -C "$DRIVER/package" --strip-components=1
rm -f /tmp/pw-core.tgz

echo "→ Download node v$NODE_VERSION (nodejs.org)..."
curl -fsSL "https://nodejs.org/dist/v$NODE_VERSION/node-v$NODE_VERSION-$SUFFIX.tar.xz" -o /tmp/node.tar.xz
tar -xJf /tmp/node.tar.xz -C /tmp
mv "/tmp/node-v$NODE_VERSION-$SUFFIX/bin/node" "$DRIVER/node"
chmod +x "$DRIVER/node"
rm -rf "/tmp/node-v$NODE_VERSION-$SUFFIX" /tmp/node.tar.xz

echo "→ Patch coreBundle.js (sama seperti Install() bawaan playwright-go)..."
"$DRIVER/node" -e '
const fs = require("fs");
const p = process.env.HOME + "/.cache/ms-playwright-go/1.61.1/package/lib/coreBundle.js";
let s = fs.readFileSync(p, "utf8");
const rep = [
  ["pageError.location.url", "pageError.location?.url || \"\""],
  ["pageError.location.lineNumber", "pageError.location?.lineNumber || 0"],
  ["pageError.location.columnNumber", "pageError.location?.columnNumber || 0"],
];
for (const [a, b] of rep) if (s.includes(a)) s = s.split(a).join(b);
fs.writeFileSync(p, s);
'

echo "→ Install chromium (download dari CDN playwright, ~350MB)..."
cd "$DRIVER/package"
"$DRIVER/node" cli.js install chromium

echo "→ Verifikasi driver..."
"$DRIVER/node" cli.js --version

echo
echo "✅ Selesai."
echo "   driver  : $DRIVER"
echo "   chromium: $HOME/.cache/ms-playwright/chromium-*"
echo "Jalankan bot sebagai user ini tanpa env tambahan."
