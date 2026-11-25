---
type: Source
title: Браузерные тесты только локально
sources: [raw/static-host-no-browser-ci.md]
timestamp: 2026-11-25T14:02:52Z
---

# Браузерные тесты только локально

> Source: `raw/static-host-no-browser-ci.md`

По решению пользователя браузерные проверки админки не запускаются в GitHub Actions. Удалён `.github/workflows/browser.yml`; локальные Playwright-тесты и `make check` сохранены. CI сохраняет быстрые проверки, сборку контейнера, smoke и публикацию в GHCR.

## timeline

- time: 2026-11-25T14:02:52Z
  kind: decision
  summary: "Браузерные проверки исключены из GitHub Actions по решению пользователя"
