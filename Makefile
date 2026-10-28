.PHONY: test vulncheck

test: vulncheck
	gitleaks detect --source . --no-git --no-banner --redact
	go test ./...

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
