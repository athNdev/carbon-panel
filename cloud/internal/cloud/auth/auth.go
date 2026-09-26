// Package auth turns bearer credentials into principal.Principal values.
//
// It covers three mechanisms: Clerk session JWT verification (local JWKS
// verification, never a Clerk call per request), long-lived org API keys
// (HMAC-hashed, constant-time verification), and Clerk webhook verification
// (Svix signature scheme) for the org-membership sync.
//
// The package takes explicit configuration structs and deliberately does not
// import the config or db lanes, so it stays parallel to them. Every failure
// denies by default with a typed error the caller maps to 401/503.
package auth
