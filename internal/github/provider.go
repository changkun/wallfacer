package github

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotConnected is returned when no GitHub token is available for a principal
// and the broker cannot mint one (the user has not connected / installed the
// "Latere AI" app). Handlers map it to 401 so the UI prompts to connect.
var ErrNotConnected = errors.New("github: principal not connected")

// Broker mints or refreshes a GitHub App token for a principal by brokering
// through the ../auth service (which owns the "Latere AI" app registration and
// the install + user-to-server flow). It is the single seam that reaches
// ../auth; everything else in this package works against a stored [Token].
//
// Implementations:
//   - the live broker calls ../auth (gated on that service exposing the token
//     endpoint; see the spec's brokering note),
//   - tests and local-without-auth runs use a fake.
//
// Token returns [ErrNotConnected] when the principal has no usable grant.
type Broker interface {
	Token(ctx context.Context, p Principal) (*Token, error)
}

// Provider yields a usable token for a principal. It serves the stored token
// while [Token.Valid], and otherwise refreshes through the [Broker] and
// persists the result, so callers never hand-roll the load/refresh/save dance.
//
// Store may be nil, in which case Provider is a thin pass-through to the broker
// (no caching); Broker may be nil, in which case Provider only serves what is
// already stored and returns [ErrNotConnected] when a refresh would be needed.
//
// Client is the shared API transport the repo/read/write surfaces use; nil
// means a default [Client] against the public API. Bundling it here keeps the
// handler's GitHub dependency a single field.
type Provider struct {
	Store  Store
	Broker Broker
	Client *Client
}

// APIClient returns the configured transport, or a default one against the
// public API when none is set.
func (pr *Provider) APIClient() *Client {
	if pr.Client != nil {
		return pr.Client
	}
	return &Client{}
}

// Get returns a valid token for p, refreshing and persisting as needed.
func (pr *Provider) Get(ctx context.Context, p Principal) (*Token, error) {
	if pr.Store != nil {
		stored, err := pr.Store.Load(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("github: load stored token: %w", err)
		}
		if stored.Valid() {
			return stored, nil
		}
	}

	if pr.Broker == nil {
		return nil, ErrNotConnected
	}
	fresh, err := pr.Broker.Token(ctx, p)
	if err != nil {
		return nil, err
	}
	if !fresh.Valid() {
		return nil, ErrNotConnected
	}
	if pr.Store != nil {
		if err := pr.Store.Save(ctx, p, fresh); err != nil {
			return nil, fmt.Errorf("github: persist refreshed token: %w", err)
		}
	}
	return fresh, nil
}
