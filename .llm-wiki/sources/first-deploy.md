---
type: Source
title: Первый deploy в Uncloud
sources: [raw/static-host-first-deploy.md]
timestamp: 2026-11-25T19:22:55Z
---

# Первый deploy в Uncloud

> Source: `raw/static-host-first-deploy.md`

По разрешению пользователя сервис развёрнут на uc-1: контейнер healthy, постоянный каталог /var/lib/static-host принадлежит 10001:10001 с правами 0700. Ресурсы ноды: 2 CPU, 2 GiB RAM, около 17 GiB свободного диска. Compose исправлен для передачи обязательного STATIC_HOST_BASE_DOMAIN. Образ latest соответствует sha-02cd4382e7dec22dcb09dece7ee5ff77ddbafb82; анонимный доступ подтверждён. Проверены публичный доверенный HTTPS панели/MCP, health, вход admin, отказ без MCP-ключа и успешный авторизованный initialize. Временный проверочный ключ удалён, сессии завершены. DNS и глобальный Caddy вручную не менялись. Backup и восстановление не настроены/не проверены; публикация сайтов не реализована.

Последующее обновление: на `uc-1` развёрнут образ коммита `9ef275c290f65c3a46364dfaa2f3300c390669b2`, закреплённый по digest `sha256:1c4516806162f78d92a405034ca91792b277688f591259e53ca687dbf8623346`. Контейнер `44cec3c63f29` healthy. Публичные HTTPS-проверки: панель и health панели/upload — 200, MCP без ключа — 401. Обычный deploy с `latest` использовал старый локальный образ; для обновления применён временный Compose override с точным digest. DNS и глобальный Caddy вручную не менялись.

## timeline

- time: 2026-11-25T14:59:35Z
  kind: evidence
  summary: "Uncloud deploy healthy; публичный TLS, вход admin и MCP initialize проверены"

- time: 2026-11-25T19:22:55Z
  kind: evidence
  summary: "Развёрнут 9ef275c по точному digest; healthy и публичные HTTPS endpoints проверены; выявлен кэш latest в Uncloud"
