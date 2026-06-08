# go-platform-kit

The thin, project-specific glue over [Azugo](https://azugo.io) that every backend service
imports so that **config, telemetry, errors, correlation, and broker access are wired
identically** across the fleet.

It does **not** replace Azugo's telemetry — it standardizes how every service turns it on
and adds the project glue Azugo cannot know about (the correlation model linking a trace
to the three audit regimes, PII/secret log redaction, and the frozen broker event
envelope). It re-implements none of Azugo's logger, metrics, or tracer.

See [`SKILL.md`](./SKILL.md) for usage conventions and the
[Platform Kit Specification](../eSignature-Portal-Platform-Kit-Spec.md) for the design.

## Install

```sh
go get github.com/gmb-sig/go-platform-kit
```

Pinned in lockstep to `azugo.io/azugo`, `azugo.io/core`, and `azugo.io/opentelemetry`
(v0.32.x).

## One-call bootstrap

```go
func (a *App) init() error {
    if err := platform.Setup(a.App, platform.Options{
        Config: a.config.BaseConfiguration,
    }); err != nil {
        return err
    }
    // …service-specific wiring…
    return nil
}
```

After `Setup` the service has standardized logging + redaction, metrics, OpenTelemetry
tracing, and the correlation middleware installed.

## Packages

| Package | Owns |
|---|---|
| `platform` | `Setup(app, Options)` — the single bootstrap entrypoint |
| `config` | `BaseConfiguration` + the standard fleet env |
| `observability` | log redaction, metric naming, OpenTelemetry enablement |
| `correlation` | `correlation_id`/`trace_id` middleware + context helpers |
| `errors` | error taxonomy + `err:domain:reason` → Azugo HTTP error mapping |
| `broker` | `Publisher`/`Consumer` over the frozen §8.1 event envelope |
| `httpclient` | outbound defaults + correlation header propagation |

## Develop

```sh
go build ./...
go test ./...
go vet ./...
```

## License

MIT — see [LICENSE](./LICENSE).
