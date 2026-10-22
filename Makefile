.PHONY: test

test:
	gitleaks detect --source . --no-git --no-banner --redact
	go test ./...
