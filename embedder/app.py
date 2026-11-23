"""Embedding and reranking service.

A small HTTP wrapper around two sentence-transformers models:
  - a bi-encoder that turns text into normalized vectors (for search), and
  - a cross-encoder that scores (query, passage) pairs (for reranking).

The Go API calls it over HTTP, so the models stay in Python, where the
tooling for them is, and everything else stays in Go.
"""

import os
import time
from functools import lru_cache

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field
from sentence_transformers import CrossEncoder, SentenceTransformer

EMBED_MODEL = os.getenv("EMBED_MODEL", "BAAI/bge-small-en-v1.5")
RERANK_MODEL = os.getenv("RERANK_MODEL", "cross-encoder/ms-marco-MiniLM-L-6-v2")
# bge models expect this prefix on queries (not on passages).
QUERY_PREFIX = os.getenv("QUERY_PREFIX", "Represent this sentence for searching relevant passages: ")
MAX_BATCH = int(os.getenv("MAX_BATCH", "128"))

app = FastAPI(title="embedder")


@lru_cache(maxsize=1)
def embedder() -> SentenceTransformer:
    return SentenceTransformer(EMBED_MODEL, device="cpu")


@lru_cache(maxsize=1)
def reranker() -> CrossEncoder:
    return CrossEncoder(RERANK_MODEL, device="cpu")


class EmbedRequest(BaseModel):
    texts: list[str] = Field(min_length=1)
    kind: str = Field("passage", pattern="^(passage|query)$")


class EmbedResponse(BaseModel):
    model: str
    dim: int
    vectors: list[list[float]]
    ms: float


class RerankRequest(BaseModel):
    query: str
    passages: list[str] = Field(min_length=1)


class RerankResponse(BaseModel):
    model: str
    scores: list[float]
    ms: float


@app.get("/health")
def health():
    m = embedder()
    return {"ok": True, "embed_model": EMBED_MODEL, "dim": m.get_sentence_embedding_dimension(), "rerank_model": RERANK_MODEL}


@app.post("/embed", response_model=EmbedResponse)
def embed(req: EmbedRequest):
    if len(req.texts) > MAX_BATCH:
        raise HTTPException(413, f"at most {MAX_BATCH} texts per request")
    texts = [QUERY_PREFIX + t for t in req.texts] if req.kind == "query" else req.texts
    t0 = time.perf_counter()
    vecs = embedder().encode(texts, normalize_embeddings=True, batch_size=32, convert_to_numpy=True)
    return EmbedResponse(model=EMBED_MODEL, dim=vecs.shape[1], vectors=vecs.tolist(), ms=(time.perf_counter() - t0) * 1000)


@app.post("/rerank", response_model=RerankResponse)
def rerank(req: RerankRequest):
    if len(req.passages) > MAX_BATCH:
        raise HTTPException(413, f"at most {MAX_BATCH} passages per request")
    t0 = time.perf_counter()
    scores = reranker().predict([(req.query, p) for p in req.passages], batch_size=32)
    return RerankResponse(model=RERANK_MODEL, scores=[float(s) for s in scores], ms=(time.perf_counter() - t0) * 1000)
