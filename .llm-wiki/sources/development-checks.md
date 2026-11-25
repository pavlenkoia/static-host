---
type: Source
title: Обязательные проверки разработки
sources: [raw/2026-11-25-development-checks.md]
timestamp: 2026-11-25T11:36:46Z
---

# Обязательные проверки разработки

> Source: `raw/2026-11-25-development-checks.md`

Обязательный gate разработки — `make check`: установка зависимостей из lockfile и Chromium, сборка Vue, Go-тесты с race detector без кэша и браузерные сценарии на настоящем сервере с временной БД. GitHub Actions выполняет тот же gate на push и pull request. Для запрета слияния требуется отдельно включить обязательный статус Required checks в защите основной ветки; наличие workflow само по себе не запрещает merge.

Правила завершения изменений закреплены в AGENTS.md. Связанные темы: [рабочий процесс](/notes/infrastructure-and-workflow.md), [локальный этап](/sources/local-owner-mcp.md).

## timeline

- time: 2026-11-25T11:36:46Z
  kind: decision
  summary: "Введён обязательный make check и CI с браузерными и backend проверками"
