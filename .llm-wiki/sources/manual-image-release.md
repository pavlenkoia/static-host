---
type: Source
title: Ручной выпуск контейнера
sources: [raw/static-host-manual-image-release.md]
timestamp: 2026-11-25T14:08:45Z
---

# Ручной выпуск контейнера

> Source: `raw/static-host-manual-image-release.md`

Workflow Container image запускается только вручную через `workflow_dispatch`, job разрешён только для `refs/heads/main`. Автоматические сборки образа на push/PR отключены из-за стоимости и частоты. Сохранены `make check-fast`, сборка linux/amd64, smoke перед публикацией в GHCR с тегами `sha-<полный SHA>` и `latest`. Deploy не выполняется; остальные workflows не менялись.

## timeline

- time: 2026-11-25T14:08:45Z
  kind: decision
  summary: "Выпуск образа только вручную из main по решению пользователя"
