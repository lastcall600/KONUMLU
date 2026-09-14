package policy

import "time"

// DestinationState is eligibility for channel planning. Email/SMS come from
// Identity-verified contacts. Web/mobile push come from active Notifications
// push endpoints (not Identity).
type DestinationState struct {
	EmailVerified   bool
	PhoneVerified   bool
	WebPushReady    bool
	MobilePushReady bool
}

type ConsentSnapshot struct {
	ID            string
	Type          ConsentType
	Status        ConsentStatus
	PolicyVersion string
	CapturedAt    time.Time
	WithdrawnAt   *time.Time
	Source        ConsentSource
	RecordedSeq   int64
}

// PreferenceSetting is one durable override: per channel + scope.
type PreferenceSetting struct {
	Channel   Channel
	ScopeType ScopeType
	ScopeKey  string
	Enabled   bool
}

type PreferenceDocument struct {
	Settings []PreferenceSetting
}

func DefaultPreferences() PreferenceDocument {
	return PreferenceDocument{}
}

func catalogChannelDefault(ch Channel) bool {
	switch ch {
	case ChannelSMS:
		return false
	default:
		return true
	}
}

func catalogCategoryDefault(cat Category) bool {
	return cat != CategoryMarketing
}

type IntentInput struct {
	RecipientUserID         string
	EventType               EventType
	DomainRef               string
	ActorRef                string
	ResourceRef             string
	LocaleHint              string
	Variables               map[string]string
	CreatedAt               time.Time
	Account                 AccountState
	Destinations            DestinationState
	Preferences             PreferenceDocument
	Consents                []ConsentSnapshot
	AlreadySeen             bool
	RequestedChannels       []Channel
	PreferenceLookupFailed  bool
	ConsentLookupFailed     bool
}

type ChannelDecision struct {
	Channel    Channel
	Eligible   bool
	Required   bool
	Suppressed SuppressionReason
}

type Resolution struct {
	Spec             EventSpec
	DedupeKey        string
	TemplateKey      string
	Purpose          Purpose
	Urgency          Urgency
	EligibleChannels []Channel
	Decisions        []ChannelDecision
	UnknownEvent     bool
}

func consentTypeFor(ch Channel) (ConsentType, bool) {
	switch ch {
	case ChannelEmail:
		return ConsentCommercialEmail, true
	case ChannelSMS:
		return ConsentCommercialSMS, true
	case ChannelWebPush, ChannelMobilePush:
		return ConsentCommercialPush, true
	default:
		return "", false
	}
}

func latestConsent(consents []ConsentSnapshot, typ ConsentType) (ConsentSnapshot, bool) {
	var best ConsentSnapshot
	found := false
	for _, c := range consents {
		if c.Type != typ {
			continue
		}
		if !found || c.RecordedSeq > best.RecordedSeq {
			best = c
			found = true
		}
	}
	return best, found
}

func channelReady(ch Channel, dest DestinationState) bool {
	switch ch {
	case ChannelInApp:
		return true
	case ChannelEmail:
		return dest.EmailVerified
	case ChannelSMS:
		return dest.PhoneVerified
	case ChannelWebPush:
		return dest.WebPushReady
	case ChannelMobilePush:
		return dest.MobilePushReady
	default:
		return false
	}
}

func required(spec EventSpec, ch Channel) bool {
	for _, r := range spec.RequiredChannels {
		if r == ch {
			return true
		}
	}
	return false
}

func allowed(spec EventSpec, ch Channel) bool {
	for _, a := range spec.AllowedChannels {
		if a == ch {
			return true
		}
	}
	return false
}

func (p PreferenceDocument) lookup(ch Channel, scope ScopeType, key string) (bool, bool) {
	for i := len(p.Settings) - 1; i >= 0; i-- {
		s := p.Settings[i]
		if s.Channel == ch && s.ScopeType == scope && s.ScopeKey == key {
			return s.Enabled, true
		}
	}
	return false, false
}

func (p PreferenceDocument) channelOn(ch Channel) bool {
	if on, ok := p.lookup(ch, ScopeChannel, "*"); ok {
		return on
	}
	return catalogChannelDefault(ch)
}

func (p PreferenceDocument) categoryOn(ch Channel, cat Category) bool {
	if on, ok := p.lookup(ch, ScopeCategory, string(cat)); ok {
		return on
	}
	return catalogCategoryDefault(cat)
}

func (p PreferenceDocument) eventOverride(event EventType, ch Channel) (bool, bool) {
	return p.lookup(ch, ScopeEvent, string(event))
}

func (p PreferenceDocument) Override(ch Channel, scope ScopeType, key string, enabled bool) PreferenceDocument {
	out := clonePrefs(p)
	replaced := false
	for i := range out.Settings {
		if out.Settings[i].Channel == ch && out.Settings[i].ScopeType == scope && out.Settings[i].ScopeKey == key {
			out.Settings[i].Enabled = enabled
			replaced = true
			break
		}
	}
	if !replaced {
		out.Settings = append(out.Settings, PreferenceSetting{Channel: ch, ScopeType: scope, ScopeKey: key, Enabled: enabled})
	}
	return out
}

func Resolve(in IntentInput) Resolution {
	spec, ok := LookupEvent(in.EventType)
	out := Resolution{DedupeKey: DedupeKey(in.EventType, in.RecipientUserID, in.DomainRef)}
	if !ok {
		out.UnknownEvent = true
		return out
	}
	out.Spec = spec
	out.TemplateKey = spec.TemplateKey
	out.Purpose = spec.Purpose
	out.Urgency = spec.DefaultUrgency

	if in.AlreadySeen {
		for _, ch := range spec.AllowedChannels {
			out.Decisions = append(out.Decisions, ChannelDecision{Channel: ch, Suppressed: SuppressDeduplicated})
		}
		return out
	}

	if in.Account == AccountDeleted && !spec.AccountSecurityOnly {
		for _, ch := range spec.AllowedChannels {
			out.Decisions = append(out.Decisions, ChannelDecision{Channel: ch, Suppressed: SuppressAccountDisabled})
		}
		return out
	}
	if in.Account == AccountDisabled && spec.Purpose != PurposeSecurity {
		for _, ch := range spec.AllowedChannels {
			out.Decisions = append(out.Decisions, ChannelDecision{Channel: ch, Suppressed: SuppressAccountDisabled})
		}
		return out
	}

	channels := spec.AllowedChannels
	if len(in.RequestedChannels) > 0 {
		channels = in.RequestedChannels
	}

	for _, ch := range channels {
		d := ChannelDecision{Channel: ch, Required: required(spec, ch)}
		if !ch.valid() || !allowed(spec, ch) {
			d.Suppressed = SuppressPolicy
			out.Decisions = append(out.Decisions, d)
			continue
		}
		if !channelReady(ch, in.Destinations) {
			d.Suppressed = SuppressNoDestination
			if ch == ChannelWebPush || ch == ChannelMobilePush {
				d.Suppressed = SuppressChannelUnavailable
			}
			out.Decisions = append(out.Decisions, d)
			continue
		}
		if spec.ConsentRequired {
			if in.ConsentLookupFailed {
				if _, hasType := consentTypeFor(ch); hasType || ch != ChannelInApp {
					d.Suppressed = SuppressConsentMissing
					out.Decisions = append(out.Decisions, d)
					continue
				}
			} else {
				typ, hasType := consentTypeFor(ch)
				if hasType {
					c, found := latestConsent(in.Consents, typ)
					if !found {
						d.Suppressed = SuppressConsentMissing
						out.Decisions = append(out.Decisions, d)
						continue
					}
					if c.Status == ConsentWithdrawn || c.WithdrawnAt != nil {
						d.Suppressed = SuppressConsentWithdrawn
						out.Decisions = append(out.Decisions, d)
						continue
					}
					if c.Status != ConsentGranted {
						d.Suppressed = SuppressConsentMissing
						out.Decisions = append(out.Decisions, d)
						continue
					}
				} else if ch != ChannelInApp {
					d.Suppressed = SuppressConsentMissing
					out.Decisions = append(out.Decisions, d)
					continue
				}
			}
		}
		if !d.Required {
			if in.PreferenceLookupFailed {
				d.Suppressed = SuppressUserPreference
				out.Decisions = append(out.Decisions, d)
				continue
			}
			if !in.Preferences.channelOn(ch) {
				d.Suppressed = SuppressUserPreference
				out.Decisions = append(out.Decisions, d)
				continue
			}
			if spec.Purpose != PurposeSecurity && !in.Preferences.categoryOn(ch, spec.Category) {
				d.Suppressed = SuppressUserPreference
				out.Decisions = append(out.Decisions, d)
				continue
			}
			if v, ok := in.Preferences.eventOverride(spec.Type, ch); ok && !v {
				d.Suppressed = SuppressUserPreference
				out.Decisions = append(out.Decisions, d)
				continue
			}
		}
		d.Eligible = true
		out.EligibleChannels = append(out.EligibleChannels, ch)
		out.Decisions = append(out.Decisions, d)
	}
	return out
}

func FilterVariables(spec EventSpec, in map[string]string) (map[string]string, error) {
	allowed := map[string]struct{}{}
	for _, k := range spec.AllowlistedVariables {
		allowed[k] = struct{}{}
	}
	forbidden := map[string]struct{}{}
	for _, k := range ForbiddenTemplateVariables() {
		forbidden[k] = struct{}{}
	}
	out := map[string]string{}
	for k, v := range in {
		if _, bad := forbidden[k]; bad {
			return nil, ErrArbitraryMetadata
		}
		if _, ok := allowed[k]; !ok {
			return nil, ErrArbitraryMetadata
		}
		out[k] = v
	}
	return out, nil
}
