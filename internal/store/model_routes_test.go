package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"vidlens/internal/domain"
)

func routeModel(t *testing.T, s *Store, supplier, upstream, brand string) CatalogModel {
	t.Helper()
	ctx := context.Background()
	p, err := s.SaveProvider(ctx, "", ProviderInput{Name: supplier, BaseURL: "https://" + supplier + ".example/v1", APIKey: "fixture-key", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.SaveModel(ctx, "", ModelInput{ProviderID: p.ID, DisplayName: "same display", UpstreamID: upstream, BrandID: brand})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateProbe(ctx, m.ID, m.RevisionID, "capabilities", domain.ID())
	if err != nil {
		t.Fatal(err)
	}
	probe, err := s.ClaimProbe(ctx, a.ID)
	if err != nil || probe == nil {
		t.Fatal(err)
	}
	if err = s.FinishProbe(ctx, *probe, map[string]bool{"synthetic_fixture_only": true}, ""); err != nil {
		t.Fatal(err)
	}
	in := modelInput(m)
	in.Enabled = true
	m, err = s.SaveModel(ctx, m.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestModelFailoverBudgetCoversAllSuppliersWithoutReset(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	var cids []string
	for i := 0; i < 5; i++ {
		m := routeModel(t, s, fmt.Sprintf("supplier-%d", i), "glm-5.3-flash", "glm")
		var cid string
		if err := s.Pool.QueryRow(ctx, "SELECT config_id FROM model_revisions WHERE id=$1", m.RevisionID).Scan(&cid); err != nil {
			t.Fatal(err)
		}
		cids = append(cids, cid)
	}
	_, claim := prepared(t, s, session(t, s))
	if _, err := s.Pool.Exec(ctx, "UPDATE job_runs SET config_id=$2 WHERE id=$1", claim.Run.ID, cids[0]); err != nil {
		t.Fatal(err)
	}
	claim.Run.ConfigID = cids[0]
	for i, cid := range cids {
		op, err := s.BeginModelSubmission(ctx, claim, "model:0", "fixture", cid)
		if err != nil || op.Submissions != i+1 {
			t.Fatalf("supplier %d was not offered one attempt: %+v %v", i, op, err)
		}
	}
	if _, err := s.BeginModelSubmission(ctx, claim, "model:0", "fixture", cids[0]); err == nil {
		t.Fatal("supplier failover reset the logical operation budget")
	}
}

func TestCatalogGroupsSameModelButKeepsAllOwnerRoutes(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := routeModel(t, s, "primary", "deepseek-v4.1-flash", "generic")
	b := routeModel(t, s, "backup", "deepseek/deepseek-v4.1-flash", "deepseek")
	routeModel(t, s, "version", "deepseek-v4.2-flash", "deepseek")
	routeModel(t, s, "brand", "deepseek-v4.1-flash", "glm")
	catalog, err := s.ModelCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetDefaultModel(ctx, b.ID, catalog["revision"].(int64)); err != nil {
		t.Fatal(err)
	}
	catalog, err = s.ModelCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	models := catalog["models"].([]domain.GenerationModel)
	if len(models) != 3 || *catalog["default_model_id"].(*string) != b.ID {
		t.Fatalf("catalog grouping/default: %+v", catalog)
	}
	all, err := s.Models(ctx)
	if err != nil || len(all) != 4 {
		t.Fatalf("owner lost routes: %+v %v", all, err)
	}
	owner := session(t, s)
	accepted, err := s.CreateRequestWithInput(ctx, owner, "group-selection", domain.RequestInput{VideoURL: "https://youtube.com/watch?v=abcdefghijk"}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	req, err := s.Request(ctx, owner, accepted.ID)
	if err != nil || req.GenerationSpec.Model.ID != b.ID {
		t.Fatalf("preferred route not frozen: %+v %v", req, err)
	}
	routes, err := s.ModelRoutes(ctx, req.ConfigID)
	if err != nil || len(routes) != 2 || routes[0].ModelID != b.ID || routes[1].ModelID != a.ID {
		t.Fatalf("wrong model/supplier routes: %+v %v", routes, err)
	}
	in := modelInput(b)
	in.Enabled = false
	if _, err = s.SaveModel(ctx, b.ID, in); err != nil {
		t.Fatal(err)
	}
	if err = s.CheckModelAvailable(ctx, req.ConfigID); err != nil {
		t.Fatalf("stopped primary blocked usable same-model backup: %v", err)
	}
	catalog, err = s.ModelCatalog(ctx)
	if err != nil || catalog["default_model_id"].(*string) == nil || *catalog["default_model_id"].(*string) != a.ID {
		t.Fatalf("same-model backup lost the preferred model: %+v %v", catalog, err)
	}
	routes, err = s.ModelRoutes(ctx, req.ConfigID)
	if err != nil || len(routes) != 1 || routes[0].ModelID != a.ID {
		t.Fatalf("stopped supplier still routed: %+v %v", routes, err)
	}
	in = modelInput(a)
	in.Enabled = false
	if _, err = s.SaveModel(ctx, a.ID, in); err != nil {
		t.Fatal(err)
	}
	if err = s.CheckModelAvailable(ctx, req.ConfigID); err == nil {
		t.Fatal("different model used when same-model suppliers stopped")
	}
}

func TestModelSubmissionValidatesActualRouteAndKeepsAuditOnRecovery(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a := routeModel(t, s, "primary", "glm-5.3-flash", "glm")
	b := routeModel(t, s, "backup", "z-ai/glm-5.3-flash", "glm")
	wrong := routeModel(t, s, "wrong", "glm-5.4-flash", "glm")
	var cid, other, wrongCID string
	for _, pair := range []struct {
		id   string
		into *string
	}{{a.ID, &cid}, {b.ID, &other}, {wrong.ID, &wrongCID}} {
		if err := s.Pool.QueryRow(ctx, "SELECT config_id FROM model_revisions WHERE model_id=$1", pair.id).Scan(pair.into); err != nil {
			t.Fatal(err)
		}
	}
	_, claim := prepared(t, s, session(t, s))
	if _, err := s.Pool.Exec(ctx, "UPDATE job_runs SET config_id=$2 WHERE id=$1", claim.Run.ID, cid); err != nil {
		t.Fatal(err)
	}
	claim.Run.ConfigID = cid
	args := map[string]string{"fixture": "one logical call"}
	if _, err := s.BeginModelSubmission(ctx, claim, "model:0", args, wrongCID); err == nil {
		t.Fatal("switched model identity")
	}
	op, err := s.BeginModelSubmission(ctx, claim, "model:0", args, cid)
	if err != nil || op.Submissions != 1 {
		t.Fatal(op, err)
	}
	if err = s.RecordSubmission(ctx, claim, "model:0", 1, map[string]string{"stream_status": "incomplete"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.BeginModelSubmission(ctx, claim, "model:0", args, other); err != nil {
		t.Fatal(err)
	}
	attempts, err := s.ModelRouteAttempts(ctx, claim.Run.ID, "model:0", cid)
	if err != nil || attempts[cid] != 1 || attempts[other] != 1 {
		t.Fatal(attempts, err)
	}
	if err = s.FinishOperation(ctx, claim, "model:0", map[string]string{"content": "fixture"}, "", nil); err != nil {
		t.Fatal(err)
	}
	preferred, err := s.SuccessfulModelRoute(ctx, claim.Run.ID)
	if err != nil || preferred != other {
		t.Fatalf("successful backup not retained: %s %v", preferred, err)
	}
	ownerReport, err := s.ModelReporting(ctx, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	rows := ownerReport["recent"].([]map[string]any)
	if len(rows) != 2 {
		t.Fatalf("physical supplier attempts missing from owner reporting: %+v", rows)
	}
	for _, row := range rows {
		wantID, wantName := a.ProviderID, "primary"
		if fmt.Sprint(row["submission"]) == "2" {
			wantID, wantName = b.ProviderID, "backup"
		}
		if row["provider_id"] != wantID || row["provider_name"] != wantName {
			t.Fatalf("owner reporting attributed backup usage to the frozen primary: %+v", row)
		}
	}
	var report map[string]any
	var raw []byte
	if err = s.Pool.QueryRow(ctx, "SELECT reporting FROM model_submissions WHERE run_id=$1 AND submission=1", claim.Run.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &report); err != nil || report["route_config_id"] != cid || report["provider_id"] != a.ProviderID {
		t.Fatal(report, err)
	}
	// Changing a supplier revision requires a fresh successful probe; the old
	// key/revision cannot be selected just because another supplier is available.
	p, err := s.Provider(ctx, b.ProviderID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveProvider(ctx, p.ID, ProviderInput{Name: p.Name, BaseURL: p.BaseURL, APIKey: "rotated-fixture", Enabled: true, ExpectedRevision: p.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.BeginModelSubmission(ctx, claim, "model:1", args, other); err == nil {
		t.Fatal("retired credential submitted")
	}
	claim.Epoch++
	if _, err = s.BeginModelSubmission(ctx, claim, "model:1", args, cid); !errors.Is(err, domain.ErrFenced) {
		t.Fatalf("stale worker submitted: %v", err)
	}
}

func TestRecommendationPublishesWithTheVisibleModel(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	primary := routeModel(t, s, "primary-reco", "deepseek-v4.1-flash", "generic")
	backup := routeModel(t, s, "backup-reco", "deepseek/deepseek-v4.1-flash", "deepseek")
	other := routeModel(t, s, "other-reco", "glm-5.3-flash", "glm")
	if _, err := s.SetModelRecommended(ctx, backup.ID, true, backup.Revision-1); err == nil {
		t.Fatal("stale recommendation revision was accepted")
	}
	saved, err := s.SetModelRecommended(ctx, backup.ID, true, backup.Revision)
	if err != nil || !saved.Recommended || saved.Revision != backup.Revision+1 {
		t.Fatalf("recommendation not stored: %+v %v", saved, err)
	}
	catalog, err := s.ModelCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var deepseek, glm bool
	for _, model := range catalog["models"].([]domain.GenerationModel) {
		if model.ID == primary.ID || model.ID == backup.ID {
			deepseek = model.Recommended
		}
		if model.ID == other.ID {
			glm = model.Recommended
		}
	}
	if !deepseek || glm {
		t.Fatalf("public recommendation = deepseek %v, glm %v, catalog %+v", deepseek, glm, catalog["models"])
	}
	stopped := modelInput(saved)
	stopped.Enabled = false
	if _, err = s.SaveModel(ctx, backup.ID, stopped); err != nil {
		t.Fatal(err)
	}
	catalog, err = s.ModelCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range catalog["models"].([]domain.GenerationModel) {
		if (model.ID == primary.ID || model.ID == backup.ID) && model.Recommended {
			t.Fatalf("stopped route still published a recommendation: %+v", model)
		}
	}
}
