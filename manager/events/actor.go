// SPDX-License-Identifier: Apache-2.0

package events

import "context"

type actorKey struct{}

// WithActor returns a context that names who causes the changes made with it:
// "user:<sub>", "svc:<name>" or "station:<key>". The events emitted with the context
// have that actor unless they name their own.
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

// ActorFrom returns the actor that ctx carries, or "" when it carries none.
func ActorFrom(ctx context.Context) string {
	actor, _ := ctx.Value(actorKey{}).(string)
	return actor
}
