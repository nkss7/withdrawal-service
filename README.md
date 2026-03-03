# Withdrawal Service

Сервис для создания заявок на вывод средств с идемпотентностью, защитой от двойного списания и базовой аутентификацией.

## Быстрый старт

### Запуск сервиса
```bash
docker compose up --build -d
```

Сервис будет доступен на `http://localhost:8080`.  
Миграции применяются автоматически при старте PostgreSQL (`migrations/001_init.sql`).

### Запуск интеграционных тестов
```bash
DATABASE_URL="postgres://withdrawal:withdrawal@localhost:5432/withdrawal?sslmode=disable" \
AUTH_TOKEN="test-secret-token" \
go test -v -race -count=1 -tags=integration ./tests/...
```

## Ключевые архитектурные решения

### Двухуровневая блокировка

Для снижения нагрузки на PostgreSQL применяется **двухуровневая стратегия**:

1. **In-memory mutex per `user_id`** (`internal/locker`) — сериализует конкурентные запросы на один баланс внутри процесса, до обращения к БД. Использует `sync.Mutex` per user с reference counting для автоматической очистки.
2. **PostgreSQL `SELECT ... FOR UPDATE`** — гарантирует корректность при горизонтальном масштабировании (несколько инстансов).

```
Горутина 1 (user_id=A)  →  Lock(A)  →  DB TX  →  Unlock(A)
Горутина 2 (user_id=A)  →  ждёт Lock(A)...    →  Lock(A)  →  DB TX  →  Unlock(A)
Горутина 3 (user_id=B)  →  Lock(B)  →  DB TX  →  Unlock(B)   // параллельно
```

### Защита от двойного списания

Корректность обеспечивается **пессимистичной блокировкой на уровне строки** в PostgreSQL:

```sql
SELECT amount FROM balances WHERE user_id = $1 FOR UPDATE
```

`SELECT ... FOR UPDATE` внутри транзакции блокирует строку баланса пользователя. Любой параллельный запрос на тот же баланс будет ждать завершения текущей транзакции. Это гарантирует, что проверка баланса, списание, создание withdrawal и запись в ledger происходят атомарно в одной транзакции.

Уровень изоляции: `READ COMMITTED` (достаточен в сочетании с `FOR UPDATE`).

### Идемпотентность

- **UNIQUE constraint** на `idempotency_key` в таблице `withdrawals` — БД-уровневая защита от дублей.
- При совпадении `idempotency_key` — сравнение payload (user_id, amount, currency, destination):
  - Payload совпадает → возврат существующего результата (без повторного списания).
  - Payload отличается → `422 Unprocessable Entity`.
- **Race condition** (два одновременных запроса с одним ключом): если INSERT падает с `unique_violation`, сервис повторно проверяет существующую запись.

### Аутентификация

Фиксированный Bearer-токен из переменной окружения `AUTH_TOKEN`. Middleware проверяет заголовок `Authorization: Bearer <token>` на каждый запрос.

### Безопасность ответов

Внутренние ошибки PostgreSQL и Go не раскрываются клиенту — возвращается `{"code":500,"message":"internal server error"}`. Детали логируются серверно через `log/slog`.


## Тесты

### Покрытие тестами

| Тест | Что проверяет |
|------|---------------|
| `TestCreateWithdrawalSuccess` | Успешное создание и получение по ID |
| `TestCreateWithdrawalInsufficientBalance` | Отказ при нехватке средств |
| `TestCreateWithdrawalInvalidAmount` | Отказ при невалидной сумме |
| `TestIdempotencySamePayload` | Повторный запрос возвращает тот же результат |
| `TestIdempotencyDifferentPayload` | Конфликт при смене payload с тем же ключом |
| `TestConcurrentWithdrawals` | 20 параллельных запросов — ровно 10 успехов |
| `TestUnauthorizedRequest` | Отказ без авторизации |
| `TestConfirmWithdrawal` | Успешное подтверждение вывода |

## Переменные окружения

| Переменная | Описание | По умолчанию |
|------------|----------|-------------|
| `DATABASE_URL` | Строка подключения к PostgreSQL | обязательная |
| `SERVER_PORT` | Порт HTTP-сервера | `8080` |
| `AUTH_TOKEN` | Bearer-токен для аутентификации | обязательная |

## Технологии

- **Go 1.24** — `net/http` с pattern routing, `log/slog`
- **PostgreSQL 16** — транзакции, `SELECT FOR UPDATE`, UNIQUE constraints
- **Docker Compose** — локальный запуск
- Без внешних Go-фреймворков
