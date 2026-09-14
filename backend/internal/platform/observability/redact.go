package observability

import (
	"net/http"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const Redacted = "[redacted]"

// Access-log and correlation keys that are safe to emit as structured fields.
var safeLogKeys = map[string]struct{}{
	"time": {}, "level": {}, "msg": {}, "env": {},
	"requestid": {}, "traceid": {}, "spanid": {},
	"method": {}, "route": {}, "path": {}, "status": {},
	"durationms": {}, "latencyms": {},
	"actorkind": {}, "sessionpresent": {}, "authorizationpresent": {},
	"errorclass": {}, "provider": {}, "providerlatencyms": {}, "ok": {},
	"authoperation": {}, "riskdecision": {}, "reasoncode": {},
	"ratelimitdimension": {}, "challenged": {}, "challengeprovider": {}, "challengeok": {},
	"addr": {}, "eventtype": {}, "eventversion": {}, "eventid": {},
	"userid": {}, "sessionid": {}, "authmethod": {}, "result": {},
	"mediaid": {}, "mediastatus": {}, "processingoutcome": {}, "objectcategory": {},
	"notifypurpose": {}, "notificationevent": {}, "suppressionreason": {},
	"deliveryoutcome": {}, "deliveryattempt": {}, "intentstatus": {},
	"channelcode": {}, "intentid": {}, "deliveryid": {},
	"endpointid": {}, "pushplatform": {}, "pushprovider": {},
}

var sensitiveKeyExact = map[string]struct{}{
	"authorization": {}, "cookie": {}, "set-cookie": {}, "setcookie": {},
	"x-csrf-token": {}, "csrf": {}, "csrftoken": {}, "xcsrftoken": {},
	"bearer": {}, "token": {}, "accesstoken": {}, "refreshtoken": {},
	"session": {}, "sessiontoken": {}, "staffdevtoken": {}, "stafftoken": {},
	"password": {}, "passwd": {}, "secret": {}, "otp": {}, "totp": {},
	"qr": {}, "qrtoken": {}, "qrsecret": {}, "qrpayload": {},
	"tckn": {}, "nationalid": {}, "tcidentity": {},
	"phone": {}, "phonenumber": {}, "msisdn": {}, "mobile": {},
	"email": {}, "emailaddress": {},
	"address": {}, "street": {}, "streetline": {}, "fulladdress": {}, "privateaddress": {},
	"apikey": {}, "apisecret": {}, "webhooksecret": {}, "webhookkey": {},
	"databaseurl": {}, "dburl": {}, "dbpassword": {}, "databasepassword": {},
	"awssecretaccesskey": {}, "secretkey": {}, "accesskey": {},
	"privatekey": {}, "passkey": {}, "credential": {},
	"signupproof": {}, "resetproof": {}, "challengetoken": {},
	"humanchallenge": {}, "humanchallengeresponse": {}, "providersecret": {},
	"turnstilesecret": {}, "turnstileresponse": {},
	"netgsmpassword": {}, "netgsmusername": {},
	"destination": {}, "verificationsecret": {},
	"pushtoken": {}, "fcmtoken": {}, "apnstoken": {}, "deviceendpoint": {},
	"vapidprivatekey": {}, "vapidpublickey": {}, "apnsprivatekey": {},
	"fcmaccess": {}, "googlecredentials": {},
	"p256dh": {}, "webpushauth": {}, "webpushendpoint": {}, "endpointurl": {},
	"pushendpoint": {}, "endpointciphertext": {}, "endpointnonce": {},
	"pushendpointencryptionkey": {}, "pushendpointhashkey": {},
	"encryptionkey": {}, "hashkey": {},
	"consentpayload": {}, "consentevidence": {}, "consentip": {},
	"notificationbody": {}, "messagecontent": {}, "renderedhtml": {},
	"ceremonytoken": {}, "stepup": {}, "webauthnchallenge": {},
	"credentialrawid": {}, "passkeycredentialid": {},
	"uploadurl": {}, "signedurl": {}, "presignedurl": {}, "objectkey": {},
}

var sensitiveKeyContains = []string{
	"password", "secret", "token", "otp", "apikey", "webhook",
	"authorization", "cookie", "tckn", "credential",
}

var sensitiveCookieNames = map[string]struct{}{
	"__host-konumlu_session": {},
	"__host-konumlu_csrf":    {},
	"konumlu_session":        {},
	"konumlu_csrf":           {},
	"session":                {},
	"csrf":                   {},
	"csrf-token":             {},
}

var (
	emailRE        = regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`)
	phoneRE        = regexp.MustCompile(`(?:\+90|0)5\d{9}`)
	bearerRE       = regexp.MustCompile(`(?i)bearer\s+\S+`)
	basicRE        = regexp.MustCompile(`(?i)basic\s+\S+`)
	tcknRE         = regexp.MustCompile(`\b[1-9]\d{10}\b`)
	secretAssignRE = regexp.MustCompile(`(?i)\b(otp|totp|qr_token|qr_secret|qr|verification_secret|webhook_secret|api_key|staff_dev_token)\s*[:=]\s*\S+`)
	amzQueryRE     = regexp.MustCompile(`(?i)X-Amz-(Algorithm|Credential|Signature|SignedHeaders|Security-Token|Date|Expires)=[^&\s]+`)
)

// RedactAttrValue redacts a structured field according to the central policy.
func RedactAttrValue(key string, value string) string {
	if key == "" {
		return RedactText(value)
	}
	if IsSafeLogKey(key) {
		return RedactText(value)
	}
	if IsSensitiveKey(key) {
		return Redacted
	}
	return RedactText(value)
}

func IsSafeLogKey(key string) bool {
	_, ok := safeLogKeys[normalizeKey(key)]
	return ok
}

func IsSensitiveKey(key string) bool {
	n := normalizeKey(key)
	if n == "" {
		return false
	}
	if _, ok := sensitiveKeyExact[n]; ok {
		return true
	}
	for _, p := range sensitiveKeyContains {
		if strings.Contains(n, p) {
			return true
		}
	}
	return false
}

func IsSensitiveCookie(name string) bool {
	_, ok := sensitiveCookieNames[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

// RedactText removes secrets and PII from an unstructured string. Prefer
// allowlisted structured fields instead of logging dumps.
func RedactText(s string) string {
	if s == "" {
		return s
	}
	s = bearerRE.ReplaceAllString(s, "Bearer "+Redacted)
	s = basicRE.ReplaceAllString(s, "Basic "+Redacted)
	s = amzQueryRE.ReplaceAllString(s, "X-Amz-$1="+Redacted)
	s = secretAssignRE.ReplaceAllStringFunc(s, func(m string) string {
		sep := "="
		if strings.Contains(m, ":") && !strings.Contains(strings.SplitN(m, ":", 2)[0], "=") {
			sep = ":"
		}
		name, _, ok := strings.Cut(m, sep)
		if !ok {
			return Redacted
		}
		return strings.TrimRight(name, " \t") + sep + Redacted
	})
	s = redactCookieDump(s)
	s = emailRE.ReplaceAllString(s, Redacted)
	s = phoneRE.ReplaceAllString(s, Redacted)
	s = tcknRE.ReplaceAllStringFunc(s, func(m string) string {
		if validTCKN(m) {
			return Redacted
		}
		return m
	})
	return s
}

func RedactHeader(name, value string) string {
	canon := textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(name))
	switch canon {
	case "Authorization", "Cookie", "Set-Cookie", "X-Csrf-Token":
		return Redacted
	}
	if IsSensitiveKey(name) {
		return Redacted
	}
	return RedactText(value)
}

// RedactHeaders never returns header contents. Request headers are not a
// loggable surface; callers must use allowlisted structured fields instead.
func RedactHeaders(http.Header) map[string]string {
	return map[string]string{}
}

func normalizeKey(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	for _, r := range key {
		if r == '_' || r == '-' || r == '.' || r == '/' || unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func redactCookieDump(s string) string {
	if !strings.Contains(strings.ToLower(s), "konumlu") &&
		!strings.Contains(strings.ToLower(s), "session") &&
		!strings.Contains(strings.ToLower(s), "csrf") &&
		!strings.Contains(s, "=") {
		return s
	}
	parts := strings.Split(s, ";")
	changed := false
	for i, part := range parts {
		name, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		if IsSensitiveCookie(name) || IsSensitiveKey(name) {
			parts[i] = strings.TrimSpace(name) + "=" + Redacted
			if strings.HasPrefix(strings.TrimSpace(part), " ") || strings.Contains(part, " ") && i > 0 {
				parts[i] = " " + strings.TrimSpace(name) + "=" + Redacted
			}
			_ = val
			changed = true
		}
	}
	if !changed {
		return s
	}
	return strings.Join(parts, ";")
}

func validTCKN(s string) bool {
	if len(s) != 11 {
		return false
	}
	digits := make([]int, 11)
	for i := 0; i < 11; i++ {
		d, err := strconv.Atoi(s[i : i+1])
		if err != nil {
			return false
		}
		digits[i] = d
	}
	if digits[0] == 0 {
		return false
	}
	odd := digits[0] + digits[2] + digits[4] + digits[6] + digits[8]
	even := digits[1] + digits[3] + digits[5] + digits[7]
	d10 := ((odd * 7) - even) % 10
	if d10 < 0 {
		d10 += 10
	}
	if digits[9] != d10 {
		return false
	}
	sum := 0
	for i := 0; i < 10; i++ {
		sum += digits[i]
	}
	return digits[10] == sum%10
}
