module github.com/operator/nssAAF

go 1.25.0

require (
	github.com/google/uuid v1.6.0
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/alicebob/miniredis/v2 v2.37.0
	github.com/fiorix/go-diameter/v4 v4.1.0
	github.com/go-chi/chi/v5 v5.1.0
	github.com/jackc/pgx/v5 v5.9.1
	github.com/operator/nssAAF/oapi-gen/gen/aiw v0.0.0-00010101000000-000000000000
	github.com/operator/nssAAF/oapi-gen/gen/nssaa v0.0.0-00010101000000-000000000000
	github.com/operator/nssAAF/oapi-gen/gen/specs v0.0.0
	github.com/prometheus/client_golang v1.20.5
	github.com/redis/go-redis/v9 v9.18.0
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.57.0
	go.opentelemetry.io/otel v1.43.0
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.32.0
	go.opentelemetry.io/otel/sdk v1.43.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/ishidawataru/sctp v0.0.0-20251114114122-19ddcbc6aae2 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/miekg/pkcs11 v1.1.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/oapi-codegen/runtime v1.4.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/operator/nssAAF/oapi-gen/gen/specs => ./oapi-gen/gen/specs

replace github.com/operator/nssAAF/oapi-gen/gen/nssaa => ./oapi-gen/gen/nssaa

replace github.com/operator/nssAAF/oapi-gen/gen/aiw => ./oapi-gen/gen/aiw
