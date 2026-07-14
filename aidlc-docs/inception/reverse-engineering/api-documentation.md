# API Documentation

REST API served by **Cooler / Manager** (gin), base path `/api/`, port 8080.
Auth: optional bearer token via `Authorization` header (enabled only when `API_AUTH_TOKEN` is set; empty token disables auth). Swagger UI at `/docs`.

## REST APIs

### Run scan
- **Method**: POST
- **Path**: `/api/run`
- **Purpose**: Create and enqueue a new scan Task.
- **Request** (`domain.NucleiRequest`):
  ```json
  {
    "urls": ["https://target.example"],
    "template_ids": ["cve-2024-1234", "tech-detect"],
    "throttle": 10,
    "variables": {"key": "value"},
    "custom_templates": ["customdir"],
    "offen-callback": "target-name"
  }
  ```
- **Validation**: at least one non-empty URL; at least one template ID; `throttle` must be < 1s (in ns; <1 disables); `offen-callback` (if set) must be a known callback target.
- **Response**: `200 {"task_id": "<uuid>"}` · `401` unauthorized · `500 {"error": "..."}`

### List tasks
- **Method**: GET
- **Path**: `/api/task/list`
- **Purpose**: All known Task IDs (read from `coolant_tasks`, not the queue).
- **Response**: `200 {"tasks": ["<id>", ...]}` · `204 {"tasks": []}` when none.

### Stop task
- **Method**: POST
- **Path**: `/api/task/{task_id}/stop`
- **Purpose**: Cancel the live attempt and transition Task → Paused (idempotent no-op if already idle). On transport-lost, conservatively settles Failed.
- **Response**: `200` · `500 {"error": "..."}`

### Start task
- **Method**: POST
- **Path**: `/api/task/{task_id}/start`
- **Purpose**: Resume a Paused task (or recover a Created-stuck task), re-enqueuing only leftover (not-yet-completed) templates. Empty leftover → straight to Completed.
- **Response**: `200` · `500 {"error": "..."}` (e.g. non-resumable state).

### Task status
- **Method**: GET
- **Path**: `/api/task/{task_id}/status`
- **Purpose**: Lifecycle state + per-templateID progress; matched findings when complete.
- **Response (complete)**:
  ```json
  {
    "status": "complete",
    "progress": {"completed": 42, "total": 100, "percent": 42},
    "result": [ /* nuclei output.ResultEvent with MatcherStatus=true */ ]
  }
  ```
  - **error**: `{"status":"error","progress":{...},"reason":"error"}`
  - **other**: `{"status":"<state>","progress":{...}}`
- **Notes**: Progress is templateID-set based; `percent` is 0 until the handler has expanded the template list. This endpoint is excluded from otelgin tracing.

### Task log
- **Method**: GET
- **Path**: `/api/task/{task_id}/log`
- **Purpose**: Engine logs captured during the scan (from `logs` collection).
- **Response**: `200 {"logs": [ {..} ]}` · `204 {"result": []}` when none · `500 {"error": "..."}`

### Docs
- `GET /docs` → redirect to `/docs/index.html` (Swagger UI). `GET /docs/*any` served by swaggo.

## Internal APIs (key interfaces)

### `httpapi.TaskService` (manager boundary)
```
NewTask(ctx, req domain.NucleiRequest, depth ...int) (string, error)
Stop(ctx, id string) error
Start(ctx, id string) error
GetTaskState(ctx, id string) (state.State, error)
GetTaskProgress(ctx, id string) (completed, total, percent int, err error)
GetAllTaskList(ctx) []string
ValidateCallbackTarget(ctx, target string) error
```

### `manager.TaskManager` (asynq transport)
```
EnqueueTask(ctx, typename, queue string, payload []byte, taskID string, opts...) error
StopTask(ctx, id string, queues...) error
ResumeTask(ctx, id string, queues...) error
Inspector() *asynq.Inspector
HandleFunc(taskType string, fn asynq.HandlerFunc)
Start() error; Shutdown()
OnSuccess(fn); OnFailure(fn)
```

### `handler.TaskStateRepo` / `task.Repository` (persistence, all guarded by `current_asynq_id`)
```
Get / Create / Exists / List / Delete
AddExpandedTemplateIDs(id, asynqID, ids)     // $addToSet, current-attempt guarded
AddCompletedTemplateIDs(id, asynqID, ids)    // $addToSet, guarded
SetState(id, s)                              // terminal → compress slices→counters
SetStateIfCurrent(id, asynqID, s) (matched)  // handler entry stale guard
RotateCurrentAsynqID(id, expected, new)      // CAS for /start
ClearCurrentAsynqID / ...AndSetState         // /stop path
Restore/SetStateIfPausedIdle                 // /stop rollback branches
CompleteIfCurrent(id, asynqID, s, errMsg)    // OnSuccess/OnFailure, atomic
```

## Data Models

### NucleiRequest (external, API → queue)
| Field | Type | Notes |
|---|---|---|
| urls | []string | scan targets (≥1 required) |
| template_ids | []string | nuclei IDs / globs (≥1 required) |
| throttle | float64 | rate limit; <1 disables, must be <1s |
| variables | map[string]string | nuclei template vars |
| custom_templates | []string | custom template subdirs |
| offen-callback | string | callback target name |

### NucleiJobData (internal, Manager → Worker via ConfigMap)
`urls`, `template_paths` (.yaml to run), `dependency_paths` (helpers, downloaded but NOT parsed as templates), `template_ids`, `custom_templates`, `dast` (fuzzing mode), `throttle`, `variables`, `offen-callback`.

### Task (`coolant_tasks` document)
`_id` (UUID), `state`, `request`, `expanded_template_ids[]`/`completed_template_ids[]` (active) → `expanded_total`/`completed_total` (terminal), `current_asynq_id`, `last_error`, timestamps.

### Persisted result collections (MongoDB)
- `coolant_tasks` — Task FSM.
- `output_excluded` — task metadata + excluded templates (id + reason).
- `output_success` — per-finding docs, `MatcherStatus=true`.
- `output_failures` — per-finding docs, non-matches/errors.
- `request_trace` — every request; `request_error` — errored requests.
- `logs` — engine logs.

> Breaking schema change (see README): results are now individual documents in `output_success`/`output_failures` (removes the 16MB embedded-array limit); old-format historical data is not queryable via the status endpoint.
