// Package typesafe is an independent, unofficial Go client for the TypeSafe API.
//
// It implements SystemOne and ListModels without Python or external Go dependencies.
// A Client may be shared between goroutines. Callers must not mutate request maps,
// slices, or custom transports while a call is in progress. Responses are mutable
// Go values and are owned by the caller.
//
// BaseURL is an API root (normally https://api.typesafe.ai), not a URL ending in /v1.
// Retries can cause duplicate evaluation and billing; use a context deadline and a
// conservative retry policy for interactive agents. No idempotency is guaranteed.
package typesafe
