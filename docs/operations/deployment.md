# Развёртывание

[Навигация](../README.md) · [Конфигурация](../reference/configuration.md) · [Эксплуатация](maintenance.md)

Все команды выполняются из корня репозитория. Изменения production, DNS и TLS требуют отдельного явного разрешения.

## Образ и размещение

Образ собирается для `linux/amd64`: `docker build -t static-host:smoke .`, затем `bash scripts/container-smoke.sh static-host:smoke`. Smoke использует временные Docker volumes и не затрагивает `./data`. Образ работает от UID/GID 10001, хранит данные в `/data` и слушает `0.0.0.0:8081`; для запуска сервера требуется `STATIC_HOST_BASE_DOMAIN`. Listener можно переопределить через `STATIC_HOST_LISTEN`.

`compose.yaml` предназначен для одного экземпляра на выбранной ноде Uncloud. Скопируйте `.env.examples` в `.env`, задайте свои `STATIC_HOST_BASE_DOMAIN`, `STATIC_HOST_MACHINE` и `STATIC_HOST_ADMIN_PASSWORD`. Compose требует пароль через обязательную интерполяцию; пример оставляет секрет пустым намеренно, его нужно заменить. Выбирайте случайный секрет длиной 12–72 байта. Защитите `.env` правами доступа и не добавляйте его в систему контроля версий. Передача пароля через переменную окружения означает, что привилегированный пользователь Docker может увидеть его в `docker inspect`; ограничьте доступ к Docker daemon и рабочей ноде. Не передавайте пароль в командной строке. Образ и каталог данных `/var/lib/static-host` заданы в Compose. Compose объявляет маршруты panel/mcp/upload и wildcard системных сайтов. DNS и глобальная конфигурация Caddy отдельно подготавливаются оператором; при обращении к wildcard Caddy использует DNS challenge для TLS. Обновление stop-first допускает простой; не запускайте две копии с одной SQLite.

На выбранной ноде создайте **новый** каталог данных с владельцем `10001:10001` и правами `0700`: `sudo install -d -m 0700 -o 10001 -g 10001 /var/lib/static-host`. Не применяйте эту команду к существующему произвольному каталогу; для существующих данных сохраняйте текущие права и владельца. При первом старте приложение автоматически создаёт состояние администратора. Последующие старты используют новый пароль из окружения; старый пароль перестаёт работать, браузерные сессии инвалидируются, а ключи агентов и `agent-key-encryption.key` сохраняются. Резервируйте SQLite и `agent-key-encryption.key` вместе вне ноды и проверяйте восстановление.

После подготовки и отдельного разрешения на развёртывание: `uc deploy -f compose.yaml --no-build`. Это не настраивает DNS или глобальный Caddy и не проверяет доступность кластера.

### Системные сайты: wildcard ingress

`compose.yaml` маршрутизирует `*.sites.${STATIC_HOST_BASE_DOMAIN}` на приложение. Для этой зоны нужны wildcard DNS на публичный ingress и Caddy с модулем Cloudflare; обычный образ Caddy не подходит. Собственные домены и On-Demand TLS этим не включаются.

Сборка и доставка Caddy на публичную ноду:

```sh
docker build -t static-host-caddy:2.11.7-cloudflare-0.2.4 deploy/caddy
uc image push static-host-caddy:2.11.7-cloudflare-0.2.4 -m edge-1
uc caddy deploy --image static-host-caddy:2.11.7-cloudflare-0.2.4 -m edge-1
```

Перед обновлением Caddy установите Cloudflare API-токен в `/var/lib/uncloud/caddy/cloudflare-api-token` на edge-1: владелец root, права 0600. Токен должен иметь Zone:DNS:Edit и Zone:Zone:Read только для нужной зоны. Entrypoint читает файл через существующий bind mount `/etc/caddy` и передаёт токен процессу Caddy через окружение; значение не включается в образ, Compose или генерируемый Caddyfile. Не выводите окружение процесса и не коммитьте секрет. Этот файл нужен при каждом запуске Caddy и для автоматического продления сертификата.

Обновление приложения выполняйте с Compose override, закрепляющим `services.static-host.image` по digest опубликованного выпуска: `uc deploy -f compose.yaml -f /путь/release.yaml --no-build --yes`. Uncloud может использовать закэшированный `latest` без загрузки нового образа. Перед применением wildcard-маршрута сначала обновите Caddy; сохраните существующий глобальный Caddyfile через `--caddyfile`, если он настроен. Не редактируйте генерируемый Caddyfile. Проверяйте trusted HTTPS реального опубликованного сайта, а не только health приложения.

Workflow **Container image** запускается только вручную: GitHub → Actions → Container image → Run workflow → ветка `main` (либо `gh workflow run image.yml --ref main`). На push и PR образ не собирается; при выборе другой ветки job пропускается. Выпуск выполняет `make check-fast`, сборку и smoke, затем публикует в GHCR теги `sha-<полный SHA>` и `latest`. Deploy не выполняется. После первой публикации владелец должен один раз выбрать package → **Change visibility → Public**, затем подтвердить публичность анонимным `docker pull ghcr.io/<owner>/<repo>:<tag>` без сохранённой авторизации. Публикация сама по себе не подтверждает публичную видимость.

Источники: [Compose](../../compose.yaml), [Dockerfile](../../Dockerfile), [образ Caddy](../../deploy/caddy/Dockerfile), [workflow выпуска](../../.github/workflows/image.yml). Историческое состояние конкретного кластера — не предусловие для новой установки: см. [датированные проверки](../history/infrastructure.md).
