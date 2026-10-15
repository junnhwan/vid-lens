#!/bin/sh
set -eu
# Copy the read-only host secret without changing its 0600 host permissions.
install -d -m 0700 -o 10001 -g 10001 /run/vidlens /var/lib/vidlens
install -m 0600 -o 10001 -g 10001 /etc/vidlens/backend.yaml /run/vidlens/backend.yaml
if [ -f /etc/vidlens/platform-cookies.txt ]; then
  install -m 0600 -o 10001 -g 10001 /etc/vidlens/platform-cookies.txt /run/vidlens/platform-cookies.txt
  export VIDLENS_PLATFORM_COOKIE_FILE=/run/vidlens/platform-cookies.txt
fi
exec setpriv --reuid 10001 --regid 10001 --init-groups vidlens --config /run/vidlens/backend.yaml "$@"
