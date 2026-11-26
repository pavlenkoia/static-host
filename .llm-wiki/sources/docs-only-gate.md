---
type: Source
title: Отдельный gate документации
sources: [raw/static-host-docs-only-gate.md]
timestamp: 2026-11-26T06:48:01Z
---

# Отдельный gate документации

> Source: `raw/static-host-docs-only-gate.md`

Для изменений только README, AGENTS, Markdown в docs и проектной памяти введён `make check-docs`: локальные ссылки, H1, fenced blocks и строгий OKF lint без сборки приложения. Код, конфигурация, Makefile и скрипты проверок не подпадают под исключение. Смешанные изменения требуют обоих применимых gates. CI не меняется. Протокол OKF загружается как установленный skill, в omp через `skill://llm-wiki-okf`, без предположения о локальном каталоге skills. Связано: [инфраструктура и рабочий процесс](/notes/infrastructure-and-workflow.md).

## timeline

- time: 2026-11-26T06:48:01Z
  kind: decision
  summary: "По запросу владельца выделен docs-only gate и исправлен способ загрузки skill в AGENTS"
