build: ## Build project for development
	go mod vendor
	go build -o bin/golang-service-apm

