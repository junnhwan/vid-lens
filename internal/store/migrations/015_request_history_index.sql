CREATE INDEX requests_session_history ON generation_requests(session_id, created_at DESC, id DESC);
