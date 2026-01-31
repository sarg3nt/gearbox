.PHONY: help security-scan install-hooks install-security-tools

help: ## Show this help message
	@echo "Gearbox Security & Development Tools"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-25s %s\n", $$1, $$2}'

install-security-tools: ## Install security scanning tools (gosec, trivy, gitleaks, nancy)
	@echo "Installing security tools..."
	@echo "Installing gosec..."
	@go install github.com/securego/gosec/v2/cmd/gosec@latest
	@echo "Installing nancy (dependency vulnerability scanner)..."
	@go install github.com/sonatype-nexus-community/nancy@latest
	@echo ""
	@echo "To install trivy, run:"
	@echo "  brew install aquasecurity/trivy/trivy  (macOS)"
	@echo "  Or visit: https://aquasecurity.github.io/trivy/latest/getting-started/installation/"
	@echo ""
	@echo "To install gitleaks, run:"
	@echo "  brew install gitleaks  (macOS)"
	@echo "  Or visit: https://github.com/gitleaks/gitleaks#installing"
	@echo ""
	@echo "Security tools installation complete!"

install-hooks: ## Install git pre-commit hooks (requires gitleaks)
	@echo "Installing pre-commit hooks..."
	@if ! command -v gitleaks &> /dev/null; then \
		echo "Error: gitleaks is not installed. Install it first:"; \
		echo "  brew install gitleaks  (macOS)"; \
		echo "  Or visit: https://github.com/gitleaks/gitleaks#installing"; \
		exit 1; \
	fi
	@echo "#!/bin/sh" > .git/hooks/pre-commit
	@echo "" >> .git/hooks/pre-commit
	@echo "# Gitleaks pre-commit hook" >> .git/hooks/pre-commit
	@echo "# Prevents committing secrets to the repository" >> .git/hooks/pre-commit
	@echo "" >> .git/hooks/pre-commit
	@echo "gitleaks protect --verbose --redact --staged" >> .git/hooks/pre-commit
	@echo "" >> .git/hooks/pre-commit
	@echo "if [ \$$? -ne 0 ]; then" >> .git/hooks/pre-commit
	@echo "  echo \"\033[0;31mError: Gitleaks detected secrets in your commit!\033[0m\"" >> .git/hooks/pre-commit
	@echo "  echo \"Please remove the secrets before committing.\"" >> .git/hooks/pre-commit
	@echo "  echo \"\"" >> .git/hooks/pre-commit
	@echo "  echo \"If this is a false positive, you can:\"" >> .git/hooks/pre-commit
	@echo "  echo \"  1. Add an exception to .gitleaks.toml\"" >> .git/hooks/pre-commit
	@echo "  echo \"  2. Use 'git commit --no-verify' to skip this check (NOT recommended)\"" >> .git/hooks/pre-commit
	@echo "  exit 1" >> .git/hooks/pre-commit
	@echo "fi" >> .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "✅ Pre-commit hook installed successfully!"
	@echo ""
	@echo "The hook will now scan for secrets before every commit."
	@echo "To test it, try committing a file with a fake API key."

security-scan: ## Run all security scans locally
	@echo "Running security scans..."
	@echo ""
	@echo "1️⃣  Running gosec on gearbox..."
	@cd gearbox && gosec -exclude-generated ./... || true
	@echo ""
	@echo "2️⃣  Running gosec on gearbox-agent..."
	@cd gearbox-agent && gosec -exclude-generated ./... || true
	@echo ""
	@echo "3️⃣  Running trivy filesystem scan..."
	@if command -v trivy &> /dev/null; then \
		trivy fs --severity HIGH,CRITICAL .; \
	else \
		echo "⚠️  trivy not installed. Skipping."; \
		echo "Install with: brew install aquasecurity/trivy/trivy"; \
	fi
	@echo ""
	@echo "4️⃣  Running npm audit..."
	@cd gearbox && npm audit --audit-level=high || true
	@echo ""
	@echo "5️⃣  Running gitleaks on repository..."
	@if command -v gitleaks &> /dev/null; then \
		gitleaks detect --verbose --redact; \
	else \
		echo "⚠️  gitleaks not installed. Skipping."; \
		echo "Install with: brew install gitleaks"; \
	fi
	@echo ""
	@echo "6️⃣  Running Go dependency vulnerability scan (nancy)..."
	@if command -v nancy &> /dev/null; then \
		cd gearbox && go list -json -m all | nancy sleuth || true; \
		cd ../gearbox-agent && go list -json -m all | nancy sleuth || true; \
	else \
		echo "⚠️  nancy not installed. Skipping."; \
		echo "Install with: go install github.com/sonatype-nexus-community/nancy@latest"; \
	fi
	@echo ""
	@echo "✅ Security scanning complete!"
	@echo ""
	@echo "Review the output above for any security issues."

gosec: ## Run gosec security scanner on both projects
	@echo "Running gosec..."
	@cd gearbox && gosec -exclude-generated ./...
	@cd gearbox-agent && gosec -exclude-generated ./...

trivy: ## Run trivy vulnerability scanner
	@echo "Running trivy..."
	@if command -v trivy &> /dev/null; then \
		trivy fs --severity HIGH,CRITICAL .; \
	else \
		echo "Error: trivy not installed."; \
		echo "Install with: brew install aquasecurity/trivy/trivy"; \
		exit 1; \
	fi

gitleaks: ## Run gitleaks secret scanner
	@echo "Running gitleaks..."
	@if command -v gitleaks &> /dev/null; then \
		gitleaks detect --verbose --redact; \
	else \
		echo "Error: gitleaks not installed."; \
		echo "Install with: brew install gitleaks"; \
		exit 1; \
	fi

npm-audit: ## Run npm security audit
	@echo "Running npm audit..."
	@cd gearbox && npm audit

clean-hooks: ## Remove git hooks
	@echo "Removing pre-commit hook..."
	@rm -f .git/hooks/pre-commit
	@echo "✅ Pre-commit hook removed!"
