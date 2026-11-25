# Checks только на pull request

2026-11-25 пользователь утвердил удаление триггера push из .github/workflows/check.yml. Workflow Checks сохраняет только pull_request и выполняет make check-fast. Прямой push не запускает Checks. Ручной выпуск образа по-прежнему выполняет собственный make check-fast перед сборкой и smoke. Локальные обязательные проверки не меняются.
