#!/usr/bin/env bash
set -euo pipefail

SOURCE="${1:-/home/virtual/stratux/stratuxnx-cloud-release}"
TARGET="/home/virtual/lemonpot/stratuxnx"
CONFIG_SOURCE="/home/virtual/stratux/stratuxnx-app.php"

test "$(id -u)" -eq 0 || { echo "Run this script as root."; exit 1; }
test -f "$SOURCE/lib.php" || { echo "Release not found at $SOURCE"; exit 1; }

install -d -o www-data -g www-data -m 0750 "$TARGET/data" "$TARGET/config"
install -o www-data -g www-data -m 0644 "$SOURCE/lib.php" "$TARGET/lib.php"
install -d -o www-data -g www-data -m 0755 "$TARGET/api/v1/installations" "$TARGET/api/v1/flights"
install -o www-data -g www-data -m 0644 "$SOURCE/api/v1/installations/register.php" "$TARGET/api/v1/installations/register.php"
install -o www-data -g www-data -m 0644 "$SOURCE/api/v1/installations/claim-code.php" "$TARGET/api/v1/installations/claim-code.php"
install -o www-data -g www-data -m 0644 "$SOURCE/api/v1/flights/upload.php" "$TARGET/api/v1/flights/upload.php"
install -o www-data -g www-data -m 0644 "$SOURCE/web/index.php" "$TARGET/web/index.php"
install -o www-data -g www-data -m 0644 "$SOURCE/web/logo-dark.png" "$TARGET/web/logo-dark.png"
install -o www-data -g www-data -m 0644 "$SOURCE/web/logo-light.png" "$TARGET/web/logo-light.png"
install -o www-data -g www-data -m 0644 "$SOURCE/web/icon.png" "$TARGET/web/icon.png"

if [[ -f "$CONFIG_SOURCE" ]]; then
  install -o www-data -g www-data -m 0640 "$CONFIG_SOURCE" "$TARGET/config/app.php"
elif [[ ! -f "$TARGET/config/app.php" ]]; then
  install -o www-data -g www-data -m 0640 "$SOURCE/config/app.php.example" "$TARGET/config/app.php"
fi

php -m | grep -qx sodium
for file in "$TARGET/lib.php" "$TARGET/api/v1/installations/"*.php "$TARGET/api/v1/flights/"*.php "$TARGET/web/index.php"; do
  php -l "$file" >/dev/null
done

chown -R www-data:www-data "$TARGET/data"
chmod 0750 "$TARGET/data"
echo "Stratux NX Flight Cloud deployed. Configure OAuth values in $TARGET/config/app.php."
