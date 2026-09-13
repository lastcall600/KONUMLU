package policy

import "errors"

// Purpose is a product class. It is not a channel and is not a legal conclusion.
const (
	PurposeSecurity      Purpose = "security"
	PurposeTransactional Purpose = "transactional"
	PurposeSocial        Purpose = "social"
	PurposeProduct       Purpose = "product"
	PurposeMarketing     Purpose = "marketing"
)

// Channel is provider-neutral. Vendor names are not valid values.
const (
	ChannelInApp      Channel = "in_app"
	ChannelWebPush    Channel = "web_push"
	ChannelMobilePush Channel = "mobile_push"
	ChannelEmail      Channel = "email"
	ChannelSMS        Channel = "sms"
)

const (
	UrgencyCritical Urgency = "critical"
	UrgencyNormal   Urgency = "normal"
	UrgencyLow      Urgency = "low"
)

const (
	CategorySecurity      Category = "security"
	CategoryMessages      Category = "messages"
	CategoryOffers        Category = "offers"
	CategoryTransactions  Category = "transactions"
	CategorySavedSearches Category = "saved_searches"
	CategoryCommunity     Category = "community"
	CategoryMarketing     Category = "marketing"
)

const (
	SuppressUserPreference     SuppressionReason = "user_preference"
	SuppressConsentMissing     SuppressionReason = "consent_missing"
	SuppressConsentWithdrawn   SuppressionReason = "consent_withdrawn"
	SuppressChannelUnavailable SuppressionReason = "channel_unavailable"
	SuppressNoDestination      SuppressionReason = "no_destination"
	SuppressDeduplicated       SuppressionReason = "deduplicated"
	SuppressPolicy             SuppressionReason = "policy_suppressed"
	SuppressAccountDisabled    SuppressionReason = "account_disabled"
	SuppressUnknownEvent       SuppressionReason = "unknown_event"
)

const (
	DeliveryPending           DeliveryState = "pending"
	DeliveryProcessing        DeliveryState = "processing"
	DeliveryAccepted          DeliveryState = "accepted"
	DeliveryRetryableFailed   DeliveryState = "retryable_failed"
	DeliveryPermanentlyFailed DeliveryState = "permanently_failed"
	DeliverySuppressed        DeliveryState = "suppressed"
)

const (
	AccountActive     AccountState = "active"
	AccountDisabled   AccountState = "disabled"
	AccountDeleted    AccountState = "deleted"
	AccountRestricted AccountState = "restricted"
)

const (
	ConsentGranted   ConsentStatus = "granted"
	ConsentWithdrawn ConsentStatus = "withdrawn"
)

const (
	ConsentCommercialEmail ConsentType = "commercial_electronic.email"
	ConsentCommercialSMS   ConsentType = "commercial_electronic.sms"
	ConsentCommercialPush  ConsentType = "commercial_electronic.push"
)

const (
	ConsentSourceSettingsWeb    ConsentSource = "settings_web"
	ConsentSourceSettingsMobile ConsentSource = "settings_mobile"
)

const (
	RetryTransient RetryClass = "transient"
	RetryPermanent RetryClass = "permanent"
)

const (
	ScopeChannel  ScopeType = "channel"
	ScopeCategory ScopeType = "category"
	ScopeEvent    ScopeType = "event"
)

var (
	ErrUnknownEvent      = errors.New("unknown notification event")
	ErrUnknownPreference = errors.New("unknown notification preference key")
	ErrUnknownConsent    = errors.New("unknown consent type")
	ErrSystemRequired    = errors.New("system-required notification policy cannot be mutated")
	ErrInvalidActor      = errors.New("notification actor required")
	ErrNotFound          = errors.New("notification resource not found")
	ErrInvalidInput      = errors.New("invalid notification policy input")
	ErrClientTimestamp   = errors.New("client timestamps are not authoritative")
	ErrArbitraryMetadata = errors.New("arbitrary notification metadata is not allowed")
)

type Purpose string
type Channel string
type Urgency string
type Category string
type SuppressionReason string
type DeliveryState string
type AccountState string
type ConsentStatus string
type ConsentType string
type ConsentSource string
type RetryClass string
type EventType string
type ScopeType string

func (p Purpose) valid() bool {
	switch p {
	case PurposeSecurity, PurposeTransactional, PurposeSocial, PurposeProduct, PurposeMarketing:
		return true
	default:
		return false
	}
}

func (c Channel) valid() bool {
	switch c {
	case ChannelInApp, ChannelWebPush, ChannelMobilePush, ChannelEmail, ChannelSMS:
		return true
	default:
		return false
	}
}

func AllChannels() []Channel {
	return []Channel{ChannelInApp, ChannelWebPush, ChannelMobilePush, ChannelEmail, ChannelSMS}
}

func AllCategories() []Category {
	return []Category{
		CategorySecurity, CategoryMessages, CategoryOffers, CategoryTransactions,
		CategorySavedSearches, CategoryCommunity, CategoryMarketing,
	}
}

func (s ScopeType) valid() bool {
	switch s {
	case ScopeChannel, ScopeCategory, ScopeEvent:
		return true
	default:
		return false
	}
}

func AllowedChannelTransition(from, to DeliveryState) bool {
	if from == to {
		return true
	}
	switch from {
	case DeliveryPending:
		switch to {
		case DeliveryProcessing, DeliveryAccepted, DeliveryRetryableFailed, DeliveryPermanentlyFailed, DeliverySuppressed:
			return true
		}
	case DeliveryProcessing:
		switch to {
		case DeliveryAccepted, DeliveryRetryableFailed, DeliveryPermanentlyFailed, DeliverySuppressed:
			return true
		}
	case DeliveryRetryableFailed:
		switch to {
		case DeliveryProcessing, DeliveryAccepted, DeliveryPermanentlyFailed, DeliverySuppressed:
			return true
		}
	case DeliveryAccepted, DeliveryPermanentlyFailed, DeliverySuppressed:
		return false
	}
	return false
}
