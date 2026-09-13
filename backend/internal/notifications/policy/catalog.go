package policy

// CatalogVersion is the server-owned event catalog revision used in dedupe keys.
const CatalogVersion = 1

// PolicyDocumentVersion is a product-policy identifier, not a legal opinion.
const PolicyDocumentVersion = "product.notification.v1"

// EventSpec is server-owned. Clients cannot set these fields.
type EventSpec struct {
	Type                 EventType
	Purpose              Purpose
	Category             Category
	DefaultUrgency       Urgency
	AllowedChannels      []Channel
	RequiredChannels     []Channel
	PreferenceKey        string
	ConsentRequired      bool
	Batchable            bool
	DedupeScope          string
	TemplateKey          string
	AllowlistedVariables []string
	AccountSecurityOnly  bool
}

const (
	EventSecurityLoginNew               EventType = "security.login_new"
	EventSecurityPasskeyAdded           EventType = "security.passkey_added"
	EventSecurityPasskeyRemoved         EventType = "security.passkey_removed"
	EventSecurityPasswordResetCompleted EventType = "security.password_reset_completed"
	EventSecuritySessionsRevoked        EventType = "security.sessions_revoked"
	EventSecurityAccountEvent           EventType = "security.account_security_event"
	EventMessagingMessageReceived       EventType = "messaging.message_received"
	EventOfferReceived                  EventType = "offer.received"
	EventOfferAccepted                  EventType = "offer.accepted"
	EventOfferRejected                  EventType = "offer.rejected"
	EventTransactionCreated             EventType = "transaction.created"
	EventTransactionCompleted           EventType = "transaction.completed"
	EventTransactionCancelled           EventType = "transaction.cancelled"
	EventDeliveryStatusChanged          EventType = "delivery.status_changed"
	EventDisputeUpdated                 EventType = "dispute.updated"
	EventModerationActionApplied        EventType = "moderation.action_applied"
	EventSavedSearchMatch               EventType = "saved_search.match"
	EventMarketingCampaign              EventType = "marketing.campaign"
	EventIdentityVerificationSignup     EventType = "identity.verification.signup"
	EventIdentityPasswordReset          EventType = "identity.password.reset"
)

func LookupEvent(t EventType) (EventSpec, bool) {
	spec, ok := catalog[t]
	return spec, ok
}

func MustEvent(t EventType) EventSpec {
	spec, ok := catalog[t]
	if !ok {
		return EventSpec{}
	}
	return spec
}

var catalog = map[EventType]EventSpec{
	EventSecurityLoginNew: {
		Type: EventSecurityLoginNew, Purpose: PurposeSecurity, Category: CategorySecurity,
		DefaultUrgency: UrgencyNormal, AllowedChannels: []Channel{ChannelInApp, ChannelEmail, ChannelWebPush, ChannelMobilePush},
		RequiredChannels: []Channel{ChannelInApp}, PreferenceKey: "security.login_new",
		ConsentRequired: false, Batchable: false, DedupeScope: "domain_ref",
		TemplateKey: "security.login_new", AllowlistedVariables: []string{"created_at"},
	},
	EventSecurityPasskeyAdded: {
		Type: EventSecurityPasskeyAdded, Purpose: PurposeSecurity, Category: CategorySecurity,
		DefaultUrgency: UrgencyCritical, AllowedChannels: []Channel{ChannelInApp, ChannelEmail},
		RequiredChannels: []Channel{ChannelInApp, ChannelEmail}, PreferenceKey: "security.passkey_added",
		ConsentRequired: false, Batchable: false, DedupeScope: "domain_ref",
		TemplateKey: "security.passkey_added", AllowlistedVariables: []string{"created_at"},
		AccountSecurityOnly: true,
	},
	EventSecurityPasskeyRemoved: {
		Type: EventSecurityPasskeyRemoved, Purpose: PurposeSecurity, Category: CategorySecurity,
		DefaultUrgency: UrgencyCritical, AllowedChannels: []Channel{ChannelInApp, ChannelEmail},
		RequiredChannels: []Channel{ChannelInApp, ChannelEmail}, PreferenceKey: "security.passkey_removed",
		ConsentRequired: false, Batchable: false, DedupeScope: "domain_ref",
		TemplateKey: "security.passkey_removed", AllowlistedVariables: []string{"created_at"},
		AccountSecurityOnly: true,
	},
	EventSecurityPasswordResetCompleted: {
		Type: EventSecurityPasswordResetCompleted, Purpose: PurposeSecurity, Category: CategorySecurity,
		DefaultUrgency: UrgencyCritical, AllowedChannels: []Channel{ChannelInApp, ChannelEmail},
		RequiredChannels: []Channel{ChannelInApp, ChannelEmail}, PreferenceKey: "security.password_reset_completed",
		ConsentRequired: false, Batchable: false, DedupeScope: "domain_ref",
		TemplateKey: "security.password_reset_completed", AllowlistedVariables: []string{"created_at"},
		AccountSecurityOnly: true,
	},
	EventSecuritySessionsRevoked: {
		Type: EventSecuritySessionsRevoked, Purpose: PurposeSecurity, Category: CategorySecurity,
		DefaultUrgency: UrgencyNormal, AllowedChannels: []Channel{ChannelInApp, ChannelEmail},
		RequiredChannels: []Channel{ChannelInApp}, PreferenceKey: "security.sessions_revoked",
		ConsentRequired: false, Batchable: false, DedupeScope: "domain_ref",
		TemplateKey: "security.sessions_revoked", AllowlistedVariables: []string{"created_at"},
		AccountSecurityOnly: true,
	},
	EventSecurityAccountEvent: {
		Type: EventSecurityAccountEvent, Purpose: PurposeSecurity, Category: CategorySecurity,
		DefaultUrgency: UrgencyNormal, AllowedChannels: []Channel{ChannelInApp, ChannelEmail},
		RequiredChannels: []Channel{ChannelInApp}, PreferenceKey: "security.account_security_event",
		ConsentRequired: false, Batchable: false, DedupeScope: "domain_ref",
		TemplateKey: "security.account_security_event", AllowlistedVariables: []string{"created_at", "event_code"},
		AccountSecurityOnly: true,
	},
	EventMessagingMessageReceived: {
		Type: EventMessagingMessageReceived, Purpose: PurposeSocial, Category: CategoryMessages,
		DefaultUrgency: UrgencyNormal, AllowedChannels: []Channel{ChannelInApp, ChannelWebPush, ChannelMobilePush},
		RequiredChannels: nil, PreferenceKey: "messages",
		ConsentRequired: false, Batchable: false, DedupeScope: "message_id",
		TemplateKey: "messaging.message_received", AllowlistedVariables: []string{"conversation_id", "message_id"},
	},
	EventOfferReceived: {
		Type: EventOfferReceived, Purpose: PurposeTransactional, Category: CategoryOffers,
		DefaultUrgency: UrgencyNormal, AllowedChannels: []Channel{ChannelInApp, ChannelWebPush, ChannelMobilePush, ChannelEmail},
		RequiredChannels: []Channel{ChannelInApp}, PreferenceKey: "offers",
		ConsentRequired: false, Batchable: false, DedupeScope: "domain_ref",
		TemplateKey: "offer.received", AllowlistedVariables: []string{"offer_id", "need_id"},
	},
	EventOfferAccepted: {
		Type: EventOfferAccepted, Purpose: PurposeTransactional, Category: CategoryOffers,
		DefaultUrgency: UrgencyNormal, AllowedChannels: []Channel{ChannelInApp, ChannelWebPush, ChannelMobilePush, ChannelEmail},
		RequiredChannels: []Channel{ChannelInApp}, PreferenceKey: "offers",
		ConsentRequired: false, Batchable: false, DedupeScope: "domain_ref",
		TemplateKey: "offer.accepted", AllowlistedVariables: []string{"offer_id", "need_id"},
	},
	EventOfferRejected: {
		Type: EventOfferRejected, Purpose: PurposeTransactional, Category: CategoryOffers,
		DefaultUrgency: UrgencyNormal, AllowedChannels: []Channel{ChannelInApp, ChannelWebPush, ChannelMobilePush},
		RequiredChannels: []Channel{ChannelInApp}, PreferenceKey: "offers",
		ConsentRequired: false, Batchable: false, DedupeScope: "domain_ref",
		TemplateKey: "offer.rejected", AllowlistedVariables: []string{"offer_id", "need_id"},
	},
	EventTransactionCreated: {
		Type: EventTransactionCreated, Purpose: PurposeTransactional, Category: CategoryTransactions,
		DefaultUrgency: UrgencyNormal, AllowedChannels: []Channel{ChannelInApp, ChannelEmail, ChannelWebPush, ChannelMobilePush},
		RequiredChannels: []Channel{ChannelInApp}, PreferenceKey: "transactions",
		ConsentRequired: false, Batchable: false, DedupeScope: "domain_ref",
		TemplateKey: "transaction.created", AllowlistedVariables: []string{"transaction_id"},
	},
	EventTransactionCompleted: {
		Type: EventTransactionCompleted, Purpose: PurposeTransactional, Category: CategoryTransactions,
		DefaultUrgency: UrgencyNormal, AllowedChannels: []Channel{ChannelInApp, ChannelEmail, ChannelWebPush, ChannelMobilePush},
		RequiredChannels: []Channel{ChannelInApp}, PreferenceKey: "transactions",
		ConsentRequired: false, Batchable: false, DedupeScope: "domain_ref",
		TemplateKey: "transaction.completed", AllowlistedVariables: []string{"transaction_id"},
	},
	EventTransactionCancelled: {
		Type: EventTransactionCancelled, Purpose: PurposeTransactional, Category: CategoryTransactions,
		DefaultUrgency: UrgencyNormal, AllowedChannels: []Channel{ChannelInApp, ChannelEmail},
		RequiredChannels: []Channel{ChannelInApp}, PreferenceKey: "transactions",
		ConsentRequired: false, Batchable: false, DedupeScope: "domain_ref",
		TemplateKey: "transaction.cancelled", AllowlistedVariables: []string{"transaction_id"},
	},
	EventDeliveryStatusChanged: {
		Type: EventDeliveryStatusChanged, Purpose: PurposeTransactional, Category: CategoryTransactions,
		DefaultUrgency: UrgencyNormal, AllowedChannels: []Channel{ChannelInApp, ChannelWebPush, ChannelMobilePush, ChannelEmail},
		RequiredChannels: []Channel{ChannelInApp}, PreferenceKey: "transactions",
		ConsentRequired: false, Batchable: false, DedupeScope: "domain_ref",
		TemplateKey: "delivery.status_changed", AllowlistedVariables: []string{"delivery_id", "status_code"},
	},
	EventDisputeUpdated: {
		Type: EventDisputeUpdated, Purpose: PurposeTransactional, Category: CategoryTransactions,
		DefaultUrgency: UrgencyNormal, AllowedChannels: []Channel{ChannelInApp, ChannelEmail},
		RequiredChannels: []Channel{ChannelInApp}, PreferenceKey: "transactions",
		ConsentRequired: false, Batchable: false, DedupeScope: "domain_ref",
		TemplateKey: "dispute.updated", AllowlistedVariables: []string{"dispute_id", "status_code"},
	},
	EventModerationActionApplied: {
		Type: EventModerationActionApplied, Purpose: PurposeTransactional, Category: CategorySecurity,
		DefaultUrgency: UrgencyNormal, AllowedChannels: []Channel{ChannelInApp},
		RequiredChannels: []Channel{ChannelInApp}, PreferenceKey: "security.moderation",
		ConsentRequired: false, Batchable: false, DedupeScope: "domain_ref",
		TemplateKey: "moderation.warning.issued", AllowlistedVariables: []string{"target_type", "target_ref", "reason_code"},
	},
	EventSavedSearchMatch: {
		Type: EventSavedSearchMatch, Purpose: PurposeProduct, Category: CategorySavedSearches,
		DefaultUrgency: UrgencyLow, AllowedChannels: []Channel{ChannelInApp, ChannelWebPush, ChannelMobilePush, ChannelEmail},
		RequiredChannels: nil, PreferenceKey: "saved_searches",
		ConsentRequired: false, Batchable: true, DedupeScope: "domain_ref",
		TemplateKey: "saved_search.match", AllowlistedVariables: []string{"saved_search_id", "listing_id"},
	},
	EventMarketingCampaign: {
		Type: EventMarketingCampaign, Purpose: PurposeMarketing, Category: CategoryMarketing,
		DefaultUrgency: UrgencyLow, AllowedChannels: []Channel{ChannelEmail, ChannelSMS, ChannelWebPush, ChannelMobilePush, ChannelInApp},
		RequiredChannels: nil, PreferenceKey: "marketing",
		ConsentRequired: true, Batchable: true, DedupeScope: "domain_ref",
		TemplateKey: "marketing.campaign", AllowlistedVariables: []string{"campaign_id"},
	},
	EventIdentityVerificationSignup: {
		Type: EventIdentityVerificationSignup, Purpose: PurposeSecurity, Category: CategorySecurity,
		DefaultUrgency: UrgencyCritical, AllowedChannels: []Channel{ChannelEmail, ChannelSMS},
		RequiredChannels: nil, PreferenceKey: "security.verification",
		ConsentRequired: false, Batchable: false, DedupeScope: "domain_ref",
		TemplateKey: "identity.verification.signup", AllowlistedVariables: []string{},
		AccountSecurityOnly: true,
	},
	EventIdentityPasswordReset: {
		Type: EventIdentityPasswordReset, Purpose: PurposeSecurity, Category: CategorySecurity,
		DefaultUrgency: UrgencyCritical, AllowedChannels: []Channel{ChannelEmail, ChannelSMS},
		RequiredChannels: nil, PreferenceKey: "security.verification",
		ConsentRequired: false, Batchable: false, DedupeScope: "domain_ref",
		TemplateKey: "identity.password.reset", AllowlistedVariables: []string{},
		AccountSecurityOnly: true,
	},
}

func ForbiddenTemplateVariables() []string {
	return []string{
		"password", "otp", "session_token", "cookie", "csrf", "proof",
		"passkey_challenge", "provider_secret", "raw_secret", "authorization",
		"email", "phone", "push_token", "fcm_token", "apns_token",
		"signup_proof", "reset_proof", "webauthn_challenge", "credential",
		"destination", "verification_secret",
	}
}

func KnownConsentType(t ConsentType) bool {
	switch t {
	case ConsentCommercialEmail, ConsentCommercialSMS, ConsentCommercialPush:
		return true
	default:
		return false
	}
}

func KnownConsentSource(s ConsentSource) bool {
	switch s {
	case ConsentSourceSettingsWeb, ConsentSourceSettingsMobile:
		return true
	default:
		return false
	}
}

func ConsentTypeForChannel(ch Channel) (ConsentType, bool) {
	return consentTypeFor(ch)
}

func DedupeKey(event EventType, recipientID, domainRef string) string {
	if domainRef == "" {
		domainRef = "none"
	}
	return string(event) + ":" + recipientID + ":" + domainRef + ":v" + itoa(CatalogVersion)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
