PROJECT_NAME := hop-hoodi-testnet
ETHEREUM_KEY_DIR := ${PWD}/hop-hoodi/ethereum
GETH_DATA_DIR := ${PWD}/hop-hoodi/geth-data
PRYSM_DATA_DIR := ${PWD}/hop-hoodi/prysm-data
NIMBUS_DATA_DIR := ${PWD}/hop-hoodi/nimbus-data
LODESTAR_DATA_DIR := ${PWD}/hop-hoodi/lodestar-data
JWT_SECRET_FILE := ${PWD}/hop-hoodi/jwt.hex
ENV_FILE := .env
ENV_EXAMPLE := .env.example
PLATFORM := linux/arm64

help: ## Show help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

init: stop ## Initialize project and set GATEWAY_PEER in .env
	@echo "Initializing ${PROJECT_NAME}..."
	mkdir -p ${ETHEREUM_KEY_DIR}
	mkdir -p ${GETH_DATA_DIR}
	mkdir -p ${PRYSM_DATA_DIR}
	mkdir -p ${LODESTAR_DATA_DIR}
	@echo "Create jwt secret"
	@chmod +x prysm.sh
	@./prysm.sh beacon-chain generate-auth-secret
	@mv jwt.hex ${JWT_SECRET_FILE}
	@if [ ! -f ${ENV_FILE} ]; then \
		echo "Creating ${ENV_FILE} from ${ENV_EXAMPLE}..."; \
		cp ${ENV_EXAMPLE} ${ENV_FILE}; \
	fi
	@if [ ! -f config/app_conf.yml ]; then \
		echo "Creating config/app_conf.yml from sample..."; \
		cp config/sample.app_conf.yml config/app_conf.yml; \
	fi
	@echo "Starting gateway container to fetch peer info..."
	@DOCKER_DEFAULT_PLATFORM=${PLATFORM} docker compose up -d --build optimum-gateway
	@echo "Waiting for gateway API..."
	@sleep 10
		@( \
		PEER_ID=$$(curl -s http://localhost:48123/api/v1/self_info | jq -r '.peer_id'); \
		ADDR=$$(curl -s http://localhost:48123/api/v1/self_info | jq -r '[.libp2p.multiaddrs[] | select(startswith("/ip4/172."))] | first // .libp2p.multiaddrs[0]'); \
		GATEWAY_PEER="$${ADDR}/p2p/$${PEER_ID}"; \
		echo "Discovered GATEWAY_PEER=$${GATEWAY_PEER}"; \
		sed -i.bak '/^GATEWAY_PEER=/d' "${ENV_FILE}"; \
		sed -i.bak '/^ADDR=/d' "${ENV_FILE}"; \
		sed -i.bak '/^PEER_ID=/d' "${ENV_FILE}"; \
		rm -f "${ENV_FILE}.bak"; \
		echo "GATEWAY_PEER=$${GATEWAY_PEER}" >> ${ENV_FILE}; \
		echo "ADDR=$${ADDR}" >> ${ENV_FILE}; \
		echo "PEER_ID=$${PEER_ID}" >> ${ENV_FILE}; \
		echo "Updated ${ENV_FILE} with gateway peer"; \
	)


reset: stop ## Reset project for re-init prysm
	@echo "Resetting ${PROJECT_NAME}..."
	rm -rf ${ETHEREUM_KEY_DIR}
	rm -rf ${GETH_DATA_DIR}
	rm -rf ${PRYSM_DATA_DIR}
	rm -rf ${NIMBUS_DATA_DIR}
	rm -rf ${LODESTAR_DATA_DIR}
	rm -f ./identity/libp2p/p2p.key
	$(MAKE) init

stop: ## Stop compose services
	@echo "Stopping ${PROJECT_NAME} services..."
	docker compose down

clean: stop ## Stop services and remove all data + env
	@echo "Cleaning ${PROJECT_NAME} completely..."
	# Stop and remove *all* docker containers
	docker stop $$(docker ps -a -q) || true
	docker rm $$(docker ps -a -q) || true
	docker volume prune -f
	docker network prune -f
	# Remove local data dirs
	rm -rf ${ETHEREUM_KEY_DIR}
	rm -rf ${GETH_DATA_DIR}
	rm -rf ${PRYSM_DATA_DIR}
	rm -rf ${NIMBUS_DATA_DIR}
	rm -rf ${LODESTAR_DATA_DIR}
	rm -f ${JWT_SECRET_FILE}

# ============================================================
# Run targets with profiles
# ============================================================

run: stop ## Run full stack with Geth + Prysm + monitoring
	@echo "Running ${PROJECT_NAME} (Geth + Prysm + Monitoring)..."
	docker compose --profile full --profile geth --profile prysm up --build -d

run_teku: stop ## Run full stack with Nethermind + Teku + monitoring
	@echo "Running ${PROJECT_NAME} (Nethermind + Teku + Monitoring)..."
	docker compose --profile full --profile nethermind --profile teku up --build -d

run_lighthouse: stop ## Run full stack with Nethermind + Lighthouse + monitoring
	@echo "Running ${PROJECT_NAME} (Nethermind + Lighthouse + Monitoring)..."
	docker compose --profile full --profile nethermind --profile lighthouse up --build -d

run_prysm: stop ## Run full stack with Nethermind + Prysm + monitoring
	@echo "Running ${PROJECT_NAME} (Nethermind + Prysm + Monitoring)..."
	docker compose --profile full --profile nethermind --profile prysm up --build -d

run_nimbus: stop ## Run full stack with Nethermind + Nimbus + monitoring
	@echo "Running ${PROJECT_NAME} (Nethermind + Nimbus + Monitoring)..."
	docker compose --profile full --profile nethermind --profile nimbus up --build -d

run_lodestar: stop ## Run full stack with Nethermind + Lodestar + monitoring
	@echo "Running ${PROJECT_NAME} (Nethermind + Lodestar + Monitoring)..."
	docker compose --profile full --profile nethermind --profile lodestar up --build -d

lite: stop ## Run Gateway + Monitoring only (no EL/CL)
	@echo "Running ${PROJECT_NAME} (lite mode: Gateway + Metrics only)..."
	docker compose --profile lite up --build -d

.PHONY: help stop run reset run_teku run_lighthouse run_prysm run_nimbus run_lodestar lite clean
.DEFAULT_GOAL := help
