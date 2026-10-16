package store

import (
	"context"
	"reflect"
	"testing"

	"vidlens/internal/domain"
)

func assertCatalog(t *testing.T, s *Store, ids []string, defaultID string, active bool) map[string]any {
	t.Helper()
	catalog, err := s.ModelCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	models := catalog["models"].([]domain.GenerationModel)
	got := make([]string, 0, len(models))
	for _, model := range models {
		got = append(got, model.ID)
	}
	if !reflect.DeepEqual(got, ids) {
		t.Fatalf("public models = %v, want %v", got, ids)
	}
	def, _ := catalog["default_model_id"].(*string)
	if defaultID == "" && def != nil || defaultID != "" && (def == nil || *def != defaultID) {
		t.Fatalf("public default = %v, want %q", def, defaultID)
	}
	if catalog["selection_enabled"] != active {
		t.Fatalf("catalog active = %v, want %v", catalog["selection_enabled"], active)
	}
	return catalog
}

func TestCatalogEnablementPublishesWithoutDefault(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	assertCatalog(t, s, []string{"legacy"}, "legacy", false)
	// Importing a disabled candidate must preserve the existing single-model service.
	if _, err := s.ImportCurrentModel(ctx); err != nil {
		t.Fatal(err)
	}
	assertCatalog(t, s, []string{"legacy"}, "legacy", false)
	first := enabledModel(t, s, "catalog-a")
	second := enabledModel(t, s, "catalog-b")
	assertCatalog(t, s, []string{first.ID, second.ID}, "", true)
	var def *string
	if err := s.Pool.QueryRow(ctx, "SELECT default_model_id FROM model_settings").Scan(&def); err != nil || def != nil {
		t.Fatalf("enabling a model assigned an owner default: %v %v", def, err)
	}
	// Already-enabled configurations from older versions also publish without an owner default.
	if _, err := s.Pool.Exec(ctx, "UPDATE model_settings SET selection_enabled=false"); err != nil {
		t.Fatal(err)
	}
	assertCatalog(t, s, []string{first.ID, second.ID}, "", true)
}

func TestCatalogDefaultOnlyControlsNewRequestPreselection(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	owner := session(t, s)
	first := enabledModel(t, s, "preselect-a")
	second := enabledModel(t, s, "preselect-b")
	create := func(key, modelID string) domain.Request {
		t.Helper()
		a, err := s.CreateRequestWithInput(ctx, owner, key, domain.RequestInput{VideoURL: "https://youtube.com/watch?v=abcdefghijk", ModelID: modelID}, "", false)
		if err != nil {
			t.Fatal(err)
		}
		r, err := s.Request(ctx, owner, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	before := create("before-default", "")
	if before.GenerationSpec.Model.ID != first.ID {
		t.Fatalf("without a preferred model chose %q, want first public model", before.GenerationSpec.Model.ID)
	}
	catalog := assertCatalog(t, s, []string{first.ID, second.ID}, "", true)
	if err := s.SetDefaultModel(ctx, second.ID, catalog["revision"].(int64)); err != nil {
		t.Fatal(err)
	}
	assertCatalog(t, s, []string{first.ID, second.ID}, second.ID, true)
	after := create("after-default", "")
	if after.GenerationSpec.Model.ID != second.ID || create("explicit-choice", first.ID).GenerationSpec.Model.ID != first.ID {
		t.Fatal("default did not control preselection independently of explicit choice")
	}
	frozen, err := s.Request(ctx, owner, before.ID)
	if err != nil || frozen.ConfigID != before.ConfigID || frozen.GenerationSpec.Model.ID != first.ID {
		t.Fatal("changing default rewrote an accepted request")
	}
	in := modelInput(second)
	in.Enabled = false
	if _, err := s.SaveModel(ctx, second.ID, in); err != nil {
		t.Fatal(err)
	}
	assertCatalog(t, s, []string{first.ID}, "", true)
	if create("disabled-default", "").GenerationSpec.Model.ID != first.ID {
		t.Fatal("a disabled default blocked a new request using the public fallback")
	}
	if _, err := s.CreateRequestWithInput(ctx, owner, "explicit-disabled", domain.RequestInput{VideoURL: "https://youtube.com/watch?v=abcdefghijk", ModelID: second.ID}, "", false); err == nil {
		t.Fatal("an explicit disabled choice silently switched models")
	}
	if err := s.CheckModelAvailable(ctx, after.ConfigID); err == nil {
		t.Fatal("an existing request switched away from its stopped model")
	}
}

func TestCatalogDoesNotResurrectLegacyAfterModelRemoval(t *testing.T) {
	for _, change := range []string{"disable_model", "disable_provider", "rotate_credentials"} {
		t.Run(change, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			legacyCID, _, err := s.CurrentConfig(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.ImportCurrentModel(ctx); err != nil {
				t.Fatal(err)
			}
			m := enabledModel(t, s, "stopped")
			// Simulate an older database whose enabled model never had a default set.
			if _, err = s.Pool.Exec(ctx, "UPDATE model_settings SET selection_enabled=false"); err != nil {
				t.Fatal(err)
			}
			assertCatalog(t, s, []string{m.ID}, "", true)
			if err = s.CheckModelAvailable(ctx, legacyCID); err == nil {
				t.Fatal("legacy binding bypassed catalog availability before a default was set")
			}
			if change == "disable_model" {
				in := modelInput(m)
				in.Enabled = false
				_, err = s.SaveModel(ctx, m.ID, in)
			} else {
				p, e := s.Provider(ctx, m.ProviderID)
				if e != nil {
					t.Fatal(e)
				}
				in := ProviderInput{Name: p.Name, BaseURL: p.BaseURL, Enabled: false, ExpectedRevision: p.Revision, ProxyPolicy: p.ProxyPolicy}
				if change == "rotate_credentials" {
					in.Enabled, in.APIKey = true, "new-fixture-key"
				}
				_, err = s.SaveProvider(ctx, p.ID, in)
			}
			if err != nil {
				t.Fatal(err)
			}
			assertCatalog(t, s, []string{}, "", true)
			if _, err = s.CreateRequest(ctx, session(t, s), "no-active-model", "https://youtube.com/watch?v=abcdefghijk", "", false); err == nil {
				t.Fatal("empty catalog accepted a new request through the legacy model")
			}
		})
	}
}
