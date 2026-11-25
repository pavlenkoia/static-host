#!/usr/bin/env bash
set -euo pipefail

if [[ $# != 1 || -z $1 ]]; then echo "usage: $0 IMAGE" >&2; exit 2; fi
image=$1
suffix="$(date +%s)-$$-$RANDOM"
volume="static-host-smoke-$suffix"
empty="static-host-smoke-empty-$suffix"
container="static-host-smoke-$suffix"
missing="static-host-smoke-missing-$suffix"
tmp=$(mktemp -d)
cleanup() {
  docker rm -f "$container" "$missing" >/dev/null 2>&1 || true
  docker volume rm "$volume" "$empty" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT

docker volume create "$volume" >/dev/null
docker volume create "$empty" >/dev/null
printf '%s\n' container-smoke-password | docker run --rm -i --mount "type=volume,src=$volume,dst=/data" "$image" owner init --login admin
start() {
  docker run -d --name "$container" --mount "type=volume,src=$volume,dst=/data" -e STATIC_HOST_LISTEN=0.0.0.0:8081 -e STATIC_HOST_BASE_DOMAIN=example.test -p 127.0.0.1::8081 "$image" >/dev/null
  port=$(docker port "$container" 8081/tcp)
  port=${port##*:}
  url="http://127.0.0.1:$port"
  for ((i=0; i<60; i++)); do
    if curl --noproxy '*' -fsS -H 'Host: panel.example.test' "$url/healthz" -o "$tmp/health" 2>/dev/null; then
      printf 'ok\n' > "$tmp/expected"
      cmp "$tmp/expected" "$tmp/health"
      return
    fi
    sleep 1
  done
  docker logs "$container" >&2
  echo 'health deadline exceeded' >&2; return 1
}
login() {
  status=$(curl --noproxy '*' -sS -o "$tmp/login" -D "$tmp/headers" -w '%{http_code}' -H 'Host: panel.example.test' -H 'Origin: https://panel.example.test' -H 'Content-Type: application/json' --data '{"login":"admin","password":"container-smoke-password"}' "$url/api/login")
  [[ $status == 200 ]]
  cookie=$(grep -i '^set-cookie:' "$tmp/headers" | tr -d '\r')
  [[ $cookie == *Secure* && $cookie != *[Dd]omain=* ]]
}
stop() {
  docker stop -t 10 "$container" >/dev/null
  [[ $(docker inspect -f '{{.State.ExitCode}}' "$container") == 0 ]]
  docker rm "$container" >/dev/null
}
start
curl --noproxy '*' -fsS -H 'Host: panel.example.test' "$url/" -o "$tmp/index"
asset=$(grep -oE '/assets/[^" ]+\.(js|css)' "$tmp/index" | head -n 1)
[[ -n $asset ]]
curl --noproxy '*' -fsS -H 'Host: panel.example.test' "$url$asset" -o "$tmp/asset"
[[ -s $tmp/asset ]]
login
[[ $(curl --noproxy '*' -sS -o /dev/null -w '%{http_code}' -H 'Host: unknown.example.test' "$url/") == 403 ]]
[[ $(curl --noproxy '*' -sS -o /dev/null -w '%{http_code}' -X POST -H 'Host: mcp.example.test' "$url/mcp") == 401 ]]
stop
start
login
stop
docker run -d --name "$missing" --mount "type=volume,src=$empty,dst=/data" "$image" >/dev/null
[[ $(docker wait "$missing") != 0 ]]
docker logs "$missing" 2>&1 | grep -q 'owner not initialized; run owner init'
echo 'Container smoke passed'
