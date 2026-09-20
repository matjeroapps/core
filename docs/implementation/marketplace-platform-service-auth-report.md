# Marketplace Platform Service-Auth Report

## Summary

The Platform web application needs a dedicated Core service identity because it
is a separate consumer of marketplace discovery. Reusing a seller, supplier, or
admin token would blur authorization boundaries and make the Platform consumer
depend on another service's credentials.

## Core Changes

- Added the `platform` service caller to Core service authentication.
- Added `CORE_INTERNAL_PLATFORM_TOKEN` to Core configuration, token mapping, and
  production validation.
- Widened `GET /internal/v1/markets/{market_code}/marketplace/collections/{collection_type}`
  to allow `platform`, `seller`, `admin`, and `supplier`.
- Left all other route permissions unchanged.
- Preserved the existing marketplace response contract.

## Infrastructure Changes

- Added `CORE_INTERNAL_PLATFORM_TOKEN` to the Core service environment.
- Added the same platform token as `CORE_API_TOKEN` for the Platform web service.
- Kept `CORE_API_BASE_URL` unchanged.
- Kept the token in server-side environment configuration and did not add any
  `NEXT_PUBLIC_*` token variable.
- No seller, admin, or supplier token is reused by the Platform web service.

## Validation

Core commands executed:

```text
make -C ../platform-infra up-infra
make -C ../platform-infra ps
gofmt -s -w .
go vet ./...
go test -timeout=20m ./...
go test ./internal/serviceauth ./internal/coreapi ./packages/config ./apps/core-api
go run ./cmd/openapi-gen
git diff --check
```

`go vet ./...`, the focused service-auth/configuration/marketplace tests, and
the OpenAPI generator completed successfully. The full repository test command
was attempted with the repository's explicit 20-minute package timeout. It
reported unrelated existing database-fixture timeouts in
`TestIntegrationAdminOverview` and `TestCatalogCategorySlugCannotCrossStores`;
the modified focused packages passed.

Platform-infra commands executed:

```text
make config
git diff --check
```

Compose inspection confirmed that `CORE_API_TOKEN` is present only in the
Platform web service's server-side environment and that no `NEXT_PUBLIC_*`
token variable was introduced. The active-profile Compose assertion also
confirmed the Core token, Platform token, and unchanged Core API base URL.

No public browser endpoint was added in this task.
