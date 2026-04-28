// Package nrm implements the Network Resource Model (NRM) for NSSAAF.
package nrm

// NRMConfig holds configuration for the NRM RESTCONF server.
type NRMConfig struct {
	// ListenAddr is the address the RESTCONF server listens on.
	// Default: ":8081".
	ListenAddr string `yaml:"listenAddr"`

	// AlarmThresholds defines thresholds for alarm evaluation.
	AlarmThresholds *AlarmThresholds `yaml:"alarmThresholds"`

	// NRMURL is the base URL of this NRM server, used by Biz Pod NRMClient
	// to push events. Set automatically from ListenAddr.
	NRMURL string `yaml:"-"`

	// ReadTimeout for the HTTP server.
	ReadTimeout int `yaml:"readTimeout"` // seconds

	// WriteTimeout for the HTTP server.
	WriteTimeout int `yaml:"writeTimeout"` // seconds

	// IdleTimeout for the HTTP server.
	IdleTimeout int `yaml:"idleTimeout"` // seconds
}

// DefaultNRMConfig returns the default NRM configuration.
func DefaultNRMConfig() *NRMConfig {
	return &NRMConfig{
		ListenAddr:     ":8081",
		AlarmThresholds: DefaultAlarmThresholds(),
		ReadTimeout:    10,
		WriteTimeout:   30,
		IdleTimeout:    120,
	}
}
