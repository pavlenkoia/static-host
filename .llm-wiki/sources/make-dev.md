---
type: Source
title: Локальный запуск make dev
sources: [raw/static-host-make-dev.md]
timestamp: 2026-11-25T14:45:42Z
---

# Локальный запуск make dev

> Source: `raw/static-host-make-dev.md`

`make dev` требует корневой `.env`, экспортирует его shell-переменные, устанавливает зависимости из lockfile, собирает интерфейс и запускает Go-сервер без базового домена для локального входа. Адрес и каталог данных настраиваются через окружение. Вход: admin и STATIC_HOST_ADMIN_PASSWORD. Сам Go-бинарник .env не читает. Секреты не коммитить; .env — доверенный shell-файл, значения со спецсимволами требуют кавычек. Production и CI не меняются.

## timeline

- time: 2026-11-25T14:45:42Z
  kind: decision
  summary: "Добавлен make dev с загрузкой .env и локальным входом"
