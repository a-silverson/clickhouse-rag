# clickhouse-rag

Go RAG-сервис для базы знаний и экстракции микросервисов, их API и топиков Pulsar. Векторы и строки хранятся в ClickHouse. Эмбеддинги считает Ollama. Извлечение структурированных строк может идти через Ollama или OpenAI (`LLM_PROVIDER`).

Образец `pet/sasha/rag-service` использовал PostgreSQL и внешний RAG API. Здесь поиск идёт по `cosineDistance` в ClickHouse, а роль внешнего RAG API выполняет Ollama `/api/embed`. HTTP-контракт и MCP-инструменты сохранены. Отдельный Next.js UI в этот репозиторий не переносился: его исходников не было, контракт для него — HTTP API ниже.

## Запуск

Локальный ClickHouse слушает native-порт `127.0.0.1:9000` и HTTP `127.0.0.1:8123`. Пользователь `default` без пароля. Ollama — `127.0.0.1:11434`, с моделью из `OLLAMA_EMBED_MODEL` (по умолчанию `nomic-embed-text`, 768 измерений).

```bash
docker compose up -d
cp .env.example .env
go run ./cmd/rag-service
```

`docker compose up -d` поднимает ClickHouse и создаёт базу `rag`. При `AUTO_MIGRATE=true` сервис сам создаёт таблицы. Остановить сервер: `docker compose down`. Данные остаются в томе `clickhouse-data`.

При `AUTO_MIGRATE=true` сервис применяет `internal/migrate/migrations/001_init.sql`. Тот же файл можно применить вручную:

```bash
docker compose exec -T clickhouse clickhouse-client --multiquery < internal/migrate/migrations/001_init.sql
```

MCP для LibreChat: HTTP `POST /mcp` или `go run ./cmd/rag-service -mcp-stdio`.

## Конфигурация

| Переменная | По умолчанию | Назначение |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | Адрес HTTP API |
| `CLICKHOUSE_ADDR` | `127.0.0.1:9000` | Native-протокол ClickHouse |
| `CLICKHOUSE_DATABASE` | `rag` | Имя базы в миграции; таблицы создаются в `rag` |
| `CLICKHOUSE_USER` / `CLICKHOUSE_PASSWORD` | `default` / пусто | Соединение сервиса: файлы, миграции, экстракции |
| `MCP_READER_USER` / `MCP_READER_PASSWORD` | пусто | Только `SELECT`. Пусто вместе с writer — используется `CLICKHOUSE_USER` |
| `MCP_WRITER_USER` / `MCP_WRITER_PASSWORD` | пусто | `INSERT` для `create_*` / `update_*`. Без `DELETE` |
| `OLLAMA_URL` | `http://127.0.0.1:11434` | Адрес Ollama |
| `OLLAMA_EMBED_MODEL` | `nomic-embed-text` | Модель эмбеддингов |
| `OLLAMA_EMBED_DIMENSIONS` | `768` | Ожидаемая длина вектора. Индекс vector similarity не создаётся, пока размер не зафиксирован |
| `LLM_PROVIDER` | `ollama` | `ollama` или `openai` — только для экстракции, не для эмбеддингов |
| `OLLAMA_LLM_MODEL` | `llama3.2` | Чат-модель Ollama |
| `OPENAI_API_KEY` | пусто | Обязателен при `LLM_PROVIDER=openai` |
| `OPENAI_MODEL` | `gpt-4o-mini` | Модель экстракции OpenAI |
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | Базовый URL OpenAI |
| `EXTRACTION_TABLES` | `microservice,api,pulsar_topic` | Таблицы экстракции |
| `EXTRACTION_MIN_CONFIDENCE` | `0.8` | Порог авто-INSERT. Ниже — экстракция остаётся `pending` |
| `EXTRACTION_CHUNKS_PER_TABLE` | `3` | Сколько чанков отдавать модели на таблицу |
| `CHUNK_RUNES` / `CHUNK_OVERLAP` | `800` / `100` | Нарезка текста |
| `AUTO_MIGRATE` | `true` | Применять SQL при старте |

Роли для отдельного reader/writer:

```sql
CREATE USER IF NOT EXISTS mcp_reader IDENTIFIED BY 'reader-secret';
GRANT SELECT ON rag.* TO mcp_reader;

CREATE USER IF NOT EXISTS mcp_writer IDENTIFIED BY 'writer-secret';
GRANT SELECT, INSERT ON rag.* TO mcp_writer;
```

Пароли в миграцию не входят.

## HTTP API

- `GET /api/files` — файлы базы знаний
- `POST /api/files` — multipart-поле `file`. Текст и JSON читаются как есть. Из PDF забираются печатные фрагменты. Загрузка сразу считает эмбеддинги и запускает экстракцию. Если имя уже есть, сравнивается SHA-256 содержимого. Та же сумма возвращает существующий файл и ничего не переписывает. Другая сумма удаляет чанки этого файла новой версией `deleted = 1` и создаёт их заново
- `DELETE /api/files/{file_id}` — мягкое удаление новой версией строки, без `ALTER DELETE`
- `GET /api/extractions` — ожидающие экстракции
- `POST /api/extractions/{id}/approve` — записать строки и пометить `approved`
- `POST /api/extractions/{id}/reject` — пометить `rejected`

Все обычные файлы одной папки отправляет `scripts/upload-dir.sh`. Скрытые файлы и вложенные каталоги пропускаются. Адрес сервиса берётся из `RAG_API_URL`, по умолчанию `http://127.0.0.1:8080`.

```bash
scripts/upload-dir.sh ./docs
```

## MCP

- `search_knowledge_base` — эмбеддинг запроса через Ollama и ближайшие чанки
- `search_projects` — один `SELECT` или `WITH`. `INSERT`, `UPDATE`, `DELETE`, `ALTER`, `DROP`, `SYSTEM` и второе выражение запрещены
- `create_*` / `update_*` для `microservice`, `api`, `pulsar_topic`

`update_*` вставляет новую версию `ReplacingMergeTree`, а не `ALTER TABLE UPDATE`. У `api` обязательна связь `microservice_id`. Пустое `description` значит, что описания нет.

## Схема

`ORDER BY` выбран по точечным чтениям: `file_id`, `(file_id, chunk_index)`, `extraction_id`, `microservice_id`, `api_id`, `topic_id`. Список API сервиса фильтрует `microservice_id` уже после точечного ключа `api_id`. Партиций нет: срока хранения у этих таблиц нет. Повторяющиеся статусы имеют тип `LowCardinality(String)`. Nullable нет. Имена и описания остаются `String`: это не повторяющийся словарь.

## Проверки

Нужен Go 1.23 или новее.

```bash
go test ./...
```

Интеграционный тест в `internal/store` применяет миграцию к локальному ClickHouse на `127.0.0.1:9000` и пропускается, если сервер недоступен.
