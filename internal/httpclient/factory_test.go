package httpclient

import (
	"os"
	"testing"

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/proto"
)

func TestNewFactory_ModeNative(t *testing.T) {
	cfg := config.InternalCommConfig{
		Mode: "native",
		Native: config.NativeCommConfig{
			Retry: config.RetryConfig{
				MaxAttempts: 3,
			},
		},
	}

	// Ensure ISTIO_MTLS is not set
	os.Unsetenv("ISTIO_MTLS")

	factory := NewFactory(cfg)

	if factory.Mode() != ModeNative {
		t.Errorf("expected ModeNative, got %v", factory.Mode())
	}
}

func TestNewFactory_ModeIstioViaConfig(t *testing.T) {
	cfg := config.InternalCommConfig{
		Mode: "istio",
		Native: config.NativeCommConfig{
			Retry: config.RetryConfig{
				MaxAttempts: 3,
			},
		},
	}

	// Ensure ISTIO_MTLS is not set
	os.Unsetenv("ISTIO_MTLS")

	factory := NewFactory(cfg)

	if factory.Mode() != ModeIstio {
		t.Errorf("expected ModeIstio, got %v", factory.Mode())
	}
}

func TestNewFactory_ModeIstioViaEnvVar(t *testing.T) {
	cfg := config.InternalCommConfig{
		Mode: "native", // Config says native
		Native: config.NativeCommConfig{
			Retry: config.RetryConfig{
				MaxAttempts: 3,
			},
		},
	}

	// Set ISTIO_MTLS env var
	os.Setenv("ISTIO_MTLS", "1")
	defer os.Unsetenv("ISTIO_MTLS")

	factory := NewFactory(cfg)

	if factory.Mode() != ModeIstio {
		t.Errorf("expected ModeIstio (from ISTIO_MTLS env), got %v", factory.Mode())
	}
}

func TestNewFactory_EnvVarOverridesConfig(t *testing.T) {
	cfg := config.InternalCommConfig{
		Mode: "native",
		Native: config.NativeCommConfig{
			Retry: config.RetryConfig{
				MaxAttempts: 3,
			},
		},
	}

	// ISTIO_MTLS=0 should not override config
	os.Setenv("ISTIO_MTLS", "0")
	defer os.Unsetenv("ISTIO_MTLS")

	factory := NewFactory(cfg)

	if factory.Mode() != ModeNative {
		t.Errorf("expected ModeNative (ISTIO_MTLS=0 should not override), got %v", factory.Mode())
	}
}

func TestNewFactory_DefaultMode(t *testing.T) {
	cfg := config.InternalCommConfig{
		Mode: "", // Empty mode
		Native: config.NativeCommConfig{
			Retry: config.RetryConfig{
				MaxAttempts: 3,
			},
		},
	}

	os.Unsetenv("ISTIO_MTLS")

	factory := NewFactory(cfg)

	if factory.Mode() != ModeNative {
		t.Errorf("expected ModeNative (default), got %v", factory.Mode())
	}
}

func TestFactory_NewBizServiceClient_Native(t *testing.T) {
	cfg := config.InternalCommConfig{
		Mode: "native",
		Native: config.NativeCommConfig{
			Retry: config.RetryConfig{
				MaxAttempts: 3,
			},
		},
	}

	os.Unsetenv("ISTIO_MTLS")

	factory := NewFactory(cfg)
	client := factory.NewBizServiceClient("http://biz-service:8080")

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Verify it's the correct type (nativeBizClient implements BizServiceClient)
	var _ proto.BizServiceClient = client
}

func TestFactory_NewBizServiceClient_Istio(t *testing.T) {
	cfg := config.InternalCommConfig{
		Mode: "istio",
		Native: config.NativeCommConfig{
			Retry: config.RetryConfig{
				MaxAttempts: 3,
			},
		},
	}

	os.Unsetenv("ISTIO_MTLS")

	factory := NewFactory(cfg)
	client := factory.NewBizServiceClient("http://biz-service:8080")

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Verify it's the correct type (istioBizClient implements BizServiceClient)
	var _ proto.BizServiceClient = client
}

func TestFactory_NewAAAClient_Native(t *testing.T) {
	cfg := config.InternalCommConfig{
		Mode: "native",
		Native: config.NativeCommConfig{
			Retry: config.RetryConfig{
				MaxAttempts: 3,
			},
		},
	}

	os.Unsetenv("ISTIO_MTLS")

	factory := NewFactory(cfg)
	client := factory.NewAAAClient("http://aaa-gateway:8080")

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Verify it's the correct type (nativeAAAClient implements BizAAAClient)
	var _ proto.BizAAAClient = client
}

func TestFactory_NewAAAClient_Istio(t *testing.T) {
	cfg := config.InternalCommConfig{
		Mode: "istio",
		Native: config.NativeCommConfig{
			Retry: config.RetryConfig{
				MaxAttempts: 3,
			},
		},
	}

	os.Unsetenv("ISTIO_MTLS")

	factory := NewFactory(cfg)
	client := factory.NewAAAClient("http://aaa-gateway:8080")

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Verify it's the correct type (istioAAAClient implements BizAAAClient)
	var _ proto.BizAAAClient = client
}

func TestFactory_ModeMethod(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		istioEnv bool
		wantMode Mode
	}{
		{
			name:     "native mode from config",
			mode:     "native",
			istioEnv: false,
			wantMode: ModeNative,
		},
		{
			name:     "istio mode from config",
			mode:     "istio",
			istioEnv: false,
			wantMode: ModeIstio,
		},
		{
			name:     "native config overridden by ISTIO_MTLS=1",
			mode:     "native",
			istioEnv: true,
			wantMode: ModeIstio,
		},
		{
			name:     "empty config defaults to native",
			mode:     "",
			istioEnv: false,
			wantMode: ModeNative,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.istioEnv {
				os.Setenv("ISTIO_MTLS", "1")
				defer os.Unsetenv("ISTIO_MTLS")
			} else {
				os.Unsetenv("ISTIO_MTLS")
			}

			cfg := config.InternalCommConfig{
				Mode: tt.mode,
				Native: config.NativeCommConfig{
					Retry: config.RetryConfig{
						MaxAttempts: 3,
					},
				},
			}

			factory := NewFactory(cfg)
			if factory.Mode() != tt.wantMode {
				t.Errorf("got mode %v, want %v", factory.Mode(), tt.wantMode)
			}
		})
	}
}
