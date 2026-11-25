---
type: Source
title: Первый deploy в Uncloud
sources: [raw/static-host-first-deploy.md]
timestamp: 2026-11-25T14:59:35Z
---

# Первый deploy в Uncloud

> Source: `raw/static-host-first-deploy.md`

По разрешению пользователя сервис развёрнут на uc-1: контейнер healthy, постоянный каталог /var/lib/static-host принадлежит 10001:10001 с правами 0700. Ресурсы ноды: 2 CPU, 2 GiB RAM, около 17 GiB свободного диска. Compose исправлен для передачи обязательного STATIC_HOST_BASE_DOMAIN. Образ latest соответствует sha-02cd4382e7dec22dcb09dece7ee5ff77ddbafb82; анонимный доступ подтверждён. Проверены публичный доверенный HTTPS панели/MCP, health, вход admin, отказ без MCP-ключа и успешный авторизованный initialize. Временный проверочный ключ удалён, сессии завершены. DNS и глобальный Caddy вручную не менялись. Backup и восстановление не настроены/не проверены; публикация сайтов не реализована.

## timeline

- time: 2026-11-25T14:59:35Z
  kind: evidence
  summary: "Uncloud deploy healthy; публичный TLS, вход admin и MCP initialize проверены"
