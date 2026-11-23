# Retrieval ablation

Run on 2026-11-23. 15 documents, 64 answerable questions. Embeddings: `BAAI/bge-small-en-v1.5`. Reranker: `cross-encoder/ms-marco-MiniLM-L-6-v2`.

A question counts as a hit at k when a chunk among the top k covers at least 80% of the passage that answers it. MRR is over the top 10.

## Chunking (dense search, no rerank)

| Config | Chunks | Avg tokens | Hit@1 | Hit@3 | Hit@5 | MRR | Prompt tokens (top 5) |
|---|---|---|---|---|---|---|---|
| `fixed-64-0` | 104 | 60 | 62% | 75% | 77% | 0.690 | 315 |
| `fixed-128-0` | 55 | 113 | 61% | 83% | 88% | 0.725 | 605 |
| `fixed-256-0` | 31 | 200 | 66% | 83% | 92% | 0.762 | 1032 |
| `fixed-512-0` | 16 | 386 | 72% | 88% | 92% | 0.810 | 1946 |
| `fixed-128-32` | 65 | 119 | 70% | 89% | 94% | 0.799 | 608 |
| `fixed-256-64` | 36 | 209 | 69% | 89% | 92% | 0.793 | 1090 |
| `structure-128-0` | 94 | 61 | 77% | 92% | 92% | 0.840 | 320 |
| `structure-256-0` | 90 | 64 | 77% | 89% | 92% | 0.834 | 354 |
| `structure-128-0-ctx` | 94 | 70 | 75% | 95% | 97% | 0.850 | 366 |
| `structure-256-0-ctx` | 90 | 73 | 73% | 94% | 95% | 0.835 | 386 |
| `structure-256-32-ctx` | 90 | 73 | 73% | 94% | 95% | 0.835 | 386 |
| `structure-512-0-ctx` | 89 | 74 | 73% | 94% | 95% | 0.835 | 392 |

## Retrieval (chunking `structure-256-32-ctx`)

| Setup | Hit@1 | Hit@3 | Hit@5 | MRR | Latency p50 |
|---|---|---|---|---|---|
| dense | 73% | 94% | 95% | 0.835 | 52 ms |
| hybrid (dense + keyword, RRF) | 84% | 97% | 100% | 0.905 | 74 ms |
| dense + rerank | 94% | 98% | 98% | 0.961 | 1879 ms |
| hybrid + rerank | 94% | 98% | 98% | 0.961 | 1421 ms |

## Hit@1 by question type

Share of questions whose answer is in the first result, per tag.

| Tag | dense | hybrid (dense + keyword, RRF) | dense + rerank | hybrid + rerank |
|---|---|---|---|---|
| context | 60% | 80% | 80% | 80% |
| distractor | 67% | 67% | 67% | 67% |
| docx | 100% | 100% | 100% | 100% |
| identifier | 56% | 89% | 100% | 100% |
| multi | 0% | 0% | 0% | 0% |
| paraphrase | 75% | 62% | 88% | 88% |
| pdf | 33% | 33% | 100% | 100% |
| plain | 82% | 93% | 100% | 100% |
| table | 44% | 78% | 78% | 78% |
| txt | 100% | 100% | 100% | 100% |

Every miss (evidence not in the top 5) is listed with what was retrieved instead in `bad_retrievals.jsonl`.
