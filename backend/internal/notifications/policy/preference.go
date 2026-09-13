package policy

import (
	"strings"
	"time"
)

type PreferenceOverride struct {
	Channel   Channel
	ScopeType ScopeType
	ScopeKey  string
	Enabled   bool
}

type PreferencePatch struct {
	Overrides []PreferenceOverride
}

func ValidatePreferenceOverride(o PreferenceOverride) error {
	if !o.Channel.valid() || !o.ScopeType.valid() {
		return ErrUnknownPreference
	}
	key := strings.TrimSpace(o.ScopeKey)
	switch o.ScopeType {
	case ScopeChannel:
		if key != "*" {
			return ErrUnknownPreference
		}
		if o.Channel == ChannelInApp && !o.Enabled {
			return ErrSystemRequired
		}
	case ScopeCategory:
		cat := Category(key)
		known := false
		for _, c := range AllCategories() {
			if c == cat {
				known = true
				break
			}
		}
		if !known {
			return ErrUnknownPreference
		}
		if cat == CategorySecurity && !o.Enabled {
			return ErrSystemRequired
		}
	case ScopeEvent:
		spec, ok := LookupEvent(EventType(key))
		if !ok {
			return ErrUnknownPreference
		}
		if spec.Purpose == PurposeSecurity {
			return ErrSystemRequired
		}
		if !allowed(spec, o.Channel) {
			return ErrUnknownPreference
		}
		if required(spec, o.Channel) && !o.Enabled {
			return ErrSystemRequired
		}
	}
	return nil
}

func ValidatePreferencePatch(p PreferencePatch) error {
	seen := map[string]struct{}{}
	for _, o := range p.Overrides {
		if err := ValidatePreferenceOverride(o); err != nil {
			return err
		}
		id := string(o.Channel) + "|" + string(o.ScopeType) + "|" + strings.TrimSpace(o.ScopeKey)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
	}
	return nil
}

func ApplyPreferencePatch(base PreferenceDocument, p PreferencePatch) (PreferenceDocument, error) {
	if err := ValidatePreferencePatch(p); err != nil {
		return PreferenceDocument{}, err
	}
	out := clonePrefs(base)
	for _, o := range p.Overrides {
		out = out.Override(o.Channel, o.ScopeType, strings.TrimSpace(o.ScopeKey), o.Enabled)
	}
	return out, nil
}

func clonePrefs(in PreferenceDocument) PreferenceDocument {
	out := PreferenceDocument{Settings: make([]PreferenceSetting, len(in.Settings))}
	copy(out.Settings, in.Settings)
	return out
}

type ConsentDecision struct {
	Type             ConsentType
	Grant            bool
	PolicyVersion    string
	Source           ConsentSource
	ClientCapturedAt *time.Time
}

func ValidateConsentDecision(d ConsentDecision, now time.Time) (ConsentSnapshot, error) {
	if d.ClientCapturedAt != nil {
		return ConsentSnapshot{}, ErrClientTimestamp
	}
	if !KnownConsentType(d.Type) {
		return ConsentSnapshot{}, ErrUnknownConsent
	}
	if !KnownConsentSource(d.Source) {
		return ConsentSnapshot{}, ErrInvalidInput
	}
	if d.PolicyVersion != "" && d.PolicyVersion != PolicyDocumentVersion {
		return ConsentSnapshot{}, ErrInvalidInput
	}
	snap := ConsentSnapshot{
		Type:          d.Type,
		PolicyVersion: PolicyDocumentVersion,
		CapturedAt:    now.UTC(),
		Source:        d.Source,
	}
	if d.Grant {
		snap.Status = ConsentGranted
	} else {
		snap.Status = ConsentWithdrawn
		t := now.UTC()
		snap.WithdrawnAt = &t
	}
	return snap, nil
}

func AccountCreatedDoesNotGrantConsent(_ string) []ConsentSnapshot {
	return nil
}

func ClassifyRetry(reason SuppressionReason, providerRetryable bool) RetryClass {
	switch reason {
	case SuppressUserPreference, SuppressConsentMissing, SuppressConsentWithdrawn,
		SuppressNoDestination, SuppressDeduplicated, SuppressPolicy, SuppressUnknownEvent,
		SuppressAccountDisabled:
		return RetryPermanent
	case SuppressChannelUnavailable:
		if providerRetryable {
			return RetryTransient
		}
		return RetryPermanent
	default:
		if providerRetryable {
			return RetryTransient
		}
		return RetryPermanent
	}
}

func BoundedBackoff(attempt int) (delaySeconds int, giveUp bool) {
	if attempt <= 0 {
		return 1, false
	}
	if attempt >= 8 {
		return 0, true
	}
	d := 1
	for i := 1; i < attempt; i++ {
		d *= 2
		if d > 300 {
			d = 300
			break
		}
	}
	return d, false
}

type EffectivePreference struct {
	Channel   Channel
	ScopeType ScopeType
	ScopeKey  string
	Stored    *bool
	Enabled   bool
	Required  bool
}

func EffectivePreferenceRows(stored PreferenceDocument) []EffectivePreference {
	var out []EffectivePreference
	for _, ch := range AllChannels() {
		storedCh, hasCh := stored.lookup(ch, ScopeChannel, "*")
		enabled := catalogChannelDefault(ch)
		if hasCh {
			enabled = storedCh
		}
		required := ch == ChannelInApp
		if required {
			enabled = true
		}
		row := EffectivePreference{Channel: ch, ScopeType: ScopeChannel, ScopeKey: "*", Enabled: enabled, Required: required}
		if hasCh {
			v := storedCh
			row.Stored = &v
		}
		out = append(out, row)
		for _, cat := range AllCategories() {
			storedCat, hasCat := stored.lookup(ch, ScopeCategory, string(cat))
			catOn := catalogCategoryDefault(cat)
			if hasCat {
				catOn = storedCat
			}
			catRequired := cat == CategorySecurity
			if catRequired {
				catOn = true
			}
			crow := EffectivePreference{Channel: ch, ScopeType: ScopeCategory, ScopeKey: string(cat), Enabled: catOn, Required: catRequired}
			if hasCat {
				v := storedCat
				crow.Stored = &v
			}
			out = append(out, crow)
		}
	}
	for _, s := range stored.Settings {
		if s.ScopeType != ScopeEvent {
			continue
		}
		spec, ok := LookupEvent(EventType(s.ScopeKey))
		if !ok {
			continue
		}
		enabled := s.Enabled
		req := required(spec, s.Channel)
		if req {
			enabled = true
		}
		v := s.Enabled
		out = append(out, EffectivePreference{
			Channel: s.Channel, ScopeType: ScopeEvent, ScopeKey: s.ScopeKey,
			Stored: &v, Enabled: enabled, Required: req,
		})
	}
	return out
}
