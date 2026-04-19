# NSSAAF Module Index

## Module → Design Doc Mapping

| Module | Design Doc | Phase | Status |
|--------|-----------|-------|--------|
| `cmd/nssAAF/` | `docs/design/01_service_model.md` | 0 | READY |
| `cmd/biz/` | `docs/design/01_service_model.md` §5.4 | R | TBD |
| `cmd/http-gateway/` | `docs/design/01_service_model.md` §5.4.4 | R | TBD |
| `cmd/aaa-gateway/` | `docs/design/01_service_model.md` §5.4.5 | R | TBD |
| `internal/proto/` | `docs/design/01_service_model.md` §5.4.6 | R | TBD |
| `internal/aaa/gateway/` | `docs/design/01_service_model.md` §5.4.5 | R | TBD |
| `internal/api/common/` | `docs/design/02_nssaa_api.md` | 1 | READY |
| `internal/types/` | `docs/design/04_data_model.md` | 1 | READY |
| `internal/api/nssaa/` | `docs/design/02_nssaa_api.md` | 1 | READY |
| `internal/api/aiw/` | `docs/design/03_aiw_api.md` | 1 | READY |
| `internal/config/` | `docs/design/04_data_model.md` | 1 | READY |
| `internal/eap/` | `docs/design/06_eap_engine.md` | 2 | READY |
| `internal/radius/` | `docs/design/07_radius_client.md` | 2 | READY |
| `internal/diameter/` | `docs/design/08_diameter_client.md` | 2 | READY |
| `internal/aaa/` | `docs/design/09_aaa_proxy.md` | 2 | READY |
| `internal/storage/postgres/` | `docs/design/11_database_ha.md` | 3 | READY |
| `internal/cache/redis/` | `docs/design/12_redis_ha.md` | 3 | READY |
| `internal/resilience/` | `docs/design/10_ha_architecture.md` | 4 | TBD |
| `internal/auth/` | `docs/design/15_sbi_security.md`, `docs/design/16_aaa_security.md` | 5 | TBD |
| `internal/crypto/` | `docs/design/17_crypto.md`, `docs/design/16_aaa_security.md` | 5 | TBD |
| `internal/nrf/` | `docs/design/05_nf_profile.md` | 6 | READY |
| `internal/udm/` | `docs/design/22_udm_integration.md` | 6 | READY |
| `internal/amf/` | `docs/design/21_amf_integration.md` | 6 | READY |
| *(external NF)* | `docs/design/23_ausf_integration.md` | 6 | TBD |
| `internal/nrm/` | `docs/design/18_nrm_fcaps.md` | 7 | TBD |
| *(cross-cutting)* | `docs/design/19_observability.md` | ALL | TBD |
| *(cross-cutting)* | `docs/design/20_config_management.md` | ALL | TBD |
| *(cross-cutting)* | `docs/design/24_test_strategy.md` | ALL | TBD |
| `deployments/helm/` | `docs/design/25_kubeadm_setup.md` | 7 | READY |


| `deployments/helm/nssaa-biz/` | `docs/design/01_service_model.md` §5.4.5 | R | TBD |
| `deployments/helm/nssaa-http-gateway/` | `docs/design/01_service_model.md` §5.4.4 | R | TBD |
| `deployments/helm/nssaa-aaa-gateway/` | `docs/design/01_service_model.md` §5.4.5 | R | TBD |
## Dependency Graph

```
cmd/nssAAF/
    └── internal/api/common/
    └── internal/api/nssaa/
        ├── internal/types/
        ├── internal/storage/postgres/
        ├── internal/cache/redis/
        ├── internal/eap/
        ├── internal/radius/
        ├── internal/aaa/
        ├── internal/auth/
        ├── internal/crypto/
        ├── internal/resilience/
        ├── internal/nrf/
        └── internal/udm/
            └── internal/nrf/

internal/api/aiw/
    ├── internal/types/
    └── internal/storage/postgres/

internal/eap/
    └── internal/types/

internal/radius/
    └── internal/eap/

internal/diameter/
    └── internal/eap/

internal/aaa/
    ├── internal/radius/
    └── internal/diameter/

internal/auth/
    └── internal/crypto/

internal/storage/postgres/
    └── internal/crypto/
```

## Test Coverage Targets

| Module | Target Coverage |
|--------|----------------|
| `internal/types/` | 95% |
| `internal/api/nssaa/` | 90% |
| `internal/api/aiw/` | 90% |
| `internal/eap/` | 85% |
| `internal/radius/` | 85% |
| `internal/diameter/` | 85% |
| `internal/storage/postgres/` | 80% |
| `internal/cache/redis/` | 80% |
| `internal/resilience/` | 90% |
| `internal/auth/` | 85% |
| `internal/crypto/` | 90% |
| `internal/nrf/` | 85% |
| `internal/udm/` | 85% |
| `cmd/nssAAF/` | 80% |
| `internal/api/common/` | 90% |
| *observability* | 70% |
| *test strategy* | 75% |
| **Overall** | **>80%** |
