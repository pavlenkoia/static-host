---
type: Source
title: Checks только на pull request
sources: [raw/static-host-pr-only-checks.md]
timestamp: 2026-11-25T14:15:40Z
---

# Checks только на pull request

> Source: `raw/static-host-pr-only-checks.md`

По решению пользователя из `.github/workflows/check.yml` удалён `push`; Checks выполняет `make check-fast` только на `pull_request`. Ручной выпуск образа сохраняет собственную проверку перед сборкой и smoke. Локальные обязательные проверки не менялись.

## timeline

- time: 2026-11-25T14:15:40Z
  kind: decision
  summary: "Checks запускается только на pull request по решению пользователя"
