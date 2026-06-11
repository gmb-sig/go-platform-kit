---
name: go-platform-kit
description: Conventions for using github.com/gmb-sig/go-platform-kit — the thin, project-specific glue over Azugo that every backend service imports so config, telemetry, errors, correlation, and broker access are wired identically. Use when bootstrapping a service (platform.Setup), defining the base configuration, adding the correlation model, mapping DB result codes to HTTP errors, propagating correlation on outbound HTTP, or publishing/consuming broker events with the §8.1 envelope. Complements the azugo-framework skill (it does not replace it).
---

# go-platform-kit — Project Glue Over Azugo

`go-platform-kit` is a **library** (no runtime of its own). It standardizes how every
service turns on **Azugo's own** telemetry and adds the project glue Azugo cannot know
about: the correlation model, PII/secret log redaction, the broker event envelope, and
the error taxonomy. It **re-implements none** of Azugo's logger, metrics, or tracer — it
configures and wraps them.

> Read the **azugo-framework** skill first for app/route/config/handler structure. This
> skill only covers the `go-platform-kit` delta on top of it.

Module: `github.com/gmb-sig/go-platform-kit` · Pinned to `azugo.io/azugo` + `azugo.io/core`
+ `azugo.io/opentelemetry` **v0.32.x** (bumped here once, inherited transitively).

---

## Packages

| Import | Owns |
|---|---|
| `…/platform` | `Setup(app, Options)` — the single bootstrap entrypoint |
| `…/config` | `BaseConfiguration` (embeds Azugo config) + the standard env |
| `…/observability` | logger redaction, metric naming helpers, `EnableTracing` |
| `…/correlation` | `correlation_id`/`trace_id` middleware + context helpers |
| `…/errors` | error taxonomy + `err:domain:reason` → Azugo HTTP error mapping |
| `…/broker` | `Publisher`/`Consumer` over the frozen §8.1 event envelope |
| `…/httpclient` | outbound defaults + correlation header propagation |

---

## 1. Bootstrap — `platform.Setup`

A service makes **one call** in its `App.init()`, right after `server.New(...)`. After it
returns, the service has standardized logging+redaction, metrics, tracing, and the
correlation middleware installed — no copy-paste.

```go
import (
    "azugo.io/azugo"
    "azugo.io/azugo/server"
    "github.com/gmb-sig/go-platform-kit/platform"
)

func New(cmd *cobra.Command, version string) (*App, error) {
    config := NewConfiguration() // embeds *pkconfig.BaseConfiguration
    a, err := server.New(cmd, server.Options{
        AppName:       "Document Service",
        AppVer:        version,
        Configuration: config,
    })
    if err != nil {
        return nil, err
    }

    instance := &App{App: a, config: config}
    if err := instance.init(); err != nil {
        return nil, err
    }
    return instance, nil
}

func (a *App) init() error {
    // FIRST thing after server.New — before any service routes/middleware.
    if err := platform.Setup(a.App, platform.Options{
        Config: a.config.BaseConfiguration,
        // Redaction: customPolicy, // optional; defaults to the fleet policy
    }); err != nil {
        return err
    }

    // …service-specific wiring (stores, routes, go-authbyte, audit emitters)…
    return nil
}
```

`Setup` wires, in order: **(1)** OpenTelemetry tracing (so trace ids exist), **(2)** log
redaction, **(3)** the correlation middleware. Call it **before** registering service
routes so correlation wraps them.

---

## 2. Base configuration — `config.BaseConfiguration`

Embed `*config.BaseConfiguration` instead of Azugo's `*config.Configuration`. It carries
the standard fleet env and already satisfies Azugo's `Configurable` (promoted
`ServerCore`). Always call `c.BaseConfiguration.Bind("", v)` from your `Bind`.

```go
import (
    pkconfig "github.com/gmb-sig/go-platform-kit/config"
    "azugo.io/core/validation"
    "github.com/spf13/viper"
)

type Configuration struct {
    *pkconfig.BaseConfiguration `mapstructure:",squash"`

    PostgresDSN string `mapstructure:"postgres_dsn" validate:"required"`
}

func NewConfiguration() *Configuration {
    return &Configuration{BaseConfiguration: pkconfig.New()}
}

func (c *Configuration) Bind(_ string, v *viper.Viper) {
    c.BaseConfiguration.Bind("", v)            // standard env first
    _ = v.BindEnv("postgres_dsn", "POSTGRES_DSN")
}

func (c *Configuration) Validate(valid *validation.Validate) error {
    if err := c.BaseConfiguration.Validate(valid); err != nil {
        return err
    }
    return valid.Struct(c)
}
```

### Standard env contributed by the base config

| Env | Purpose |
|---|---|
| `SERVICE_NAME` | broker client id + default project-metric label (**required**) |
| `ENVIRONMENT` | **Azugo's own** var: `development`/`test`/`staging`/`production`. Drives the `service.environment` log field and the OTel `deployment.environment` via `app.Env()`. The kit does **not** re-declare it — set Azugo's vocabulary, not `local`/`prod` |
| `LOG_LEVEL`, `LOG_FORMAT` | Azugo log policy (`ecsjson` default outside `development`) |
| `METRICS_ENABLED` | Azugo metrics toggle |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP collector — **unset ⇒ tracing off** |
| `OTEL_SDK_DISABLED`, `OTEL_RESOURCE_ATTRIBUTES` | Standard OTel SDK knobs |
| `ELASTIC_APM_SECRET_TOKEN(_FILE)` | Only if exporting to Elastic APM (secret) |
| `BROKER_URL` | Broker endpoint |
| `BROKER_TLS_CERT/KEY/CA(_FILE)` | Broker client TLS material (secret, via Vault) |

Secrets follow the Vault-agent convention: `<NAME>_FILE` points at the secret file
(`config.LoadRemoteSecret`). Each service still owns its own sub-config.

> `service.name`/`service.version` in logs come from Azugo's `server.Options.AppName`
> /`AppVer`, not `SERVICE_NAME` — set them consistently if you want them to match.

---

## 3. Correlation — the project-only piece

`platform.Setup` installs `correlation.Middleware()`. For every request it resolves the
`correlation_id` — the inbound `X-Correlation-ID` header, else **Azugo's own per-request
id (`ctx.ID()`)** rather than a parallel ULID — adopts the OTel `trace_id`/`span_id`,
binds all three to the context, **adds them to every log line emitted via `ctx.Log()`**,
and echoes `X-Correlation-ID` on the response.

> Note: Azugo's built-in **access log** (`middleware.RequestLogger`) writes through the
> *app* logger, not `ctx.Log()`, so it carries only its own `http.request.id` — which,
> because the kit adopts `ctx.ID()`, holds the *same value* as `correlation_id`. So one id
> still joins the access log to every handler/audit line; correlation appears as a named
> `correlation_id` field on the latter.

In handlers, read the ids and pass them onward:

```go
import "github.com/gmb-sig/go-platform-kit/correlation"

func (r *router) handler(ctx *azugo.Context) {
    cid := correlation.ID(ctx)          // the correlation id
    ids := correlation.FromContext(ctx) // {CorrelationID, TraceID, SpanID}
    _ = cid; _ = ids
}
```

The same ids ride outbound HTTP (§5), broker events (§6), and the audit envelope
(stamped by the emitter libraries) — so one incident is one correlated trail across logs,
traces, and all three audit regimes. **Do not** build your own request-id scheme.

---

## 4. Errors — taxonomy & DB result-code mapping

Map the DB layer's namespaced result codes (`result_error('err:document:notFound', …)`)
to consistent HTTP responses. Pass the mapped error to `ctx.Error(err)` — Azugo derives
the status and safe message. **Never** return a raw DB error to the client.

```go
import pkerrors "github.com/gmb-sig/go-platform-kit/errors"

func (r *router) getDocument(ctx *azugo.Context) {
    doc, code, err := r.Store().GetDocument(ctx, ctx.Params.String("id"))
    if err != nil {
        ctx.Error(err) // unexpected — 500, logged, no leak
        return
    }
    if code != "" {
        ctx.Error(pkerrors.FromResultCode(code)) // e.g. err:document:notFound → 404
        return
    }
    ctx.JSON(doc)
}
```

Reason → status (case-insensitive, `_`/`-` ignored): `notFound`→404, `forbidden`→403,
`unauthorized`→401, `conflict`/`alreadyExists`→409, `expired`/`gone`→410,
`invalid`→400, `required`→400. **Unknown/unmapped → 500 with a fixed safe message**
(never leaks). Use `pkerrors.HTTP(domain, reason)` when classifying without a DB code.
Auth-specific mappings stay in `go-authbyte`.

---

## 5. Outbound HTTP — correlation propagation

Use `ctx.HTTPClient()` (never a raw client). `go-platform-kit` adds the `correlation_id`
header; W3C `traceparent` is injected automatically by `azugo.io/opentelemetry`;
`go-authbyte` adds the DPoP-bound token.

```go
import "github.com/gmb-sig/go-platform-kit/httpclient"

func (c *DocumentClient) Fetch(ctx *azugo.Context, id string) (*Doc, error) {
    client := httpclient.Outbound(ctx, c.baseURL) // == ctx.HTTPClient().WithBaseURL(...)
    var doc Doc
    opts := httpclient.CorrelationOptions(ctx) // propagate correlation_id (0 or 1 options)
    // opts = append(opts, authClient.AttachToken(ctx)) // go-authbyte attaches DPoP + token
    err := client.GetJSON("/v1/documents/"+id, &doc, opts...)
    return &doc, err
}
```

---

## 6. Broker — the §8.1 event envelope

Audit/security emitters (`go-eidas-audit`, `go-gdpr-audit`, `go-sec-events`) build on
these helpers; a service rarely publishes directly. The `Envelope` is the **frozen §8.1
schema**; `Publisher.Publish` stamps `event_id` (ULID), `occurred_at`, and
correlation/trace ids, validates, and strips any bearer-token-shaped attributes —
**events carry correlation, never tokens**.

```go
import "github.com/gmb-sig/go-platform-kit/broker"

pub := broker.NewPublisher(transport, cfg.ServiceName) // transport: your broker client

func (r *router) onPreviewed(ctx *azugo.Context, env, doc string) error {
    return pub.Publish(ctx, "signing.events", &broker.Envelope{
        EventType:  "document.previewed",
        Categories: []broker.Category{broker.CategorySigning},
        Actor:      &broker.Actor{ID: ctx.User().ID(), Type: "user"},
        Resource:   &broker.Resource{Type: "document", ID: doc},
        Operation:  broker.OpRead,
        Outcome:    broker.OutcomeSuccess,
        Attributes: map[string]any{"envelope_id": env}, // no PII, no document content
    })
}
```

Consume idempotently (at-least-once delivery assumed). The event id is marked processed
**only after the handler succeeds** — a failed handling is redelivered, so the handler
itself must be idempotent (e.g. `INSERT … ON CONFLICT (event_id) DO NOTHING`):

```go
store := broker.NewMemoryIdempotencyStore() // bounded FIFO; back with Redis for multi-replica
err := broker.Dispatch(ctx, payload, store, func(ctx context.Context, ev *broker.Envelope) error {
    // idempotent handling, keyed on ev.EventID
    return nil
})
```

`Transport` is an interface (`Publish(ctx, topic, key, payload)`) — inject your broker
client; `go-platform-kit` stays transport-agnostic glue.

---

## 7. Logging & redaction — automatic

After `Setup`, redaction is **always on**. Use `ctx.Log()` as normal; the redacting core
**drops** credential/secret/document-content fields and **masks** free-text PII before
they reach the sink — a handler cannot accidentally log a token (Security Checklist A10).

```go
ctx.Log().Info("issued token",
    zap.String("authorization", tok), // DROPPED
    zap.String("email", subjectEmail), // MASKED → "[REDACTED]"
    zap.String("document_id", id),     // kept
)
```

Override the policy via `platform.Options.Redaction` only to **add** keys, never to weaken
the defaults. Metric naming helpers live in `observability` (`IncCounter`,
`ObserveSeconds`) on Azugo's VictoriaMetrics registry.

---

## Non-goals (do not add here)

No business/domain logic; no auth (that's `go-authbyte`); no audit/security **emission**
(those libraries ride this glue); no data access; no forking Azugo's logger/metrics/tracer.
If it is not a genuine every-service concern, it does not belong in `go-platform-kit`.

---

## Summary

| Concern | API | Pattern |
|---|---|---|
| Bootstrap | `platform.Setup(app, Options)` | one call in `App.init()` after `server.New` |
| Base config | `config.New()` / `*config.BaseConfiguration` | embed + `BaseConfiguration.Bind/Validate` |
| Correlation | `correlation.ID/FromContext` | middleware auto-installed by Setup |
| Errors | `errors.FromResultCode` / `errors.HTTP` | `ctx.Error(...)` maps to status + safe msg |
| Outbound | `httpclient.Outbound` + `CorrelationOptions` | over `ctx.HTTPClient()` |
| Broker | `broker.NewPublisher` / `broker.Dispatch` | §8.1 `Envelope`, idempotent consume |
| Redaction | automatic | `ctx.Log()`; policy via `Options.Redaction` |
