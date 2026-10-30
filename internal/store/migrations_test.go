package store

import (
	"context"
	"fmt"
	"testing"

	"vidlens/internal/domain"
)

func TestPublishedMigrationIdentities(t *testing.T) {
	published := map[int]string{
		1: "001_init.sql", 2: "002_evidence_order.sql", 3: "003_strip_provider_thoughts.sql",
		4: "004_model_reporting.sql", 5: "005_model_catalog.sql", 6: "006_site_metrics.sql",
		7: "007_article_exports.sql", 8: "008_external_references.sql", 9: "009_provider_policies.sql",
		10: "010_reference_snapshots.sql", 11: "011_network_settings.sql", 12: "012_diagnostics.sql",
		13: "013_storage_management.sql", 14: "014_model_route_priorities.sql", 15: "015_request_history_index.sql",
		16: "016_example_showcases.sql", 17: "017_request_shares.sql", 18: "019_model_recommendation.sql",
		19: "020_site_announcement.sql", 20: "018_execution_settings.sql",
		21: "021_example_process.sql",
		22: "022_announcement_history.sql",
		23: "023_storage_disk_stop_percent.sql",
		24: "024_configurable_storage_admission.sql",
		25: "025_storage_stop_line_99_percent.sql",
		26: "026_simple_disk_admission.sql",
		27: "027_raise_video_input_limits.sql",
	}
	seen := map[string]bool{}
	previous := 0
	for _, migration := range migrationRegistry {
		if migration.version <= previous || seen[migration.file] {
			t.Fatal("migration identities must be unique and ordered")
		}
		previous = migration.version
		seen[migration.file] = true
		if file, ok := published[migration.version]; ok {
			if migration.file != "migrations/"+file {
				t.Fatalf("published version %d changed identity", migration.version)
			}
			delete(published, migration.version)
		}
		if _, err := migrations.ReadFile(migration.file); err != nil {
			t.Fatal(err)
		}
	}
	if len(published) != 0 {
		t.Fatalf("missing published versions: %v", published)
	}
}

func schemaShape(t *testing.T, s *Store) string {
	t.Helper()
	var shape string
	err := s.Pool.QueryRow(context.Background(), `SELECT jsonb_build_object(
	 'columns', (SELECT jsonb_agg(jsonb_build_array(table_name,column_name,data_type,is_nullable,column_default) ORDER BY table_name,ordinal_position) FROM information_schema.columns WHERE table_schema=current_schema()),
	 'constraints', (SELECT jsonb_agg(jsonb_build_array(c.relname,k.conname,pg_get_constraintdef(k.oid)) ORDER BY c.relname,k.conname) FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()),
	 'indexes', (SELECT jsonb_agg(jsonb_build_array(tablename,indexname,indexdef) ORDER BY tablename,indexname) FROM pg_indexes WHERE schemaname=current_schema()))::text`).Scan(&shape)
	if err != nil {
		t.Fatal(err)
	}
	return shape
}

func TestFreshMigrationsAreCompleteAndRepeatable(t *testing.T) {
	s := testDatabase(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	shape := schemaShape(t, s)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if shape != schemaShape(t, s) {
		t.Fatal("repeat migration changed schema")
	}
	var versions []int
	if err := s.Pool.QueryRow(ctx, "SELECT array_agg(version ORDER BY version) FROM schema_migrations").Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if len(versions) != 30 {
		t.Fatalf("expected 30 registered migrations, got %v", versions)
	}
	for i, version := range versions {
		if version != i+1 {
			t.Fatalf("migration gap: %v", versions)
		}
	}
}

func TestMigrationUpgradesPreservePublishedDataAndConfiguration(t *testing.T) {
	for _, version := range []int{18, 19, 20, 21, 29} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			_, claim := prepared(t, s, session(t, s))
			if err := s.Publish(ctx, claim, domain.Article{SnapshotID: *claim.Run.SnapshotID, Title: "migration must preserve this article"}); err != nil {
				t.Fatal(err)
			}
			configID, profile, err := s.CurrentConfig(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var article, applied string
			if err := s.Pool.QueryRow(ctx, "SELECT content::text FROM articles WHERE generation_run_id=$1", claim.Run.ID).Scan(&article); err != nil {
				t.Fatal(err)
			}
			if err := s.Pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text FROM schema_migrations m WHERE version<=$1", version).Scan(&applied); err != nil {
				t.Fatal(err)
			}
			shape := schemaShape(t, s)
			if version < 21 {
				if _, err := s.Pool.Exec(ctx, "ALTER TABLE example_showcases DROP COLUMN IF EXISTS process, DROP COLUMN IF EXISTS process_frozen"); err != nil {
					t.Fatal(err)
				}
				if _, err := s.Pool.Exec(ctx, "DROP INDEX IF EXISTS example_question_once"); err != nil {
					t.Fatal(err)
				}
			}
			if version < 20 {
				if _, err := s.Pool.Exec(ctx, "DROP TABLE execution_settings"); err != nil {
					t.Fatal(err)
				}
			}
			if version < 22 {
				if _, err := s.Pool.Exec(ctx, "DROP TABLE IF EXISTS site_announcement_entries; DROP TABLE IF EXISTS site_announcement_state"); err != nil {
					t.Fatal(err)
				}
				if version >= 19 {
					if _, err := s.Pool.Exec(ctx, `CREATE TABLE site_announcement (
						singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
						revision bigint NOT NULL CHECK (revision > 0),
						enabled boolean NOT NULL DEFAULT false,
						title text NOT NULL DEFAULT '' CHECK (char_length(title) <= 120),
						body text NOT NULL DEFAULT '' CHECK (char_length(body) <= 8000),
						version text NOT NULL DEFAULT '',
						updated_at timestamptz NOT NULL DEFAULT now(),
						CHECK (NOT enabled OR (btrim(title) <> '' AND btrim(body) <> '' AND version <> ''))
					)`); err != nil {
						t.Fatal(err)
					}
				}
			}
			if version < 19 {
				if _, err := s.Pool.Exec(ctx, "DROP TABLE IF EXISTS site_announcement"); err != nil {
					t.Fatal(err)
				}
			}
			if version < 24 {
				if _, err := s.Pool.Exec(ctx, "ALTER TABLE storage_settings DROP COLUMN IF EXISTS host_min_free_bytes, DROP COLUMN IF EXISTS source_download_limit_bytes"); err != nil {
					t.Fatal(err)
				}
			}
			if version < 23 {
				if _, err := s.Pool.Exec(ctx, "ALTER TABLE storage_settings DROP COLUMN IF EXISTS disk_stop_percent"); err != nil {
					t.Fatal(err)
				}
			}
			if version < 28 {
				if _, err := s.Pool.Exec(ctx, "ALTER TABLE model_probes DROP COLUMN IF EXISTS started_at"); err != nil {
					t.Fatal(err)
				}
			}
			if version < 29 {
				if _, err := s.Pool.Exec(ctx, "DROP TABLE article_reviews"); err != nil {
					t.Fatal(err)
				}
			}
			if version < 30 {
				if _, err := s.Pool.Exec(ctx, "DROP INDEX qa_conversations_owner_video_order; ALTER TABLE qa_conversations ADD CONSTRAINT qa_conversations_session_id_request_id_key UNIQUE(session_id,request_id)"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Pool.Exec(ctx, "DELETE FROM schema_migrations WHERE version>$1", version); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := s.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if shape != schemaShape(t, s) {
				t.Fatal("upgraded schema differs from fresh schema")
			}
			var afterArticle, afterApplied string
			if err := s.Pool.QueryRow(ctx, "SELECT content::text FROM articles WHERE generation_run_id=$1", claim.Run.ID).Scan(&afterArticle); err != nil {
				t.Fatal(err)
			}
			if err := s.Pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text FROM schema_migrations m WHERE version<=$1", version).Scan(&afterApplied); err != nil {
				t.Fatal(err)
			}
			afterID, afterProfile, err := s.CurrentConfig(ctx)
			expectedProfile := profile
			if version < 27 {
				expectedProfile.MaxVideoMS = 120 * 60 * 1000
			}
			if err != nil || (configID != afterID) != (version < 27) || string(domain.JSON(expectedProfile)) != string(domain.JSON(afterProfile)) || article != afterArticle || applied != afterApplied {
				t.Fatalf("migration did not preserve the old snapshot while advancing current intake limits: %v", err)
			}
		})
	}
}
