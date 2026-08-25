## What changed?

Describe the change concisely.

## Why?

What problem, use case, bug, or threat does this address?

## Security impact

Does this change affect any of the following?

- authorization or capability scope
- token signing, verification, expiry, or claims
- replay protection
- schema validation or request parsing
- upstream routing or credential injection
- headers, query handling, or URL construction
- secret handling
- error responses or information disclosure
- request/response size limits

If none, write `No meaningful security impact`.

## Testing

Describe the tests run and any new tests added.

For security-sensitive changes, include both successful-path and negative/bypass tests.

## Compatibility / rollout

Describe any impact on configuration, public Go APIs, token behavior, status/error codes, or deployment.

## Checklist

- [ ] The change is focused and explained clearly.
- [ ] `gofmt` has been run.
- [ ] `go test -race ./...` passes.
- [ ] `go vet ./...` passes.
- [ ] Relevant negative/bypass tests were added for security-sensitive behavior.
- [ ] Documentation was updated if behavior, configuration, or the threat model changed.
- [ ] No real credentials, tokens, private endpoints, or personal data were committed.
- [ ] New dependencies, if any, are justified.
- [ ] I have read `SECURITY.md` and `CONTRIBUTING.md` where applicable.
