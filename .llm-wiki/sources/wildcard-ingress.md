---
type: Source
title: Публичный wildcard ingress системных сайтов
sources: [raw/static-host-wildcard-ingress.md]
timestamp: 2026-11-25T19:43:45Z
---

# Публичный wildcard ingress системных сайтов

> Source: `raw/static-host-wildcard-ingress.md`

На edge-1 развёрнут Caddy 2.11.7 с модулем Cloudflare v0.2.4 из `deploy/caddy`; Compose объявляет wildcard `*.sites.uc.afbmarket.ru`. Существующий DNS уже указывал на ingress, DNS-записи не менялись. Let's Encrypt выпустил wildcard-сертификат через DNS challenge. Секрет хранится отдельным файлом root:0600 вне репозитория и образа, читается entrypoint через существующий bind mount и доступен Caddy для продления. Собственные домены и On-Demand TLS не включены.

Сквозная проверка подтвердила MCP create → HTTPS ZIP upload → publish → доверенный публичный HTTPS с точным HTML. Управляющие endpoints на сайте возвращают 404. Временные сайт, MCP-ключ и сессия удалены; панель/upload health возвращают 200, MCP без ключа — 401. Caddy и приложение healthy. `make check-fast` и проверка Compose прошли. Приложение сохранено на digest выпуска 9ef275c. Инструкции эксплуатации — README. Связано: [домены и безопасность](/notes/domains-and-security.md).

## timeline

- time: 2026-11-25T19:43:45Z
  kind: evidence
  summary: "Публичный wildcard TLS и сквозная MCP публикация проверены"
