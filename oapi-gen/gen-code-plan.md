1. Create oapi-gen/specs/ ✅
   - Copy 4 YAML files (NSSAA, AIW, CommonData[stripped], NAUSF_UEAuth)
   - Strip TS29571_CommonData.yaml to only needed schemas (~200 lines)

2. Write config YAML for oapi-codegen ✅
   - oapi-gen/nssaa_config.yaml (generate: types, chi-server)
   - oapi-gen/aiw_config.yaml   (generate: types, chi-server)

3. Run oapi-codegen ✅
   - oapi-gen/gen/nssaa/nssaa.gen.go  (344 lines)
   - oapi-gen/gen/aiw/aiw.gen.go    (317 lines)

4. Create Makefile for auto codegen ✅
   - oapi-gen/Makefile
   - Targets: all, generate, generate-nssaa, generate-aiw, tidy-deps, verify-deps, clean

7. Verify:
   - go build ✅ (gen modules compile independently)
   - go test ./...    (run from oapi-gen/gen/{specs,nssaa,aiw})
   - golangci-lint run ./...  (run from oapi-gen/gen/{specs,nssaa,aiw})