VERSION ?= latest
REGISTRY ?= europe-docker.pkg.dev/srlinux/eu.gcr.io
PROJECT ?= capis
IMG ?= $(REGISTRY)/${PROJECT}:$(VERSION)

REPO = github.com/henderiw/apiserver-runtime-example

## Tool Binaries
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
CONTROLLER_TOOLS_VERSION ?= v0.12.1

.PHONY: codegen fix fmt vet lint test tidy

GOBIN := $(shell go env GOPATH)/bin

all: codegen fmt vet lint test tidy

docker:
	GOOS=linux GOARCH=arm64 go build -o install/bin/apiserver
	docker build install --tag apiserver-caas:v0.0.0

.PHONY:
docker-build: ## Build docker image with the manager.
	docker build -t ${IMG} .

.PHONY: docker-push
docker-push:  docker-build ## Push docker image with the manager.
	docker push ${IMG}

install: docker
	kustomize build install | kubectl apply -f -

reinstall: docker
	kustomize build install | kubectl apply -f -
	kubectl delete pods -n config-system --all

apiserver-logs:
	kubectl logs -l apiserver=true --container apiserver -n config-system -f --tail 1000

codegen:
	(which apiserver-runtime-gen || go get sigs.k8s.io/apiserver-runtime/tools/apiserver-runtime-gen)
	go generate

genclients:
	go run ./tools/apiserver-runtime-gen \
		-g client-gen \
		-g deepcopy-gen \
		-g informer-gen \
		-g lister-gen \
		-g openapi-gen \
		--module $(REPO) \
		--versions $(REPO)/apis/config/v1alpha1

.PHONY: generate
generate: controller-gen 
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./apis/inv/..."

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./apis/inv/..." output:crd:artifacts:config=artifacts

fix:
	go fix ./...

fmt:
	test -z $(go fmt ./tools/...)

tidy:
	go mod tidy

lint:
	(which golangci-lint || go get github.com/golangci/golangci-lint/cmd/golangci-lint)
	$(GOBIN)/golangci-lint run ./...

test:
	go test -cover ./...

vet:
	go vet ./...

local-run:
	apiserver-boot run local --run=etcd,apiserver

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)