.PHONY: build test integration services embedder ablation eval web up

build:
	go build -o bin/ragd ./cmd/ragd

test:
	go test -race ./...

# Needs Qdrant and the embedder running (make services embedder).
integration:
	RAG_INTEGRATION=1 go test -count=1 ./internal/server/

# Qdrant in Docker on localhost:6333.
services:
	docker run -d --name rag-qdrant -p 127.0.0.1:6333:6333 -v rag-qdrant-data:/qdrant/storage qdrant/qdrant:v1.20.2

# The embedding service in a local virtualenv on localhost:8001.
embedder:
	python3 -m venv embedder/.venv
	embedder/.venv/bin/pip install -r embedder/requirements.txt
	embedder/.venv/bin/uvicorn app:app --app-dir embedder --port 8001

# Retrieval study: no LLM calls, about 4 minutes on a laptop CPU.
ablation: build
	./bin/ragd ablation

# Answer quality: calls the Claude API (about 140 requests).
eval: build
	./bin/ragd eval

web:
	cd web && npm ci && npm run build

up:
	docker compose up --build
