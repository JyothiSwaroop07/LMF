REGISTRY ?= ghcr.io/5g-lmf
TAG      ?= latest

SERVICES = sbi-gateway location-request session-manager protocol-handler \
           method-selector gnss-engine tdoa-engine ecid-engine rtt-engine \
           fusion-engine assistance-data event-manager privacy-auth qos-manager

.PHONY: all build test lint fmt proto docker-build docker-push deploy clean

all: build

## Build all services
build:
	@for svc in $(SERVICES); do \
		echo "==> Building $$svc"; \
		(cd services/$$svc && go build ./...) || exit 1; \
	done

## Build individual service: make build-svc SERVICE=gnss-engine
build-svc:
	(cd services/$(SERVICE) && go build ./...)

## Run all tests
test:
	@for svc in $(SERVICES); do \
		echo "==> Testing $$svc"; \
		(cd services/$$svc && go test ./... -v -count=1) || exit 1; \
	done
	(cd common && go test ./... -v -count=1)

## Run tests for individual service
test-svc:
	(cd services/$(SERVICE) && go test ./... -v -count=1)

## Lint all services
lint:
	@for svc in $(SERVICES); do \
		echo "==> Linting $$svc"; \
		(cd services/$$svc && golangci-lint run ./...) || exit 1; \
	done

## Format all Go code
fmt:
	@for svc in $(SERVICES); do \
		(cd services/$$svc && gofmt -w .) || exit 1; \
	done
	(cd common && gofmt -w .)

## Clean build artifacts
clean:
	@for svc in $(SERVICES); do \
		(cd services/$$svc && go clean) || true; \
	done

## Tidy all go.mod files
tidy:
	@for svc in $(SERVICES); do \
		echo "==> go mod tidy $$svc"; \
		(cd services/$$svc && go mod tidy) || exit 1; \
	done
	(cd common && go mod tidy)

## Build all Docker images (build context = repo root so common/ is accessible)
docker-build:
	@for svc in $(SERVICES); do \
		echo "==> Docker build $$svc"; \
		docker build -t $(REGISTRY)/$$svc:$(TAG) -f services/$$svc/Dockerfile .; \
	done

## Push all Docker images
docker-push:
	@for svc in $(SERVICES); do \
		echo "==> Docker push $$svc"; \
		docker push $(REGISTRY)/$$svc:$(TAG); \
	done

## Deploy to Kubernetes
deploy:
	kubectl apply -f deploy/k8s/namespace/
	kubectl apply -f deploy/k8s/configmaps/
	kubectl apply -f deploy/k8s/infra/
	kubectl apply -f deploy/k8s/deployments/
	kubectl apply -f deploy/k8s/services/
	kubectl apply -f deploy/k8s/hpa/
	kubectl apply -f deploy/k8s/networkpolicies/

## Deploy using Helm
helm-install:
	helm upgrade --install lmf deploy/helm/lmf \
		--namespace lmf-system \
		--create-namespace \
		--values deploy/helm/lmf/values.yaml

## Undeploy from Kubernetes
undeploy:
	kubectl delete -f deploy/k8s/ --ignore-not-found

## Clean build artifacts
clean:
	@for svc in $(SERVICES); do \
		cd services/$$svc && go clean && cd ../..; \
	done

## Tidy all go.mod files
tidy:
	@for svc in $(SERVICES); do \
		echo "==> go mod tidy $$svc"; \
		cd services/$$svc && go mod tidy && cd ../..; \
	done
	cd common && go mod tidy && cd ..

## Show help
help:
	@grep -E '^##' $(MAKEFILE_LIST) | sed 's/## //'
