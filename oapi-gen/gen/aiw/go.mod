module github.com/operator/nssAAF/oapi-gen/gen/aiw

go 1.25.0

require (
	github.com/go-chi/chi/v5 v5.1.0
	github.com/oapi-codegen/runtime v1.4.0
	github.com/operator/nssAAF/oapi-gen/gen/specs v0.0.0
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
)

replace github.com/operator/nssAAF/oapi-gen/gen/specs => ../specs
