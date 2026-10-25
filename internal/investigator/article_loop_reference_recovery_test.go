package investigator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"vidlens/internal/domain"
)

func TestArticleLoopReferenceJournalRecoveryNeverResendsCompletedCalls(t *testing.T) {
	for _, point := range []string{"stage_insert", "stage_turn", "accepted_insert", "accepted_checkpoint", "publish"} {
		t.Run(point, func(t *testing.T) {
			var sends atomic.Int32
			s := loopFixture(t, func(w http.ResponseWriter, r *http.Request) {
				p := loopPayload(t, r)
				sends.Add(1)
				if p["ticket"] != nil {
					var ticket domain.ArticleReferenceTicket
					_ = json.Unmarshal(p["ticket"], &ticket)
					writeReviewWireArgs(w, loopReferenceRepairTool, domain.ArticleReferenceRepair{TicketID: ticket.ID, Patches: []domain.ArticleReferencePatch{{Path: ticket.Errors[0].Path, Ref: "t1"}}})
					return
				}
				if p["scope"] != nil {
					report := loopGoodReferenceReport(t, p)
					var a domain.ArticleReading
					_ = json.Unmarshal(p["candidate"], &a)
					report.Issues = []domain.ArticleReferenceIssue{{AC: "AC06", Severity: "advisory", Location: a.Sections[0].Blocks[0].ID, EvidenceRefs: []string{"t999"}, Problem: "合成文风建议", Correction: "可保留原文"}}
					writeReviewWireArgs(w, loopReviewTool, report)
					return
				}
				writeReviewWireArgs(w, loopSaveTool, loopGoodWrite())
			}, domain.ArticleConvergenceReviewV6)
			ctx := context.Background()
			table, action, condition := "run_operations", "INSERT", "NEW.kind='article_reference_stage'"
			switch point {
			case "stage_turn":
				action = "UPDATE"
				condition = "NEW.kind='article_loop_turn' AND NEW.status='completed' AND COALESCE(NEW.result->>'reference_stage_key','')<>''"
			case "accepted_insert":
				condition = "NEW.kind='article_loop_review'"
			case "accepted_checkpoint":
				table = "job_runs"
				action = "UPDATE"
				condition = "COALESCE(OLD.checkpoint->'article_loop'->>'reference_stage_key','')<>'' AND COALESCE(NEW.checkpoint->'article_loop'->>'reference_stage_key','')=''"
			case "publish":
				table = "articles"
				condition = "true"
			}
			statement := fmt.Sprintf("CREATE FUNCTION reference_test_crash() RETURNS trigger LANGUAGE plpgsql AS $body$ BEGIN IF %s THEN RAISE EXCEPTION 'synthetic reference crash'; END IF; RETURN NEW; END $body$; CREATE TRIGGER reference_crash BEFORE %s ON %s FOR EACH ROW EXECUTE FUNCTION reference_test_crash()", condition, action, table)
			if _, err := s.engine.Store.Pool.Exec(ctx, statement); err != nil {
				t.Fatal(err)
			}
			if err := s.engine.Run(ctx, s.claim, s.profile); err == nil || !strings.Contains(err.Error(), "synthetic reference crash") {
				t.Fatalf("crash boundary was not exercised: %v", err)
			}
			if _, err := s.engine.Store.Pool.Exec(ctx, "DROP FUNCTION reference_test_crash() CASCADE"); err != nil {
				t.Fatal(err)
			}
			s = reloadLoopSession(t, s)
			if err := s.engine.Run(ctx, s.claim, s.profile); err != nil {
				t.Fatal("recovery failed", err)
			}
			if sends.Load() != 3 {
				t.Fatalf("completed request resent at %s: %d", point, sends.Load())
			}
			var receipts, stages, articles int
			_ = s.engine.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM run_operations WHERE run_id=$1 AND kind='article_loop_review'", s.claim.Run.ID).Scan(&receipts)
			_ = s.engine.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM run_operations WHERE run_id=$1 AND kind='article_reference_stage'", s.claim.Run.ID).Scan(&stages)
			_ = s.engine.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM articles WHERE generation_run_id=$1", s.claim.Run.ID).Scan(&articles)
			if receipts != 1 || stages != 1 || articles != 1 {
				t.Fatalf("non-atomic or duplicate receipt/obligation/publication: %d/%d/%d", receipts, stages, articles)
			}
			s = reloadLoopSession(t, s)
			if err := s.engine.Run(ctx, s.claim, s.profile); err != nil || sends.Load() != 3 {
				t.Fatal("published recovery changed result or resent", err)
			}
		})
	}
}
