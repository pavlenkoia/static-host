# Проверка динамических доменов на установленной версии

Дата: 2026-11-25. Основание — наблюдавшиеся команды и изолированный runtime-тест в ходе проверки, заказанной владельцем. Production не изменялся.

## Фактические версии

Команды чтения: uc version; uc machine ls; uc inspect caddy; uc caddy config; uc exec -T caddy caddy version.
Обе ноды: Uncloud 0.21.0. CLI commit: c58108aa829cca4fe3b5f837e0f1918be1687599. Caddy на edge: 2.11.7, build hash h1:yj0Y4fYZGPkSvibBJ1sTWE33xC0fxztVyXEW5iIdUT4=.

## Исходники Uncloud

Клонирован тег v0.21.0 с тем же commit. internal/machine/caddyconfig/caddyfile.go подтверждает глобальный конфиг Caddy-сервиса и добавление x-caddy приложений. template.go поддерживает upstreams с именем сервиса и портом. pkg/client/compose/service.go переносит x-caddy в спецификацию и запрещает совмещение с ingress x-ports.
Ссылки:
- https://github.com/psviderski/uncloud/tree/v0.21.0/internal/machine/caddyconfig
- https://github.com/psviderski/uncloud/blob/v0.21.0/pkg/client/compose/service.go

## Изолированный тест

Локальный отдельный Docker-контейнер caddy:2.11.7 имел ту же версию и build hash. Production volumes не подключались. Порты: 19080/19443, тестовый HTTP backend и ask на 19081. Конфигурация объединяла глобальный on_demand_tls/ask, явные маршруты другого сервиса и панели, HTTPS catch-all с tls internal/on_demand и reverse_proxy, HTTP endpoint /.uncloud-verify. Домены под example.test; внутренний CA, без публичного ACME. Тестовый backend имитировал разрешения в памяти, это не проверка готового приложения.

Наблюдавшийся вывод:
PASS: Caddy 2.11.7 accepts combined global, explicit routes and catch-all config
PASS: explicit other-service and panel routes beat catch-all
PASS: unknown domain refused through ask
PASS: domain added after startup gets local certificate and reaches backend, no reload
PASS: removed domain serves no site even with existing certificate
PASS: Uncloud HTTP verification endpoint preserved
NOTE: isolated internal CA; no public ACME or production changes

caddy adapt --validate завершился успешно. Временный контейнер остановлен, тестовый скрипт удалён.

## Вывод и границы

Необходимые механизмы поддерживаются Uncloud 0.21.0 и Caddy 2.11.7, обновление Uncloud ради них не требуется. Исходники Uncloud исследованы, поведение Caddy проверено запуском; реальный uc deploy с этой конфигурацией не выполнялся.
Не проверены публичный ACME/Let's Encrypt, DNS, wildcard TLS, связь будущего ask между нодами, итоговый production Compose, HTTPS readiness и диагностика ошибок приложения. Реальное приложение для доменов и ask ещё не реализовано. Это подтверждение механизма, не готовности production. Конфиги, DNS и сервисы production не менялись.
