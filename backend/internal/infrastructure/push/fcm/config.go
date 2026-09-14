package fcm

import (
	"fmt"
	"strings"
	"time"
)

const (
	ProviderName   = "fcm"
	defaultTimeout = 5 * time.Second
	maxTimeout     = 30 * time.Second
	fcmScope       = "https://www.googleapis.com/auth/firebase.messaging"
)

// Config is FCM HTTP v1 settings. Credential JSON is never stored here.
type Config struct {
	ProjectID       string
	CredentialsFile string
	Timeout         time.Duration
}

func (c Config) String() string {
	return fmt.Sprintf("fcm.Config{project_id_configured:%t credentials_file_configured:%t timeout:%s}",
		c.ProjectID != "", c.CredentialsFile != "", c.Timeout)
}

func (c Config) GoString() string { return c.String() }

func (c Config) normalized() Config {
	c.ProjectID = strings.TrimSpace(c.ProjectID)
	c.CredentialsFile = strings.TrimSpace(c.CredentialsFile)
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	return c
}

func (c Config) Validate() error {
	c = c.normalized()
	if c.ProjectID == "" {
		return fmt.Errorf("FCM_PROJECT_ID must not be empty when FCM is enabled")
	}
	if strings.ContainsAny(c.ProjectID, " \t\r\n/") {
		return fmt.Errorf("FCM_PROJECT_ID is malformed")
	}
	if strings.ContainsAny(c.CredentialsFile, "\r\n") {
		return fmt.Errorf("FCM_CREDENTIALS_FILE is malformed")
	}
	if c.Timeout > maxTimeout {
		return fmt.Errorf("FCM_TIMEOUT must be %s or less", maxTimeout)
	}
	return nil
}

func sendURL(projectID string) string {
	return "https://fcm.googleapis.com/v1/projects/" + projectID + "/messages:send"
}
