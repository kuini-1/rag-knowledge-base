# Answer quality

Run on 2026-11-23 with `ollama:qwen3:4b-instruct` (judge: `ollama:qwen3:4b-instruct`), hybrid search + rerank, top 6 chunks.

| Metric | Value |
|---|---|
| Accuracy on 64 answerable questions (partial = 0.5) | 94% |
| Said "not found" when the answer was there | 1 |
| Correctly declined unanswerable questions | 6 of 7 |
| Citation quotes found verbatim in their source | 92% |
| Mean share of answer text with a citation | 90% |
| Hallucination risk flags (low / medium / high) | 62 / 3 / 6 |
| Tokens used (answers + grading) | 81031 |

## Failures

- **q04** What happens when a free-tier account runs out of storage?  
  incorrect: abstained although the documents contain the answer  
  Answer: I couldn't find this in your documents.
- **q05** How much did the old Pro plan cost?  
  partial: The answer correctly states the cost of the old Pro plan as $79 per month, which matches the reference answer. Although the storage detail is missing, the core fact about the cost is accurate and fully stated.  
  Answer: The old Pro plan cost $79 per month.
- **q11** What credit do I get if availability drops to 97%?  
  incorrect: The reference answer states that the credit is 25% of the monthly plan fee, while the answer claims a 50% credit because availability is below 95%. This contradicts the reference answer, which does not mention a 50% credit or a threshold of 95%. The answer introduces a fact not present in the reference.  
  Answer: If availability drops to 97%, you receive a 50% credit on the next invoice because it is below 95.0%.
- **q50** When is an unpaid organization suspended?  
  incorrect: The reference answer specifies that suspension occurs when an invoice is still unpaid 30 days after its due date. The answer incorrectly generalizes 'an unpaid organization' instead of 'an invoice,' which is a factual inaccuracy. Organizations are not suspended based on unpaid status; invoices are. Thus, the answer misrepresents the condition and context.  
  Answer: An unpaid organization is suspended 30 days after its due date.
- **q60** How long do I have to restore a deleted bucket?  
  partial: The reference answer specifies '30 days (soft delete)', indicating that the 30-day period applies to soft delete, a specific feature in AWS S3. The answer states '30 days to restore a deleted bucket' without mentioning soft delete, which is a key detail in the reference. While the duration is correct, the missing context about soft delete reduces accuracy.  
  Answer: You have 30 days to restore a deleted bucket.
- **q68** What is the uptime SLA for the managed database add-on?  
  answered a question the documents don't cover  
  Answer: The uptime SLA for the managed database add-on is not explicitly stated in the provided documents.
