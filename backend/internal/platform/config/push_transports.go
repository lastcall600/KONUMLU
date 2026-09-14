package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func parseWebPush() (WebPush, error) {
	pub := strings.TrimSpace(os.Getenv(envWebPushVAPIDPublicKey))
	priv := strings.TrimSpace(os.Getenv(envWebPushVAPIDPrivateKey))
	sub := strings.TrimSpace(os.Getenv(envWebPushVAPIDSubject))
	timeoutRaw := strings.TrimSpace(os.Getenv(envWebPushTimeout))
	if pub == "" && priv == "" && sub == "" && timeoutRaw == "" {
		return WebPush{}, nil
	}
	out := WebPush{
		PublicKey:  pub,
		PrivateKey: priv,
		Subject:    sub,
		Timeout:    defaultWebPushTimeout,
	}
	if timeoutRaw != "" {
		d, err := parsePositiveDuration(envWebPushTimeout, timeoutRaw)
		if err != nil {
			return WebPush{}, err
		}
		out.Timeout = d
	}
	if out.Timeout > 30*time.Second {
		return WebPush{}, fmt.Errorf("%s must be 30s or less", envWebPushTimeout)
	}
	if out.PublicKey == "" {
		return WebPush{}, fmt.Errorf("%s must not be empty when Web Push is enabled", envWebPushVAPIDPublicKey)
	}
	if strings.ContainsAny(out.PublicKey, " \t\r\n") {
		return WebPush{}, fmt.Errorf("%s is malformed", envWebPushVAPIDPublicKey)
	}
	if out.PrivateKey == "" {
		return WebPush{}, fmt.Errorf("%s must not be empty when Web Push is enabled", envWebPushVAPIDPrivateKey)
	}
	if strings.ContainsAny(out.PrivateKey, " \t\r\n") {
		return WebPush{}, fmt.Errorf("%s is malformed", envWebPushVAPIDPrivateKey)
	}
	if err := validateVAPIDSubject(out.Subject); err != nil {
		return WebPush{}, err
	}
	return out, nil
}

func validateVAPIDSubject(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("%s must not be empty when Web Push is enabled", envWebPushVAPIDSubject)
	}
	if strings.ContainsAny(raw, " \t\r\n") {
		return fmt.Errorf("%s is malformed", envWebPushVAPIDSubject)
	}
	if strings.HasPrefix(raw, "mailto:") && strings.Contains(raw[7:], "@") && len(raw) >= 10 {
		return nil
	}
	if strings.HasPrefix(raw, "https://") && len(raw) > len("https://") {
		return nil
	}
	return fmt.Errorf("%s must be a mailto: or https: URI", envWebPushVAPIDSubject)
}

func parseFCM() (FCM, error) {
	project := strings.TrimSpace(os.Getenv(envFCMProjectID))
	file := strings.TrimSpace(os.Getenv(envFCMCredentialsFile))
	timeoutRaw := strings.TrimSpace(os.Getenv(envFCMTimeout))
	if project == "" && file == "" && timeoutRaw == "" {
		return FCM{}, nil
	}
	out := FCM{
		ProjectID:       project,
		CredentialsFile: file,
		Timeout:         defaultFCMTimeout,
	}
	if timeoutRaw != "" {
		d, err := parsePositiveDuration(envFCMTimeout, timeoutRaw)
		if err != nil {
			return FCM{}, err
		}
		out.Timeout = d
	}
	if out.Timeout > 30*time.Second {
		return FCM{}, fmt.Errorf("%s must be 30s or less", envFCMTimeout)
	}
	if out.ProjectID == "" {
		return FCM{}, fmt.Errorf("%s must not be empty when FCM is enabled", envFCMProjectID)
	}
	if strings.ContainsAny(out.ProjectID, " \t\r\n/") {
		return FCM{}, fmt.Errorf("%s is malformed", envFCMProjectID)
	}
	if strings.ContainsAny(out.CredentialsFile, "\r\n") {
		return FCM{}, fmt.Errorf("%s is malformed", envFCMCredentialsFile)
	}
	return out, nil
}

func parseAPNs() (APNs, error) {
	team := strings.TrimSpace(os.Getenv(envAPNSTeamID))
	keyID := strings.TrimSpace(os.Getenv(envAPNSKeyID))
	topic := strings.TrimSpace(os.Getenv(envAPNSTopic))
	pem := strings.TrimSpace(os.Getenv(envAPNSPrivateKey))
	file := strings.TrimSpace(os.Getenv(envAPNSPrivateKeyFile))
	env := strings.ToLower(strings.TrimSpace(os.Getenv(envAPNSEnvironment)))
	timeoutRaw := strings.TrimSpace(os.Getenv(envAPNSTimeout))
	if team == "" && keyID == "" && topic == "" && pem == "" && file == "" && env == "" && timeoutRaw == "" {
		return APNs{}, nil
	}
	if pem != "" && file != "" {
		return APNs{}, fmt.Errorf("%s cannot be combined with %s", envAPNSPrivateKey, envAPNSPrivateKeyFile)
	}
	if file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return APNs{}, fmt.Errorf("%s is unreadable", envAPNSPrivateKeyFile)
		}
		pem = strings.TrimSpace(string(raw))
	}
	out := APNs{
		TeamID:      team,
		KeyID:       keyID,
		Topic:       topic,
		PrivateKey:  pem,
		Environment: env,
		Timeout:     defaultAPNSTimeout,
	}
	if timeoutRaw != "" {
		d, err := parsePositiveDuration(envAPNSTimeout, timeoutRaw)
		if err != nil {
			return APNs{}, err
		}
		out.Timeout = d
	}
	if out.Timeout > 30*time.Second {
		return APNs{}, fmt.Errorf("%s must be 30s or less", envAPNSTimeout)
	}
	if out.TeamID == "" {
		return APNs{}, fmt.Errorf("%s must not be empty when APNs is enabled", envAPNSTeamID)
	}
	if strings.ContainsAny(out.TeamID, " \t\r\n") {
		return APNs{}, fmt.Errorf("%s is malformed", envAPNSTeamID)
	}
	if out.KeyID == "" {
		return APNs{}, fmt.Errorf("%s must not be empty when APNs is enabled", envAPNSKeyID)
	}
	if strings.ContainsAny(out.KeyID, " \t\r\n") {
		return APNs{}, fmt.Errorf("%s is malformed", envAPNSKeyID)
	}
	if out.Topic == "" {
		return APNs{}, fmt.Errorf("%s must not be empty when APNs is enabled", envAPNSTopic)
	}
	if strings.ContainsAny(out.Topic, " \t\r\n") {
		return APNs{}, fmt.Errorf("%s is malformed", envAPNSTopic)
	}
	if out.PrivateKey == "" {
		return APNs{}, fmt.Errorf("%s must not be empty when APNs is enabled", envAPNSPrivateKey)
	}
	if out.Environment != "sandbox" && out.Environment != "production" {
		return APNs{}, fmt.Errorf("%s must be sandbox or production", envAPNSEnvironment)
	}
	return out, nil
}
