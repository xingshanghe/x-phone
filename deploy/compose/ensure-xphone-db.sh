#!/bin/sh
set -eu

# Creates the xphone schema inside the MySQL container that is already running.
# Does not start, stop, or reconfigure that container.

container="${MYSQL_CONTAINER:-mysql-prod}"
root_password="${MYSQL_ROOT_PASSWORD:?set MYSQL_ROOT_PASSWORD}"
database="${XPHONE_DB_NAME:-xphone}"
app_user="${XPHONE_DB_USER:-xphone}"
app_password="${XPHONE_DB_PASSWORD:-xphone}"

running="$(docker inspect -f '{{.State.Running}}' "$container" 2>/dev/null || true)"
if [ "$running" != "true" ]; then
  echo "$container is not running. Start the existing middleware stack first; this script does not launch a second MySQL." >&2
  exit 1
fi

docker exec -e MYSQL_PWD="$root_password" "$container" \
  mysql -uroot -e "CREATE DATABASE IF NOT EXISTS \`$database\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS '$app_user'@'%' IDENTIFIED BY '$app_password';
GRANT ALL PRIVILEGES ON \`$database\`.* TO '$app_user'@'%';
FLUSH PRIVILEGES;"

echo "database $database is ready on $container"
