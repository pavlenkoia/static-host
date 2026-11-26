#!/usr/bin/env bash
set -euo pipefail

if [[ $# != 1 || -z $1 ]]; then echo "usage: $0 IMAGE" >&2; exit 2; fi
image=$1
suffix="$(date +%s)-$$-$RANDOM"
volume="static-host-smoke-$suffix"
empty="static-host-smoke-empty-$suffix"
container="static-host-smoke-$suffix"
missing="static-host-smoke-missing-$suffix"
invalid="static-host-smoke-invalid-$suffix"
tmp=$(mktemp -d)
password=container-smoke-password
new_password=container-smoke-password-updated
cleanup() {
  docker rm -f "$container" "$missing" "$invalid" >/dev/null 2>&1 || true
  docker volume rm "$volume" "$empty" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT

docker volume create "$volume" >/dev/null
docker volume create "$empty" >/dev/null

start() {
  docker run -d --name "$container" \
    --health-interval=1s --health-start-period=1s \
    --mount "type=volume,src=$volume,dst=/data" \
    -e STATIC_HOST_BASE_DOMAIN=example.test \
    -e "STATIC_HOST_ADMIN_PASSWORD=$password" \
    -p 127.0.0.1::8081 "$image" >/dev/null
  port=$(docker port "$container" 8081/tcp)
  port=${port##*:}
  url="http://127.0.0.1:$port"
  for ((i=0; i<60; i++)); do
    if curl --noproxy '*' -fsS -H 'Host: panel.example.test' "$url/healthz" -o "$tmp/health" 2>/dev/null; then
      printf 'ok\n' > "$tmp/expected"
      cmp "$tmp/expected" "$tmp/health"
      for ((health_attempt=0; health_attempt<10; health_attempt++)); do
        if [[ $(docker inspect --format '{{.State.Health.Status}}' "$container") == healthy ]]; then
          return
        fi
        sleep 1
      done
      echo 'container healthcheck did not become healthy' >&2
      docker inspect --format '{{json .State.Health}}' "$container" >&2
      return 1
    fi
    sleep 1
  done
  docker logs "$container" >&2
  echo 'health deadline exceeded' >&2
  return 1
}

login() {
  login_password=$1
  status=$(curl --noproxy '*' -sS -o "$tmp/login" -D "$tmp/headers" -w '%{http_code}' \
    -H 'Host: panel.example.test' -H 'Origin: https://panel.example.test' \
    -H 'Content-Type: application/json' \
    --data "{\"login\":\"admin\",\"password\":\"$login_password\"}" "$url/api/login")
  [[ $status == 200 ]]
  cookie=$(grep -i '^set-cookie:' "$tmp/headers" | tr -d '\r')
  [[ $cookie == *Secure* && $cookie != *[Dd]omain=* ]]
}

stop() {
  docker stop -t 10 "$container" >/dev/null
  [[ $(docker inspect -f '{{.State.ExitCode}}' "$container") == 0 ]]
  docker rm "$container" >/dev/null
}

# A fresh data volume bootstraps the fixed admin account without a CLI step.
start
curl --noproxy '*' -fsS -H 'Host: panel.example.test' "$url/" -o "$tmp/index"
asset=$(grep -oE '/assets/[^" ]+\.(js|css)' "$tmp/index" | head -n 1)
[[ -n $asset ]]
curl --noproxy '*' -fsS -H 'Host: panel.example.test' "$url$asset" -o "$tmp/asset"
[[ -s $tmp/asset ]]
status=$(curl --noproxy '*' -sS -o /dev/null -w '%{http_code}' \
  -H 'Host: panel.example.test' -H 'Origin: https://panel.example.test' \
  -H 'Content-Type: application/json' \
  --data '{"login":"owner","password":"container-smoke-password"}' "$url/api/login")
[[ $status == 401 ]]
login "$password"
old_cookie=$(grep -i '^set-cookie:' "$tmp/headers" | sed -E 's/^[^:]+: *([^;]+).*/\1/' | tr -d '\r')
csrf=$(jq -r '.csrf_token' "$tmp/login")
status=$(curl --noproxy '*' -sS -o "$tmp/key" -w '%{http_code}' -X POST \
  -H 'Host: panel.example.test' -H 'Origin: https://panel.example.test' \
  -H "Cookie: $old_cookie" -H "X-CSRF-Token: $csrf" \
  -H 'Content-Type: application/json' \
  --data '{"name":"container-smoke-key","expires_in_days":null}' "$url/api/keys")
[[ $status == 201 ]]
token=$(jq -er '.token' "$tmp/key")
key_id=$(jq -er '.key.id' "$tmp/key")
[[ -n $token && -n $key_id ]]
printf '%s' '<h1>container fixture</h1>' > "$tmp/index.html"
tar -czf "$tmp/site.tar.gz" -C "$tmp" index.html
mcp_call() {
  curl --noproxy '*' -fsS -H 'Host: mcp.example.test' -H "Authorization: Bearer $token" \
    -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
    -H 'MCP-Protocol-Version: 2025-11-25' --data "$1" "$url/mcp"
}
mcp_call '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sites_create","arguments":{"name":"container site"}}}' > "$tmp/site.json"
site_id=$(jq -er '.result.structuredContent.site.id' "$tmp/site.json")
mcp_call "$(jq -cn --arg id "$site_id" '{jsonrpc:"2.0",id:1,method:"tools/call",params:{name:"uploads_create",arguments:{site_id:$id,format:"tar.gz",expected_generation:0}}}')" > "$tmp/grant.json"
upload_id=$(jq -er '.result.structuredContent.upload_id' "$tmp/grant.json")
capability=$(jq -er '.result.structuredContent.upload_url | split("?")[1]' "$tmp/grant.json")
curl --noproxy '*' -fsS -X PUT -H 'Host: upload.example.test' -H "Authorization: Bearer $token" \
  -H 'Content-Type: application/gzip' --data-binary @"$tmp/site.tar.gz" "$url/uploads/$upload_id?$capability" > /dev/null
mcp_call "$(jq -cn --arg site "$site_id" --arg upload "$upload_id" '{jsonrpc:"2.0",id:1,method:"tools/call",params:{name:"sites_publish",arguments:{site_id:$site,upload_id:$upload,expected_generation:0,idempotency_key:"container-publish"}}}')" > "$tmp/published.json"
[[ $(jq -r '.result.structuredContent.site.generation' "$tmp/published.json") == 1 ]]
[[ $(curl --noproxy '*' -fsS -H "Host: $site_id.sites.example.test" "$url/") == '<h1>container fixture</h1>' ]]
[[ $(curl --noproxy '*' -sS -o /dev/null -w '%{http_code}' -H 'Host: unknown.example.test' "$url/") == 404 ]]
[[ $(curl --noproxy '*' -sS -o /dev/null -w '%{http_code}' -X POST -H 'Host: mcp.example.test' "$url/mcp") == 401 ]]
stop

# Restarting with a new runtime password invalidates sessions but preserves MCP keys.
password=$new_password
start
[[ $(curl --noproxy '*' -sS -o /dev/null -w '%{http_code}' \
  -H 'Host: panel.example.test' -H "Cookie: $old_cookie" "$url/api/session") == 401 ]]
status=$(curl --noproxy '*' -sS -o /dev/null -w '%{http_code}' \
  -H 'Host: panel.example.test' -H 'Origin: https://panel.example.test' \
  -H 'Content-Type: application/json' \
  --data "{\"login\":\"admin\",\"password\":\"container-smoke-password\"}" "$url/api/login")
[[ $status == 401 ]]
login "$new_password"
cookie=$(grep -i '^set-cookie:' "$tmp/headers" | sed -E 's/^[^:]+: *([^;]+).*/\1/' | tr -d '\r')
csrf=$(jq -r '.csrf_token' "$tmp/login")
[[ $(curl --noproxy '*' -fsS -H "Host: $site_id.sites.example.test" "$url/") == '<h1>container fixture</h1>' ]]
status=$(curl --noproxy '*' -sS -o "$tmp/revealed" -w '%{http_code}' -X POST \
  -H 'Host: panel.example.test' -H 'Origin: https://panel.example.test' \
  -H "Cookie: $cookie" -H "X-CSRF-Token: $csrf" "$url/api/keys/$key_id/reveal")
[[ $status == 200 && $(jq -r '.token' "$tmp/revealed") == "$token" ]]
stop

docker run -d --name "$missing" --mount "type=volume,src=$empty,dst=/data" \
  -e STATIC_HOST_BASE_DOMAIN=example.test "$image" >/dev/null
[[ $(docker wait "$missing") != 0 ]]
docker logs "$missing" 2>&1 | grep -q 'STATIC_HOST_ADMIN_PASSWORD'
docker run -d --name "$invalid" --mount "type=volume,src=$empty,dst=/data" \
  -e STATIC_HOST_BASE_DOMAIN=example.test -e STATIC_HOST_ADMIN_PASSWORD=short "$image" >/dev/null
[[ $(docker wait "$invalid") != 0 ]]
docker logs "$invalid" 2>&1 | grep -q '12–72 bytes'

echo 'Container smoke passed'
