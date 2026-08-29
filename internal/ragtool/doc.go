// Package ragtool holds offline RAG evaluation, projection audit, and reindex
// helpers used by cmd/rag-eval, cmd/rag-audit, and cmd/rag-reindex.
//
// These types are intentionally outside the product request path. Prefer
// internal/service for online chat, indexing, and media workflows.
package ragtool
