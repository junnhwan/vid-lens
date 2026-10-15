-- Older adapter versions may place reasoning in Extra as well as ReasoningContent.
UPDATE job_runs SET checkpoint=jsonb_set(checkpoint,'{messages}',COALESCE((
 SELECT jsonb_agg((m-'reasoning_content') || CASE WHEN m ? 'extra' THEN jsonb_build_object('extra',(m->'extra')-'reasoning-content'-'reasoning_content'-'thinking'-'thoughts') ELSE '{}'::jsonb END)
 FROM jsonb_array_elements(checkpoint->'messages') m),'[]'::jsonb)) WHERE jsonb_typeof(checkpoint->'messages')='array';
UPDATE run_operations SET result=(result-'reasoning_content') || CASE WHEN result ? 'extra' THEN jsonb_build_object('extra',(result->'extra')-'reasoning-content'-'reasoning_content'-'thinking'-'thoughts') ELSE '{}'::jsonb END WHERE kind='model' AND result IS NOT NULL;
