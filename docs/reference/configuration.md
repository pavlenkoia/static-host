# Конфигурация

[Навигация](../README.md) · [Быстрый старт](../getting-started.md)

## Параметры приложения

Явный флаг имеет приоритет над окружением. Бинарник сам не читает `.env`; это делает `make dev`, а при deploy переменные подставляет Compose.

| Флаг | Окружение | По умолчанию | Назначение |
| --- | --- | --- | --- |
| `--listen` | `STATIC_HOST_LISTEN` | `127.0.0.1:8081` | Числовой IP и порт 1–65535 |
| `--data-dir` | `STATIC_HOST_DATA_DIR` | `./data` | Непустой каталог постоянных данных |
| `--base-domain` | `STATIC_HOST_BASE_DOMAIN` | пусто | Базовый публичный домен |
| `--local-origin` | `STATIC_HOST_LOCAL_ORIGIN` | вычисляется из listener | Явный HTTP origin локального режима |
| нет | `STATIC_HOST_ADMIN_PASSWORD` | нет, обязателен | Пароль `admin`, 12–72 байта |

Базовый домен — ASCII hostname минимум из двух labels, не IP. Он приводится к нижнему регистру. Секреты не передавайте флагами и не коммитьте.

## Локальный режим

Без базового домена listener допускает только числовой loopback или wildcard (`0.0.0.0`, `::`). Wildcard расширяет bind, но не список доверенных Host: управляющий origin по умолчанию остаётся loopback с тем же портом.

Для доверенной сети задайте wildcard listener и `STATIC_HOST_LOCAL_ORIGIN=http://<IP-сервера>:<порт>`. Origin должен содержать конкретный числовой IP, совпадающий с listener порт, без пути и завершающего `/`. Принимается только настроенный управляющий Host; переданный Origin проверяется строго. `--local-origin` несовместим с непустым базовым доменом.

HTTP не шифрует пароль, сессии и ключи. Этот режим не подходит для недоверенной сети. Системные сайты всё равно получают адреса на `.localhost`.

## Публичный режим

| Поверхность | Адрес |
| --- | --- |
| Кабинет | `https://panel.<домен>/` |
| MCP Streamable HTTP | `https://mcp.<домен>/mcp` |
| Capability upload | `https://upload.<домен>/uploads/<id>?token=<секрет>` |
| Сайт | `https://<slug>.sites.<домен>/` |

Внешний прокси завершает TLS и сохраняет Host. Приложение не настраивает DNS и не выпускает сертификаты. Compose содержит явные panel/mcp/upload маршруты и wildcard сайтов; подготовка ingress описана в [развёртывании](../operations/deployment.md).

## Контейнер и Compose

Образ задаёт `STATIC_HOST_LISTEN=0.0.0.0:8081` и `STATIC_HOST_DATA_DIR=/data`, работает от UID/GID 10001. Compose требует `STATIC_HOST_BASE_DOMAIN` и `STATIC_HOST_ADMIN_PASSWORD`; `STATIC_HOST_MACHINE` выбирает ноду Uncloud, но не является параметром Go-приложения. Постоянный bind mount — `/var/lib/static-host:/data`.

Источники: [config.go](../../internal/config/config.go), [Dockerfile](../../Dockerfile), [compose.yaml](../../compose.yaml).
