---
type: Source
title: Обязательные проверки разработки
sources: [raw/2026-11-25-development-checks.md, raw/2026-11-25-ci-check-split.md]
timestamp: 2026-11-25T13:18:43Z
---

# Обязательные проверки разработки

> Source: `raw/2026-11-25-development-checks.md`

До завершения каждого изменения выполняется `make check-fast`: установка зависимостей из lockfile, сборка Vue и Go-тесты с race detector без кэша. Для изменений интерфейса дополнительно выполняется полный `make check` с Chromium и браузерными сценариями на настоящем сервере с временной БД. GitHub Actions запускает быстрый gate на push и pull request; полный gate с браузером доступен вручную. Для запрета слияния требуется отдельно включить обязательный статус Required checks в защите основной ветки; наличие workflow само по себе не запрещает merge.

Правила завершения изменений закреплены в AGENTS.md. Связанные темы: [рабочий процесс](/notes/infrastructure-and-workflow.md), [локальный этап](/sources/local-owner-mcp.md).

## timeline

- time: 2026-11-25T11:36:46Z
  kind: decision
  summary: "Введён обязательный make check и CI с браузерными и backend проверками"

- time: 2026-11-25T13:18:43Z
  kind: decision
  summary: "Быстрый CI на push и PR; браузерные проверки локально для UI и вручную в CI"
