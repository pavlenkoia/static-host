# static-host

Публикация готовых статических сайтов через MCP: агент передаёт полный архив HTML/CSS/JS, сервис атомарно заменяет файлы и возвращает URL. Пользовательский код на сервере не выполняется.

Реализованы кабинет одного владельца, ключи агентов, ZIP/tar.gz upload, системные адреса и собственные домены при настройке ingress. Multi-user, SPA fallback, история содержимого и rollback не поддерживаются.

## Быстрый запуск

Требуются GNU Make, Go 1.27.4 или новее, Node.js 24 и свободный порт 8081.

```sh
cp .env.examples .env
chmod 600 .env
# Задайте в .env STATIC_HOST_ADMIN_PASSWORD длиной 12–72 байта.
make dev
```

Откройте `http://127.0.0.1:8081/`, логин — `admin`. Не коммитьте `.env`. `make dev` читает его как shell-файл, собирает UI и запускает сервер; бинарник сам `.env` не читает. Остановка — Ctrl+C.

## Документация

**[Вход в документацию — docs/README.md](docs/README.md)**

- [Локальный запуск](docs/getting-started.md) и [конфигурация](docs/reference/configuration.md).
- [MCP, публикация, лимиты и ошибки](docs/reference/mcp.md).
- [Архитектура и гарантии](docs/architecture.md).
- [Развёртывание в Uncloud](docs/operations/deployment.md) и [эксплуатация, backup, диагностика](docs/operations/maintenance.md).
- [Разработка и проверки](docs/development.md).
- [Планы multi-user и решения о доменах](docs/roadmap/custom-domains.md) и [история инфраструктуры](docs/history/infrastructure.md).

Для изменений только документации и проектной памяти — `make check-docs`; для остальных — `make check-fast`, для UI дополнительно `make check` с Playwright. При смешанных изменениях проверяется и документация; точные границы — в [правилах разработки](docs/development.md). Команды production, DNS и TLS требуют отдельного разрешения. Проектная память агентов — `.llm-wiki/`, правила — `AGENTS.md`.
