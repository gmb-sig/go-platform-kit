// Package httpclient adds project conventions over Azugo's ctx.HTTPClient():
// correlation propagation on outbound calls and sane transport defaults
// (go-platform-kit Spec §5.5).
//
// It owns transport defaults and correlation; go-authbyte owns auth — it
// attaches the DPoP-bound service token + proof. The two compose: build the
// outbound client here, then layer go-authbyte's request options on top.
//
// The W3C `traceparent` header is injected automatically by
// azugo.io/opentelemetry's HTTP-client instrumentation, so this package only
// adds the project correlation_id header.
package httpclient

import (
	"time"

	"azugo.io/azugo"
	"azugo.io/core/http"

	"github.com/gmb-sig/go-platform-kit/correlation"
)

// Default transport conventions. Per-request deadlines still come from the
// inbound request context (ctx.HTTPClient() inherits it); these document the
// fleet defaults a service configures on the Azugo http_client section.
const (
	// DefaultTimeout is the recommended overall timeout for an outbound call.
	DefaultTimeout = 10 * time.Second
	// DefaultMaxRetries is the recommended bound on retries (with backoff).
	DefaultMaxRetries = 2
)

// WithCorrelation returns an Azugo HTTP request option that propagates the
// request's correlation_id to the upstream service. It returns nil (a no-op
// option, which Azugo's request pipeline skips) when no correlation id is bound
// — e.g. outside an inbound request.
func WithCorrelation(ctx *azugo.Context) http.RequestOption {
	if cid := correlation.ID(ctx); cid != "" {
		return http.WithHeader(correlation.HeaderCorrelationID, cid)
	}

	return nil
}

// Outbound returns the context-bound HTTP client targeting baseURL. The client
// is already OpenTelemetry-instrumented (via azugo.io/opentelemetry) and
// inherits the inbound request's deadline and tracing. Pass WithCorrelation(ctx)
// — and, for authenticated calls, go-authbyte's token option — to each request:
//
//	c := httpclient.Outbound(ctx, "https://document-svc")
//	err := c.GetJSON("/v1/documents/"+id, &doc, httpclient.WithCorrelation(ctx))
func Outbound(ctx *azugo.Context, baseURL string) http.Client {
	return ctx.HTTPClient().WithBaseURL(baseURL)
}
