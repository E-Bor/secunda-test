# Secunda

REST API для командных задач. Реализованы:

- Регистрация, JWT-аутентификация, команды с ролями `owner`, `admin`, `member` и изоляция данных.
- Задачи со статусами и исполнителями, комментарии и история изменений.
- Транзакции и контроль версии задачи для защиты от потери конкурентных изменений.
- Фильтры, offset- и курсорная пагинация.
- Redis-кеш на 5 минут с инвалидацией при изменениях и circuit breaker.
- SQL-отчёт: задачи по статусам, топ-3 исполнителей за 30 дней, среднее время закрытия и комментарии.
- Rate limiting, JSON-логи, graceful shutdown, OpenAPI, unit- и интеграционные тесты.

Стек: Go 1.27.3, `net/http`, `database/sql`, MySQL 9.7, Redis 8.10, Docker Compose.

## Быстрый запуск

Нужны Docker с Compose v2, Make и OpenSSL.

```sh
make init
make up
curl -fsS http://localhost:8080/healthz
```

`make init` создаёт `.env` со случайными секретами и сохраняет существующий файл. Настройки — в [.env.example](.env.example). Compose поднимает зависимости, применяет миграции и запускает API.

- API: `http://localhost:8080/api/v1`.
- OpenAPI: [docs/openapi.yaml](docs/openapi.yaml) или `http://localhost:8080/openapi.yaml`.

остановка:

```sh
make down
```

## Пример работы с API

Для примера нужен `jq`. 

```sh
API=http://localhost:8080/api/v1

curl -fsS "$API/register" -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"strong-password","name":"Alice"}'

TOKEN=$(curl -fsS "$API/login" -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"strong-password"}' | jq -r .token)

TEAM_ID=$(curl -fsS "$API/teams" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"Backend"}' | jq -r .id)

curl -fsS "$API/tasks" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"team_id\":$TEAM_ID,\"title\":\"Implement API\"}"

curl -fsS "$API/tasks?team_id=$TEAM_ID&status=todo&limit=20" \
  -H "Authorization: Bearer $TOKEN"
```

Все бизнес-операции доступны только внутри своей команды. Owner и admin управляют любыми её задачами и видят отчёт; member редактирует созданные им задачи, а в назначенных ему чужих задачах меняет только статус.

При `PUT /api/v1/tasks/{id}` передавайте текущую `version`: устаревшая версия возвращает `409`. Полный контракт запросов и ответов описан в OpenAPI.

## Проверки

```sh
make test
make vet
make integration
```

`test` и `vet` требуют локальный Go 1.27.3. `integration` запускает тесты с `-race` в Docker на отдельных MySQL и Redis. Проверяются права, SQL-отчёт, транзакции, конкурентные изменения, кеш и сценарии безопасности.

## Структура

```text
cmd/api                      запуск сервера и миграций
internal/app                 бизнес-логика и SQL, включая stats.sql
internal/httpapi             HTTP-обработчики, авторизация и лимиты
internal/cache               Redis и circuit breaker
internal/database            подключение и SQL-миграции
internal/config              настройки из окружения
internal/testutil            окружение интеграционных тестов
docs/openapi.yaml            контракт API
```
