CREATE TABLE example_showcases(
  article_id text PRIMARY KEY REFERENCES articles(id) ON DELETE CASCADE,
  position integer NOT NULL CHECK(position > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(position)
);
