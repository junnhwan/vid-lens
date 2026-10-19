ALTER TABLE example_showcases
  ADD COLUMN process jsonb NOT NULL DEFAULT '{"status":"unavailable","steps":[]}'::jsonb,
  ADD COLUMN process_frozen boolean NOT NULL DEFAULT false;

CREATE UNIQUE INDEX example_question_once ON generation_requests(session_id, article_id)
  WHERE reuse = 'example_question' AND article_id IS NOT NULL;
