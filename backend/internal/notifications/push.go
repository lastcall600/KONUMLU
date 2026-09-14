package notifications

import (
	"crypto/hmac"
	"encoding/json"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"backend/internal/notifications/policy"
	"backend/internal/platform/crypto"
)

const (
	PushPlatformWeb     PushPlatform = "web"
	PushPlatformAndroid PushPlatform = "android"
	PushPlatformIOS     PushPlatform = "ios"

	PushProviderWebPush PushProvider = "webpush"
	PushProviderFCM     PushProvider = "fcm"
	PushProviderAPNs    PushProvider = "apns"

	pushPayloadVersion = 1
	// pushIdentityHashVersion is the keyed uniqueness encoding version.
	// HMAC-SHA256(v1|channel|platform|provider|canonical_endpoint_identity)
	pushIdentityHashVersion = "v1"
	maxWebEndpointLen       = 2048
	maxWebKeyLen       = 256
	minMobileTokenLen  = 16
	maxMobileTokenLen  = 4096
)

type PushPlatform string
type PushProvider string

type PushRegistration struct {
	Channel  policy.Channel
	Platform PushPlatform
	Provider PushProvider
	Web      *WebPushMaterial
	Mobile   *MobilePushMaterial
}

type WebPushMaterial struct {
	Endpoint string
	P256dh   string
	Auth     string
}

func (WebPushMaterial) String() string   { return "notifications.WebPushMaterial" }
func (WebPushMaterial) GoString() string { return "notifications.WebPushMaterial{}" }

type MobilePushMaterial struct {
	Token string
}

func (MobilePushMaterial) String() string   { return "notifications.MobilePushMaterial" }
func (MobilePushMaterial) GoString() string { return "notifications.MobilePushMaterial{}" }

type pushStoredPayload struct {
	Version  int    `json:"version"`
	Kind     string `json:"kind"`
	Endpoint string `json:"endpoint,omitempty"`
	P256dh   string `json:"p256dh,omitempty"`
	Auth     string `json:"auth,omitempty"`
	Token    string `json:"token,omitempty"`
}

func (pushStoredPayload) String() string   { return "notifications.pushStoredPayload" }
func (pushStoredPayload) GoString() string { return "notifications.pushStoredPayload{}" }

func ValidatePushRegistration(in PushRegistration) error {
	if err := validatePushCombo(in.Channel, in.Platform, in.Provider); err != nil {
		return err
	}
	switch in.Channel {
	case policy.ChannelWebPush:
		if in.Mobile != nil || in.Web == nil {
			return policy.ErrInvalidInput
		}
		return validateWebPush(*in.Web)
	case policy.ChannelMobilePush:
		if in.Web != nil || in.Mobile == nil {
			return policy.ErrInvalidInput
		}
		return validateMobileToken(in.Mobile.Token)
	default:
		return policy.ErrInvalidInput
	}
}

func validatePushCombo(ch policy.Channel, platform PushPlatform, provider PushProvider) error {
	switch {
	case ch == policy.ChannelWebPush && platform == PushPlatformWeb && provider == PushProviderWebPush:
		return nil
	case ch == policy.ChannelMobilePush && platform == PushPlatformAndroid && provider == PushProviderFCM:
		return nil
	case ch == policy.ChannelMobilePush && platform == PushPlatformIOS && provider == PushProviderAPNs:
		return nil
	default:
		return policy.ErrInvalidInput
	}
}

func validateWebPush(m WebPushMaterial) error {
	ep := strings.TrimSpace(m.Endpoint)
	p256 := strings.TrimSpace(m.P256dh)
	auth := strings.TrimSpace(m.Auth)
	if ep != m.Endpoint || p256 != m.P256dh || auth != m.Auth {
		return policy.ErrInvalidInput
	}
	if len(ep) < 12 || len(ep) > maxWebEndpointLen || !utf8.ValidString(ep) {
		return policy.ErrInvalidInput
	}
	u, err := url.Parse(ep)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return policy.ErrInvalidInput
	}
	if len(p256) < 16 || len(p256) > maxWebKeyLen || !tokenCharset(p256) {
		return policy.ErrInvalidInput
	}
	if len(auth) < 8 || len(auth) > maxWebKeyLen || !tokenCharset(auth) {
		return policy.ErrInvalidInput
	}
	return nil
}

func validateMobileToken(token string) error {
	if token != strings.TrimSpace(token) || !utf8.ValidString(token) {
		return policy.ErrInvalidInput
	}
	if len(token) < minMobileTokenLen || len(token) > maxMobileTokenLen {
		return policy.ErrInvalidInput
	}
	for _, r := range token {
		if r < 33 || r > 126 || unicode.IsSpace(r) {
			return policy.ErrInvalidInput
		}
	}
	return nil
}

func tokenCharset(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) || r < 33 || r > 126 {
			return false
		}
	}
	return true
}

func canonicalPushHashInput(in PushRegistration) ([]byte, error) {
	if err := ValidatePushRegistration(in); err != nil {
		return nil, err
	}
	ident, err := canonicalEndpointIdentity(in)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString(pushIdentityHashVersion)
	b.WriteByte('|')
	b.WriteString(string(in.Channel))
	b.WriteByte('|')
	b.WriteString(string(in.Platform))
	b.WriteByte('|')
	b.WriteString(string(in.Provider))
	b.WriteByte('|')
	b.WriteString(ident)
	return []byte(b.String()), nil
}

// canonicalEndpointIdentity is the uniqueness subject, not the sealed credential
// bundle. web_push uses the subscription endpoint URL; p256dh/auth stay inside
// ciphertext and must not mint a new endpoint row. mobile_push uses the opaque
// provider token.
func canonicalEndpointIdentity(in PushRegistration) (string, error) {
	switch in.Channel {
	case policy.ChannelWebPush:
		if in.Web == nil {
			return "", policy.ErrInvalidInput
		}
		return in.Web.Endpoint, nil
	case policy.ChannelMobilePush:
		if in.Mobile == nil {
			return "", policy.ErrInvalidInput
		}
		return in.Mobile.Token, nil
	default:
		return "", policy.ErrInvalidInput
	}
}

func marshalPushPayload(in PushRegistration) ([]byte, error) {
	if err := ValidatePushRegistration(in); err != nil {
		return nil, err
	}
	p := pushStoredPayload{Version: pushPayloadVersion}
	switch in.Channel {
	case policy.ChannelWebPush:
		p.Kind = "webpush"
		p.Endpoint = in.Web.Endpoint
		p.P256dh = in.Web.P256dh
		p.Auth = in.Web.Auth
	case policy.ChannelMobilePush:
		p.Kind = "mobile"
		p.Token = in.Mobile.Token
	}
	return json.Marshal(p)
}

func unmarshalPushPayload(raw []byte) (PushProviderMaterial, error) {
	var p pushStoredPayload
	if err := json.Unmarshal(raw, &p); err != nil || p.Version != pushPayloadVersion {
		return PushProviderMaterial{}, errUnavailable
	}
	switch p.Kind {
	case "webpush":
		m := WebPushMaterial{Endpoint: p.Endpoint, P256dh: p.P256dh, Auth: p.Auth}
		if err := validateWebPush(m); err != nil {
			return PushProviderMaterial{}, errUnavailable
		}
		return PushProviderMaterial{Web: &m}, nil
	case "mobile":
		if err := validateMobileToken(p.Token); err != nil {
			return PushProviderMaterial{}, errUnavailable
		}
		return PushProviderMaterial{Mobile: &MobilePushMaterial{Token: p.Token}}, nil
	default:
		return PushProviderMaterial{}, errUnavailable
	}
}

func hashPushMaterial(key crypto.HMACKey, in PushRegistration) ([]byte, error) {
	msg, err := canonicalPushHashInput(in)
	if err != nil {
		return nil, err
	}
	sum, err := key.Sum(msg)
	if err != nil {
		return nil, errUnavailable
	}
	return sum, nil
}

func hashesEqual(a, b []byte) bool {
	return hmac.Equal(a, b)
}

func pushAAD(endpointID, userID ID) []byte {
	var b []byte
	b = append(b, "konumlu.notifications.push_endpoint.v1"...)
	b = append(b, 0)
	b = append(b, endpointID[:]...)
	b = append(b, 0)
	b = append(b, userID[:]...)
	return b
}
