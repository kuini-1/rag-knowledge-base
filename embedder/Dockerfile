FROM python:3.12-slim

WORKDIR /app
# CPU-only torch keeps the image a fraction of the CUDA build's size.
RUN pip install --no-cache-dir torch --index-url https://download.pytorch.org/whl/cpu
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Bake the models into the image so containers start without network access.
ENV EMBED_MODEL=BAAI/bge-small-en-v1.5 \
    RERANK_MODEL=cross-encoder/ms-marco-MiniLM-L-6-v2 \
    HF_HOME=/models
RUN python -c "from sentence_transformers import SentenceTransformer, CrossEncoder; \
SentenceTransformer('$EMBED_MODEL'); CrossEncoder('$RERANK_MODEL')"

COPY app.py .
RUN useradd -u 10001 app && chown -R app /models
USER app
EXPOSE 8001
CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8001"]
