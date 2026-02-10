.PHONY: help build run stop logs clean test docker-build docker-push k8s-deploy k8s-status k8s-logs k8s-cleanup

# Variables
IMAGE_NAME := kouke-bot
IMAGE_TAG := latest
REGISTRY ?=
NAMESPACE := kouke-bot

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Local Development
build: ## Build the Go binary
	@echo "Building Go binary..."
	go build -o kouke-bot ./cmd/character

run: ## Run the bot locally
	@echo "Running bot..."
	./kouke-bot

stop: ## Stop the running bot
	@echo "Stopping bot..."
	@pkill -f kouke-bot || true

logs: ## Show local logs
	@tail -f logs/*.log 2>/dev/null || echo "No log files found"

clean: ## Clean build artifacts
	@echo "Cleaning..."
	rm -f kouke-bot
	go clean

test: ## Run tests
	@echo "Running tests..."
	go test ./...

# Docker
docker-build: ## Build Docker image
	@echo "Building Docker image: $(IMAGE_NAME):$(IMAGE_TAG)"
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .

docker-run: docker-build ## Build and run with Docker Compose
	@echo "Starting with Docker Compose..."
	docker-compose up -d

docker-stop: ## Stop Docker Compose
	@echo "Stopping Docker Compose..."
	docker-compose down

docker-logs: ## Show Docker logs
	docker-compose logs -f

docker-push: docker-build ## Build and push Docker image to registry
	@if [ -z "$(REGISTRY)" ]; then \
		echo "Error: REGISTRY not set. Usage: make docker-push REGISTRY=your-registry"; \
		exit 1; \
	fi
	@echo "Pushing to registry: $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)"
	docker tag $(IMAGE_NAME):$(IMAGE_TAG) $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
	docker push $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)

# Kubernetes
k8s-deploy: ## Deploy to Kubernetes
	@echo "Deploying to Kubernetes..."
	cd k8s && chmod +x deploy.sh && ./deploy.sh deploy

k8s-update: ## Update existing Kubernetes deployment
	@echo "Updating Kubernetes deployment..."
	cd k8s && ./deploy.sh update

k8s-status: ## Show Kubernetes status
	@echo "Kubernetes status:"
	kubectl get all -n $(NAMESPACE)
	@echo ""
	@echo "Persistent Volumes:"
	kubectl get pvc -n $(NAMESPACE)

k8s-logs: ## Follow Kubernetes logs
	kubectl logs -n $(NAMESPACE) -l app=kouke-bot -f

k8s-shell: ## Get shell in Kubernetes pod
	kubectl exec -n $(NAMESPACE) -it deployment/kouke-bot -- sh

k8s-port-forward: ## Port forward to local machine
	@echo "Forwarding port 8080..."
	kubectl port-forward -n $(NAMESPACE) service/kouke-bot 8080:8080

k8s-cleanup: ## Delete all Kubernetes resources
	@echo "WARNING: This will delete all resources in namespace $(NAMESPACE)"
	@read -p "Are you sure? [y/N] " -n 1 -r; \
	echo; \
	if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
		cd k8s && ./deploy.sh cleanup; \
	fi

k8s-kustomize: ## Deploy with Kustomize
	@echo "Deploying with Kustomize..."
	kubectl apply -k k8s/

# Web Development
web-dev: ## Run web UI in development mode
	cd web && npm run dev

web-build: ## Build web UI
	cd web && npm run build

web-install: ## Install web dependencies
	cd web && npm install

# Combined Commands
dev: ## Run full development environment
	@echo "Starting development environment..."
	make -j2 web-dev run

all: clean test build ## Clean, test, and build

# Git Commands
git-status: ## Show git status
	@git status

git-log: ## Show git log
	@git log --oneline -10

# Quick deploy to K8s with fresh image
deploy-fresh: docker-build k8s-update ## Build new image and update K8s deployment
	@echo "Deployment updated with fresh image"
