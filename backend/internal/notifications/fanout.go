package notifications

import (
	"context"
	"html"
	"strings"

	"backend/internal/notifications/policy"
	"backend/internal/platform/observability"
)

func MaterializeFanout(ctx context.Context, m *Materializer, eventType policy.EventType, actorRef, domainRef, resourceRef string, recipients []string, vars map[string]string) error {
	if m == nil {
		return errStoreRequired
	}
	seen := map[string]struct{}{}
	for _, raw := range recipients {
		id, err := ParseID(raw)
		if err != nil || id.IsZero() {
			continue
		}
		key := id.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if actorRef != "" && key == actorRef {
			continue
		}
		observability.FromContext(ctx).Info("notification_producer_received",
			"notification_event", string(eventType),
			"user_id", key,
		)
		if _, err := m.Materialize(ctx, MaterializeInput{
			RecipientUserID: id,
			EventType:       eventType,
			DomainRef:       domainRef,
			ActorRef:        actorRef,
			ResourceRef:     resourceRef,
			Variables:       vars,
		}); err != nil {
			return err
		}
	}
	return nil
}

// RenderPlain is a minimum safe render boundary: template key + locale + escaped allowlisted variables.
// It does not encode product copy or HTML documents.
func RenderPlain(templateKey, locale string, vars map[string]string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		locale = "tr"
	}
	var b strings.Builder
	b.WriteString(templateKey)
	b.WriteByte('|')
	b.WriteString(locale)
	for k, v := range vars {
		b.WriteByte('|')
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(html.EscapeString(v))
	}
	return b.String()
}
